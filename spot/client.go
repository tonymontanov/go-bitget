/*
FILE: spot/client.go

DESCRIPTION:
Root sub-client for the Bitget V2 SPOT profile. Holds a reference to
the parent bitget.Client (REST, signer, logger, config) and exposes
four domain sub-clients — Trading, Account, MarketData, Stream.

CONTRACT:

  - Client is safe for concurrent use; sub-clients are read-only after
    construction.
  - All REST calls go through parent.REST() — the shared connection
    pool used by every profile (mix, spot, future uta).
  - In M1 every sub-client is a stub (struct + constructor). M2 wires
    REST endpoints (Trading + MarketData), M3 wires Account + history,
    M4 wires public WS, M5 wires private WS. This mirrors the mix/
    rollout that shipped under v1.0.
  - Spot has no productType / marginMode / marginCoin / holdSide /
    tradeSide — see spot/doc.go for the contrast with mix.

INSTRUMENT TYPE ON THE WIRE:

Bitget V2 uses the literal string "SPOT" in the WebSocket subscription
arg's `instType` field for every spot channel (books, ticker, trade,
candles, account, orders, fills). REST does NOT take instType — the
URL path itself (/api/v2/spot/...) selects the product. The constant
SpotInstType is exposed here so sub-clients reference it via a single
source of truth.

LAZY ENTRY POINT:

bitget.Client.Spot() returns *spot.Client only after this package is
imported (the init() below registers the factory). Trying to consume
.Spot() without importing spot logs a warning and returns nil — same
pattern as mix.
*/

package spot

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
)

// SpotInstType is the literal string Bitget V2 expects in the
// `instType` field of every WebSocket subscription arg targeting the
// spot product (books / ticker / trade / candles / account / orders /
// fills). REST endpoints do not take instType; the URL path selects
// the product.
const SpotInstType = "SPOT"

// Client — Bitget V2 SPOT profile client.
type Client struct {
	parent *bitget.Client

	trading    *TradingClient
	account    *AccountClient
	marketData *MarketDataClient
	stream     *StreamClient
}

// NewClient creates a SPOT profile client bound to the given root
// bitget.Client. Returns nil if parent is nil — every public method
// guards against that explicitly so a nil propagation does not panic
// in caller code.
//
// Spot has no construction-time settings (no productType, no
// marginMode, no marginCoin) — the wire format is fully self-
// describing per request. M2 may introduce optional knobs (e.g. demo
// hosts) but the v2.0 contract is the same as mix/ NewClient.
func NewClient(parent *bitget.Client) *Client {
	if parent == nil {
		return nil
	}
	var c *Client = &Client{parent: parent}
	c.trading = newTradingClient(c)
	c.account = newAccountClient(c)
	c.marketData = newMarketDataClient(c)
	c.stream = newStreamClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// Trading returns the trading sub-client. M1 ships a stub; M2 wires
// the REST place / amend / cancel endpoints (single + batch).
func (c *Client) Trading() *TradingClient { return c.trading }

// Account returns the account sub-client. M1 ships a stub; M3 wires
// the REST account-info / asset-balance / order-history / fills-
// history endpoints.
func (c *Client) Account() *AccountClient { return c.account }

// MarketData returns the public market-data sub-client. M1 ships a
// stub; M2 wires the REST symbols / tickers / orderbook / candles /
// recent-fills endpoints.
func (c *Client) MarketData() *MarketDataClient { return c.marketData }

// Stream returns the WebSocket subscription sub-client. M1 ships a
// stub; M4 wires public streams (books with CRC32 resync, ticker,
// trade, candles); M5 wires private streams (account, orders, fills).
func (c *Client) Stream() *StreamClient { return c.stream }

// Internal shortcuts shared by sub-clients. Mirror of the mix.Client
// helpers — every sub-client reaches REST / signer / logger / config
// through these (never via direct parent.* calls) so future
// refactors of the parent surface stay localised here.
func (c *Client) logger() bitget.Logger   { return c.parent.Logger() }
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }
func (c *Client) config() bitget.Config   { return c.parent.Config() }
func (c *Client) signerEnabled() bool     { return c.parent.Signer().Enabled() }

// init registers the factory in the root package so that
// bitget.Client.Spot() lazily returns *spot.Client. Callers that
// only need the root Client do not have to reference spot symbols
// directly — a blank import of "github.com/tonymontanov/go-bitget/v2/spot"
// is enough to pull this init() into the binary.
func init() {
	bitget.RegisterSpotFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
