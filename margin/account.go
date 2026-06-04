/*
FILE: margin/account.go

DESCRIPTION:
Account / assets / borrow-repay / history sub-client for the Bitget V2
MARGIN profile.

M3 wires the authenticated endpoints (all under /api/v2/margin/<mode>/):

	GET  account/assets                      — GetAccountAssets
	POST account/borrow                      — Borrow
	POST account/repay                       — Repay
	POST account/flash-repay                 — FlashRepay
	GET  account/query-flash-repay-status    — GetFlashRepayResult
	GET  account/risk-rate                   — GetRiskRate
	GET  account/max-borrowable-amount       — GetMaxBorrowable
	GET  account/max-transfer-out-amount     — GetMaxTransferOut
	GET  interest-rate-and-limit             — GetInterestRateAndLimit
	GET  tier-data                           — GetTierData
	GET  open-orders                         — GetOpenOrders
	GET  history-orders                      — GetOrderHistory
	GET  fills                               — GetFills
	GET  liquidation-order                   — GetLiquidationOrders
	GET  borrow-history / repay-history /
	     interest-history / liquidation-history /
	     financial-records                   — paged record queries

This file is a stub in M1; the trading surface (margin/trading.go) is
wired in M2 and exercised independently.
*/

package margin

// AccountClient — account / balance / borrow-repay / history sub-client.
// Built once per margin.Client (see client.go) and safe for concurrent
// use.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}
