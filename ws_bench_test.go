package hyperliquid

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var benchWsEnvelopeSmall = []byte(
	`{"channel":"trades","data":[{"coin":"BTC","side":"B","px":"65000.5","sz":"0.25","time":1703001234567,"hash":"0x5e43f6","tid":1234567890,"users":["0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0","0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0"]}]}`,
)

var benchWsEnvelopeLarge = []byte(
	`{"channel":"webData2","data":{"clearinghouseState":{"marginSummary":{"accountValue":"100000.0"},"assetPositions":[` +
		strings.Repeat(
			`{"position":{"coin":"BTC","szi":"1.5","entryPx":"65000.0","positionValue":"97500.0","unrealizedPnl":"100.0","returnOnEquity":"0.001"}},`,
			400,
		) +
		`{"position":{"coin":"ETH","szi":"2.0","entryPx":"3000.0","positionValue":"6000.0","unrealizedPnl":"10.0","returnOnEquity":"0.002"}}]}}}`,
)

// BenchmarkDecodeWsEnvelope measures the per-message envelope extraction in
// readPump: struct decode below wsEnvelopeAstThreshold, zero-copy sonic AST
// extraction above it.
func BenchmarkDecodeWsEnvelope(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(benchWsEnvelopeSmall)))
		for b.Loop() {
			msg, err := decodeWsEnvelope(benchWsEnvelopeSmall)
			if err != nil {
				b.Fatal(err)
			}
			if msg.Channel != ChannelTrades || msg.Data == "" {
				b.Fatal("bad decode")
			}
		}
	})
	b.Run("Large", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(benchWsEnvelopeLarge)))
		for b.Loop() {
			msg, err := decodeWsEnvelope(benchWsEnvelopeLarge)
			if err != nil {
				b.Fatal(err)
			}
			if msg.Channel != ChannelWebData2 || msg.Data == "" {
				b.Fatal("bad decode")
			}
		}
	})
}

// benchWsServer streams messages of the given size forever per connection.
func benchWsServer(tb testing.TB, payload []byte) *httptest.Server {
	tb.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		}
	}))
	tb.Cleanup(server.Close)
	return server
}

// BenchmarkReadMessage measures the websocket read path: pooled buffers
// (readMessage) vs the stdlib-per-message allocation (conn.ReadMessage).
func BenchmarkReadMessage(b *testing.B) {
	smallPayload := []byte(benchWsEnvelopeSmall)
	largePayload := []byte(benchWsEnvelopeLarge)

	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"Small", smallPayload},
		{"Large", largePayload},
	} {
		server := benchWsServer(b, tc.payload)
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

		b.Run(tc.name+"/Pooled", func(b *testing.B) {
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			cli := &WebsocketClient{conn: conn, readTimeout: 30 * time.Second}

			b.ReportAllocs()
			b.SetBytes(int64(len(tc.payload)))
			for b.Loop() {
				buf, err := cli.readMessage()
				if err != nil {
					b.Fatal(err)
				}
				if buf.Len() != len(tc.payload) {
					b.Fatalf("got %d bytes", buf.Len())
				}
				releaseWSBuf(buf)
			}
		})

		b.Run(tc.name+"/Stdlib", func(b *testing.B) {
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = conn.Close() }()

			b.ReportAllocs()
			b.SetBytes(int64(len(tc.payload)))
			for b.Loop() {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					b.Fatal(err)
				}
				if len(msg) != len(tc.payload) {
					b.Fatalf("got %d bytes", len(msg))
				}
			}
		})
	}
}
