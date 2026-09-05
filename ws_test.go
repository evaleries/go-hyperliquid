package hyperliquid

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// wsLargeTradesMessage builds a trades envelope big enough to take the
// zero-copy AST path in decodeWsEnvelope (>= wsEnvelopeAstThreshold).
func wsLargeTradesMessage(coin, px string, trades int) []byte {
	var sb strings.Builder
	sb.WriteString(`{"channel":"trades","data":[`)
	for i := range trades {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(
			&sb,
			`{"coin":%q,"side":"B","px":%q,"sz":"0.25","time":1703001234567,`+
				`"hash":"0x5e43f6","tid":%d,"users":["0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",`+
				`"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]}`,
			coin, px, 1234567890+i,
		)
	}
	sb.WriteString(`]}`)
	return []byte(sb.String())
}

// TestDecodeWsEnvelopeDoesNotAliasReadBuffer pins the invariant that the
// pooled read buffer depends on: decodeWsEnvelope returns a Data string that
// references the read buffer (zero copy, CopyReturn=false), so the codec MUST
// copy every dispatched value out of it. A payload type whose UnmarshalJSON
// retains its input — or a jsonCodec configured without CopyString — would
// silently corrupt user data when the buffer is refilled by the next message.
func TestDecodeWsEnvelopeDoesNotAliasReadBuffer(t *testing.T) {
	src := wsLargeTradesMessage("BTC", "65000.5", 20)
	require.GreaterOrEqual(t, len(src), wsEnvelopeAstThreshold,
		"fixture must be large enough to exercise the zero-copy AST path")

	msg, err := decodeWsEnvelope(src)
	require.NoError(t, err)
	require.Equal(t, ChannelTrades, msg.Channel)

	var trades Trades
	require.NoError(t, jsonCodec.UnmarshalFromString(msg.Data, &trades))
	require.Len(t, trades, 20)

	// Simulate the pooled buffer being released and refilled.
	for i := range src {
		src[i] = 'Z'
	}

	require.Equal(t, "BTC", trades[0].Coin)
	require.Equal(t, "65000.5", trades[0].Px)
	require.Equal(t, "0x5e43f6", trades[0].Hash)
	require.Equal(t, int64(1703001234567), trades[0].Time)
	require.Equal(t, []string{
		"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}, trades[0].Users)
}

// TestReadPumpPayloadSurvivesBufferReuse is the end-to-end counterpart: a
// subscriber retains the payload it was handed, then several more large
// messages recycle the same pooled buffer through readMessage. The retained
// payload must still read back its original values.
func TestReadPumpPayloadSurvivesBufferReuse(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	first := wsLargeTradesMessage("BTC", "65000.5", 20)
	later := wsLargeTradesMessage("BTC", "1.5", 40)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil { // subscribe frame
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, first); err != nil {
			return
		}
		for range 3 {
			if err := conn.WriteMessage(websocket.TextMessage, later); err != nil {
				return
			}
		}
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

	var (
		mu       sync.Mutex
		retained []Trades
	)
	done := make(chan struct{})
	_, err := client.Trades(TradesSubscriptionParams{Coin: "BTC"}, func(trades []Trade, err error) {
		require.NoError(t, err)
		mu.Lock()
		defer mu.Unlock()
		retained = append(retained, trades)
		if len(retained) == 4 {
			close(done)
		}
	})
	require.NoError(t, err)

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for four dispatches")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, retained[0], 20)
	require.Equal(t, "BTC", retained[0][0].Coin)
	require.Equal(t, "65000.5", retained[0][0].Px,
		"payload retained by a subscriber must survive pooled buffer reuse")
	require.Equal(t, "1.5", retained[3][0].Px)
}

// wsDispatchTestServer starts an httptest websocket server that drains the
// client's subscribe frame, pushes each message in order, then keeps the
// connection open until the client disconnects.
func wsDispatchTestServer(t *testing.T, msgs ...[]byte) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil { // subscribe frame
			return
		}
		for _, msg := range msgs {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// wsCollectDispatch subscribes to a channel on a fresh client connected to
// server and returns the first payload the subscriber callback receives.
func wsCollectDispatch[T any](
	t *testing.T,
	server *httptest.Server,
	subscribe func(client *WebsocketClient, deliver func(T)) (*Subscription, error),
) T {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewWebsocketClient(server.URL)
	require.NoError(t, client.Connect(ctx))
	defer func() { _ = client.Close() }()

	got := make(chan T, 1)
	_, err := subscribe(client, func(v T) {
		select {
		case got <- v:
		default:
		}
	})
	require.NoError(t, err)

	select {
	case v := <-got:
		return v
	case <-ctx.Done():
		t.Fatal("timed out waiting for dispatch")
		var zero T
		return zero
	}
}

// TestReadPumpDispatchesOpenOrders pins the openOrders dispatch key:
// subscriptions key on "openOrders:<user>[:<dex>]" (keyOpenOrders), so the
// message-side Key() must rebuild the same key from the payload's user/dex.
// Returning the bare channel name would silently drop every message.
func TestReadPumpDispatchesOpenOrders(t *testing.T) {
	const user = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const order = `{"coin":"BTC","side":"B","limitPx":"65000.0","sz":"0.1",` +
		`"oid":12345,"timestamp":1703001234567,"origSz":"0.2","cloid":null}`

	t.Run("UserOnly", func(t *testing.T) {
		server := wsDispatchTestServer(t, []byte(
			`{"channel":"openOrders","data":{"dex":"","user":"`+user+`","orders":[`+order+`]}}`,
		))
		orders := wsCollectDispatch(t, server,
			func(c *WebsocketClient, deliver func(OpenOrders)) (*Subscription, error) {
				return c.OpenOrders(
					OpenOrdersSubscriptionParams{User: user},
					func(o OpenOrders, err error) {
						require.NoError(t, err)
						deliver(o)
					},
				)
			},
		)
		require.Equal(t, user, orders.User)
		require.Len(t, orders.Orders, 1)
		require.Equal(t, "BTC", orders.Orders[0].Coin)
		require.Equal(t, int64(12345), orders.Orders[0].Oid)
	})

	t.Run("UserAndDex", func(t *testing.T) {
		dex := "xyz"
		server := wsDispatchTestServer(t, []byte(
			`{"channel":"openOrders","data":{"dex":"`+dex+`","user":"`+user+`","orders":[`+order+`]}}`,
		))
		orders := wsCollectDispatch(t, server,
			func(c *WebsocketClient, deliver func(OpenOrders)) (*Subscription, error) {
				return c.OpenOrders(
					OpenOrdersSubscriptionParams{User: user, Dex: &dex},
					func(o OpenOrders, err error) {
						require.NoError(t, err)
						deliver(o)
					},
				)
			},
		)
		require.Equal(t, user, orders.User)
		require.Equal(t, dex, orders.Dex)
		require.Len(t, orders.Orders, 1)
	})
}

// TestReadPumpDispatchesTwapStates pins the twapStates dispatch key under the
// same "twapStates:<user>[:<dex>]" contract as openOrders.
func TestReadPumpDispatchesTwapStates(t *testing.T) {
	const user = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const state = `{"coin":"BTC","user":"` + user + `","side":"B","sz":"1.0",` +
		`"executedSz":"0.5","executedNtl":"32500.0","minutes":30,"reduceOnly":false,` +
		`"randomize":true,"timestamp":1703001234567}`

	t.Run("UserOnly", func(t *testing.T) {
		server := wsDispatchTestServer(t, []byte(
			`{"channel":"twapStates","data":{"dex":"","user":"`+user+`","states":[[1,`+state+`]]}}`,
		))
		states := wsCollectDispatch(t, server,
			func(c *WebsocketClient, deliver func(TwapStates)) (*Subscription, error) {
				return c.TwapStates(
					TwapStatesSubscriptionParams{User: user},
					func(s TwapStates, err error) {
						require.NoError(t, err)
						deliver(s)
					},
				)
			},
		)
		require.Equal(t, user, states.User)
		require.Len(t, states.States, 1)
		require.Equal(t, 1, states.States[0].First)
		require.Equal(t, "BTC", states.States[0].Second.Coin)
	})

	t.Run("UserAndDex", func(t *testing.T) {
		dex := "xyz"
		server := wsDispatchTestServer(t, []byte(
			`{"channel":"twapStates","data":{"dex":"`+dex+`","user":"`+user+`","states":[[1,`+state+`]]}}`,
		))
		states := wsCollectDispatch(t, server,
			func(c *WebsocketClient, deliver func(TwapStates)) (*Subscription, error) {
				return c.TwapStates(
					TwapStatesSubscriptionParams{User: user, Dex: &dex},
					func(s TwapStates, err error) {
						require.NoError(t, err)
						deliver(s)
					},
				)
			},
		)
		require.Equal(t, user, states.User)
		require.Equal(t, dex, states.Dex)
		require.Len(t, states.States, 1)
	})
}

// TestReadPumpDispatchesWebData3 pins the webData3 dispatch key: messages
// carry only userState.user (no dex), so both sides key on
// "webData3:<user>" — a subscription made WITH a dex shares that key.
func TestReadPumpDispatchesWebData3(t *testing.T) {
	const user = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	msg := []byte(
		`{"channel":"webData3","data":{"userState":{"serverTime":1703001234567,` +
			`"cumLedger":"100.5","isVault":false,"user":"` + user + `"},` +
			`"perpDexStates":[{"totalVaultEquity":"1000.0"}]}}`,
	)

	t.Run("UserOnly", func(t *testing.T) {
		server := wsDispatchTestServer(t, msg)
		state := wsCollectDispatch(t, server,
			func(c *WebsocketClient, deliver func(WebData3)) (*Subscription, error) {
				return c.WebData3(
					WebData3SubscriptionParams{User: user},
					func(w WebData3, err error) {
						require.NoError(t, err)
						deliver(w)
					},
				)
			},
		)
		require.Equal(t, user, state.UserState.User)
		require.Equal(t, int64(1703001234567), state.UserState.ServerTime)
		require.Len(t, state.PerpDexStates, 1)
	})

	t.Run("UserAndDexSharesUserKey", func(t *testing.T) {
		dex := "xyz"
		server := wsDispatchTestServer(t, msg)
		state := wsCollectDispatch(t, server,
			func(c *WebsocketClient, deliver func(WebData3)) (*Subscription, error) {
				return c.WebData3(
					WebData3SubscriptionParams{User: user, Dex: &dex},
					func(w WebData3, err error) {
						require.NoError(t, err)
						deliver(w)
					},
				)
			},
		)
		require.Equal(t, user, state.UserState.User)
	})
}

// TestDecodeWsEnvelopeLargeFrameErrors pins error reporting on the zero-copy
// AST path (>= wsEnvelopeAstThreshold): a frame whose channel is missing or
// not a string, or whose JSON is truncated, must surface a parse error like
// the small-message struct path does — not a silent empty/garbage channel
// that downstream logs as `no dispatcher for channel: ""`.
func TestDecodeWsEnvelopeLargeFrameErrors(t *testing.T) {
	pad := strings.Repeat("x", 2*wsEnvelopeAstThreshold)
	valid := `{"channel":"trades","data":[{"coin":"BTC","pad":"` + pad + `"}]}`

	tests := []struct {
		name         string
		msg          string
		wantErr      bool
		wantChannel  string
		wantDataPart string
	}{
		{"Valid", valid, false, ChannelTrades, `"coin":"BTC"`},
		{"NonStringChannel",
			`{"channel":123,"data":[{"coin":"BTC","pad":"` + pad + `"}]}`, true, "", ""},
		{"NullChannel",
			`{"channel":null,"data":[{"coin":"BTC","pad":"` + pad + `"}]}`, true, "", ""},
		{"MissingChannel",
			`{"data":[{"coin":"BTC","pad":"` + pad + `"}]}`, true, "", ""},
		{"Truncated", valid[:len(valid)/2], true, "", ""},
		// data-less frames are valid envelopes (e.g. pong); validating the
		// data subtree is the dispatcher's job.
		{"MissingData", `{"channel":"pong","pad":"` + pad + `"}`, false, ChannelPong, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(tc.msg), wsEnvelopeAstThreshold,
				"fixture must exercise the zero-copy AST path")

			msg, err := decodeWsEnvelope([]byte(tc.msg))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantChannel, msg.Channel)
			if tc.wantDataPart == "" {
				require.Empty(t, msg.Data)
			} else {
				require.Contains(t, msg.Data, tc.wantDataPart)
			}
		})
	}
}
