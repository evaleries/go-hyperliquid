package hyperliquid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// testPrivateKeyHex is the Hardhat/Anvil well-known account #0. It is a
// public test vector, not a secret; it must never control real funds.
const testPrivateKeyHex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// newOfflineExchange builds an Exchange whose info is seeded from the given
// meta, so coin resolution works without any network access.
func newOfflineExchange(t *testing.T, baseURL string, universe []AssetInfo) *Exchange {
	t.Helper()
	privateKey, err := crypto.HexToECDSA(testPrivateKeyHex)
	require.NoError(t, err)
	return NewExchange(
		context.Background(),
		privateKey,
		baseURL,
		&Meta{Universe: universe},
		"",
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		&SpotMeta{},
		nil,
	)
}

func f64ptr(v float64) *float64 { return &v }

func TestNewTrailingStopAction(t *testing.T) {
	universe := []AssetInfo{{Name: "BTC"}, {Name: "ETH"}, {Name: "SOL"}}
	exchange := newOfflineExchange(t, MainnetAPIURL, universe)

	t.Run("percent with activation", func(t *testing.T) {
		action, err := newTrailingStopAction(exchange, TrailingStopOrderRequest{
			Coin:         "SOL",
			IsBuy:        false,
			Size:         4.12,
			ReduceOnly:   true,
			Retracement:  TrailingStopRetracement{Percent: f64ptr(1.5)},
			ActivationPx: f64ptr(250.5),
		})
		require.NoError(t, err)
		got, err := json.Marshal(action)
		require.NoError(t, err)
		// Exact bytes pin the frontend's key order: the API recomputes the
		// action hash from the posted JSON.
		require.JSONEq(t, `{
			"type": "trailingStop", "asset": 2, "isBuy": false, "sz": "4.12",
			"reduceOnly": true, "retracement": {"pct": "1.5000%"}, "activationPx": "250.5"
		}`, string(got))
		require.Equal(t,
			`{"type":"trailingStop","asset":2,"isBuy":false,"sz":"4.12","reduceOnly":true,"retracement":{"pct":"1.5000%"},"activationPx":"250.5"}`,
			string(got))
	})

	t.Run("price distance without activation", func(t *testing.T) {
		action, err := newTrailingStopAction(exchange, TrailingStopOrderRequest{
			Coin:        "BTC",
			IsBuy:       true,
			Size:        0.1,
			Retracement: TrailingStopRetracement{PriceDistance: f64ptr(10)},
		})
		require.NoError(t, err)
		got, err := json.Marshal(action)
		require.NoError(t, err)
		require.Equal(t,
			`{"type":"trailingStop","asset":0,"isBuy":true,"sz":"0.1","reduceOnly":false,"retracement":{"px":"10"},"activationPx":null}`,
			string(got))
	})

	t.Run("both retracement fields set", func(t *testing.T) {
		_, err := newTrailingStopAction(exchange, TrailingStopOrderRequest{
			Coin: "BTC", Size: 1,
			Retracement: TrailingStopRetracement{Percent: f64ptr(1), PriceDistance: f64ptr(10)},
		})
		require.ErrorContains(t, err, "only one of Percent or PriceDistance")
	})

	t.Run("no retracement field set", func(t *testing.T) {
		_, err := newTrailingStopAction(exchange, TrailingStopOrderRequest{Coin: "BTC", Size: 1})
		require.ErrorContains(t, err, "one of Percent or PriceDistance must be set")
	})

	t.Run("unknown coin", func(t *testing.T) {
		_, err := newTrailingStopAction(exchange, TrailingStopOrderRequest{
			Coin: "DOGE", Size: 1,
			Retracement: TrailingStopRetracement{Percent: f64ptr(1)},
		})
		require.ErrorContains(t, err, "coin DOGE not found in info")
	})

	t.Run("size needing more than 8 decimals", func(t *testing.T) {
		_, err := newTrailingStopAction(exchange, TrailingStopOrderRequest{
			Coin: "BTC", Size: 4.123456789,
			Retracement: TrailingStopRetracement{Percent: f64ptr(1)},
		})
		require.ErrorContains(t, err, "failed to wire size")
	})
}

// TestTrailingStopSignatureMatchesPythonSDK signs trailingStop actions and
// compares against vectors produced by hyperliquid-python-sdk 0.24.0
// sign_l1_action with the same test key. A match proves the msgpack action
// serialization — field order, null activationPx, nested retracement map,
// vault and mainnet/testnet paths — is byte-identical to the Python
// reference implementation.
func TestTrailingStopSignatureMatchesPythonSDK(t *testing.T) {
	privateKey, err := crypto.HexToECDSA(testPrivateKeyHex)
	require.NoError(t, err)

	// 6-asset universe so vector 1's asset index 5 resolves.
	universe := []AssetInfo{
		{Name: "BTC"}, {Name: "ETH"}, {Name: "SOL"},
		{Name: "DOGE"}, {Name: "XRP"}, {Name: "LINK"},
	}
	exchange := newOfflineExchange(t, MainnetAPIURL, universe)

	vectors := []struct {
		name    string
		req     TrailingStopOrderRequest
		vault   string
		nonce   int64
		mainnet bool
		r, s    string
		v       int
	}{
		{
			name: "percent with activation, mainnet",
			req: TrailingStopOrderRequest{
				Coin: "LINK", IsBuy: false, Size: 4.12, ReduceOnly: true,
				Retracement:  TrailingStopRetracement{Percent: f64ptr(1.5)},
				ActivationPx: f64ptr(250.5),
			},
			nonce: 1757000000000, mainnet: true,
			r: "0x15c385af4d595b07dcb0c204a3da263a6ef5cb263fd09f3ecc36caece61555b3",
			s: "0x60ac69422a0372a8dd946b4b91e2376a7d8300cd5c285cc473aed25f6967e02",
			v: 27,
		},
		{
			name: "price distance, no activation, testnet",
			req: TrailingStopOrderRequest{
				Coin: "BTC", IsBuy: true, Size: 0.1,
				Retracement: TrailingStopRetracement{PriceDistance: f64ptr(10)},
			},
			nonce: 1757000001234, mainnet: false,
			r: "0x1f27366df952e6cda9c662015623bb0349a43280ddd607769df0d25d14d0043c",
			s: "0x79afcdd511a61641c406768f440126adbfbe77c5308058951664fb1dfd44b63d",
			v: 28,
		},
		{
			name: "vault, mainnet",
			req: TrailingStopOrderRequest{
				Coin: "SOL", IsBuy: false, Size: 100, ReduceOnly: true,
				Retracement: TrailingStopRetracement{Percent: f64ptr(0.25)},
			},
			vault: "0x00000000000000000000000000000000deadbeef",
			nonce: 1757000009999, mainnet: true,
			r: "0x64e04e37257f7970e1375e6489bde0f7e06790f21c631e89086f16c25f51b9ff",
			s: "0x5471d1519c0541731003fa52e10c521dc4a4d50bec3d1a537af6f7fc494974ac",
			v: 27,
		},
	}
	for _, tv := range vectors {
		t.Run(tv.name, func(t *testing.T) {
			action, err := newTrailingStopAction(exchange, tv.req)
			require.NoError(t, err)
			sig, err := SignL1Action(privateKey, action, tv.vault, tv.nonce, nil, tv.mainnet)
			require.NoError(t, err)
			require.Equal(t, tv.r, sig.R, "R mismatch")
			require.Equal(t, tv.s, sig.S, "S mismatch")
			require.Equal(t, tv.v, sig.V, "V mismatch")
		})
	}
}

// TestPlaceTrailingStop drives the full method against a fake /exchange
// endpoint: signed POST shape on the wire and typed response parsing.
func TestPlaceTrailingStop(t *testing.T) {
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		rawBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","response":{"type":"trailingStop","data":{"statuses":[{"resting":{"oid":777}}]}}}`))
	}))
	defer srv.Close()

	exchange := newOfflineExchange(t, srv.URL, []AssetInfo{{Name: "BTC"}, {Name: "ETH"}, {Name: "SOL"}})
	resp, err := exchange.PlaceTrailingStop(context.Background(), TrailingStopOrderRequest{
		Coin:         "SOL",
		IsBuy:        false,
		Size:         4.12,
		ReduceOnly:   true,
		Retracement:  TrailingStopRetracement{Percent: f64ptr(1.5)},
		ActivationPx: f64ptr(250.5),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.Ok)
	require.Len(t, resp.Data.Statuses, 1)
	require.NotNil(t, resp.Data.Statuses[0].Resting)
	require.Equal(t, int64(777), resp.Data.Statuses[0].Resting.Oid)

	// The signed payload carries the action in the frontend's key order.
	require.Contains(t, rawBody, `"action":{"type":"trailingStop","asset":2,"isBuy":false,"sz":"4.12","reduceOnly":true,"retracement":{"pct":"1.5000%"},"activationPx":"250.5"}`)
	// And the standard signed-action envelope fields.
	for _, key := range []string{`"nonce":`, `"signature":`} {
		require.True(t, strings.Contains(rawBody, key), "payload missing %s: %s", key, rawBody)
	}
}
