/*
FILE: copytrading/spot_trader.go

DESCRIPTION:
Spot TRADER (lead) sub-client — the lead side of spot copy trading
(/api/v2/copy/spot-trader/...).

This file is a stub in M1 (struct + constructor). M4 wires the REST
surface: config-query-settings / config-setting-symbols,
config-query-followers / config-remove-follower, order-current-track /
order-history-track / order-total-detail, order-close-tracking,
order-modify-tpsl, and the profit queries.
*/

package copytrading

// SpotTraderClient — spot lead-trader sub-client. Built once per
// copytrading.Client and safe for concurrent use.
type SpotTraderClient struct {
	c *Client
}

func newSpotTraderClient(c *Client) *SpotTraderClient {
	return &SpotTraderClient{c: c}
}
