/*
FILE: client.go

DESCRIPTION:
The root SDK client. Holds shared resources (REST transport, signer,
config, logger) and exposes lazy domain sub-clients on demand. Domain
profiles (mix, spot, uta) are implemented in their own packages and
register a factory at init() time so the root package never imports them
directly (avoids a circular dependency: domain packages import the root
for Config/Error/etc.).

USAGE:

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.APIKey = "..."
	cfg.SecretKey = "..."
	cfg.Passphrase = "..."
	var c, err = bitget.NewClient(cfg)
	if err != nil { panic(err) }
	defer c.Close()

	// Once the mix package is imported (anonymously is fine):
	//   import _ "github.com/tonymontanov/go-bitget/v2/mix"
	var mixClient = c.Mix().(*mix.Client)

The .(*mix.Client) cast is by design: the root package returns `any`
because it cannot know about the mix.Client type without importing
the mix package (which already imports root). The cast is a single
line and keeps the SDK structure flat.
*/

package bitget

import (
	"sync"

	"github.com/tonymontanov/go-bitget/v2/internal/auth"
	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// Client is the root SDK object. Safe for concurrent use; methods on Client
// itself are stateless apart from the lazy sub-client cache.
type Client struct {
	cfg    Config
	signer *auth.Signer
	rest   *rest.Client
	logger Logger

	mixOnce sync.Once
	mixVal  any

	spotOnce sync.Once
	spotVal  any

	utaOnce sync.Once
	utaVal  any
}

// NewClient validates cfg, fills defaults, and returns a configured root
// client. Returns an *Error with ErrorKindInvalidRequest on configuration
// problems.
func NewClient(cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	var signer *auth.Signer = auth.NewSigner(cfg.APIKey, cfg.SecretKey, cfg.Passphrase)

	var restCfg rest.Config = rest.Config{
		RequestTimeout:      cfg.REST.RequestTimeout,
		MaxIdleConns:        cfg.REST.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.REST.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.REST.IdleConnTimeout,
		Locale:              cfg.REST.Locale,
		RateLimitObserver:   cfg.RateLimitObserver,
	}
	// Forward the typed event observer through a thin adapter. The
	// public RateLimitEvent struct lives in the root package and CANNOT
	// be passed directly into internal/rest (import cycle). The transport
	// invokes the callback with flat arguments and we assemble
	// RateLimitEvent here.
	if cfg.RateLimitEventObserver != nil {
		var userObserver = cfg.RateLimitEventObserver
		restCfg.RateLimitEventObserver = func(endpoint, method string, headers map[string]string, meta rest.RequestMeta) {
			userObserver(RateLimitEvent{
				Endpoint:   endpoint,
				Method:     method,
				Headers:    headers,
				OrderCount: meta.OrderCount,
				Symbols:    meta.Symbols,
				Category:   RateLimitCategory(meta.Category),
			})
		}
	}

	var transportLogger = cfg.Logger
	var restClient *rest.Client = rest.NewClient(cfg.REST.BaseURL, signer, restCfg, cfg.UserAgent, transportLogger)

	return &Client{
		cfg:    cfg,
		signer: signer,
		rest:   restClient,
		logger: cfg.Logger,
	}, nil
}

// Config returns a copy of the resolved Config (after defaults applied).
func (c *Client) Config() Config { return c.cfg }

// Logger returns the configured logger. Useful for the same logger to be
// reused by a desk-side adapter.
func (c *Client) Logger() Logger { return c.logger }

// Signer is exposed to internal SDK sub-packages (mix, spot, uta) so they
// can sign WS auth payloads. User code SHOULD NOT touch it.
func (c *Client) Signer() *auth.Signer { return c.signer }

// REST is exposed to internal SDK sub-packages.
func (c *Client) REST() *rest.Client { return c.rest }

// Close releases idle HTTP connections. WS connections owned by domain
// sub-clients close on their own contexts.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.rest.Close()
	return nil
}

// ----------------------------------------------------------------------------
// Sub-client factories (registered by domain packages via init).
// ----------------------------------------------------------------------------

// mixFactory is set by mix.init() via RegisterMixFactory.
var mixFactory func(c *Client) any

// RegisterMixFactory wires the mix.Client builder. Idempotent —
// only the first call is honoured.
func RegisterMixFactory(f func(c *Client) any) {
	if mixFactory == nil {
		mixFactory = f
	}
}

// Mix returns the *mix.Client (typed as any for import-cycle reasons).
// nil when the mix package has not been imported.
func (c *Client) Mix() any {
	c.mixOnce.Do(func() {
		if mixFactory == nil {
			c.logger.Warn(`bitget.Client.Mix: mix factory is not registered; import _ "github.com/tonymontanov/go-bitget/v2/mix"`)
			return
		}
		c.mixVal = mixFactory(c)
	})
	return c.mixVal
}

// spotFactory is set by spot.init() via RegisterSpotFactory.
var spotFactory func(c *Client) any

// RegisterSpotFactory wires the spot.Client builder. Idempotent. Available
// from v2.0; v1.0 ships only the mix profile.
func RegisterSpotFactory(f func(c *Client) any) {
	if spotFactory == nil {
		spotFactory = f
	}
}

// Spot returns the *spot.Client (typed as any). nil when the spot package
// has not been imported. Available from v2.0.
func (c *Client) Spot() any {
	c.spotOnce.Do(func() {
		if spotFactory == nil {
			c.logger.Warn(`bitget.Client.Spot: spot factory is not registered; available from v2.0`)
			return
		}
		c.spotVal = spotFactory(c)
	})
	return c.spotVal
}

// utaFactory is set by uta.init() via RegisterUTAFactory.
var utaFactory func(c *Client) any

// RegisterUTAFactory wires the uta.Client builder. Idempotent. Available
// from v2.5; v1.0 / v2.0 ship only the legacy mix / spot profiles.
func RegisterUTAFactory(f func(c *Client) any) {
	if utaFactory == nil {
		utaFactory = f
	}
}

// UTA returns the *uta.Client (typed as any). nil when the uta package
// has not been imported. Available from v2.5.
func (c *Client) UTA() any {
	c.utaOnce.Do(func() {
		if utaFactory == nil {
			c.logger.Warn(`bitget.Client.UTA: uta factory is not registered; available from v2.5`)
			return
		}
		c.utaVal = utaFactory(c)
	})
	return c.utaVal
}

// Compile-time assertion: *Error implements the error interface.
var _ error = (*bgerr.Error)(nil)
