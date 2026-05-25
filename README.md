# go-bitget

High-performance Go SDK for the **Bitget** exchange API, targeting
HFT / algorithmic trading.

Module path: `github.com/tonymontanov/go-bitget/v2`

Latest stable: **v1.0.0-dev** — under active development. See the table
below for the current state of each milestone.

## Status

`v1.0` covers the **MIX (USDT-margined perpetuals)** category end-to-end.
Spot is deferred to v2.0 and the new **UTA (V3)** family to v2.5.

| Module | Status | Notes |
| --- | --- | --- |
| **M0** scaffolding (root client, config, errors, logger, metrics, rate-limit event) | done | unit tests for codec / signer / error mapping / REST transport / WS protocol |
| M0 internal/auth (HMAC-SHA256 base64 for REST + WS) | done | property tests + composition tests covering each axis of the pre-hash |
| M0 internal/bgerr (`Error` / `Kind` / `MapBitgetCode` / `MapHTTPStatus`) | done | table-driven tests |
| M0 internal/rest (Bitget envelope `{code,msg,data,requestTime}`, `ACCESS-*` headers, observers) | done | httptest-based tests for GET / POST / 4xx / 5xx / ctx-cancel |
| M0 internal/ws (Conn, login, plain-text ping, reconnect+jitter, resubscribe, dispatch) | done | mock-server tests for public / private / reconnect / pre-Start subscribe |
| **M1** `mix/` REST core (Trading / Account / MarketData) | pending | CreateOrder / Modify / Cancel / Batch\* / CancelAll / CancelForgottenOrders / GetOpenOrders / GetPosition / GetWalletBalance / SetLeverage / SetMarginMode / GetSymbolInfo / GetOrderBook / GetHistoricalCandles |
| **M2** `orderbook/` engine (snapshot + delta + checksum + resync) | pending | `books` channel CRC32 validation, gap detection |
| **M3** `mix/stream.go` (WS subscriptions) | pending | public: WatchOrderBook / WatchTicker / WatchTrades / WatchKline; private: WatchOrders / WatchPositions / WatchAccount |
| **M4** errors mapping + examples | pending | extended `MapBitgetCode` (40762/40774/45117/40725) with table-driven tests; `examples/` for marketdata, signed trade, WS orderbook |
| **v2.0** `spot/` profile | pending | Trading / Account / MarketData / Stream mirroring `mix/` |
| **v2.5** `uta/` profile + demo / testnet support | pending | V3 endpoints, hedge mode, simulated trading hosts |

## Quick start

```go
import (
    bitget "github.com/tonymontanov/go-bitget/v2"
    "github.com/tonymontanov/go-bitget/v2/mix"
    mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
)

cfg := bitget.DefaultConfig()
cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "...", "...", "..."

client, err := bitget.NewClient(cfg)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

mc := client.Mix().(*mix.Client)

// REST: top of book.
ob, _ := mc.MarketData().GetOrderBook(ctx, "BTCUSDT", 50)

// WS: keep top-of-book in sync (engine-backed).
_ = mc.Stream().WatchOrderBook(ctx, "BTCUSDT", 50, 5,
    func(ob mixtypes.OrderBookSnapshot) { /* ... */ },
    func(err error) { /* ErrorKindInvalidRequest on gap, etc. */ },
)
```

End-to-end runnable demos will live under `examples/` (marketdata,
signed trade, WS orderbook) once `mix/` is implemented in M1.

## Dependencies

```
github.com/json-iterator/go      v1.1.12
github.com/shopspring/decimal    v1.4.0
github.com/gorilla/websocket     v1.5.3
```

The same minimal set used by the sibling `go-bybit` / `go-okx` SDKs.

## Layout

```
go-bitget/
  client.go / config.go / doc.go               # public root API
  errors.go / logger.go / metrics.go / rate-limit-event.go
  internal/
    auth/      — HMAC-SHA256 signing for Bitget REST + WS
    bgerr/     — Error type, ErrorKind, MapBitgetCode / MapHTTPStatus
    bglog/     — Logger interface + Field / NoopLogger
    bgmet/     — Counter / CounterFactory + NoopMetrics
    codec/     — jsoniter wrappers + ParseDecimal / ParseInt64 / RawJSON
    bgcommon/  — domain-agnostic helpers (level/candle parsing) shared
                 by mix/, spot/, uta/
    rest/      — low-level HTTP client, Bitget envelope { code, msg, data, requestTime }
    ws/        — Conn: connect / login / plain-text ping / reconnect+jitter / resubscribe
  types/                  # protocol-common domain types
                          #   Side / OrderType / TIF / OrderStatus /
                          #   ProductType / PositionMode / MarginMode /
                          #   OrderBookLevel / Snapshot / Candle /
                          #   Timeframe / TradeUpdate / KlineUpdate /
                          #   CancelOrderRequest / Balance / CoinBalance
  mix/                    # v1.0 — MIX (USDT-margined perps)
                          #   mix/types: alias re-exports + mix-only types
                          #   (SymbolInfo / OrderInfo / Create/Modify /
                          #    ExecutionInfo / TickerUpdate / PositionInfo /
                          #    BatchOrderResult)
  spot/                   # v2.0 — Bitget spot category (planned)
  uta/                    # v2.5 — Unified Trading Account (planned)
  orderbook/              # M2 — profile-agnostic engine (planned)
  examples/               # runnable end-to-end demos (planned)
  scripts/run.sh          # loads .env and forwards to `go run`
```

## Architecture (brief)

Domain-based: the user receives a "fat" sub-client per profile
(`mix.Client`, `spot.Client`, `uta.Client`). Each profile exposes four
domain sub-clients:

- `Trading()`     — Create/Modify/Cancel + Batch* + CancelAllOrders + CancelForgottenOrders.
- `Account()`     — Wallet balance, positions, open orders, leverage, margin-mode, ClosePosition.
- `MarketData()`  — Symbol-info, order-book snapshot, historical candles.
- `Stream()`      — Watch* (WebSocket subscriptions).

Low-level transport (`internal/rest`, `internal/ws`, `internal/auth`) is
hidden from the user and shared across every profile.

## Errors

All SDK methods return `*bitget.Error`. Branch on `Kind`:

```go
if bitget.IsRateLimit(err) { /* back off */ }
if bitget.IsAuth(err)       { /* terminate */ }
```

The Bitget code is preserved in `Error.BitgetCode` for debugging.
Selected mapping (see `internal/bgerr/codes.go` for the full table):

| Bitget code | Kind | Notes |
| --- | --- | --- |
| `40001` / `40002` / `40003` / `40005` / `40006` / `40009` / `40011` / `40012` / `40018` | Auth | apikey/secret/passphrase/signature/timestamp/IP-whitelist |
| `40007` / `40017` / `40021` / `40034` / `40037` / `43011` / `45110` / `45117` | InvalidRequest | content-type, params, symbol, order-not-found, qty/price step |
| `40029` / `47001` | RateLimit | IP / UID rate limit |
| `40010` / `40015` / `40725` | Network | transient timeouts / server-side hiccup |
| `40754` / `50067` | Exchange | insufficient position quantity / balance |
| anything else | Exchange | preserved verbatim in `Error.BitgetCode` |

## Rate-limit observer

```go
cfg.RateLimitEventObserver = func(ev bitget.RateLimitEvent) {
    // ev.Endpoint, ev.Method, ev.Headers,
    // ev.OrderCount, ev.Symbols, ev.Category
}
```

The observer fires once per completed REST response (success or
exchange-level rejection) and is invoked synchronously from the
goroutine that issued the request. Implementations must be O(1) — a
non-blocking send to a buffered channel is the typical shape.

The headers map carries `X-RateLimit-Limit` / `X-RateLimit-Remaining` /
`X-RateLimit-Used` / `X-RateLimit-Reset` / `Retry-After` when Bitget
returns them.

## WebSocket

- Public stream:  `wss://ws.bitget.com/v2/ws/public`.
- Private stream: `wss://ws.bitget.com/v2/ws/private`.
- Application-level keep-alive: plain-text TEXT frame body `ping`,
  echoed back as `pong` (every 20s by default).
- Login payload (private):

  ```json
  {"op":"login","args":[{"apiKey":"...","passphrase":"...","timestamp":"...","sign":"..."}]}
  ```

  with `sign = base64(HMAC_SHA256(secret, timestamp + "GET" + "/user/verify"))`.

Reconnect, backoff with jitter, resubscribe and login (for private) are
transparent to the caller.

## Code style

- File-level comments and GoDoc are written in English (this is a public
  project).
- Explicit variable declarations: `var name type = value`.
- `camelCase` for local identifiers, `PascalCase` for exported ones.
- `jsoniter` via `internal/codec` for hot-path JSON; `encoding/json` is
  not used directly.
- Every method takes `context.Context` as the first parameter; passing
  `context.Background()` inside a method that already has a `ctx` is
  forbidden.

## License

See `LICENSE` (Apache-2.0).
