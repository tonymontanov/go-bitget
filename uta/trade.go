/*
FILE: uta/trade.go

DESCRIPTION:
Trade sub-client — order flow (/api/v3/trade/...). Implemented in
milestone P7-M4.
*/

package uta

// TradeClient — order-flow sub-client.
type TradeClient struct {
	c *Client
}

func newTradeClient(c *Client) *TradeClient {
	return &TradeClient{c: c}
}
