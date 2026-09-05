package hyperliquid

import (
	"reflect"

	"github.com/bytedance/sonic"
)

// jsonCodec is the SDK-wide JSON codec: sonic with the stdlib-compatible
// configuration (HTML escaping enabled, map keys sorted), keeping wire
// payloads byte-compatible with encoding/json while getting sonic's
// JIT-accelerated (un)marshaling.
//
// CopyString is load-bearing, not a tunable: the websocket receive path
// hands decoders a Data string that points into a pooled read buffer (see
// decodeWsEnvelope), so decoded values must be copies. Switching to a
// config without CopyString would corrupt dispatched payloads as soon as
// the buffer is reused — TestDecodeWsEnvelopeDoesNotAliasReadBuffer and
// TestReadPumpPayloadSurvivesBufferReuse guard this.
var jsonCodec = sonic.ConfigStd

// pretouchJSON compiles sonic codecs for the given (zero) values upfront.
// Sonic JIT-compiles a codec on the first marshal/unmarshal of each type,
// which costs tens-to-hundreds of microseconds per type; pretouching moves
// that cost to client construction. A failure here is non-fatal (types then
// compile lazily on first use), so the error is intentionally dropped.
func pretouchJSON(values ...any) {
	types := make([]reflect.Type, len(values))
	for i, v := range values {
		types[i] = reflect.TypeOf(v)
	}
	_ = sonic.PretouchMany(types) //nolint:errcheck // lazy compile on first use otherwise
}
