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

A SINGLE WS PRIVATE ENDPOINT serves every contract type (USDT-FUTURES,
USDC-FUTURES, COIN-FUTURES) — auth is per-UID, not per-product, so the
SDK does not split it by profile. Spot and UTA introduce additional
endpoints in v2.0 / v2.5.

TESTNET / DEMO are out of v1.0 scope (see project plan); the URL
constants are NOT shipped. They will be added together with the
demo/testnet support in v2.5.
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

	// LoginTimeout — how long to wait for the login ack. Default 5s.
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
			LoginTimeout:            5 * time.Second,
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
