package hyperliquid

import (
	"fmt"
)

// subscriberLookup resolves a subscription key (e.g. "trades:BTC") to its
// subscriber. It lets dispatchers do an O(1) map lookup per message instead of
// scanning a freshly-allocated slice of all subscribers.
type subscriberLookup func(key string) (*uniqSubscriber, bool)

type msgDispatcher interface {
	Dispatch(lookup subscriberLookup, msg wsMessage) error
}

type msgDispatcherFunc[T any] func(lookup subscriberLookup, msg wsMessage) error

func (d msgDispatcherFunc[T]) Dispatch(lookup subscriberLookup, msg wsMessage) error {
	return d(lookup, msg)
}

func NewMsgDispatcher[T subscriptable](channel string) msgDispatcher {
	return msgDispatcherFunc[T](func(lookup subscriberLookup, msg wsMessage) error {
		if msg.Channel != channel {
			return nil
		}

		var x T
		if err := jsonCodec.UnmarshalFromString(msg.Data, &x); err != nil {
			return fmt.Errorf("failed to unmarshal message: %v", err)
		}

		if sub, ok := lookup(x.Key()); ok {
			sub.dispatch(x)
		}

		return nil
	})
}

func NewNoopDispatcher() msgDispatcher {
	return msgDispatcherFunc[any](func(subscriberLookup, wsMessage) error {
		// println(string(msg.Data))
		return nil
	})
}

func NewPongDispatcher() msgDispatcher {
	return msgDispatcherFunc[any](func(_ subscriberLookup, msg wsMessage) error {
		if msg.Channel != ChannelPong {
			return nil
		}
		// keepalive is handled implicitly: every successful ReadMessage (including
		// pongs) resets the per-read deadline in readPump before the next iteration.
		return nil
	})
}
