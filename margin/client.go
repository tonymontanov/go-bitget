/*
FILE: margin/client.go

DESCRIPTION:
Root sub-client for the Bitget V2 MARGIN profile. Holds a reference to
the parent bitget.Client (REST, signer, logger, config) and exposes the
domain sub-clients — Trading, Account, Public, Stream.

CONTRACT:

  - Client is safe for concurrent use; sub-clients are read-only after
    construction.
  - All REST calls go through parent.REST() — the shared connection
    pool used by every profile (mix, spot, margin).
  - The margin MODE (crossed / isolated) is pinned at construction time
    and drives the URL path segment on every endpoint
    (/api/v2/margin/<mode>/...). Callers that need BOTH modes construct
    two clients. This mirrors how mix pins ProductType.

LAZY ENTRY POINT:

bitget.Client.Margin() returns *margin.Client (crossed default) only
after this package is imported (the init() below registers the
factory). Callers needing isolated margin call NewClientWithSettings.
*/

package margin

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// MarginInstType is the literal string Bitget V2 expects in the
// `instType` field of every MARGIN WebSocket subscription arg
// (account-<mode> / orders-<mode>). REST does not take instType — the
// URL path segment selects crossed vs isolated.
const MarginInstType = "MARGIN"

// ClientSettings — the settings pinned to a margin.Client at
// construction time.
//
// Mode selects the crossed / isolated path segment on every REST and
// WS call. Empty falls back to crossed (the industry default for
// market makers, matching mix).
type ClientSettings struct {
	Mode roottypes.MarginMode
}

// Client — Bitget V2 MARGIN profile client.
type Client struct {
	parent *bitget.Client

	// mode — crossed / isolated, pinned at construction time. Drives
	// the URL path segment (/api/v2/margin/<mode>/...) on every
	// endpoint and the channel suffix (account-<mode> / orders-<mode>)
	// on the private WS.
	mode roottypes.MarginMode

	trading *TradingClient
	account *AccountClient
	public  *PublicClient
	stream  *StreamClient
}

// NewClient creates a MARGIN profile client pinned to the SDK default
// (crossed margin). Returns nil if parent is nil.
func NewClient(parent *bitget.Client) *Client {
	return NewClientWithSettings(parent, ClientSettings{})
}

// NewClientWithMode creates a MARGIN profile client pinned to the given
// margin mode. Convenience wrapper over NewClientWithSettings.
func NewClientWithMode(parent *bitget.Client, mode roottypes.MarginMode) *Client {
	return NewClientWithSettings(parent, ClientSettings{Mode: mode})
}

// NewClientWithSettings creates a MARGIN profile client with explicit
// settings. Empty Mode falls back to crossed. Returns nil if parent is
// nil — every public method guards against that so a nil propagation
// does not panic in caller code.
func NewClientWithSettings(parent *bitget.Client, s ClientSettings) *Client {
	if parent == nil {
		return nil
	}
	if s.Mode == "" {
		s.Mode = roottypes.MarginModeCrossed
	}

	var c *Client = &Client{
		parent: parent,
		mode:   s.Mode,
	}
	c.trading = newTradingClient(c)
	c.account = newAccountClient(c)
	c.public = newPublicClient(c)
	c.stream = newStreamClient(c)
	return c
}

// Parent returns the root bitget.Client.
func (c *Client) Parent() *bitget.Client { return c.parent }

// Mode returns the resolved margin mode (crossed / isolated) pinned at
// construction time.
func (c *Client) Mode() roottypes.MarginMode { return c.mode }

// Trading returns the trading sub-client (place / batch-place / cancel
// / batch-cancel).
func (c *Client) Trading() *TradingClient { return c.trading }

// Account returns the account / assets / borrow-repay / history
// sub-client.
func (c *Client) Account() *AccountClient { return c.account }

// Public returns the public reference sub-client (margin currencies).
func (c *Client) Public() *PublicClient { return c.public }

// Stream returns the private WebSocket sub-client (account-<mode> /
// orders-<mode>). Margin has no public WS — use the spot Stream for
// books / tickers.
func (c *Client) Stream() *StreamClient { return c.stream }

// modeSegment returns the URL path segment for the pinned margin mode
// ("crossed" / "isolated"). Every REST path is built as
// "/api/v2/margin/" + c.modeSegment() + "/...". Centralised here so the
// crossed/isolated spelling has a single source of truth.
func (c *Client) modeSegment() string { return string(c.mode) }

// Internal shortcuts shared by sub-clients. Mirror of the mix/spot
// helpers — every sub-client reaches REST / signer / logger / config
// through these so future refactors of the parent surface stay
// localised here.
func (c *Client) logger() bitget.Logger   { return c.parent.Logger() }
func (c *Client) rest() bgcommon.RestDoer { return c.parent.REST() }
func (c *Client) config() bitget.Config   { return c.parent.Config() }
func (c *Client) signerEnabled() bool     { return c.parent.Signer().Enabled() }

// init registers the factory in the root package so that
// bitget.Client.Margin() lazily returns *margin.Client (crossed
// default). A blank import of
// "github.com/tonymontanov/go-bitget/v2/margin" is enough to pull this
// init() into the binary.
func init() {
	bitget.RegisterMarginFactory(func(parent *bitget.Client) any {
		return NewClient(parent)
	})
}
