package hyperliquid

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

// fastAssetCtxsWire encodes a raw JSON payload the same way the server does
// on the fastAssetCtxs channel (see WsFastAssetCtxs.UnmarshalJSON):
// JSON -> raw DEFLATE (RFC 1951, no zlib/gzip wrapper) -> base64 -> JSON string.
// It returns the wire form of the enclosing JSON string token.
func fastAssetCtxsWire(t *testing.T, payload string) string {
	t.Helper()

	var buf bytes.Buffer
	zw, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = zw.Write([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	encoded, err := jsonCodec.Marshal(base64.StdEncoding.EncodeToString(buf.Bytes()))
	require.NoError(t, err)
	return string(encoded)
}

// TestWsFastAssetCtxsUnmarshal exercises WsFastAssetCtxs.UnmarshalJSON through
// the same entry point the websocket dispatcher uses
// (jsonCodec.UnmarshalFromString, see NewMsgDispatcher).
func TestWsFastAssetCtxsUnmarshal(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		// Realistic shape: coin -> {markPx, midPx} with ,string-encoded prices;
		// midPx may be absent (omitempty on the struct).
		data := fastAssetCtxsWire(t,
			`{"BTC":{"markPx":"65000.5","midPx":"65000.4"},"ETH":{"markPx":"3500.25"}}`,
		)

		var ctxs WsFastAssetCtxs
		require.NoError(t, jsonCodec.UnmarshalFromString(data, &ctxs))
		require.Len(t, ctxs, 2)

		btc := ctxs["BTC"]
		require.NotNil(t, btc.MarkPx)
		require.Equal(t, 65000.5, *btc.MarkPx)
		require.NotNil(t, btc.MidPx)
		require.Equal(t, 65000.4, *btc.MidPx)

		eth := ctxs["ETH"]
		require.NotNil(t, eth.MarkPx)
		require.Equal(t, 3500.25, *eth.MarkPx)
		require.Nil(t, eth.MidPx)
	})

	t.Run("payload is not a JSON string", func(t *testing.T) {
		var ctxs WsFastAssetCtxs
		require.Error(t, jsonCodec.UnmarshalFromString(`123`, &ctxs))
	})

	t.Run("invalid base64", func(t *testing.T) {
		var ctxs WsFastAssetCtxs
		require.Error(t, jsonCodec.UnmarshalFromString(`"!!!not-base64!!!"`, &ctxs))
	})

	t.Run("valid base64 but not deflate", func(t *testing.T) {
		b64 := base64.StdEncoding.EncodeToString([]byte("this is not a deflate stream"))
		var ctxs WsFastAssetCtxs
		require.Error(t, jsonCodec.UnmarshalFromString(`"`+b64+`"`, &ctxs))
	})

	t.Run("valid deflate but not JSON", func(t *testing.T) {
		data := fastAssetCtxsWire(t, `definitely not json`)
		var ctxs WsFastAssetCtxs
		require.Error(t, jsonCodec.UnmarshalFromString(data, &ctxs))
	})
}
