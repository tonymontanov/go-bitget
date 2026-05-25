/*
FILE: mix/client.go

DESCRIPTION:
Root sub-client for the Bitget MIX (legacy V2) profile. Holds a
reference to the parent bitget.Client (REST, signer, logger, config)
and exposes four domain sub-clients — Trading, Account, MarketData,
Stream.

CONTRACT:

  - Client is safe for concurrent use; sub-clients are read-only after
    construction.
  - All REST calls go through parent.REST() — shared connection pool.
  - The Stream sub-client is a stub in M1 (M4 wires public WS, M5
    private WS). Calling Watch* on it returns ErrorKindInvalidRequest.

PRODUCT TYPE:

  - The MIX endpoints take productType={USDT,USDC,COIN}-FUTURES on
    every request. The SDK pins the value at construction time so
    callers do not pass it on every method invocation. The default
    factory (NewClient and the bitget.Client.Mix() lazy entry point)
    uses USDT-FUTURES — the only product type v1.0 of the desk
    exercises. Callers that need a different product type should call
    NewClientWithProductType directly; bitget.Client.Mix() always
    returns the USDT-FUTURES variant.
*/

package mix

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// Client — Bitget MIX (legacy V2) profile client.
type Client struct {
	parent *bitget.Client

	// productType — the value sent on every MIX request. Locked in at
	// construction time, see NewClient / NewClientWithProductType.
	productType roottypes.ProductType

	trading    *TradingClient
	account    *AccountClient
	marketData *MarketDataClient
	stream     *StreamClient
}

// NewClient creates a MIX profile client pinned to USDT-FUTURES — the
// default for v1.0 of the desk. Returns nil if parent is nil.
func NewClient(parent *bitget.Client) *Client {
	return NewClientWithProductType(parent, roottypes.ProductTypeUSDTFutures)
}

// NewClientWithProductType creates a MIX profile client pinned to the
// given product type. Returns nil if parent is nil. An empty
// productType is treated as USDT-FUTURES.
func NewClientWithProductType(parent *bitget.Client, productType roottypes.ProductType) *Client {
	if parent == nil {
		return nil
	}
	if productType == "" {
		productType = roottypes.ProductTypeUSDTFutures
	}
	var c *Client = &Client{
		parent:      parent,
		productType: productType,
	}
	c.trading = newTradingClient(c)
	c.account = newAccountClient(c)
	c.marketData = newMarketDataClient(c)
	c.stream = newStreamClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// ProductType returns the resolved product type (one of
// USDT-FUTURES / USDC-FUTURES / COIN-FUTURES, or the demo variants).
func (c *Client) ProductType() roottypes.ProductType { return c.productType }

// Trading returns the trading sub-client. M1 ships stubs; M2 wires the
// REST place/amend/cancel endpoints.
func (c *Client) Trading() *TradingClient { return c.trading }

// Account returns the account / position sub-client. M1 ships stubs;
// M3 wires the REST endpoints.
func (c *Client) Account() *AccountClient { return c.account }

// MarketData returns the public market-data sub-client. Production-ready
// in M1.
func (c *Client) MarketData() *MarketDataClient { return c.marketData }

// Stream returns the WebSocket subscription sub-client. M1 ships stubs;
// M4 wires public streams, M5 private streams.
func (c *Client) Stream() *StreamClient { return c.stream }

// Internal shortcuts shared by sub-clients.
func (c *Client) logger() bitget.Logger { return c.parent.Logger() }
func (c *Client) rest() restDoer        { return c.parent.REST() }
func (c *Client) config() bitget.Config { return c.parent.Config() }
func (c *Client) signerEnabled() bool   { return c.parent.Signer().Enabled() }

// init registers the factory in the root package so that
// bitget.Client.Mix() lazily returns *mix.Client. This allows users to
// avoid an explicit mix import when only working through the root
// Client (a blank-import of "github.com/tonymontanov/go-bitget/v2/mix"
// is still required to trigger this init).
func init() {
	bitget.RegisterMixFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
