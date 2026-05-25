/*
Package bitget is a high-performance Go SDK for the Bitget exchange API,
targeting HFT / algorithmic trading.

The package is organised as a domain-based "fat" client with a layered
type system:

  - bitget.Client    — root SDK object: REST transport, signer, config,
    logger; lazily exposes domain sub-clients.
  - mix.Client       — MIX category (USDT-margined, USDC-margined and
    coin-margined perpetuals on the legacy V2 / MIX
    endpoints). Exposes Trading / Account /
    MarketData / Stream sub-clients. Default in v1.0.
  - spot.Client      — Spot category (added in v2.0). Same shape as mix.
  - uta.Client       — UTA / V3 unified trading account (added in v2.5).
    Adds two-way (hedge) mode and the new V3 endpoint
    family.

Type layout:

  - github.com/tonymontanov/go-bitget/v2/types       layer 1 — protocol-common
    types reused by every profile (Side / OrderType / TIF /
    OrderBookLevel / Snapshot / Candle / Timeframe / TradeUpdate /
    KlineUpdate / CancelOrderRequest / Balance / CoinBalance / ProductType).
  - github.com/tonymontanov/go-bitget/v2/mix/types   layer 2 (profile)
    — alias re-exports of layer 1 + mix-only types
    (PositionMode, MarginMode, SymbolInfo, OrderInfo,
    Create/Modify Request, ExecutionInfo, TickerUpdate,
    PositionInfo, BatchOrderResult).

The two profile packages (when both ship) are siblings: neither imports
the other; both import only the neutral layer-1 package. Mixing
derivatives methods into the spot client (or vice versa) is impossible
by construction.

Errors are typed as *bitget.Error (Kind = Network|RateLimit|Auth|
InvalidRequest|Exchange|Unknown). Callers branch on bitget.IsRateLimit /
bitget.IsAuth / etc. The Bitget code is preserved in Error.BitgetCode
for debugging and contract tests.

Rate-limiting is exposed via Config.RateLimitEventObserver: every REST
response yields one RateLimitEvent with the path, the X-RateLimit-*
headers and structured RequestMeta (OrderCount/Symbols/Category) so an
external rate-limiter can model Bitget's per-(UID+Symbol) and
sub-account-level budgets accurately.

WebSocket streams (orderbook/ticker/orders/positions) reconnect with
exponential backoff + jitter, re-authenticate, and re-subscribe
transparently. The application-level keep-alive (plain-text "ping" every
20s) is built in; users do not interact with it.

The SDK module path is github.com/tonymontanov/go-bitget/v2. Versioning
follows semver:

  - v1.0 — MIX (USDT-margined perpetuals), one-way mode.
  - v2.0 — adds Spot.
  - v2.5 — adds UTA, hedge mode, demo / testnet support.

Quick start:

	import (
	    bitget "github.com/tonymontanov/go-bitget/v2"
	    "github.com/tonymontanov/go-bitget/v2/mix"
	    "github.com/tonymontanov/go-bitget/v2/mix/types"
	)

	func main() {
	    var cfg bitget.Config = bitget.DefaultConfig()
	    cfg.APIKey = "..."
	    cfg.SecretKey = "..."
	    cfg.Passphrase = "..."

	    var c, err = bitget.NewClient(cfg)
	    if err != nil { panic(err) }
	    defer c.Close()

	    var mc = c.Mix().(*mix.Client)
	    _ = mc
	}

End-to-end runnable demos live under examples/.
*/
package bitget
