/*
FILE: config.go

DESCRIPTION:
Public SDK configuration — REST/WS endpoints, timeouts, reconnect policy,
orderbook tuning, observer hooks. Default values match the production
Bitget endpoints and conservative HFT-friendly timeouts.

ENDPOINTS (defaults, v1.0):

	REST:           https://api.bitget.com
	WS public:      wss://ws.bitget.com/v2/ws/public
	WS private:     wss://ws.bitget.com/v2/ws/private
	WS UTA public:  wss://ws.bitget.com/v3/ws/public    (demo: wss://wspap.bitget.com/v3/ws/public)
	WS UTA private: wss://ws.bitget.com/v3/ws/private   (demo: wss://wspap.bitget.com/v3/ws/private)

A SINGLE WS PRIVATE ENDPOINT serves every contract type (USDT-FUTURES,
USDC-FUTURES, COIN-FUTURES) — auth is per-UID, not per-product, so the
SDK does not split it by profile. Spot and UTA introduce additional
endpoints in v2.0 / v2.5.

The V3 (UTA) WebSocket lives on its own pair of endpoints (WS.UTAPublicURL
/ WS.UTAPrivateURL) and is the only WS surface with a DEMO host: when
Config.Demo is true and the UTA URLs are left empty they resolve to the
wspap.bitget.com pair. The V2 endpoints have no demo counterpart.
*/

package bitget

import "time"

// Bitget endpoints. Declared as vars so tests can override them
// (e.g. point at a mock server).
var (
	// DefaultRestBaseURL — production REST endpoint.
	DefaultRestBaseURL string = "https://api.bitget.com"

	// DefaultWsPublicURL — production public WS endpoint (v2 protocol).
	// Serves market data for every contract product type and for spot.
	DefaultWsPublicURL string = "wss://ws.bitget.com/v2/ws/public"

	// DefaultWsPrivateURL — production private WS endpoint (login required).
	DefaultWsPrivateURL string = "wss://ws.bitget.com/v2/ws/private"

	// DefaultWsUTAPublicURL — production V3 (UTA) public WS endpoint. Used
	// by uta.StreamClient; the V2 profiles keep DefaultWsPublicURL.
	DefaultWsUTAPublicURL string = "wss://ws.bitget.com/v3/ws/public"

	// DefaultWsUTAPrivateURL — production V3 (UTA) private WS endpoint
	// (login required).
	DefaultWsUTAPrivateURL string = "wss://ws.bitget.com/v3/ws/private"

	// DefaultWsPublicURLDemo — DEMO public WS endpoint (UTA paper trading).
	// Picked for WS.UTAPublicURL when Config.Demo is true and the field is
	// empty.
	DefaultWsPublicURLDemo string = "wss://wspap.bitget.com/v3/ws/public"

	// DefaultWsPrivateURLDemo — DEMO private WS endpoint (UTA paper trading).
	// Picked for WS.UTAPrivateURL when Config.Demo is true and the field is
	// empty. Requires a Demo API Key.
	DefaultWsPrivateURLDemo string = "wss://wspap.bitget.com/v3/ws/private"
)

// Config — public SDK configuration. Pass to NewClient.
type Config struct {
	// APIKey — Bitget public API key. Required for signed endpoints; safe
	// to leave empty for public-only access.
	APIKey string
	// SecretKey — Bitget secret used to compute ACCESS-SIGN.
	SecretKey string
	// Passphrase — Bitget API passphrase (set when the key was created).
	// Required by every signed call alongside the signature.
	Passphrase string

	// REST — REST transport settings. Empty fields fall back to defaults.
	REST RestConfig
	// WS — WebSocket transport settings. Empty fields fall back to defaults.
	WS WsConfig
	// Orderbook — orderbook engine settings. Empty fields fall back to
	// defaults.
	Orderbook OrderbookConfig

	// Logger — optional logger. NoopLogger if nil.
	Logger Logger
	// Metrics — optional counter factory. NoopMetrics if nil.
	Metrics CounterFactory

	// UserAgent — User-Agent value sent on REST requests. Default
	// "go-bitget/1".
	UserAgent string

	// Demo — when true, SIGNED V3 (UTA) requests carry the `paptrading: 1`
	// header so Bitget routes them to the DEMO TRADING (paper) environment.
	// Demo trading runs on the SAME production host with a dedicated Demo
	// API Key (create one in the web UI under Demo mode). Available from
	// v2.5; it is a UTA(V3), account-scoped concept, so the header is sent
	// ONLY on signed /api/v3/* calls — public market data is environment-
	// agnostic and several public/common endpoints (server time,
	// announcements) actually 40404 when the header is present. The UTA WS
	// follows the same switch: with Demo set and WS.UTAPublicURL /
	// WS.UTAPrivateURL left empty, the UTA stream client connects to
	// wss://wspap.bitget.com/v3/ws/... instead of the production host.
	Demo bool

	// RateLimitObserver — legacy observer (endpoint, headers). Kept for
	// source-level back-compat with the OKX-style pattern. nil → no-op.
	RateLimitObserver func(endpoint string, headers map[string]string)

	// RateLimitEventObserver — primary observer. Receives the full
	// RateLimitEvent with OrderCount/Symbols/Category/Headers.
	//
	// Speed contract: called synchronously from the goroutine that issued
	// the REST call. Implementations must be O(1) (typically a
	// non-blocking send to a buffered channel).
	//
	// nil → no-op.
	RateLimitEventObserver func(RateLimitEvent)
}

// RestConfig — REST transport parameters.
type RestConfig struct {
	// BaseURL — REST host. Default DefaultRestBaseURL.
	BaseURL string
	// RequestTimeout — global timeout for one REST call. Default 10s.
	// A ctx with its own deadline overrides this for a single request.
	RequestTimeout time.Duration
	// MaxIdleConns — http.Transport pool size. Default 100.
	MaxIdleConns int
	// MaxIdleConnsPerHost — per-host pool size. Default 100.
	MaxIdleConnsPerHost int
	// IdleConnTimeout — keep-alive idle timeout. Default 90s.
	IdleConnTimeout time.Duration
	// Locale — value of the "locale" header. Bitget uses it to localise
	// `msg` strings on errors. Default "en-US"; set to "" to omit the
	// header entirely.
	Locale string
}

// WsConfig — WebSocket transport parameters.
type WsConfig struct {
	// PublicURL — public WS endpoint URL. Empty value picks the production
	// default (DefaultWsPublicURL).
	PublicURL string
	// PrivateURL — private WS endpoint URL. Empty value picks the production
	// default (DefaultWsPrivateURL).
	PrivateURL string

	// UTAPublicURL — V3 (UTA) public WS endpoint URL, used by
	// uta.StreamClient. Empty value picks DefaultWsUTAPublicURL, or
	// DefaultWsPublicURLDemo when Config.Demo is true. DefaultConfig leaves
	// it EMPTY on purpose so that flipping Demo after DefaultConfig() still
	// selects the demo host.
	UTAPublicURL string
	// UTAPrivateURL — V3 (UTA) private WS endpoint URL (login required).
	// Empty value picks DefaultWsUTAPrivateURL, or DefaultWsPrivateURLDemo
	// when Config.Demo is true. Left empty by DefaultConfig (see
	// UTAPublicURL).
	UTAPrivateURL string

	// HandshakeTimeout — TLS+HTTP upgrade timeout. Default 10s.
	HandshakeTimeout time.Duration
	// ReadTimeout — read deadline. Default 35s; the server's idle timeout
	// is 30s, so this gives one full ping cycle of slack.
	ReadTimeout time.Duration
	// WriteTimeout — write deadline. Default 5s.
	WriteTimeout time.Duration
	// PingInterval — interval between application-level "ping" frames.
	// Default 20s. Bitget's server-side idle timeout is 30s; pinging at
	// 20s leaves comfortable margin.
	PingInterval time.Duration

	// LoginTimeout — how long to wait for the login ack on the
	// private WS endpoint. Default 15s. The previous default (5s)
	// was tight on overlay networks (Cloudflare WARP / VPN
	// split-tunnels routing through 198.18.0.0/15) where the RTT
	// to ws.bitget.com is regularly 2-3s; 15s leaves room for a
	// retry without false-positive reconnects.
	LoginTimeout time.Duration

	// ReconnectInitialBackoff — first sleep after a connection failure.
	// Default 200ms.
	ReconnectInitialBackoff time.Duration
	// ReconnectMaxBackoff — backoff cap. Default 10s.
	ReconnectMaxBackoff time.Duration
	// ReconnectJitter — relative jitter [0..1] applied to backoff.
	// Default 0.2.
	ReconnectJitter float64

	// ReadBufferSize / WriteBufferSize — gorilla/websocket buffer sizes.
	// Defaults: 64KB / 16KB.
	ReadBufferSize  int
	WriteBufferSize int
}

// OrderbookConfig — orderbook engine parameters. Used by the M2 engine
// (added in a later milestone); settings are exposed in M0 so the public
// surface is stable from the start.
type OrderbookConfig struct {
	// MaxDepth — depth of the local order book per side. Default 200.
	// The "books" channel ships the full book; "books5" / "books15" cap
	// it at 5 / 15 and require no engine logic.
	MaxDepth int
}

// DefaultConfig returns a Config pre-populated with production endpoints
// and HFT-friendly timeouts. Callers can override individual fields and
// pass the result to NewClient — empty sub-fields fall back to these
// defaults.
//
// WS.UTAPublicURL / WS.UTAPrivateURL are deliberately NOT pre-populated:
// their default depends on Config.Demo, which callers typically set AFTER
// DefaultConfig(). They are resolved by NewClient (see withDefaults).
func DefaultConfig() Config {
	return Config{
		REST: RestConfig{
			BaseURL:             DefaultRestBaseURL,
			RequestTimeout:      10 * time.Second,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			Locale:              "en-US",
		},
		WS: WsConfig{
			PublicURL:               DefaultWsPublicURL,
			PrivateURL:              DefaultWsPrivateURL,
			HandshakeTimeout:        10 * time.Second,
			ReadTimeout:             35 * time.Second,
			WriteTimeout:            5 * time.Second,
			PingInterval:            20 * time.Second,
			LoginTimeout:            30 * time.Second,
			ReconnectInitialBackoff: 200 * time.Millisecond,
			ReconnectMaxBackoff:     10 * time.Second,
			ReconnectJitter:         0.2,
			ReadBufferSize:          64 * 1024,
			WriteBufferSize:         16 * 1024,
		},
		Orderbook: OrderbookConfig{
			MaxDepth: 200,
		},
		Logger:    NoopLogger(),
		Metrics:   NoopMetrics(),
		UserAgent: "go-bitget/1",
	}
}

// withDefaults returns a copy of c with empty fields filled from
// DefaultConfig. Already-set explicit URLs/values are preserved.
func (c Config) withDefaults() Config {
	var def Config = DefaultConfig()

	// REST.
	if c.REST.BaseURL == "" {
		c.REST.BaseURL = def.REST.BaseURL
	}
	if c.REST.RequestTimeout == 0 {
		c.REST.RequestTimeout = def.REST.RequestTimeout
	}
	if c.REST.MaxIdleConns == 0 {
		c.REST.MaxIdleConns = def.REST.MaxIdleConns
	}
	if c.REST.MaxIdleConnsPerHost == 0 {
		c.REST.MaxIdleConnsPerHost = def.REST.MaxIdleConnsPerHost
	}
	if c.REST.IdleConnTimeout == 0 {
		c.REST.IdleConnTimeout = def.REST.IdleConnTimeout
	}
	// REST.Locale: an empty string is a legitimate "omit header" choice.
	// We only fill the default when the field is the zero value of a
	// completely-unset Config.
	if c.REST.Locale == "" {
		c.REST.Locale = def.REST.Locale
	}

	// WS.
	if c.WS.PublicURL == "" {
		c.WS.PublicURL = def.WS.PublicURL
	}
	if c.WS.PrivateURL == "" {
		c.WS.PrivateURL = def.WS.PrivateURL
	}
	// UTA (V3) endpoints: production by default, the wspap demo host when
	// Demo is set. An explicit URL always wins (tests point it at a mock).
	if c.WS.UTAPublicURL == "" {
		c.WS.UTAPublicURL = DefaultWsUTAPublicURL
		if c.Demo {
			c.WS.UTAPublicURL = DefaultWsPublicURLDemo
		}
	}
	if c.WS.UTAPrivateURL == "" {
		c.WS.UTAPrivateURL = DefaultWsUTAPrivateURL
		if c.Demo {
			c.WS.UTAPrivateURL = DefaultWsPrivateURLDemo
		}
	}
	if c.WS.HandshakeTimeout == 0 {
		c.WS.HandshakeTimeout = def.WS.HandshakeTimeout
	}
	if c.WS.ReadTimeout == 0 {
		c.WS.ReadTimeout = def.WS.ReadTimeout
	}
	if c.WS.WriteTimeout == 0 {
		c.WS.WriteTimeout = def.WS.WriteTimeout
	}
	if c.WS.PingInterval == 0 {
		c.WS.PingInterval = def.WS.PingInterval
	}
	if c.WS.LoginTimeout == 0 {
		c.WS.LoginTimeout = def.WS.LoginTimeout
	}
	if c.WS.ReconnectInitialBackoff == 0 {
		c.WS.ReconnectInitialBackoff = def.WS.ReconnectInitialBackoff
	}
	if c.WS.ReconnectMaxBackoff == 0 {
		c.WS.ReconnectMaxBackoff = def.WS.ReconnectMaxBackoff
	}
	if c.WS.ReconnectJitter == 0 {
		c.WS.ReconnectJitter = def.WS.ReconnectJitter
	}
	if c.WS.ReadBufferSize == 0 {
		c.WS.ReadBufferSize = def.WS.ReadBufferSize
	}
	if c.WS.WriteBufferSize == 0 {
		c.WS.WriteBufferSize = def.WS.WriteBufferSize
	}

	if c.Orderbook.MaxDepth == 0 {
		c.Orderbook.MaxDepth = def.Orderbook.MaxDepth
	}

	if c.Logger == nil {
		c.Logger = NoopLogger()
	}
	if c.Metrics == nil {
		c.Metrics = NoopMetrics()
	}
	if c.UserAgent == "" {
		c.UserAgent = def.UserAgent
	}

	return c
}

// validate ensures the minimal set of required fields is present.
// Credentials are NOT enforced here — public endpoints work without keys
// and the signer surfaces auth.ErrSignerDisabled at call time.
func (c Config) validate() error {
	if c.REST.BaseURL == "" {
		return NewError(ErrorKindInvalidRequest, "", "config: REST.BaseURL is empty", nil)
	}
	return nil
}
