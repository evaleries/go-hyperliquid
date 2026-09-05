package hyperliquid

// Wire-compatibility guards for the JSON codec (sonic, ConfigStd).
// The expected bytes below were captured from the previous easyjson-generated
// implementation; the codec MUST keep producing byte-identical payloads for
// the exchange API to accept them.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// wireOrderAction is the reference action for wire-format guards.
var wireOrderAction = OrderAction{
	Type: "order",
	Orders: []OrderWire{
		{
			Asset: 0, IsBuy: true, LimitPx: "100.5", Size: "1.5", ReduceOnly: false,
			OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifGtc}},
		},
	},
	Grouping: "na",
}

func TestJSONWireOrderActionMarshal(t *testing.T) {
	want := `{"type":"order","orders":[{"a":0,"b":true,"p":"100.5","s":"1.5","r":false,"t":{"limit":{"tif":"Gtc"}}}],"grouping":"na"}`

	got, err := jsonCodec.Marshal(wireOrderAction)
	require.NoError(t, err)
	require.Equal(t, want, string(got))
}

func TestJSONWireLevelStringFloats(t *testing.T) {
	// Level prices/sizes are `,string`-encoded floats on the wire.
	got, err := jsonCodec.Marshal(Level{N: 3, Px: 65000.5, Sz: 1.25})
	require.NoError(t, err)
	require.Equal(t, `{"n":3,"px":"65000.5","sz":"1.25"}`, string(got))

	var lvl Level
	require.NoError(t, jsonCodec.Unmarshal([]byte(`{"n":3,"px":"65000.5","sz":"1.25"}`), &lvl))
	require.Equal(t, Level{N: 3, Px: 65000.5, Sz: 1.25}, lvl)
}

func TestJSONWireL2BookUnmarshal(t *testing.T) {
	data := []byte(
		`{"coin":"BTC","levels":[[{"px":"65000.0","sz":"1.5","n":1}],[{"px":"64999.5","sz":"2.0","n":2}]],"time":1703001234567}`,
	)
	var book L2Book
	require.NoError(t, jsonCodec.Unmarshal(data, &book))
	require.Equal(t, "BTC", book.Coin)
	require.Len(t, book.Levels, 2)
	require.Equal(t, 65000.0, book.Levels[0][0].Px)
	require.Equal(t, 1.5, book.Levels[0][0].Sz)
	require.Equal(t, int64(1703001234567), book.Time)
}

func TestJSONWireOrderActionRoundTrip(t *testing.T) {
	var decoded OrderAction
	require.NoError(t, jsonCodec.Unmarshal([]byte(
		`{"type":"order","orders":[{"a":0,"b":true,"p":"100.5","s":"1.5","r":false,"t":{"limit":{"tif":"Gtc"}}}],"grouping":"na"}`,
	), &decoded))
	require.Equal(t, "order", decoded.Type)
	require.Len(t, decoded.Orders, 1)
	require.Equal(t, 0, decoded.Orders[0].Asset)
	require.True(t, decoded.Orders[0].IsBuy)
	require.Equal(t, "100.5", decoded.Orders[0].LimitPx)
	require.Equal(t, TifGtc, decoded.Orders[0].OrderType.Limit.Tif)
	require.Equal(t, "na", decoded.Grouping)
}

// TestJSONWireOrderResponseStatuses pins decoding of the /exchange "statuses"
// data field into OrderResponse. Payloads are copied verbatim from the
// recorded cassettes: Orders_Above10.yaml (resting, oid only),
// CancelByCloid_Success.yaml (resting with cloid) and Orders_Below10.yaml
// (error). The "filled" shape is not present in any cassette; its keys come
// from the OrderStatusFilled struct tags (totalSz/avgPx/oid), matching the
// documented API response shape.
func TestJSONWireOrderResponseStatuses(t *testing.T) {
	tests := []struct {
		name string
		data string
		want OrderStatus
	}{
		{
			// testdata/Orders_Above10.yaml
			name: "resting with oid",
			data: `{"statuses":[{"resting":{"oid":41545810396}}]}`,
			want: OrderStatus{Resting: &OrderStatusResting{Oid: 41545810396}},
		},
		{
			// testdata/CancelByCloid_Success.yaml
			name: "resting with oid and cloid",
			data: `{"statuses":[{"resting":{"oid":41569108691,"cloid":"0x285ad26a251f390c83d065af51e3f8d9"}}]}`,
			want: OrderStatus{Resting: &OrderStatusResting{
				Oid:      41569108691,
				ClientID: stringPtr("0x285ad26a251f390c83d065af51e3f8d9"),
			}},
		},
		{
			// keys from OrderStatusFilled struct tags (no cassette records a fill)
			name: "filled with avgPx and totalSz",
			data: `{"statuses":[{"filled":{"totalSz":"1.5","avgPx":"65000.5","oid":41545810397}}]}`,
			want: OrderStatus{Filled: &OrderStatusFilled{
				TotalSz: "1.5",
				AvgPx:   "65000.5",
				Oid:     41545810397,
			}},
		},
		{
			// testdata/Orders_Below10.yaml
			name: "error status",
			data: `{"statuses":[{"error":"Order must have minimum value of $10. asset=173"}]}`,
			want: OrderStatus{Error: stringPtr("Order must have minimum value of $10. asset=173")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var res OrderResponse
			require.NoError(t, jsonCodec.Unmarshal([]byte(tt.data), &res))
			require.Len(t, res.Statuses, 1)
			require.Equal(t, tt.want, res.Statuses[0])
		})
	}
}

// TestJSONWireCancelOrderResponseStatuses pins decoding of the cancel
// endpoint's "statuses" data field into CancelOrderResponse (a MixedArray,
// since cancel statuses are heterogeneous: bare "success" strings or error
// objects). Payloads are copied verbatim from the recorded cassettes
// Cancel_Success.yaml and Cancel_NonExistent.yaml.
func TestJSONWireCancelOrderResponseStatuses(t *testing.T) {
	t.Run("success string", func(t *testing.T) {
		// testdata/Cancel_Success.yaml
		var res CancelOrderResponse
		require.NoError(t, jsonCodec.Unmarshal(
			[]byte(`{"statuses":["success"]}`), &res,
		))
		require.Len(t, res.Statuses, 1)
		s, ok := res.Statuses[0].String()
		require.True(t, ok)
		require.Equal(t, "success", s)
		require.NoError(t, res.Statuses.FirstError())
	})

	t.Run("error object", func(t *testing.T) {
		// testdata/Cancel_NonExistent.yaml
		const errMsg = "Order was never placed, already canceled, or filled. asset=173"
		var res CancelOrderResponse
		require.NoError(t, jsonCodec.Unmarshal(
			[]byte(`{"statuses":[{"error":"`+errMsg+`"}]}`), &res,
		))
		require.Len(t, res.Statuses, 1)
		require.EqualError(t, res.Statuses.FirstError(), errMsg)
	})
}

// TestJSONWireOrderWireCloidMarshal pins the `c,omitempty` inclusion path of
// OrderWire: present when Cloid is set, absent otherwise. Expected bytes are
// derived from the OrderWire struct tags (a,b,p,s,r,t,c in declaration order,
// matching the Python SDK insertion order); the cloid value itself comes from
// testdata/Orders_Cloid.yaml.
func TestJSONWireOrderWireCloidMarshal(t *testing.T) {
	base := OrderWire{
		Asset: 0, IsBuy: true, LimitPx: "100.5", Size: "1.5", ReduceOnly: false,
		OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifGtc}},
	}

	t.Run("cloid set", func(t *testing.T) {
		wire := base
		wire.Cloid = stringPtr("0x06c60000000000000000000000003f5a")
		got, err := jsonCodec.Marshal(wire)
		require.NoError(t, err)
		require.Equal(t,
			`{"a":0,"b":true,"p":"100.5","s":"1.5","r":false,"t":{"limit":{"tif":"Gtc"}},"c":"0x06c60000000000000000000000003f5a"}`,
			string(got),
		)
	})

	t.Run("cloid unset", func(t *testing.T) {
		got, err := jsonCodec.Marshal(base)
		require.NoError(t, err)
		require.Equal(t,
			`{"a":0,"b":true,"p":"100.5","s":"1.5","r":false,"t":{"limit":{"tif":"Gtc"}}}`,
			string(got),
		)
	})
}

// TestJSONWireTriggerOrderActionMarshal pins the trigger variant of
// OrderWireType (only the limit variant was pinned before). Expected bytes
// are derived from the struct tags: OrderWireTypeTrigger declares
// isMarket/triggerPx/tpsl in that order (matching the Python SDK insertion
// order documented on the struct), nested under "trigger".
func TestJSONWireTriggerOrderActionMarshal(t *testing.T) {
	action := OrderAction{
		Type: "order",
		Orders: []OrderWire{
			{
				Asset: 1, IsBuy: false, LimitPx: "2000.5", Size: "0.1", ReduceOnly: true,
				OrderType: OrderWireType{Trigger: &OrderWireTypeTrigger{
					IsMarket:  true,
					TriggerPx: "1999.9",
					Tpsl:      StopLoss,
				}},
			},
		},
		Grouping: "na",
	}

	want := `{"type":"order","orders":[{"a":1,"b":false,"p":"2000.5","s":"0.1","r":true,` +
		`"t":{"trigger":{"isMarket":true,"triggerPx":"1999.9","tpsl":"sl"}}}],"grouping":"na"}`

	got, err := jsonCodec.Marshal(action)
	require.NoError(t, err)
	require.Equal(t, want, string(got))
}

// TestJSONWireCancelActionsMarshal pins the marshal bytes of the three
// pretouched cancel-side actions (NewExchange pretouchJSON), none of which
// was previously covered. Expected bytes are derived from the struct tags in
// actions.go (Dex is omitempty and omitted); the cancel action reuses the
// values of goldenCancelAction (signing_golden_test.go), the cloid and asset
// come from testdata/CancelByCloid_Success.yaml, and the modify oid from
// testdata/Orders_Above10.yaml.
func TestJSONWireCancelActionsMarshal(t *testing.T) {
	tests := []struct {
		name   string
		action any
		want   string
	}{
		{
			name: "CancelAction",
			action: CancelAction{
				Type:    "cancel",
				Cancels: []CancelOrderWire{{Asset: 5, OrderID: 123456789}},
			},
			want: `{"type":"cancel","cancels":[{"a":5,"o":123456789}]}`,
		},
		{
			name: "CancelByCloidAction",
			action: CancelByCloidAction{
				Type:    "cancelByCloid",
				Cancels: []CancelByCloidWire{{Asset: 173, ClientID: "0x285ad26a251f390c83d065af51e3f8d9"}},
			},
			want: `{"type":"cancelByCloid","cancels":[{"asset":173,"cloid":"0x285ad26a251f390c83d065af51e3f8d9"}]}`,
		},
		{
			name: "BatchModifyAction",
			action: BatchModifyAction{
				Type: "batchModify",
				Modifies: []ModifyAction{
					{
						Oid: int64(41545810396),
						Order: OrderWire{
							Asset: 0, IsBuy: true, LimitPx: "100.5", Size: "1.5", ReduceOnly: false,
							OrderType: OrderWireType{Limit: &OrderWireTypeLimit{Tif: TifGtc}},
						},
					},
				},
			},
			want: `{"type":"batchModify","modifies":[{"oid":41545810396,"order":` +
				`{"a":0,"b":true,"p":"100.5","s":"1.5","r":false,"t":{"limit":{"tif":"Gtc"}}}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonCodec.Marshal(tt.action)
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

// TestJSONWireAPIError pins the API error envelope (errors.go). Expected keys
// come from the APIError struct tags (code/msg/data). No cassette records an
// HTTP error envelope, so the shape is asserted from the tags; the
// Error() string is the consumer-visible formatting from errors.go.
func TestJSONWireAPIError(t *testing.T) {
	t.Run("marshal without data", func(t *testing.T) {
		got, err := jsonCodec.Marshal(APIError{Code: 429, Message: "Too many requests"})
		require.NoError(t, err)
		require.Equal(t, `{"code":429,"msg":"Too many requests"}`, string(got))
	})

	t.Run("marshal with data", func(t *testing.T) {
		got, err := jsonCodec.Marshal(APIError{
			Code:    422,
			Message: "Invalid order",
			Data:    map[string]string{"field": "price"},
		})
		require.NoError(t, err)
		require.Equal(t, `{"code":422,"msg":"Invalid order","data":{"field":"price"}}`, string(got))
	})

	t.Run("unmarshal without data", func(t *testing.T) {
		var apiErr APIError
		require.NoError(t, jsonCodec.Unmarshal(
			[]byte(`{"code":429,"msg":"Too many requests"}`), &apiErr,
		))
		require.Equal(t, APIError{Code: 429, Message: "Too many requests"}, apiErr)
		require.Equal(t, "API error 429: Too many requests", apiErr.Error())
	})

	t.Run("unmarshal with data", func(t *testing.T) {
		var apiErr APIError
		require.NoError(t, jsonCodec.Unmarshal(
			[]byte(`{"code":422,"msg":"Invalid order","data":{"field":"price"}}`), &apiErr,
		))
		require.Equal(t, 422, apiErr.Code)
		require.Equal(t, "Invalid order", apiErr.Message)
		require.Equal(t, map[string]any{"field": "price"}, apiErr.Data)
	})
}

// TestAPIErrorFromPostErrorPath pins the non-2xx handling of client.post:
// a valid JSON body is decoded into APIError and returned as the error;
// a non-JSON body falls back to a "status N: body" error.
func TestAPIErrorFromPostErrorPath(t *testing.T) {
	t.Run("JSON error body decodes into APIError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"code":422,"msg":"Invalid signature"}`))
		}))
		defer server.Close()

		cli := newClient(server.URL)
		_, err := cli.post(context.Background(), "/exchange", map[string]string{"type": "order"})
		require.Error(t, err)

		var apiErr APIError
		require.True(t, errors.As(err, &apiErr))
		require.Equal(t, APIError{Code: 422, Message: "Invalid signature"}, apiErr)
	})

	t.Run("non-JSON error body surfaces status and body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`Internal Server Error`))
		}))
		defer server.Close()

		cli := newClient(server.URL)
		_, err := cli.post(context.Background(), "/exchange", map[string]string{"type": "order"})
		require.Error(t, err)
		require.ErrorContains(t, err, "status 500")
		require.ErrorContains(t, err, "Internal Server Error")
	})

	t.Run("JSON error body of another shape keeps status and body", func(t *testing.T) {
		// The codec ignores unknown keys, so this body decodes into a zero
		// APIError; reporting it as "API error 0: " would throw away both the
		// status and the server's actual message.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
		}))
		defer server.Close()

		cli := newClient(server.URL)
		_, err := cli.post(context.Background(), "/exchange", map[string]string{"type": "order"})
		require.Error(t, err)
		require.ErrorContains(t, err, "status 502")
		require.ErrorContains(t, err, "upstream unavailable")

		var apiErr APIError
		require.False(t, errors.As(err, &apiErr), "must not masquerade as a typed APIError")
	})
}
