/*
FILE: mix/account.go

DESCRIPTION:
Account / position sub-client for Bitget MIX (legacy V2). M1 ships
ONLY the signatures; every method returns ErrorKindInvalidRequest
with "not implemented yet (M3)". M3 wires:

	GET  /api/v2/mix/account/account            — GetAccount
	GET  /api/v2/mix/position/single-position   — GetPosition
	GET  /api/v2/mix/order/orders-pending       — GetOpenOrders
	GET  /api/v2/mix/order/detail               — GetOrderDetail
	POST /api/v2/mix/order/close-positions      — ClosePosition (market close)
	POST /api/v2/mix/account/set-leverage       — SetLeverage
	POST /api/v2/mix/account/set-position-mode  — SetPositionMode
	                                              (one_way_mode | hedge_mode)

The trading-side ID mapping cache (clientOrderID ↔ exchange orderID)
also lives in this sub-client, since the desk needs it during open-
order reconciliation more than during placement. The cache itself is
added in M3 alongside the real implementations.
*/

package mix

import (
	"context"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// AccountClient — account / position sub-client.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// errNotImplementedM3 — sentinel for skeleton account methods.
func errNotImplementedM3(method string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account."+method+": not implemented yet (M3)", nil)
}

// GetAccount — placeholder (M3). Returns the account-level balance
// snapshot for the configured product type.
func (a *AccountClient) GetAccount(ctx context.Context) (roottypes.Balance, error) {
	return roottypes.Balance{}, errNotImplementedM3("GetAccount")
}

// GetPosition — placeholder (M3). Returns the open position for one
// symbol; in hedge mode there may be one long and one short position
// per symbol — M3 will return them in a stable order (long first).
func (a *AccountClient) GetPosition(ctx context.Context, symbol string) (mixtypes.PositionInfo, error) {
	return mixtypes.PositionInfo{}, errNotImplementedM3("GetPosition")
}

// GetOpenOrders — placeholder (M3). Bitget MIX paginates via the
// `lastEndId` cursor; M3 follows pagination internally and returns
// the full list.
func (a *AccountClient) GetOpenOrders(ctx context.Context, symbol string) ([]mixtypes.OrderInfo, error) {
	return nil, errNotImplementedM3("GetOpenOrders")
}

// GetOrderDetail — placeholder (M3). Identifies the order by either
// OrderID or ClientOrderID (mutually exclusive on the wire).
func (a *AccountClient) GetOrderDetail(ctx context.Context, symbol, orderID, clientOrderID string) (mixtypes.OrderInfo, error) {
	return mixtypes.OrderInfo{}, errNotImplementedM3("GetOrderDetail")
}

// ClosePosition — placeholder (M3). Sends a market close order for
// the given symbol; in hedge mode the holdSide must be specified
// (M3 introduces the explicit signature for both modes).
func (a *AccountClient) ClosePosition(ctx context.Context, symbol string) error {
	return errNotImplementedM3("ClosePosition")
}

// SetLeverage — placeholder (M3). Bitget MIX requires the holdSide on
// every leverage change in hedge mode and ignores it in one-way mode;
// M3 will accept an optional holdSide argument.
func (a *AccountClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	return errNotImplementedM3("SetLeverage")
}

// SetPositionMode — placeholder (M3). Bitget MIX position mode is
// account-global, not per-symbol.
func (a *AccountClient) SetPositionMode(ctx context.Context, mode roottypes.PositionMode) error {
	return errNotImplementedM3("SetPositionMode")
}
