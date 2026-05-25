/*
FILE: mix/trading.go

DESCRIPTION:
Trading sub-client for Bitget MIX (legacy V2). M1 ships ONLY the type
+ method signatures; every method returns ErrorKindInvalidRequest with
"not implemented yet (M2)" so callers fail fast instead of hitting
nil dereferences. M2 wires the actual REST endpoints:

	POST /api/v2/mix/order/place-order        — CreateOrder
	POST /api/v2/mix/order/modify-order       — ModifyOrder
	POST /api/v2/mix/order/cancel-order       — CancelOrder
	POST /api/v2/mix/order/batch-place-order  — CreateBatchOrders
	POST /api/v2/mix/order/batch-modify-order — ModifyBatchOrders
	POST /api/v2/mix/order/batch-cancel-orders — CancelBatchOrders
	POST /api/v2/mix/order/cancel-all-orders  — CancelAllOrders

CONTRACT KEPT STABLE:
The signatures land here in M1 so the desk's BitgetMixConnector can be
written against them; M2 fills the bodies without changing the
interface.
*/

package mix

import (
	"context"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// TradingClient — trading sub-client.
type TradingClient struct {
	c *Client
}

func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{c: c}
}

// errNotImplementedM2 — sentinel for skeleton trading methods. Replaced
// in M2 with the real implementations.
func errNotImplementedM2(method string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading."+method+": not implemented yet (M2)", nil)
}

// CreateOrder — placeholder (M2).
func (t *TradingClient) CreateOrder(ctx context.Context, req mixtypes.CreateOrderRequest) (mixtypes.OrderInfo, error) {
	return mixtypes.OrderInfo{}, errNotImplementedM2("CreateOrder")
}

// ModifyOrder — placeholder (M2).
func (t *TradingClient) ModifyOrder(ctx context.Context, req mixtypes.ModifyOrderRequest) (mixtypes.OrderInfo, error) {
	return mixtypes.OrderInfo{}, errNotImplementedM2("ModifyOrder")
}

// CancelOrder — placeholder (M2). Uses the protocol-common
// CancelOrderRequest from the root types package.
func (t *TradingClient) CancelOrder(ctx context.Context, req roottypes.CancelOrderRequest) error {
	return errNotImplementedM2("CancelOrder")
}

// CreateBatchOrders — placeholder (M2). Bitget MIX batch limit is 50
// orders per call (smaller than Bybit's 20 cap on linear); the M2
// implementation will enforce it client-side.
func (t *TradingClient) CreateBatchOrders(ctx context.Context, reqs []mixtypes.CreateOrderRequest) ([]mixtypes.BatchOrderResult, error) {
	return nil, errNotImplementedM2("CreateBatchOrders")
}

// ModifyBatchOrders — placeholder (M2).
func (t *TradingClient) ModifyBatchOrders(ctx context.Context, reqs []mixtypes.ModifyOrderRequest) ([]mixtypes.BatchOrderResult, error) {
	return nil, errNotImplementedM2("ModifyBatchOrders")
}

// CancelBatchOrders — placeholder (M2).
func (t *TradingClient) CancelBatchOrders(ctx context.Context, reqs []roottypes.CancelOrderRequest) ([]mixtypes.BatchOrderResult, error) {
	return nil, errNotImplementedM2("CancelBatchOrders")
}

// CancelAllOrders — placeholder (M2). Bitget MIX cancels by
// (productType, symbol, marginCoin); when symbol is empty, it cancels
// every order under the product type.
func (t *TradingClient) CancelAllOrders(ctx context.Context, symbol string) error {
	return errNotImplementedM2("CancelAllOrders")
}
