/*
FILE: copytrading/futures_trader.go

DESCRIPTION:
Futures TRADER (lead) sub-client — the lead side of futures copy
trading (/api/v2/copy/mix-trader/...). A trader broadcasts futures
positions that followers mirror.

This file is a stub in M1 (struct + constructor). M3 wires the REST
surface: config-query-symbols / config-setting-symbols /
config-settings-base, config-query-followers / config-remove-follower,
order-current-track / order-history-track / order-total-detail,
order-close-positions, order-modify-tpsl, and the profit / profit-share
queries.
*/

package copytrading

// FuturesTraderClient — futures lead-trader sub-client. Built once per
// copytrading.Client and safe for concurrent use.
type FuturesTraderClient struct {
	c *Client
}

func newFuturesTraderClient(c *Client) *FuturesTraderClient {
	return &FuturesTraderClient{c: c}
}
