package hyperliquid

import (
	"context"
	"fmt"
)

// TrailingStopRetracement configures how far the mark price must retrace
// from the watermark (highest mark since activation for a sell, lowest for
// a buy) before the trailing stop submits its market order. Exactly one
// field must be set:
//
//	Percent:       percentage of the watermark price (e.g. 1.5 for 1.5%)
//	PriceDistance: fixed distance in quote currency (e.g. 10 for $10)
type TrailingStopRetracement struct {
	Percent       *float64
	PriceDistance *float64
}

// TrailingStopOrderRequest is a trailing stop order request. The trigger
// price follows the mark price as it moves in favor of the order and submits
// a market order for Size once the mark retraces by Retracement from the
// watermark. ActivationPx defers tracking until the mark price reaches it;
// nil starts tracking immediately.
type TrailingStopOrderRequest struct {
	Coin         string
	IsBuy        bool
	Size         float64
	ReduceOnly   bool
	Retracement  TrailingStopRetracement
	ActivationPx *float64
}

func newTrailingStopAction(e *Exchange, req TrailingStopOrderRequest) (TrailingStopAction, error) {
	sizeWire, err := floatToWire(req.Size)
	if err != nil {
		return TrailingStopAction{}, fmt.Errorf("failed to wire size for trailing stop: %w", err)
	}

	asset, ok := e.info.CoinToAsset(req.Coin)
	if !ok {
		return TrailingStopAction{}, fmt.Errorf("coin %s not found in info", req.Coin)
	}

	var retracement TrailingStopRetracementWire
	switch r := req.Retracement; {
	case r.Percent != nil && r.PriceDistance != nil:
		return TrailingStopAction{}, fmt.Errorf(
			"trailing stop retracement: only one of Percent or PriceDistance may be set")
	case r.Percent != nil:
		// Percent retracement is sent with 4 decimals and a '%' suffix
		// (e.g. "1.5000%"), matching the frontend wire format.
		pct := fmt.Sprintf("%.4f%%", *r.Percent)
		retracement.Pct = &pct
	case r.PriceDistance != nil:
		px, err := floatToWire(*r.PriceDistance)
		if err != nil {
			return TrailingStopAction{}, fmt.Errorf("failed to wire retracement for trailing stop: %w", err)
		}
		retracement.Px = &px
	default:
		return TrailingStopAction{}, fmt.Errorf(
			"trailing stop retracement: one of Percent or PriceDistance must be set")
	}

	var activationPx *string
	if req.ActivationPx != nil {
		px, err := floatToWire(*req.ActivationPx)
		if err != nil {
			return TrailingStopAction{}, fmt.Errorf("failed to wire activation price for trailing stop: %w", err)
		}
		activationPx = &px
	}

	return TrailingStopAction{
		Type:         "trailingStop",
		Asset:        asset,
		IsBuy:        req.IsBuy,
		Size:         sizeWire,
		ReduceOnly:   req.ReduceOnly,
		Retracement:  retracement,
		ActivationPx: activationPx,
	}, nil
}

// PlaceTrailingStop places a trailing stop order as a standalone
// "trailingStop" exchange action (not the "order" action). The resulting
// resting order appears in open orders and is cancelled through the regular
// Cancel/BulkCancel actions.
func (e *Exchange) PlaceTrailingStop(
	ctx context.Context,
	req TrailingStopOrderRequest,
) (result *APIResponse[OrderResponse], err error) {
	action, err := newTrailingStopAction(e, req)
	if err != nil {
		return nil, err
	}
	err = e.executeAction(ctx, action, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
