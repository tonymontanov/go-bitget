/*
FILE: copytrading/client.go

DESCRIPTION:
Root sub-client for the Bitget V2 COPY-TRADING profile. Holds a
reference to the parent bitget.Client (REST, signer, logger, config)
and exposes the four product×role sub-clients — FuturesTrader,
FuturesFollower, SpotTrader, SpotFollower.

CONTRACT:

  - Client is safe for concurrent use; sub-clients are read-only after
    construction.
  - All REST calls go through parent.REST() — the shared connection
    pool used by every profile.
  - The futures PRODUCT TYPE (USDT / COIN / USDC-FUTURES) is pinned at
    construction time via ClientSettings and sent on every futures
    request. Spot sub-clients ignore it. Callers that need a different
    futures product type construct a second client.

LAZY ENTRY POINT:

bitget.Client.CopyTrading() returns *copytrading.Client (USDT-FUTURES
default) only after this package is imported (the init() below
registers the factory). Callers needing COIN / USDC futures copy
trading call NewClientWithSettings.
*/

package copytrading

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ClientSettings — settings pinned to a copytrading.Client at
// construction time.
//
// ProductType selects the futures venue (USDT / COIN / USDC-FUTURES)
// sent on every futures copy-trading call. Empty falls back to
// USDT-FUTURES (the desk default, matching mix). It does not affect the
// spot sub-clients.
type ClientSettings struct {
	ProductType roottypes.ProductType
}

// Client — Bitget V2 COPY-TRADING profile client.
type Client struct {
	parent *bitget.Client

	// productType — the futures venue sent on every mix-trader /
	// mix-follower request. Pinned at construction time.
	productType roottypes.ProductType

	futuresTrader   *FuturesTraderClient
	futuresFollower *FuturesFollowerClient
	spotTrader      *SpotTraderClient
	spotFollower    *SpotFollowerClient
}

// NewClient creates a COPY-TRADING profile client pinned to the SDK
// default (USDT-FUTURES). Returns nil if parent is nil.
func NewClient(parent *bitget.Client) *Client {
	return NewClientWithSettings(parent, ClientSettings{})
}

// NewClientWithProductType creates a COPY-TRADING profile client pinned
// to the given futures product type. Convenience wrapper over
// NewClientWithSettings.
func NewClientWithProductType(parent *bitget.Client, productType roottypes.ProductType) *Client {
	return NewClientWithSettings(parent, ClientSettings{ProductType: productType})
}

// NewClientWithSettings creates a COPY-TRADING profile client with
// explicit settings. Empty ProductType falls back to USDT-FUTURES.
// Returns nil if parent is nil — every public method guards against
// that so a nil propagation does not panic in caller code.
func NewClientWithSettings(parent *bitget.Client, s ClientSettings) *Client {
	if parent == nil {
		return nil
	}
	if s.ProductType == "" {
		s.ProductType = roottypes.ProductTypeUSDTFutures
	}

	var c *Client = &Client{
		parent:      parent,
		productType: s.ProductType,
	}
	c.futuresTrader = newFuturesTraderClient(c)
	c.futuresFollower = newFuturesFollowerClient(c)
	c.spotTrader = newSpotTraderClient(c)
	c.spotFollower = newSpotFollowerClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// ProductType returns the resolved futures product type pinned at
// construction time.
func (c *Client) ProductType() roottypes.ProductType { return c.productType }

// FuturesTrader returns the futures lead-trader sub-client
// (/api/v2/copy/mix-trader/...).
func (c *Client) FuturesTrader() *FuturesTraderClient { return c.futuresTrader }

// FuturesFollower returns the futures follower sub-client
// (/api/v2/copy/mix-follower/...).
func (c *Client) FuturesFollower() *FuturesFollowerClient { return c.futuresFollower }

// SpotTrader returns the spot lead-trader sub-client
// (/api/v2/copy/spot-trader/...).
func (c *Client) SpotTrader() *SpotTraderClient { return c.spotTrader }

// SpotFollower returns the spot follower sub-client
// (/api/v2/copy/spot-follower/...).
func (c *Client) SpotFollower() *SpotFollowerClient { return c.spotFollower }

// Internal shortcuts shared by sub-clients. Mirror of the mix/spot/
// margin helpers — every sub-client reaches REST / signer / logger /
// config through these.
func (c *Client) logger() bitget.Logger   { return c.parent.Logger() }
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }
func (c *Client) config() bitget.Config   { return c.parent.Config() }
func (c *Client) signerEnabled() bool     { return c.parent.Signer().Enabled() }

// init registers the factory in the root package so that
// bitget.Client.CopyTrading() lazily returns *copytrading.Client
// (USDT-FUTURES default). A blank import of
// "github.com/tonymontanov/go-bitget/v2/copytrading" pulls this init()
// into the binary.
func init() {
	bitget.RegisterCopyTradingFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
