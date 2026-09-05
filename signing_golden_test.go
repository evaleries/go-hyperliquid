package hyperliquid

import (
	"encoding/hex"
	"strings"
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

	// Actions carrying sized integer fields (int64, *int64, any-boxed int64).
	// Their msgpack encoding depends on the encoder's UseCompactInts flag,
	// which plain `int` fields (goldenAction above) cannot detect: reflect.Int
	// is always compact-encoded, sized kinds only when the flag is armed.
	goldenScheduleTime = int64(1703001234567)

	goldenCancelAction = CancelAction{
		Type:    "cancel",
		Cancels: []CancelOrderWire{{Asset: 5, OrderID: 123456789}},
	}
	goldenScheduleCancelAction = ScheduleCancelAction{
		Type: "scheduleCancel",
		Time: &goldenScheduleTime,
	}
	goldenModifyAction = ModifyAction{
		Type: "modify",
		Oid:  int64(987654321),
		Order: OrderWire{
			Asset: 0, IsBuy: true, LimitPx: "100.5", Size: "1.5", ReduceOnly: false,
			OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifGtc}},
		},
	}
)

func mustActionHash(
	t *testing.T,
	action any,
	vault string,
	nonce int64,
	expiresAfter *int64,
) string {
	t.Helper()
	h, err := actionHash(action, vault, nonce, expiresAfter)
	require.NoError(t, err)
	return hex.EncodeToString(h[:])
}

func TestActionHashGolden(t *testing.T) {
	require.Equal(
		t,
		"9abd2d8da923b619dd9f00c693b490be58e3a96c4c50021231d0f1295b2fcb8a",
		mustActionHash(t, goldenAction, "", 1703001234567, nil),
	)
	require.Equal(
		t,
		"1c238848aa819bae6f0312f461919a4b098e8aa0f7a3fac8c76daa58a813595e",
		mustActionHash(t, goldenAction, goldenVault, 1703001234567, &goldenExpires),
	)
}

// TestActionHashGoldenSizedInts pins actions whose msgpack bytes change if the
// encoder loses UseCompactInts (e.g. a pooled encoder that is Reset without
// re-arming its flags). Values captured from the pre-optimization
// implementation at 5af1f02.
func TestActionHashGoldenSizedInts(t *testing.T) {
	require.Equal(
		t,
		"908568f49c7bbd13da73d0d19cc062defd068bd7290b00dda4e6d93632410388",
		mustActionHash(t, goldenCancelAction, "", 1703001234567, nil),
		"cancel action hash must stay byte-identical (int64 OrderID)",
	)
	require.Equal(
		t,
		"1b0dcead90c33787f51005c9daf29cd6d886f762346f3ee99196842789e88c13",
		mustActionHash(t, goldenCancelAction, goldenVault, 1703001234567, &goldenExpires),
	)
	require.Equal(
		t,
		"886d339d10b972f7586d992d5d8bc38c929b2dff52d256e4729a5bc60d14e742",
		mustActionHash(t, goldenScheduleCancelAction, "", 1703001234567, nil),
		"scheduleCancel action hash must stay byte-identical (*int64 Time)",
	)
	require.Equal(
		t,
		"c8ddbd0d91f3d6bc047c356f2a5cf448604f812376be0883a32e6157cad88a97",
		mustActionHash(t, goldenModifyAction, "", 1703001234567, nil),
		"modify action hash must stay byte-identical (any-boxed int64 Oid)",
	)
}

// TestSignL1ActionGoldenCancel pins the end-to-end signature of a cancel,
// the most common sized-int action. Its S component also happens to have a
// leading zero nibble, exercising hexEncodeBigEndian's minimal-hex output.
func TestSignL1ActionGoldenCancel(t *testing.T) {
	pk, err := crypto.ToECDSA(goldenKeyBytes)
	require.NoError(t, err)

	sig, err := SignL1Action(pk, goldenCancelAction, "", 1703001234567, nil, false)
	require.NoError(t, err)
	require.Equal(
		t,
		"0x1ed87d58a961b0a3f40c1088b40b90848828451fb893d63e9222fff3c4c44dd2",
		sig.R,
	)
	require.Equal(
		t,
		"0x1184e4fadb4d178041f5c8f787fa5b736ee06e3d602b168d3c2f7cecc22042e",
		sig.S,
	)
	require.Equal(t, 27, sig.V)
}

// A malformed vault address must surface as an error, never a panic or a
// silently zero-padded address inside a signed payload.
func TestSignL1ActionRejectsMalformedVaultAddress(t *testing.T) {
	pk, err := crypto.ToECDSA(goldenKeyBytes)
	require.NoError(t, err)

	for _, vault := range []string{
		"0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0ff", // too long
		"0x1719889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2",     // too short
		"0xZZ19889b0F7C6F2EB0B1B0e8B2B62e05A4f3d2A0",   // not hex
	} {
		_, err := SignL1Action(pk, goldenCancelAction, vault, 1703001234567, nil, false)
		require.Error(t, err, "vault %q must be rejected", vault)
	}

	// The 0x prefix stays optional.
	withPrefix, err := SignL1Action(pk, goldenCancelAction, goldenVault, 1, nil, false)
	require.NoError(t, err)
	withoutPrefix, err := SignL1Action(
		pk, goldenCancelAction, strings.TrimPrefix(goldenVault, "0x"), 1, nil, false,
	)
	require.NoError(t, err)
	require.Equal(t, withPrefix, withoutPrefix)
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
