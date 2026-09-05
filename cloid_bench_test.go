package hyperliquid

import "testing"

// BenchmarkNormalizeCloid measures client-order-id normalization, executed
// per order that carries a cloid.
func BenchmarkNormalizeCloid(b *testing.B) {
	withPrefix := "0x0123456789abcdef0123456789abcdef"
	withoutPrefix := "0123456789abcdef0123456789abcdef"

	b.Run("WithPrefix", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := normalizeCloid(&withPrefix); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("WithoutPrefix", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := normalizeCloid(&withoutPrefix); err != nil {
				b.Fatal(err)
			}
		}
	})
}
