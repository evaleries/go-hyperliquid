package hyperliquid

import (
	"reflect"

	"github.com/bytedance/sonic"
)

// jsonCodec is the SDK-wide JSON codec: sonic with the stdlib-compatible
// configuration (HTML escaping enabled, map keys sorted), keeping wire
// payloads byte-compatible with encoding/json while getting sonic's
// JIT-accelerated (un)marshaling. Single point of change if codec options
// ever need tuning (e.g. CopyString for cached payloads).
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
