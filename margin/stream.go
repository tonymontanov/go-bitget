/*
FILE: margin/stream.go

DESCRIPTION:
Private WebSocket sub-client for the Bitget V2 MARGIN profile.

Margin has NO public WS — books / tickers / trades / candles for the
underlying spot instruments come from the spot Stream. The margin
profile ships ONLY private channels, on instType="MARGIN":

	account-<mode>   per-coin borrow / available / interest snapshot
	orders-<mode>    margin order lifecycle pushes

Both channel names carry the crossed/isolated suffix that matches the
client's pinned mode (account-crossed / orders-isolated / ...).

This file is a stub in M1 (struct + constructor + Close). M4 wires the
private connection lifecycle (login + signed subscribe) and the
WatchAccount / WatchOrders primitives.
*/

package margin

// StreamClient — private WebSocket subscription sub-client. Built once
// per margin.Client (see client.go) and safe for concurrent use.
type StreamClient struct {
	c *Client
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// Close shuts the private WS connection down. Idempotent. A no-op until
// M4 wires the connection lifecycle.
func (s *StreamClient) Close() error {
	return nil
}
