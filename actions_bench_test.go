package hyperliquid

import (
	"encoding/json"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

// BenchmarkOrderActionMsgpackMarshal isolates the msgpack encoding cost of an
// order action from the keccak hashing cost measured in BenchmarkActionHash.
func BenchmarkOrderActionMsgpackMarshal(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		data, err := msgpack.Marshal(benchOrderAction)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}
}

// BenchmarkOrderActionJSONMarshal measures JSON encoding of an order action,
// used in the exchange POST body. "Sonic" is the production path (jsonCodec);
// "Stdlib" is the pre-migration encoding/json reference.
func BenchmarkOrderActionJSONMarshal(b *testing.B) {
	b.Run("Stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, err := json.Marshal(benchOrderAction)
			if err != nil {
				b.Fatal(err)
			}
			_ = data
		}
	})

	b.Run("Sonic", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, err := jsonCodec.Marshal(benchOrderAction)
			if err != nil {
				b.Fatal(err)
			}
			_ = data
		}
	})
}
