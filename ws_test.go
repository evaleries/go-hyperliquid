package hyperliquid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestNewWebsocketClient(t *testing.T) {
	require.PanicsWithValue(t,
		"baseURL must have a scheme set, either wss or ws",
		func() { _ = NewWebsocketClient("foobar.com") },
	)

	require.NotPanics(t,
		func() { _ = NewWebsocketClient(MainnetAPIURL) },
		"Mainnet should always work",
	)

	require.NotPanics(t,
		func() { _ = NewWebsocketClient("") },
		"empty URL should default to Mainnet",
	)
}

func TestWsOptReadTimeout(t *testing.T) {
	client := NewWebsocketClient(MainnetAPIURL, WsOptReadTimeout(42*time.Second))
	require.Equal(t, 42*time.Second, client.readTimeout)
}

// TestReadPumpReconnectsOnTimeout spins up a WebSocket server that accepts
// connections but never sends a message.  The client should time out and
// reconnect, resulting in more than one TCP-level upgrade.
func TestReadPumpReconnectsOnTimeout(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	var connectCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connectCount.Add(1)
		// Hold the connection open; drain any frames the client sends (e.g. ping)
		// so the TCP link itself stays alive — only application-layer data is absent.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				_ = conn.Close()
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 200 ms timeout gives us several reconnect cycles within the 3 s context.
	client := NewWebsocketClient(server.URL, WsOptReadTimeout(200*time.Millisecond))
	require.NoError(t, client.Connect(ctx))

	// Allow enough wall-clock time for multiple timeout → reconnect cycles.
	time.Sleep(2 * time.Second)

	require.GreaterOrEqual(t, int(connectCount.Load()), 2,
		"client should have reconnected at least once after read timeout")

	require.NoError(t, client.Close())
}

// TestReadPumpDispatchesMessage exercises the live receive path end-to-end:
// pooled readMessage -> decodeWsEnvelope -> dispatch -> subscriber callback.
func TestReadPumpDispatchesMessage(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	tradesMsg := []byte(
		`{"channel":"trades","data":[{"coin":"BTC","side":"B","px":"65000.5","sz":"0.25","time":1703001234567,"hash":"0x5e43f6","tid":1234567890,"users":["0xaaa","0xbbb"]}]}`,
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Drain the client's subscribe frame, then push one trades message.
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, tradesMsg); err != nil {
			return
		}
		// Keep the connection open until the client is done.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewWebsocketClient(server.URL)
	require.NoError(t, client.Connect(ctx))
	defer func() { _ = client.Close() }()

	got := make(chan Trades, 1)
	_, err := client.Trades(TradesSubscriptionParams{Coin: "BTC"}, func(trades []Trade, err error) {
		require.NoError(t, err)
		select {
		case got <- trades:
		default:
		}
	})
	require.NoError(t, err)

	select {
	case trades := <-got:
		require.Len(t, trades, 1)
		require.Equal(t, "BTC", trades[0].Coin)
		require.Equal(t, "65000.5", trades[0].Px)
	case <-ctx.Done():
		t.Fatal("timed out waiting for trades dispatch")
	}
}
