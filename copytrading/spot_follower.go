/*
FILE: copytrading/spot_follower.go

DESCRIPTION:
Spot FOLLOWER sub-client — the copier side of spot copy trading
(/api/v2/copy/spot-follower/...).

This file is a stub in M1 (struct + constructor). M4 wires the REST
surface: query-traders, query-trader-symbols, settings / query-settings,
setting-tpsl, cancel-trader (unfollow), query-current-orders /
query-history-orders, order-close-tracking, stop-order.
*/

package copytrading

// SpotFollowerClient — spot follower sub-client. Built once per
// copytrading.Client and safe for concurrent use.
type SpotFollowerClient struct {
	c *Client
}

func newSpotFollowerClient(c *Client) *SpotFollowerClient {
	return &SpotFollowerClient{c: c}
}
