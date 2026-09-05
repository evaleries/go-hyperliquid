package hyperliquid

import (
	"encoding/json"
	"fmt"
	"testing"
)

// BenchmarkMsgDispatcherDispatch measures the per-message dispatch path:
// channel match, JSON decode, subscriber key lookup and callback fan-out.
func BenchmarkMsgDispatcherDispatch(b *testing.B) {
	trade := Trade{
		Coin:  "BTC",
		Side:  "B",
		Px:    "65000.5",
		Sz:    "0.25",
		Time:  1703001234567,
		Hash:  "0x5e43f6c1f8b4a0d2e9f8a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f6",
		Tid:   1234567890,
		Users: []string{"0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0", "0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0"},
	}
	tradesJSON, err := json.Marshal([]Trade{trade})
	if err != nil {
		b.Fatal(err)
	}
	msg := wsMessage{Channel: ChannelTrades, Data: string(tradesJSON)}

	dispatcher := NewMsgDispatcher[Trades](ChannelTrades)

	// The dispatch path resolves the target subscriber through an O(1) map
	// lookup; the numSubs variants measure map lookup under different map sizes
	// plus callback fan-out on the single matching subscriber.
	for _, numSubs := range []int{1, 10} {
		b.Run(fmt.Sprintf("Subscribers%d", numSubs), func(b *testing.B) {
			subByKey := make(map[string]*uniqSubscriber, numSubs)
			target := newUniqSubscriber(
				keyTrades("BTC"),
				remoteTradesSubscriptionPayload{Coin: "BTC"},
				func(subscriptable) {},
				func(subscriptable) {},
			)
			target.subscribe("cb-0", func(any) {})
			subByKey[target.id] = target
			for i := 1; i < numSubs; i++ {
				other := newUniqSubscriber(
					fmt.Sprintf("trades:COIN%d", i),
					remoteTradesSubscriptionPayload{Coin: fmt.Sprintf("COIN%d", i)},
					func(subscriptable) {},
					func(subscriptable) {},
				)
				subByKey[other.id] = other
			}
			lookup := func(key string) (*uniqSubscriber, bool) {
				sub, ok := subByKey[key]
				return sub, ok
			}

			b.ReportAllocs()
			for b.Loop() {
				if err := dispatcher.Dispatch(lookup, msg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
