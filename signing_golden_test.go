package hyperliquid

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/stretchr/testify/require"
)

// Golden (known-answer) tests pinning the exact signing outputs.
// Captured from the pre-optimization implementation; any refactoring of the
// signing pipeline (actionHash, phantom agent, EIP-712 encoding) MUST keep
// these values byte-identical.
var (
	goldenKeyBytes, _ = hex.DecodeString(
		"abcd1234567890abcd1234567890abcd1234567890abcd1234567890abcd1234",
	)
	goldenAction = OrderAction{
		Type: "order",
		Orders: []OrderWire{
			{
				Asset: 0, IsBuy: true, LimitPx: "100.5", Size: "1.5", ReduceOnly: false,
				OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifGtc}},
			},
		},
		Grouping: "na",
	}
	goldenVault   = "0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0"
	goldenExpires = int64(1703001294567)
)

func TestActionHashGolden(t *testing.T) {
	plain := actionHash(goldenAction, "", 1703001234567, nil)
	require.Equal(
		t,
		"9abd2d8da923b619dd9f00c693b490be58e3a96c4c50021231d0f1295b2fcb8a",
		hex.EncodeToString(plain[:]),
	)
	vault := actionHash(goldenAction, goldenVault, 1703001234567, &goldenExpires)
	require.Equal(
		t,
		"1c238848aa819bae6f0312f461919a4b098e8aa0f7a3fac8c76daa58a813595e",
		hex.EncodeToString(vault[:]),
	)
}

func TestSignL1ActionGolden(t *testing.T) {
	pk, err := crypto.ToECDSA(goldenKeyBytes)
	require.NoError(t, err)

	sig, err := SignL1Action(pk, goldenAction, "", 1703001234567, nil, false)
	require.NoError(t, err)
	require.Equal(
		t,
		"0x599d9dd5017ba925b1c169c9e70c38ed730b9853bda6a6639ebaa37a58dbf947",
		sig.R,
	)
	require.Equal(
		t,
		"0x277e37a7d105fe45f2be1efce9b80f7c6c84abbd63aba72de42553c8e9299166",
		sig.S,
	)
	require.Equal(t, 27, sig.V)

	sig, err = SignL1Action(pk, goldenAction, goldenVault, 1703001234567, nil, true)
	require.NoError(t, err)
	require.Equal(
		t,
		"0x60a13ba74efc4df079e74f0d4117531af77e20e459c3c17d922742f858cbdba0",
		sig.R,
	)
	require.Equal(
		t,
		"0x2bf1f1bf950b62e8048aa3b40c8a061f9f5bf232cc581fb4457b8a56d824dc5f",
		sig.S,
	)
	require.Equal(t, 28, sig.V)
}

func TestSignUserSignedActionGolden(t *testing.T) {
	pk, err := crypto.ToECDSA(goldenKeyBytes)
	require.NoError(t, err)

	action := map[string]any{
		"hyperliquidChain": "Testnet",
		"destination":      "0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0",
		"amount":           "100.5",
		"time":             int64(1703001234567),
	}
	payloadTypes := []apitypes.Type{
		{Name: "hyperliquidChain", Type: "string"},
		{Name: "destination", Type: "string"},
		{Name: "amount", Type: "string"},
		{Name: "time", Type: "uint64"},
	}

	sig, err := SignUserSignedAction(
		pk,
		action,
		payloadTypes,
		"HyperliquidTransaction:UsdSend",
		false,
	)
	require.NoError(t, err)
	require.Equal(
		t,
		"0xf79b12fead206d0f906fd7f36ae75b8e334214e9d697326fa94d5dd06eee286f",
		sig.R,
	)
	require.Equal(
		t,
		"0x2a03f504c0c405198fc54a538278e07544de46687919860907efabd52f6b50a0",
		sig.S,
	)
	require.Equal(t, 28, sig.V)
}
