package hyperliquid

import "testing"

// BenchmarkParseFloat measures the lenient float parsing used when decoding
// numeric strings off the wire.
func BenchmarkParseFloat(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = parseFloat("0.00012345")
	}
}

// BenchmarkFloatToWire measures float64 -> wire string conversion, executed
// twice per order (price and size) on every order placement.
func BenchmarkFloatToWire(b *testing.B) {
	values := []struct {
		name  string
		value float64
	}{
		{"Integer", 100.0},
		{"Decimals", 0.00012345},
		{"Large", 98765.4321},
	}
	for _, v := range values {
		b.Run(v.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := floatToWire(v.value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRoundToSignificantFigures measures price rounding used by
// market-open/market-close helpers.
func BenchmarkRoundToSignificantFigures(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = roundToSignificantFigures(12345.6789, 5)
	}
}
