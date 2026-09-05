package hyperliquid

import (
	"encoding/json"
	"fmt"
	"testing"
)

// benchL2BookJSON builds a realistic l2Book payload with the given number of
// levels per side.
func benchL2BookJSON(tb testing.TB, levelsPerSide int) []byte {
	tb.Helper()
	mkLevels := func(n int) []Level {
		levels := make([]Level, n)
		for i := range n {
			levels[i] = Level{
				N:  i + 1,
				Px: 65000.0 + float64(i)*0.5,
				Sz: 1.5 + float64(i)*0.01,
			}
		}
		return levels
	}
	book := L2Book{
		Coin:   "BTC",
		Levels: [][]Level{mkLevels(levelsPerSide), mkLevels(levelsPerSide)},
		Time:   1703001234567,
	}
	data, err := json.Marshal(book)
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// BenchmarkTradesUnmarshal measures decoding of trades websocket messages.
// "Sonic" is the production path (jsonCodec); "Stdlib" is the reference.
func BenchmarkTradesUnmarshal(b *testing.B) {
	trades := make([]Trade, 10)
	for i := range trades {
		trades[i] = Trade{
			Coin:  "BTC",
			Side:  "B",
			Px:    "65000.5",
			Sz:    "0.25",
			Time:  1703001234567,
			Hash:  "0x5e43f6c1f8b4a0d2e9f8a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f6",
			Tid:   1234567890 + int64(i),
			Users: []string{"0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0", "0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0"},
		}
	}
	data, err := json.Marshal(Trades(trades))
	if err != nil {
		b.Fatal(err)
	}

	b.Run("Stdlib", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var t Trades
			if err := json.Unmarshal(data, &t); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Sonic", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var t Trades
			if err := jsonCodec.Unmarshal(data, &t); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkAllMidsUnmarshal measures decoding of allMids websocket messages.
func BenchmarkAllMidsUnmarshal(b *testing.B) {
	mids := AllMids{Mids: make(map[string]string, 30)}
	coins := []string{
		"BTC", "ETH", "SOL", "ARB", "DOGE", "AVAX", "LINK", "MATIC", "OP",
		"ATOM", "NEAR", "APT", "SUI", "INJ", "TIA", "SEI", "LTC", "BCH",
		"ETC", "FIL", "AAVE", "UNI", "MKR", "CRV", "SNX", "LDO", "RNDR",
		"STX", "IMX", "GMX",
	}
	for i, coin := range coins {
		mids.Mids[coin] = fmt.Sprintf("%d.5", 1000+i)
	}
	data, err := json.Marshal(mids)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("Stdlib", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var m AllMids
			if err := json.Unmarshal(data, &m); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Sonic", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var m AllMids
			if err := jsonCodec.Unmarshal(data, &m); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkL2BookUnmarshal measures decoding of l2Book websocket messages,
// one of the highest-frequency messages in market-data workloads.
func BenchmarkL2BookUnmarshal(b *testing.B) {
	for _, levels := range []int{5, 20} {
		data := benchL2BookJSON(b, levels)
		b.Run(fmt.Sprintf("Levels%d/Stdlib", levels), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var book L2Book
				if err := json.Unmarshal(data, &book); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("Levels%d/Sonic", levels), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var book L2Book
				if err := jsonCodec.Unmarshal(data, &book); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkWsOrderFillUnmarshal measures decoding of order fill messages,
// the hot path for execution-driven strategies.
func BenchmarkWsOrderFillUnmarshal(b *testing.B) {
	data := []byte(
		`{"coin":"BTC","px":"65000.5","sz":"0.25","side":"B","time":1703001234567,"startPosition":"1.0","dir":"Open Long","closedPnl":"0.0","hash":"0x5e43f6c1f8b4a0d2e9f8a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f6","oid":1234567890,"crossed":true,"fee":"0.0325","tid":1234567890,"feeToken":"USDC"}`,
	)

	b.Run("Stdlib", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var f WsOrderFill
			if err := json.Unmarshal(data, &f); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Sonic", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var f WsOrderFill
			if err := jsonCodec.Unmarshal(data, &f); err != nil {
				b.Fatal(err)
			}
		}
	})
}
