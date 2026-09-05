package hyperliquid

import (
	"strings"
	"sync"

	"github.com/sonirico/vago/fp"
)

func key(args ...string) string {
	return strings.Join(args, ":")
}

// keyCache interns derived subscription keys so that per-message Key() calls
// on high-frequency channels don't allocate. Entries are bounded by the
// symbol/channel universe, which is small and stable.
type keyCache[K comparable] struct {
	mu sync.RWMutex
	m  map[K]string
}

func (c *keyCache[K]) get(id K, build func() string) string {
	c.mu.RLock()
	k, ok := c.m[id]
	c.mu.RUnlock()
	if ok {
		return k
	}
	k = build()
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[K]string)
	}
	c.m[id] = k
	c.mu.Unlock()
	return k
}

var (
	tradesKeyCache    keyCache[string]
	activeCtxKeyCache keyCache[string]
	l2BookKeyCache    keyCache[string]
	bboKeyCache       keyCache[string]
	candleKeyCache    keyCache[candleKey]
)

type candleKey struct{ symbol, interval string }

func keyTrades(coin string) string {
	return tradesKeyCache.get(coin, func() string { return key(ChannelTrades, coin) })
}

func keyActiveAssetCtx(coin string) string {
	return activeCtxKeyCache.get(coin, func() string { return key(ChannelActiveAssetCtx, coin) })
}

func keyFastAssetCtxs() string {
	return ChannelFastAssetCtxs
}

func keyAllDexsAssetCtxs() string {
	return ChannelAllDexsAssetCtxs
}

func keyCandles(symbol, interval string) string {
	return candleKeyCache.get(candleKey{symbol, interval}, func() string {
		return key(ChannelCandle, symbol, interval)
	})
}

func keyL2Book(coin string) string {
	return l2BookKeyCache.get(coin, func() string { return key(ChannelL2Book, coin) })
}

func keyAllMids(_ fp.Option[string]) string {
	// Unfortunately, "dex" parameter is not returned neither in subscription ACK nor in the
	// allMids message, no we are rendered unable to distinguish between different DEXes from
	// subscriber's standpoint.
	// Single-part keys equal the channel name — return it directly (no alloc).
	return ChannelAllMids
}

func keyNotification(_ string) string {
	// Notification messages are user-specific but don't contain user info in the message itself.
	// The dispatching is handled by the subscription system based on the subscription key.
	return ChannelNotification
}

func keyOrderUpdates(_ string) string {
	// Order updates are user-specific but don't contain user info in the message itself.
	// The dispatching is handled by the subscription system based on the subscription key.
	return ChannelOrderUpdates
}

func keyUserFills(user string) string {
	return key(ChannelUserFills, user)
}

func keyWebData2(_ string) string {
	// WebData2 messages are user-specific but don't contain user info in the message itself.
	// The dispatching is handled by the subscription system based on the subscription key.
	return ChannelWebData2
}

func keyBbo(coin string) string {
	return bboKeyCache.get(coin, func() string { return key(ChannelBbo, coin) })
}

func keyClearinghouseState(user string, dex fp.Option[string]) string {
	if dex.IsNone() {
		return key(ChannelClearinghouseState, user)
	}
	return key(ChannelClearinghouseState, user, dex.UnwrapUnsafe())
}

func keyOpenOrders(user string, dex fp.Option[string]) string {
	if dex.IsNone() {
		return key(ChannelOpenOrders, user)
	}
	return key(ChannelOpenOrders, user, dex.UnwrapUnsafe())
}

func keyTwapStates(user string, dex fp.Option[string]) string {
	if dex.IsNone() {
		return key(ChannelTwapStates, user)
	}
	return key(ChannelTwapStates, user, dex.UnwrapUnsafe())
}

func keyWebData3(user string, dex fp.Option[string]) string {
	if dex.IsNone() {
		return key(ChannelWebData3, user)
	}
	return key(ChannelWebData3, user, dex.UnwrapUnsafe())
}
