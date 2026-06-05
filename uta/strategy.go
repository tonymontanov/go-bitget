/*
FILE: uta/strategy.go

DESCRIPTION:
Strategy sub-client — plan (TP/SL) orders (/api/v3/trade/*-strategy-*).
Implemented in milestone P7-M6.
*/

package uta

// StrategyClient — plan-order sub-client.
type StrategyClient struct {
	c *Client
}

func newStrategyClient(c *Client) *StrategyClient {
	return &StrategyClient{c: c}
}
