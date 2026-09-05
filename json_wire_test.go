package hyperliquid

// Wire-compatibility guards for the JSON codec (sonic, ConfigStd).
// The expected bytes below were captured from the previous easyjson-generated
// implementation; the codec MUST keep producing byte-identical payloads for
// the exchange API to accept them.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// wireOrderAction is the reference action for wire-format guards.
var wireOrderAction = OrderAction{
	Type: "order",
	Orders: []OrderWire{
		{
			Asset: 0, IsBuy: true, LimitPx: "100.5", Size: "1.5", ReduceOnly: false,
			OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifGtc}},
		},
	},
	Grouping: "na",
}

func TestJSONWireOrderActionMarshal(t *testing.T) {
	want := `{"type":"order","orders":[{"a":0,"b":true,"p":"100.5","s":"1.5","r":false,"t":{"limit":{"tif":"Gtc"}}}],"grouping":"na"}`

	got, err := jsonCodec.Marshal(wireOrderAction)
	require.NoError(t, err)
	require.Equal(t, want, string(got))
}

func TestJSONWireLevelStringFloats(t *testing.T) {
	// Level prices/sizes are `,string`-encoded floats on the wire.
	got, err := jsonCodec.Marshal(Level{N: 3, Px: 65000.5, Sz: 1.25})
	require.NoError(t, err)
	require.Equal(t, `{"n":3,"px":"65000.5","sz":"1.25"}`, string(got))

	var lvl Level
	require.NoError(t, jsonCodec.Unmarshal([]byte(`{"n":3,"px":"65000.5","sz":"1.25"}`), &lvl))
	require.Equal(t, Level{N: 3, Px: 65000.5, Sz: 1.25}, lvl)
}

func TestJSONWireL2BookUnmarshal(t *testing.T) {
	data := []byte(
		`{"coin":"BTC","levels":[[{"px":"65000.0","sz":"1.5","n":1}],[{"px":"64999.5","sz":"2.0","n":2}]],"time":1703001234567}`,
	)
	var book L2Book
	require.NoError(t, jsonCodec.Unmarshal(data, &book))
	require.Equal(t, "BTC", book.Coin)
	require.Len(t, book.Levels, 2)
	require.Equal(t, 65000.0, book.Levels[0][0].Px)
	require.Equal(t, 1.5, book.Levels[0][0].Sz)
	require.Equal(t, int64(1703001234567), book.Time)
}

func TestJSONWireOrderActionRoundTrip(t *testing.T) {
	var decoded OrderAction
	require.NoError(t, jsonCodec.Unmarshal([]byte(
		`{"type":"order","orders":[{"a":0,"b":true,"p":"100.5","s":"1.5","r":false,"t":{"limit":{"tif":"Gtc"}}}],"grouping":"na"}`,
	), &decoded))
	require.Equal(t, "order", decoded.Type)
	require.Len(t, decoded.Orders, 1)
	require.Equal(t, 0, decoded.Orders[0].Asset)
	require.True(t, decoded.Orders[0].IsBuy)
	require.Equal(t, "100.5", decoded.Orders[0].LimitPx)
	require.Equal(t, TifGtc, decoded.Orders[0].OrderType.Limit.Tif)
	require.Equal(t, "na", decoded.Grouping)
}
