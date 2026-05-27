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

PRODUCT TYPE / MARGIN MODE / MARGIN COIN:

  - The MIX endpoints take productType={USDT,USDC,COIN}-FUTURES on
    every request, marginMode={isolated,crossed} on every place /
    modify order, and marginCoin (capitalised, e.g. "USDT") on most
    trading endpoints. All three are pinned at construction time so
    callers do not pass them on every method invocation.

  - Defaults: productType=USDT-FUTURES, marginMode=crossed,
    marginCoin auto-derived from productType (USDT-FUTURES→USDT,
    USDC-FUTURES→USDC, COIN-FUTURES→"" — coin-margined contracts use
    a per-symbol coin and the SDK leaves the field empty on the wire,
    letting Bitget infer it from the symbol).

  - Override via NewClientWithSettings (see below). The legacy
    NewClient and NewClientWithProductType wrappers stay backwards-
    compatible: they pin productType and use the defaults above.

  - bitget.Client.Mix() lazy entry point always returns the SDK
    default (USDT-FUTURES + crossed + USDT). Callers needing a
    different combo must construct the mix.Client explicitly via
    NewClientWithSettings.
*/

package mix

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ClientSettings — the trio of settings pinned to a mix.Client at
// construction time and embedded into every request the trading and
// account sub-clients send to Bitget.
//
// Empty fields fall back to SDK defaults:
//   - ProductType "" → USDT-FUTURES.
//   - MarginMode  "" → crossed (industry default for market makers).
//   - MarginCoin  "" → derived from ProductType
//     (USDT-FUTURES→"USDT", USDC-FUTURES→"USDC", COIN-FUTURES→"").
//     For COIN-FUTURES the SDK leaves the field empty on the wire so
//     Bitget infers the per-symbol margin coin (e.g. "BTC" for
//     BTCUSD). Callers that pin a single coin can override.
type ClientSettings struct {
	ProductType roottypes.ProductType
	MarginMode  roottypes.MarginMode
	MarginCoin  string
}

// Client — Bitget MIX (legacy V2) profile client.
type Client struct {
	parent *bitget.Client

	// productType — the value sent on every MIX request. Locked in at
	// construction time, see NewClient / NewClientWithProductType /
	// NewClientWithSettings.
	productType roottypes.ProductType
	// marginMode — sent on every place / modify order. Defaults to
	// crossed (industry default for market makers).
	marginMode roottypes.MarginMode
	// marginCoin — sent on every order placement and most cancellation
	// endpoints. Empty string means "let Bitget infer it from the
	// symbol" (the COIN-FUTURES default).
	marginCoin string

	trading    *TradingClient
	account    *AccountClient
	marketData *MarketDataClient
	stream     *StreamClient
}

// NewClient creates a MIX profile client pinned to the SDK defaults
// (USDT-FUTURES + crossed + USDT). Returns nil if parent is nil.
func NewClient(parent *bitget.Client) *Client {
	return NewClientWithSettings(parent, ClientSettings{})
}

// NewClientWithProductType creates a MIX profile client pinned to the
// given product type. MarginMode and MarginCoin fall back to SDK
// defaults (crossed; coin derived from productType). Kept for
// backwards-compat with M1 callers; new code should use
// NewClientWithSettings.
func NewClientWithProductType(parent *bitget.Client, productType roottypes.ProductType) *Client {
	return NewClientWithSettings(parent, ClientSettings{ProductType: productType})
}

// NewClientWithSettings creates a MIX profile client with explicit
// productType / marginMode / marginCoin. Empty fields fall back to
// the SDK defaults documented on ClientSettings. Returns nil if parent
// is nil.
func NewClientWithSettings(parent *bitget.Client, s ClientSettings) *Client {
	if parent == nil {
		return nil
	}
	if s.ProductType == "" {
		s.ProductType = roottypes.ProductTypeUSDTFutures
	}
	if s.MarginMode == "" {
		s.MarginMode = roottypes.MarginModeCrossed
	}
	if s.MarginCoin == "" {
		s.MarginCoin = defaultMarginCoinFor(s.ProductType)
	}

	var c *Client = &Client{
		parent:      parent,
		productType: s.ProductType,
		marginMode:  s.MarginMode,
		marginCoin:  s.MarginCoin,
	}
	c.trading = newTradingClient(c)
	c.account = newAccountClient(c)
	c.marketData = newMarketDataClient(c)
	c.stream = newStreamClient(c)
	return c
}

// defaultMarginCoinFor derives the default margin coin for a given
// product type. COIN-FUTURES has no single coin (each symbol pins its
// own), so it returns the empty string and the SDK lets Bitget infer
// it from the symbol on the wire.
func defaultMarginCoinFor(productType roottypes.ProductType) string {
	switch productType {
	case roottypes.ProductTypeUSDTFutures, roottypes.ProductTypeSusdtFutures:
		return "USDT"
	case roottypes.ProductTypeUSDCFutures, roottypes.ProductTypeSusdcFutures:
		return "USDC"
	default:
		return ""
	}
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// ProductType returns the resolved product type (one of
// USDT-FUTURES / USDC-FUTURES / COIN-FUTURES, or the demo variants).
func (c *Client) ProductType() roottypes.ProductType { return c.productType }

// MarginMode returns the resolved margin mode (isolated / crossed)
// pinned at construction time.
func (c *Client) MarginMode() roottypes.MarginMode { return c.marginMode }

// MarginCoin returns the resolved margin coin (e.g. "USDT"). May be
// empty for COIN-FUTURES — the SDK then lets Bitget infer the coin
// from the symbol on the wire.
func (c *Client) MarginCoin() string { return c.marginCoin }

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
func (c *Client) logger() bitget.Logger   { return c.parent.Logger() }
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }
func (c *Client) config() bitget.Config   { return c.parent.Config() }
func (c *Client) signerEnabled() bool     { return c.parent.Signer().Enabled() }

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
