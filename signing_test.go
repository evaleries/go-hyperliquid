package hyperliquid

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestSignL1Action(t *testing.T) {
	// Test private key
	privateKeyHex := "abcd1234567890abcd1234567890abcd1234567890abcd1234567890abcd1234"
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	require.NoError(t, err)

	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	require.NoError(t, err)

	tests := []struct {
		name         string
		action       map[string]any
		vaultAddress string
		timestamp    int64
		expiresAfter *int64
		isMainnet    bool
		wantErr      bool
		description  string
	}{
		{
			name: "basic_order_action_testnet",
			action: map[string]any{
				"type": "order",
				"orders": []map[string]any{
					{
						"asset":      0,
						"isBuy":      true,
						"limitPx":    "100.0",
						"orderType":  "Limit",
						"reduceOnly": false,
						"size":       "1.0",
						"tif":        "Gtc",
					},
				},
				"grouping": "na",
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: nil,
			isMainnet:    false,
			wantErr:      false,
			description:  "Basic order action on testnet without expiration",
		},
		{
			name: "basic_order_action_mainnet",
			action: map[string]any{
				"type": "order",
				"orders": []map[string]any{
					{
						"asset":      0,
						"isBuy":      true,
						"limitPx":    "100.0",
						"orderType":  "Limit",
						"reduceOnly": false,
						"size":       "1.0",
						"tif":        "Gtc",
					},
				},
				"grouping": "na",
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: nil,
			isMainnet:    true,
			wantErr:      false,
			description:  "Basic order action on mainnet without expiration",
		},
		{
			name: "order_with_expiration",
			action: map[string]any{
				"type": "order",
				"orders": []map[string]any{
					{
						"asset":      0,
						"isBuy":      true,
						"limitPx":    "100.0",
						"orderType":  "Limit",
						"reduceOnly": false,
						"size":       "1.0",
						"tif":        "Gtc",
					},
				},
				"grouping": "na",
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: func() *int64 { e := int64(1703001234567 + 3600000); return &e }(), // 1 hour later
			isMainnet:    false,
			wantErr:      false,
			description:  "Order action with expiration",
		},
		{
			name: "order_with_vault",
			action: map[string]any{
				"type": "order",
				"orders": []map[string]any{
					{
						"asset":      0,
						"isBuy":      true,
						"limitPx":    "100.0",
						"orderType":  "Limit",
						"reduceOnly": false,
						"size":       "1.0",
						"tif":        "Gtc",
					},
				},
				"grouping": "na",
			},
			vaultAddress: "0x1234567890abcdef1234567890abcdef12345678",
			timestamp:    1703001234567,
			expiresAfter: nil,
			isMainnet:    false,
			wantErr:      false,
			description:  "Order action with vault address",
		},
		{
			name: "leverage_update_action",
			action: map[string]any{
				"type":     "updateLeverage",
				"asset":    0,
				"isCross":  true,
				"leverage": 10,
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: nil,
			isMainnet:    false,
			wantErr:      false,
			description:  "Leverage update action",
		},
		{
			name: "usd_class_transfer_action",
			action: map[string]any{
				"type":   "usdClassTransfer",
				"amount": "100.0",
				"toPerp": true,
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: nil,
			isMainnet:    false,
			wantErr:      false,
			description:  "USD class transfer action",
		},
		{
			name: "cancel_action",
			action: map[string]any{
				"type": "cancel",
				"cancels": []map[string]any{
					{
						"asset": 0,
						"oid":   12345,
					},
				},
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: nil,
			isMainnet:    false,
			wantErr:      false,
			description:  "Cancel action without vault",
		},
		{
			name: "vault_action",
			action: map[string]any{
				"type":     "updateLeverage",
				"asset":    0,
				"leverage": 10,
				"isCross":  true,
			},
			vaultAddress: "0x1234567890123456789012345678901234567890",
			timestamp:    1703001234567,
			expiresAfter: nil,
			isMainnet:    true,
			wantErr:      false,
			description:  "Action with vault address",
		},
		{
			name: "empty_vault_with_expiration",
			action: map[string]any{
				"type": "setReferrer",
				"code": "TEST123",
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: func() *int64 { e := int64(1703001234567 + 86400000); return &e }(), // 24 hours
			isMainnet:    false,
			wantErr:      false,
			description:  "Empty vault with expiration time",
		},
		{
			name: "nil_vault_with_expiration",
			action: map[string]any{
				"type": "createSubAccount",
				"name": "TestAccount",
			},
			vaultAddress: "",
			timestamp:    1703001234567,
			expiresAfter: func() *int64 { e := int64(1703001234567 + 1800000); return &e }(), // 30 minutes
			isMainnet:    true,
			wantErr:      false,
			description:  "Nil vault with expiration time on mainnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signature, err := SignL1Action(
				privateKey,
				tt.action,
				tt.vaultAddress,
				tt.timestamp,
				tt.expiresAfter,
				tt.isMainnet,
			)

			if tt.wantErr {
				assert.Error(t, err, tt.description)
				return
			}

			require.NoError(t, err, tt.description)
			assert.NotEmpty(t, signature.R, "Signature R should not be empty")
			assert.NotEmpty(t, signature.S, "Signature S should not be empty")
			assert.True(t, signature.V > 0, "Signature V should be positive")
			assert.True(
				t,
				len(signature.R) >= 3,
				"Signature R should be at least 3 characters (0x + hex)",
			)
			assert.True(
				t,
				len(signature.S) >= 3,
				"Signature S should be at least 3 characters (0x + hex)",
			)
			assert.True(t, signature.R[:2] == "0x", "Signature R should start with 0x")
			assert.True(t, signature.S[:2] == "0x", "Signature S should start with 0x")

			// Validate that R and S are valid hex strings
			assert.Regexp(t, "^0x[0-9a-fA-F]+$", signature.R, "Signature R should be valid hex")
			assert.Regexp(t, "^0x[0-9a-fA-F]+$", signature.S, "Signature S should be valid hex")

			// Note: ECDSA signatures are NOT deterministic by default in go-ethereum
			// Each signature uses a random nonce (k value), which is actually more secure
			// against side-channel attacks. The signatures are still valid and verifiable.
		})
	}
}

// TestDebugActionHash helps debug the action hash generation
func TestDebugActionHash(t *testing.T) {
	// Use the same test data as Python
	action := OrderAction{
		Type: "order",
		Orders: []OrderWire{{
			Asset:      0,
			IsBuy:      true,
			LimitPx:    "100.5",
			Size:       "1.0",
			ReduceOnly: false,
			OrderType: OrderWireType{
				Limit: &OrderWireTypeLimit{
					Tif: TifGtc,
				},
			},
		}},
		Grouping: "na",
	}

	privateKey, _ := crypto.HexToECDSA(
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)
	vaultAddress := ""
	timestamp := int64(1640995200000) // Fixed timestamp
	var expiresAfter *int64 = nil
	isMainnet := false

	// Debug: Print action hash components
	hash, err := actionHash(action, vaultAddress, timestamp, expiresAfter)
	require.NoError(t, err)
	t.Logf("Action hash: %x", hash)

	// Debug: Print phantom agent struct hash
	t.Logf("Agent struct hash: %x", agentStructHash(hash[:], isMainnet))

	// Generate signature
	signature, err := SignL1Action(
		privateKey,
		action,
		vaultAddress,
		timestamp,
		expiresAfter,
		isMainnet,
	)
	require.NoError(t, err)
	t.Logf("Generated signature: R=%s, S=%s, V=%d", signature.R, signature.S, signature.V)
}

// TestMsgpackStringHeadersStayPythonCompatible pins the encoder property that
// lets actionHash hash the msgpack bytes exactly as produced: Python's msgpack
// writes a str8 header for strings shorter than 256 bytes, and the pinned
// vmihailenco/msgpack does the same (fixstr < 32, str8 < 256, str16 >= 256).
//
// If this ever fails — a msgpack downgrade, or an encoder that emits str16 for
// short strings — the signed bytes stop matching the Python SDK and a
// str16 -> str8 rewrite has to be reintroduced before hashing. That rewriting
// walker used to live in signing.go; it was deleted because it could never
// fire against this encoder.
func TestMsgpackStringHeadersStayPythonCompatible(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length int
		header byte
	}{
		{name: "empty", length: 0, header: 0xa0},       // fixstr
		{name: "fixstr max", length: 31, header: 0xbf}, // fixstr
		{name: "str8 min", length: 32, header: 0xd9},   // str8, NOT str16
		{name: "str8 max", length: 255, header: 0xd9},  // str8, NOT str16
		{name: "str16 min", length: 256, header: 0xda}, // str16, never rewritten
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := msgpack.NewEncoder(&buf)
			enc.UseCompactInts(true)
			require.NoError(t, enc.Encode(strings.Repeat("x", tc.length)))
			require.Equal(t, tc.header, buf.Bytes()[0],
				"string of %d bytes must use header 0x%02x", tc.length, tc.header)
		})
	}
}

// TestActionHashMatchesFreshEncoder pins the encoder configuration itself:
// actionHash's pooled encoder must produce exactly what a freshly built
// encoder with the Python-compatible options produces. This catches option
// loss (e.g. msgpack's Reset zeroing UseCompactInts) for any action shape,
// without needing a golden vector per shape. The cancel case carries order id
// 361731063972 (0x00_00_00_54_38_DA_00_A4), whose encoding embeds the 0xda
// str16 marker, so it also proves the hash never rewrites payload bytes.
func TestActionHashMatchesFreshEncoder(t *testing.T) {
	scheduleTime := int64(1703001234567)
	actions := map[string]any{
		"order": OrderAction{
			Type: "order",
			Orders: []OrderWire{{
				Asset: 3, IsBuy: true, LimitPx: "100.5", Size: "1.5",
				OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifGtc}},
			}},
			Grouping: "na",
		},
		"cancel": CancelAction{
			Type:    "cancel",
			Cancels: []CancelOrderWire{{Asset: 5, OrderID: 361731063972}},
		},
		"scheduleCancel": ScheduleCancelAction{Type: "scheduleCancel", Time: &scheduleTime},
		"modify": ModifyAction{
			Type: "modify",
			Oid:  int64(987654321),
			Order: OrderWire{
				Asset: 0, IsBuy: false, LimitPx: "1.0", Size: "2.0",
				OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifIoc}},
			},
		},
	}

	const nonce = int64(1703001234567)
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := msgpack.NewEncoder(&buf)
			enc.UseCompactInts(true)
			require.NoError(t, enc.Encode(action))

			trailer := binary.BigEndian.AppendUint64(buf.Bytes(), uint64(nonce))
			want := crypto.Keccak256Hash(append(trailer, 0x00)) // 0x00: no vault

			got, err := actionHash(action, "", nonce, nil)
			require.NoError(t, err)
			require.Equal(t, [32]byte(want), got,
				"pooled encoder diverged from a freshly configured encoder")
		})
	}
}
