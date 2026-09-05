package hyperliquid

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/gorilla/websocket"
	"github.com/sonirico/vago/lol"
)

const (
	// pingInterval is the interval for sending ping messages to keep WebSocket alive
	pingInterval = 50 * time.Second

	// wsReadTimeout is the default maximum duration to wait for a single read from
	// the server before treating the connection as stalled. Must exceed pingInterval
	// so that normal pong responses do not trigger a false timeout.
	wsReadTimeout = 90 * time.Second
)

type Subscription struct {
	ID      string
	Payload any
	Close   func()
}

type WebsocketClient struct {
	url                   string
	conn                  *websocket.Conn
	dialer                *websocket.Dialer
	mu                    sync.RWMutex
	writeMu               sync.Mutex
	subscribers           map[string]*uniqSubscriber
	msgDispatcherRegistry map[string]msgDispatcher
	nextSubID             atomic.Int64
	done                  chan struct{}
	closeOnce             sync.Once
	reconnectWait         time.Duration
	readTimeout           time.Duration
	debug                 bool
	logger                lol.Logger
}

var upstreamHosts map[string]struct{}

func init() {
	mustHost := func(s string) string {
		u, err := url.Parse(s)
		if err != nil {
			panic(fmt.Sprintf("invalid upstream URL %q: %v", s, err))
		}
		return strings.ToLower(u.Hostname())
	}
	upstreamHosts = map[string]struct{}{
		mustHost(MainnetAPIURL): {},
		mustHost(TestnetAPIURL): {},
	}
}

func isUpstream(u *url.URL) bool {
	_, ok := upstreamHosts[strings.ToLower(u.Hostname())]
	return ok
}

func NewWebsocketClient(baseURL string, opts ...WsOpt) *WebsocketClient {
	if baseURL == "" {
		baseURL = MainnetAPIURL
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		log.Fatalf("invalid URL: %v", err)
	}

	// the current usage expects a full address (https://api.hyp..) to keep compatibility check if
	// that host is set and just use the old method. any new caller with their own endpoint will be
	// forced to provide a full URI
	if isUpstream(parsedURL) {
		parsedURL.Scheme = "wss"
		parsedURL.Path = "/ws"
	} else {
		switch parsedURL.Scheme {
		case "https":
			parsedURL.Scheme = "wss"
		case "http":
			parsedURL.Scheme = "ws"
		case "":
			// baseURL has no scheme set, odd
			panic("baseURL must have a scheme set, either wss or ws")
		}
	}

	wsURL := parsedURL.String()

	cli := &WebsocketClient{
		url:           wsURL,
		done:          make(chan struct{}),
		reconnectWait: time.Second,
		readTimeout:   wsReadTimeout,
		subscribers:   make(map[string]*uniqSubscriber),
		msgDispatcherRegistry: map[string]msgDispatcher{
			ChannelPong:             NewPongDispatcher(),
			ChannelTrades:           NewMsgDispatcher[Trades](ChannelTrades),
			ChannelActiveAssetCtx:   NewMsgDispatcher[ActiveAssetCtx](ChannelActiveAssetCtx),
			ChannelFastAssetCtxs:    NewMsgDispatcher[WsFastAssetCtxs](ChannelFastAssetCtxs),
			ChannelAllDexsAssetCtxs: NewMsgDispatcher[WsAllDexsAssetCtxs](ChannelAllDexsAssetCtxs),
			ChannelL2Book:           NewMsgDispatcher[L2Book](ChannelL2Book),
			ChannelCandle:           NewMsgDispatcher[Candle](ChannelCandle),
			ChannelAllMids:          NewMsgDispatcher[AllMids](ChannelAllMids),
			ChannelNotification:     NewMsgDispatcher[Notification](ChannelNotification),
			ChannelOrderUpdates:     NewMsgDispatcher[WsOrders](ChannelOrderUpdates),
			ChannelWebData2:         NewMsgDispatcher[WebData2](ChannelWebData2),
			ChannelBbo:              NewMsgDispatcher[Bbo](ChannelBbo),
			ChannelUserFills:        NewMsgDispatcher[WsOrderFills](ChannelUserFills),
			ChannelSubResponse:      NewNoopDispatcher(),
			ChannelClearinghouseState: NewMsgDispatcher[ClearinghouseStateMessage](
				ChannelClearinghouseState,
			),
			ChannelOpenOrders: NewMsgDispatcher[OpenOrders](ChannelOpenOrders),
			ChannelTwapStates: NewMsgDispatcher[TwapStates](ChannelTwapStates),
			ChannelWebData3:   NewMsgDispatcher[WebData3](ChannelWebData3),
		},
	}

	for _, opt := range opts {
		opt.Apply(cli)
	}

	// Precompile JSON codecs for every dispatchable payload type so the first
	// message of each channel doesn't pay sonic's JIT compilation cost.
	pretouchJSON(
		wsMessageWire{},
		Trades(nil),
		ActiveAssetCtx{},
		WsFastAssetCtxs(nil),
		WsAllDexsAssetCtxs{},
		L2Book{},
		Candle{},
		AllMids{},
		Notification{},
		WsOrders(nil),
		WebData2{},
		Bbo{},
		WsOrderFills{},
		ClearinghouseStateMessage{},
		OpenOrders{},
		TwapStates{},
		WebData3{},
	)

	return cli
}

func (w *WebsocketClient) Connect(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		return nil
	}

	if w.dialer == nil {
		w.dialer = websocket.DefaultDialer
	}

	//nolint:bodyclose // WebSocket connections don't have response bodies to close
	conn, _, err := w.dialer.DialContext(ctx, w.url, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	w.conn = conn

	go w.readPump(ctx)
	go w.pingPump(ctx)

	return w.resubscribeAll()
}

type Handler[T subscriptable] func(wsMessage) (T, error)

func (w *WebsocketClient) subscribe(
	payload subscriptable,
	callback func(any),
) (*Subscription, error) {
	if callback == nil {
		return nil, fmt.Errorf("callback cannot be nil")
	}

	w.mu.Lock()

	pkey := payload.Key()
	subscriber, exists := w.subscribers[pkey]
	if !exists {
		subscriber = newUniqSubscriber(
			pkey,
			payload,
			// on subscribe
			func(p subscriptable) {
				if err := w.sendSubscribe(p); err != nil {
					w.logErrf("failed to subscribe: %v", err)
				}
			},
			// on unsubscribe
			func(p subscriptable) {
				w.mu.Lock()
				defer w.mu.Unlock()
				delete(w.subscribers, pkey)
				if err := w.sendUnsubscribe(p); err != nil {
					w.logErrf("failed to unsubscribe: %v", err)
				}
			},
		)

		w.subscribers[pkey] = subscriber
	}

	w.mu.Unlock()

	nextID := w.nextSubID.Add(1)
	subID := key(pkey, strconv.Itoa(int(nextID)))
	subscriber.subscribe(subID, callback)

	return &Subscription{
		ID: subID,
		Close: func() {
			subscriber.unsubscribe(subID)
		},
	}, nil
}

func (w *WebsocketClient) Close() error {
	var err error
	w.closeOnce.Do(func() {
		err = w.close()
	})
	return err
}

func (w *WebsocketClient) close() error {
	close(w.done)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		return w.conn.Close()
	}

	for _, subscriber := range w.subscribers {
		subscriber.clear()
	}
	return nil
}

// Private methods

// wsBufPool recycles websocket message buffers: conn.ReadMessage allocates a
// fresh slice per message (a ~100KB webData2 message is ~250KB of garbage
// with buffer regrowth), while a pooled buffer is reused across messages.
var wsBufPool = sync.Pool{New: func() any {
	return bytes.NewBuffer(make([]byte, 0, 8*1024))
}}

// wsMaxRetainedBuf caps pooled buffer capacity — a single huge message must
// not pin a large allocation in the pool forever.
const wsMaxRetainedBuf = 512 * 1024

func releaseWSBuf(buf *bytes.Buffer) {
	if buf.Cap() <= wsMaxRetainedBuf {
		wsBufPool.Put(buf)
	}
}

// readMessage reads one websocket message into a pooled buffer. The caller
// must releaseWSBuf it after the message has been fully processed (dispatch
// is synchronous; sonic's stdlib-compatible config copies decoded strings,
// so nothing references the buffer afterwards).
func (w *WebsocketClient) readMessage() (*bytes.Buffer, error) {
	_, r, err := w.conn.NextReader()
	if err != nil {
		return nil, err
	}
	buf := wsBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if _, err := buf.ReadFrom(r); err != nil {
		releaseWSBuf(buf)
		return nil, err
	}
	return buf, nil
}

func (w *WebsocketClient) readPump(ctx context.Context) {
	shouldReconnect := false
	defer func() {
		w.mu.Lock()
		if w.conn != nil {
			_ = w.conn.Close() // Ignore close error in defer
			w.conn = nil
		}
		w.mu.Unlock()

		if shouldReconnect {
			w.reconnect(ctx)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		default:
			if err := w.conn.SetReadDeadline(time.Now().Add(w.readTimeout)); err != nil {
				w.logErrf("websocket set read deadline: %v", err)
				return
			}

			buf, err := w.readMessage()
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					w.logErrf("websocket read timeout, reconnecting")
					shouldReconnect = true
					return
				}
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					w.logErrf("websocket read error: %v", err)
				}
				return
			}

			if w.debug {
				w.logDebugf("[<] %s", buf.String())
			}

			wsMsg, err := decodeWsEnvelope(buf.Bytes())
			if err != nil {
				w.logErrf("websocket message parse error: %v", err)
				releaseWSBuf(buf)
				continue
			}

			if err := w.dispatch(wsMsg); err != nil {
				w.logErrf("failed to dispatch websocket message: %v", err)
			}
			releaseWSBuf(buf)
		}
	}
}

func (w *WebsocketClient) pingPump(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.sendPing(); err != nil {
				w.logErrf("ping error: %v", err)
				w.reconnect(ctx)
				return
			}
		}
	}
}

// wsEnvelopeAstThreshold is the message size above which the envelope is
// extracted via sonic's lazy AST (zero-copy reference into the read buffer)
// instead of a struct decode (which copies the whole data subtree into a
// json.RawMessage). Measured on arm64: for a ~54KB webData2 message the AST
// path costs 141µs/1KB vs 211µs/250KB for the copying path; below ~2KB the
// struct decode is ~300ns cheaper.
const wsEnvelopeAstThreshold = 2048

func decodeWsEnvelope(msg []byte) (wsMessage, error) {
	if len(msg) >= wsEnvelopeAstThreshold {
		// CopyReturn=false: the envelope string references the (pooled) read
		// buffer, which outlives the synchronous dispatch — no subtree copy.
		// ValidateJSON=false: the dispatcher's UnmarshalFromString validates
		// the data subtree anyway, so a full pre-validation scan is redundant.
		node, err := sonic.GetWithOptions(
			msg,
			ast.SearchOptions{CopyReturn: false, ValidateJSON: false},
		)
		if err != nil {
			return wsMessage{}, fmt.Errorf("failed to parse websocket message: %w", err)
		}
		channel, _ := node.Get("channel").String()
		wsMsg := wsMessage{Channel: channel}
		if data := node.Get("data"); data.Valid() {
			raw, err := data.Raw()
			if err != nil {
				return wsMessage{}, fmt.Errorf("failed to parse websocket message data: %w", err)
			}
			wsMsg.Data = raw
		}
		return wsMsg, nil
	}

	var wire wsMessageWire
	if err := jsonCodec.Unmarshal(msg, &wire); err != nil {
		return wsMessage{}, fmt.Errorf("failed to parse websocket message: %w", err)
	}
	return wsMessage{Channel: wire.Channel, Data: string(wire.Data)}, nil
}

func (w *WebsocketClient) dispatch(msg wsMessage) error {
	dispatcher, ok := w.msgDispatcherRegistry[msg.Channel]
	if !ok {
		return fmt.Errorf("no dispatcher for channel: %s", msg.Channel)
	}

	return dispatcher.Dispatch(w.lookupSubscriber, msg)
}

// lookupSubscriber resolves a subscription key to its subscriber with an O(1)
// map lookup. The returned pointer is safe to use after the lock is released:
// uniqSubscriber has its own mutex, and dispatching to a subscriber that was
// concurrently removed is a harmless no-op.
func (w *WebsocketClient) lookupSubscriber(key string) (*uniqSubscriber, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	sub, ok := w.subscribers[key]
	return sub, ok
}

func (w *WebsocketClient) reconnect(ctx context.Context) {
	for {
		select {
		case <-w.done:
			return
		case <-ctx.Done():
			return
		default:
			if err := w.Connect(ctx); err == nil {
				return
			}
			time.Sleep(w.reconnectWait)
			w.reconnectWait *= 2 // TODO: configurable strategies such as exponential backoff and the like
			if w.reconnectWait > time.Minute {
				w.reconnectWait = time.Minute
			}
		}
	}
}

func (w *WebsocketClient) resubscribeAll() error {
	for _, subscriber := range w.subscribers {
		if err := w.sendSubscribe(subscriber.subscriptionPayload); err != nil {
			return fmt.Errorf("resubscribe: %w", err)
		}
	}
	return nil
}

func (w *WebsocketClient) sendSubscribe(payload subscriptable) error {
	return w.writeJSON(wsCommand{
		Method:       "subscribe",
		Subscription: payload,
	})
}

func (w *WebsocketClient) sendUnsubscribe(payload subscriptable) error {
	return w.writeJSON(wsCommand{
		Method:       "unsubscribe",
		Subscription: payload,
	})
}

func (w *WebsocketClient) sendPing() error {
	return w.writeJSON(wsCommand{Method: "ping"})
}

func (w *WebsocketClient) writeJSON(v any) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("connection closed")
	}

	if w.debug {
		bts, _ := jsonCodec.Marshal(v)
		w.logDebugf("[>] %s", string(bts))
	}

	return w.conn.WriteJSON(v)
}

func (w *WebsocketClient) logErrf(fmt string, args ...any) {
	if w.logger == nil {
		return
	}

	w.logger.Errorf(fmt, args...)
}

func (w *WebsocketClient) logDebugf(fmt string, args ...any) {
	if w.logger == nil {
		return
	}

	w.logger.Debugf(fmt, args...)
}
