/*
FILE: margin/public.go

DESCRIPTION:
Public reference sub-client for the Bitget V2 MARGIN profile.

Margin trades SPOT instruments, so there is NO margin-specific price /
orderbook / candle endpoint — callers use spot.MarketData() for those.
The one public margin endpoint is the supported-currencies reference:

	GET /api/v2/margin/currencies — Currencies (margin-coin list + limits)

Note this endpoint is mode-agnostic (no crossed/isolated segment): the
supported-margin-coin catalogue is shared across both modes.

This file is a stub in M1; Currencies is wired in M3.
*/

package margin

// PublicClient — public reference sub-client (margin currencies). Built
// once per margin.Client (see client.go) and safe for concurrent use.
type PublicClient struct {
	c *Client
}

func newPublicClient(c *Client) *PublicClient {
	return &PublicClient{c: c}
}
