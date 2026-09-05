package hyperliquid

import (
	"fmt"
	"strings"
	"testing"
)

var benchAPIResponseOrder = []byte(
	`{"status":"ok","response":{"type":"order","data":{"statuses":[{"resting":{"oid":12345678901,"cloid":"0x00000000000000000000000000000000"}}]}}}`,
)

func benchAPIResponseLarge(tb testing.TB, statuses int) []byte {
	tb.Helper()
	var sb strings.Builder
	sb.WriteString(`{"status":"ok","response":{"type":"order","data":{"statuses":[`)
	for i := range statuses {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(
			&sb,
			`{"resting":{"oid":%d,"cloid":"0x%032x"}}`,
			12345678901+int64(i),
			i,
		)
	}
	sb.WriteString(`]}}}`)
	return []byte(sb.String())
}

// BenchmarkAPIResponseUnmarshal measures decoding of the exchange/info API
// envelope executed on every REST response.
func BenchmarkAPIResponseUnmarshal(b *testing.B) {
	b.Run("OrderResponse", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(benchAPIResponseOrder)))
		for b.Loop() {
			var res APIResponse[OrderResponse]
			if err := res.UnmarshalJSON(benchAPIResponseOrder); err != nil {
				b.Fatal(err)
			}
		}
	})

	large := benchAPIResponseLarge(b, 50)
	b.Run("OrderResponse50", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(large)))
		for b.Loop() {
			var res APIResponse[OrderResponse]
			if err := res.UnmarshalJSON(large); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Error", func(b *testing.B) {
		data := []byte(`{"status":"err","response":"unknown asset"}`)
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var res APIResponse[OrderResponse]
			if err := res.UnmarshalJSON(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}
