package hyperliquid

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	benchPrivateKey = func() []byte {
		b, err := hex.DecodeString(
			"abcd1234567890abcd1234567890abcd1234567890abcd1234567890abcd1234",
		)
		if err != nil {
			panic(err)
		}
		return b
	}()

	benchOrderAction = OrderAction{
		Type: "order",
		Orders: []OrderWire{
			{
				Asset:      0,
				IsBuy:      true,
				LimitPx:    "100.5",
				Size:       "1.5",
				ReduceOnly: false,
				OrderType: OrderWireType{
					Limit: &OrderWireTypeLimit{Tif: TifGtc},
				},
			},
		},
		Grouping: string(GroupingNA),
	}

	benchVaultAddress = "0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0"
	benchExpiresAfter = int64(1703001294567)
)

// actionHashInput returns the raw msgpack encoding of the benchmark order
// action, as produced inside actionHash before the str16->str8 conversion.
func actionHashInput(tb testing.TB) []byte {
	tb.Helper()
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.UseCompactInts(true)
	if err := enc.Encode(benchOrderAction); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// BenchmarkConvertStr16ToStr8 measures the custom msgpack walker used to
// normalize str16 headers to str8 for Python SDK compatibility.
func BenchmarkConvertStr16ToStr8(b *testing.B) {
	data := actionHashInput(b)

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = convertStr16ToStr8(data)
	}
}

// BenchmarkActionHash measures the msgpack + keccak256 hashing pipeline,
// executed once per signed action (order, cancel, etc.).
func BenchmarkActionHash(b *testing.B) {
	b.Run("NoVault", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = actionHash(benchOrderAction, "", 1703001234567, nil)
		}
	})

	b.Run("Vault", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = actionHash(benchOrderAction, benchVaultAddress, 1703001234567, nil)
		}
	})

	b.Run("VaultAndExpiresAfter", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = actionHash(
				benchOrderAction,
				benchVaultAddress,
				1703001234567,
				&benchExpiresAfter,
			)
		}
	})
}

// BenchmarkECDSAL1Signer measures signing through the signer interface used
// by Exchange, including the context plumbing.
func BenchmarkECDSAL1Signer(b *testing.B) {
	privateKey, err := crypto.ToECDSA(benchPrivateKey)
	if err != nil {
		b.Fatal(err)
	}
	signer := ECDSAL1Signer(privateKey)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := signer.SignL1Action(
			ctx,
			benchOrderAction,
			"",
			1703001234567,
			nil,
			false,
		); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSignUserSignedAction measures the direct EIP-712 path used for
// user-signed actions (transfers, withdrawals, etc.).
func BenchmarkSignUserSignedAction(b *testing.B) {
	privateKey, err := crypto.ToECDSA(benchPrivateKey)
	if err != nil {
		b.Fatal(err)
	}

	newAction := func() map[string]any {
		return map[string]any{
			"hyperliquidChain": "Testnet",
			"destination":      "0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0",
			"amount":           "100.5",
			"time":             int64(1703001234567),
		}
	}
	payloadTypes := []apitypes.Type{
		{Name: "hyperliquidChain", Type: "string"},
		{Name: "destination", Type: "string"},
		{Name: "amount", Type: "string"},
		{Name: "time", Type: "uint64"},
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := SignUserSignedAction(
			privateKey,
			newAction(),
			payloadTypes,
			"HyperliquidTransaction:UsdSend",
			false,
		); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSignL1Action measures the full L1 signing pipeline:
// actionHash -> phantom agent -> EIP-712 typed data -> ECDSA sign.
func BenchmarkSignL1Action(b *testing.B) {
	privateKey, err := crypto.ToECDSA(benchPrivateKey)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("NoVault", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := SignL1Action(
				privateKey,
				benchOrderAction,
				"",
				1703001234567,
				nil,
				false,
			); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Vault", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := SignL1Action(
				privateKey,
				benchOrderAction,
				benchVaultAddress,
				1703001234567,
				nil,
				true,
			); err != nil {
				b.Fatal(err)
			}
		}
	})
}
