package hyperliquid

import "github.com/sonirico/vago/fp"

type subscriptable interface {
	Key() string
}

type (
	Trades   []Trade
	WsOrders []WsOrder

	WsFastAssetCtxs map[string]FastAssetCtx
)

func (t Trades) Key() string {
	if len(t) == 0 {
		return ""
	}
	return keyTrades(t[0].Coin)
}

func (a ActiveAssetCtx) Key() string {
	return keyActiveAssetCtx(a.Coin)
}

func (w WsFastAssetCtxs) Key() string {
	return keyFastAssetCtxs()
}

func (w WsAllDexsAssetCtxs) Key() string {
	return keyAllDexsAssetCtxs()
}

func (c Candle) Key() string {
	return keyCandles(c.Symbol, c.Interval)
}

func (c L2Book) Key() string {
	return keyL2Book(c.Coin)
}

func (a AllMids) Key() string {
	return keyAllMids(fp.None[string]())
}

func (n Notification) Key() string {
	// Notification messages are user-specific but don't contain user info in the message itself.
	// The dispatching is handled by the subscription system based on the subscription key.
	return ChannelNotification
}

func (w WsOrders) Key() string {
	// WsOrder messages are user-specific but don't contain user info in the message itself.
	// The dispatching is handled by the subscription system based on the subscription key.
	return ChannelOrderUpdates
}

func (w WebData2) Key() string {
	// WebData2 messages are user-specific but don't contain user info in the message itself.
	// The dispatching is handled by the subscription system based on the subscription key.
	return ChannelWebData2
}

func (w Bbo) Key() string { return keyBbo(w.Coin) }

func (w WsOrderFills) Key() string {
	return keyUserFills(w.User)
}

func (c ClearinghouseState) Key() string {
	// ClearinghouseState messages are user-specific but don't contain user/dex info in the message itself.
	// The dispatching is handled by the subscription system based on the subscription key.
	return ChannelClearinghouseState
}

func (c ClearinghouseStateMessage) Key() string {
	return keyClearinghouseState(c.User, dexOption(c.Dex))
}

func (o OpenOrders) Key() string {
	return keyOpenOrders(o.User, dexOption(o.Dex))
}

func (t TwapStates) Key() string {
	return keyTwapStates(t.User, dexOption(t.Dex))
}

func (w WebData3) Key() string {
	return keyWebData3(w.UserState.User, fp.None[string]())
}
