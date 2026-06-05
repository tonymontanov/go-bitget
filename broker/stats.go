/*
FILE: broker/stats.go

DESCRIPTION:
Stats sub-client — institutional-broker reporting
(/api/v2/broker/{subaccounts,commissions,trade-volume,total-commission,
order-commission,rebate-info}). Implemented in milestone P5-M3.
*/

package broker

// StatsClient — institutional-broker reporting sub-client.
type StatsClient struct {
	c *Client
}

func newStatsClient(c *Client) *StatsClient {
	return &StatsClient{c: c}
}
