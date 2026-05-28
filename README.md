# go-bitget

High-performance Go SDK for the **Bitget** exchange API, targeting
HFT / algorithmic trading.

Module path: `github.com/tonymontanov/go-bitget/v2`

Latest stable: **v1.2.2** — production-ready MIX (USDT-margined perps).
Latest milestone: **v2.0.0-m4** — `spot/` public WebSocket (books / ticker / trade / candles); M5 fills in private WS.
See [`CHANGELOG.md`](./CHANGELOG.md) for release notes.

## Status

`v1.0` covers the **MIX (USDT-margined perpetuals)** category end-to-end.
Spot work has begun under **v2.0** (scaffolding shipped in v2.0.0-m1).
The new **UTA (V3)** family is deferred to v2.5.

| Module | Status | Notes |
| --- | --- | --- |
| **M0** scaffolding (root client, config, errors, logger, metrics, rate-limit event) | done | unit tests for codec / signer / error mapping / REST transport / WS protocol |
| M0 internal/auth (HMAC-SHA256 base64 for REST + WS) | done | property tests + composition tests covering each axis of the pre-hash |
| M0 internal/bgerr (`Error` / `Kind` / `MapBitgetCode` / `MapHTTPStatus`) | done | table-driven tests; ~115 V2 codes mapped across Auth/Invalid/RateLimit/Network/Exchange |
| M0 internal/rest (Bitget envelope `{code,msg,data,requestTime}`, `ACCESS-*` headers, observers) | done | httptest-based tests for GET / POST / 4xx / 5xx / ctx-cancel |
| M0 internal/ws (Conn, login, plain-text ping, reconnect+jitter, resubscribe, dispatch) | done | mock-server tests for public / private / reconnect / pre-Start subscribe |
| **M1** `mix/` REST core + market-data | done | `client.Mix()` factory, MIX `MarketDataClient` (GetSymbolInfo / GetOrderBook / GetMarketTicker / GetHistoricalCandles + 1m shortcut). |
| **M2** `mix/trading.go` (REST trading) | done | CreateOrder / ModifyOrder / CancelOrder + batch (place / modify / cancel, ≤50 rows) + CancelAllOrders (global, by productType+marginCoin). Client-side validation (size>0, price>0 on limit, identifier required on modify/cancel), per-row clientOid pairing in batches, RateLimitEvent meta filled with category + OrderCount. `mix.Client` now takes a `ClientSettings{ProductType, MarginMode, MarginCoin}` triple at construction. |
| **M3** `mix/account.go` (REST account) | done | GetAccount (`/accounts`, filtered by pinned marginCoin) / GetPosition (`/single-position`, zero-row filter, single non-empty leg) / GetOpenOrders (`/orders-pending`, internal cursor pagination via `idLessThan`, hard ceiling 10 pages × 100 orders) / GetOrderDetail (`/detail`, dispatches by orderId xor clientOid) / ClosePosition (`/close-positions`, market close in one-way mode; per-row failure → typed exchange error) / SetLeverage (`/set-leverage`, one-way mode) / SetPositionMode (`/set-position-mode`, account-global). |
| **M4** `mix/stream.go` (public WS + order-book engine) | done | WatchOrderbook (`books` channel: full-depth snapshot + incremental deltas, top-25 CRC32 validation, dirty-on-mismatch + auto-resubscribe round-trip), WatchTicker, WatchTrades (per-tick fan-out), WatchKline (`candle{tf}`); shared lazy-init public `*ws.Conn` multiplexes every channel; per-Watch ctx scopes the subscription, not the connection. |
| **M5** `mix/stream-private.go` (private WS) | done | WatchOrders / WatchPositions / WatchAccount on a lazily-dialed signed `*ws.Conn`; per-row fan-out so the caller handler is invoked once per state change; auth pre-flight returns `ErrorKindAuth` when the signer has no credentials. |
| **v1.0 release** | done | extended error-code coverage (~115 V2 codes); runnable `examples/` (marketdata, place-order, private-stream); `CHANGELOG.md`. |
| **v2.0-m1** `spot/` scaffolding | done | `spot.Client` + Trading / Account / MarketData / Stream sub-client stubs; factory wired into `bitget.Client.Spot()`; smoke tests pin the M1 contract. |
| **v2.0-m2** `spot/` MarketData + Trading REST | done | `MarketDataClient`: `GetSymbolInfo` / `GetOrderBook` (numeric `limit`, 1..150) / `GetMarketTicker` (24h roll-ups) / `GetHistoricalCandles` (+1m). `TradingClient`: `CreateOrder` / `ModifyOrder` / `CancelOrder` + batch (place / **native** modify / cancel, ≤50 rows) + per-symbol `CancelAllOrders` (`/cancel-symbol-order`). Native `batch-cancel-replace-order` (single REST call vs. mix client-side fan-out). `s-<32-hex>` modify-clientOid prefix. `internal/bgcommon` lifted batch + clientOid helpers (`GenClientOid` / `ChooseClientOid` / `BatchEnvelope` / `ValidateBatchSize`); `mix/` rewired through them with byte-stable error messages. Contract tests on a local `httptest.Server` pin every wired endpoint plus the "no productType / marginMode / marginCoin / tradeSide on the spot wire" regression. |
| **v2.0-m3** `spot/` Account + history REST | done | `AccountClient`: `GetAccountInfo` (`/account/info`) / `GetAccount` (`/account/assets`, all coins) / `GetOpenOrders` (`/trade/unfilled-orders`, paginated) / `GetOrderDetail` (POST `/trade/orderInfo`) / `GetOrderHistory` (`/trade/history-orders`, paginated, time-window) / `GetFills` (`/trade/fills`, paginated by tradeId, optional orderID filter). New `bgcommon.PaginateByCursor[T]` generic helper drives every paged call (mix `GetOpenOrders` rewired through it; ceiling message byte-stable). New types: `AccountInfo`, `Fill`. Contract tests pin pagination protocol on a stateful 250-row mock (3 pages: 100+100+50, cursor = last `orderId`). |
| **v2.0-m4** `spot/` public WebSocket | done | `StreamClient`: `WatchOrderbook` (full-depth + CRC32 resync via shared `bgcommon/orderbook.Engine`) / `WatchTicker` (24h roll-ups: `open24h` / `high24h` / `low24h` / `change24h` / ...; no mark/index/funding) / `WatchTrades` (fan-out + buy/sell normalisation) / `WatchKline` (7-element row decoder via `bgcommon.ParseCandleRow`). Lazy `*ws.Conn` over `cfg.WS.PublicURL` (multiplexes spot + future uta on the same socket). New `bgcommon.OrderbookFrame` / `TradeFrame` / `ParseTradeFrame` / `ParseCandleRow` lifted from mix; ticker shape stays profile-local. Subscribe args pin `instType="SPOT"` (regression guard tested on every `Watch*`). |
| **v2.0-m5** `spot/` private WebSocket | pending | account / orders / fills with login + auto-resub via `internal/ws.Conn`. |
| **v2.5** `uta/` profile + demo / testnet support | pending | V3 endpoints, hedge mode, simulated trading hosts |

## Quick start

The MIX profile is wired through `client.Mix()`. Make sure the package
is imported (anonymously is fine) so its `init()` registers the
factory.

```go
import (
    bitget "github.com/tonymontanov/go-bitget/v2"
    "github.com/tonymontanov/go-bitget/v2/mix"
    roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

cfg := bitget.DefaultConfig()
cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "...", "...", "..."

client, err := bitget.NewClient(cfg)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// USDT-margined perpetuals + crossed margin + USDT margin coin (the
// SDK defaults). Use mix.NewClientWithSettings to override any of
// the three knobs (e.g. isolated margin or USDC-FUTURES).
mc := client.Mix().(*mix.Client)

// REST market data — production-ready in M1.
info, _ := mc.MarketData().GetSymbolInfo(ctx, "BTCUSDT")
ob,   _ := mc.MarketData().GetOrderBook(ctx, "BTCUSDT", 50)
tk,   _ := mc.MarketData().GetMarketTicker(ctx, "BTCUSDT")
candles, _ := mc.MarketData().GetHistoricalCandles(ctx, "BTCUSDT",
    roottypes.Timeframe1m, 100)

// REST trading — production-ready in M2.
import "github.com/shopspring/decimal"
import mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"

placed, _ := mc.Trading().CreateOrder(ctx, mixtypes.CreateOrderRequest{
    Symbol:        "BTCUSDT",
    Side:          roottypes.SideTypeBuy,
    OrderType:     roottypes.OrderTypeLimit,
    TimeInForce:   roottypes.TimeInForcePostOnly,
    Quantity:      decimal.RequireFromString("0.001"),
    Price:         decimal.RequireFromString("43500.5"),
    ClientOrderID: "core-uuid-1",
})

_ = mc.Trading().CancelOrder(ctx, roottypes.CancelOrderRequest{
    Symbol:  "BTCUSDT",
    OrderID: placed.OrderID,
})

// REST account / position queries — production-ready in M3.
balance, _ := mc.Account().GetAccount(ctx)
position, _ := mc.Account().GetPosition(ctx, "BTCUSDT")
openOrders, _ := mc.Account().GetOpenOrders(ctx, "BTCUSDT")
_ = mc.Account().SetLeverage(ctx, "BTCUSDT", 10)
_ = mc.Account().SetPositionMode(ctx, roottypes.PositionModeOneWay)

// Public WebSocket streams — production-ready in M4.
//
// All Watch* take a ctx scoping the subscription lifetime, a typed
// handler invoked once per delivered frame, and an errHandler called
// on decode errors / CRC mismatches (nil = log-only). The underlying
// public *ws.Conn is shared across every Watch* call and is lazily
// dialed on the first subscription.
streamCtx, streamCancel := context.WithCancel(ctx)
defer streamCancel()

_ = mc.Stream().WatchOrderbook(streamCtx, "BTCUSDT",
    func(ob roottypes.OrderBookSnapshot) {
        // Full local book — engine has applied snapshot/deltas and
        // validated the Bitget top-25 CRC32; mismatches trigger a
        // transparent unsubscribe→subscribe round-trip.
    },
    nil,
)
_ = mc.Stream().WatchTrades(streamCtx, "BTCUSDT",
    func(t roottypes.TradeUpdate) { /* one TradeUpdate per fill */ },
    nil,
)
_ = mc.Stream().WatchKline(streamCtx, "BTCUSDT", roottypes.Timeframe1m,
    func(k roottypes.KlineUpdate) { /* in-progress 1m candle updates */ },
    nil,
)

// Private WebSocket streams — production-ready in M5.
//
// They run on a separate signed *ws.Conn that is lazily dialed on
// the first private Watch* and performs the V2 login op
// transparently. Calling them without API credentials returns
// ErrorKindAuth.
_ = mc.Stream().WatchOrders(streamCtx, "BTCUSDT",
    func(o mixtypes.OrderInfo) { /* one OrderInfo per state change */ },
    nil,
)
_ = mc.Stream().WatchPositions(streamCtx, "BTCUSDT",
    func(p mixtypes.PositionInfo) { /* size / margin / pnl updates */ },
    nil,
)
_ = mc.Stream().WatchAccount(streamCtx,
    func(b roottypes.Balance) { /* per-margin-coin wallet snapshot */ },
    nil,
)
```

### Spot profile (v2.0.0-m2)

The spot profile mirrors the mix shape: import the package once, then
reach the typed sub-clients via `bitget.Client.Spot()`.

```go
import (
    bitget "github.com/tonymontanov/go-bitget/v2"
    "github.com/tonymontanov/go-bitget/v2/spot"
    spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
    roottypes "github.com/tonymontanov/go-bitget/v2/types"
    "github.com/shopspring/decimal"
)

cfg := bitget.DefaultConfig()
cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "...", "...", "..."

client, _ := bitget.NewClient(cfg)
defer client.Close()

sc := client.Spot().(*spot.Client)

// REST market data — production-ready in v2.0.0-m2.
info,    _ := sc.MarketData().GetSymbolInfo(ctx, "BTCUSDT")
ob,      _ := sc.MarketData().GetOrderBook(ctx, "BTCUSDT", 50)
tk,      _ := sc.MarketData().GetMarketTicker(ctx, "BTCUSDT")
candles, _ := sc.MarketData().GetHistoricalCandles(ctx, "BTCUSDT",
    roottypes.Timeframe1m, 100)

// REST trading — production-ready in v2.0.0-m2.
//
// IMPORTANT: on spot, market BUY orders take Quantity in QUOTE
// (USDT) — Bitget interprets the field as "spend this much USDT
// at market". Limit orders and market SELLs take Quantity in BASE.
// The SDK ships req.Quantity verbatim; conversion (if needed)
// happens one layer up.
placed, _ := sc.Trading().CreateOrder(ctx, spottypes.CreateOrderRequest{
    Symbol:        "BTCUSDT",
    Side:          roottypes.SideTypeBuy,
    OrderType:     roottypes.OrderTypeLimit,
    TimeInForce:   roottypes.TimeInForcePostOnly,
    Quantity:      decimal.RequireFromString("0.001"),
    Price:         decimal.RequireFromString("43500.5"),
    ClientOrderID: "core-uuid-1",
})

// Native batch-cancel-replace — single REST call, no fan-out.
results, _ := sc.Trading().ModifyBatchOrders(ctx, []spottypes.ModifyOrderRequest{
    {Symbol: "BTCUSDT", ClientOrderID: "core-1", NewPrice: decimal.RequireFromString("43500")},
    {Symbol: "BTCUSDT", ClientOrderID: "core-2", NewPrice: decimal.RequireFromString("43600")},
})

_ = sc.Trading().CancelAllOrders(ctx, "BTCUSDT") // per-symbol on spot.
```

End-to-end runnable demos live under [`examples/`](./examples):

- [`examples/marketdata`](./examples/marketdata) — public REST + WS
  orderbook (no creds).
- [`examples/place-order`](./examples/place-order) — signed REST: place
  a post-only LIMIT 5 % below ask, inspect, then cancel.
- [`examples/private-stream`](./examples/private-stream) — signed WS:
  subscribe to orders / positions / account for a symbol.

Run with `go run ./examples/<name>`.

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
                          #   client.go         — *mix.Client + RegisterMixFactory init
                          #   market.go         — REST market-data (M1, done)
                          #   trading.go        — REST trading (M2, done)
                          #   account.go        — REST account/position (M3, done)
                          #   stream.go         — public WS (M4, done)
                          #   orderbook-engine.go — local book + CRC32 (M4)
                          #   stream-private.go — private WS (M5, done)
                          #   types/            — MIX-only domain types
                          #   contract_test.go  — JSON-fixture parser tests
  spot/                   # v2.0 — Bitget spot category (planned)
  uta/                    # v2.5 — Unified Trading Account (planned)
  examples/               # runnable end-to-end demos (v1.0)
                          #   marketdata/      — public REST + WS book
                          #   place-order/     — signed REST trade
                          #   private-stream/  — signed WS streams
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
v1.0 maps **~115 V2 codes**; selected highlights below (see
[`internal/bgerr/codes.go`](./internal/bgerr/codes.go) for the full
table):

| Family | Bitget codes | Kind | Notes |
| --- | --- | --- | --- |
| Auth — credentials / IP / signature | `40001`-`40009` (sig fields), `40011`-`40014` (passphrase / status / perms), `40018` / `40038` (IP whitelist), `40022`-`40026` (account state), `40036` (passphrase error), `40037` (apikey not found), `40040` / `40041` (perm setup) | Auth | terminate; never retry |
| InvalidRequest — params / lifecycle / risk | `40007` / `40017` / `40034` (params), `22001` / `22002` (no-op cancel/close), `40768` (order not exist), `40923` (amend no-change), `40939` (reduce-only conflict), `40920` (position-mode lock), `45034` (clientOid duplicate), `45035` (price step), `45044`-`45045` / `45054` (leverage), `45055`-`45057` (cancel state), `45110`-`45120` (qty/price/value caps) | InvalidRequest | fix request before retry |
| RateLimit | `40029`, `45129` (cancel too frequent), `47001`, `59044` | RateLimit | back off |
| Network — transient | `40010` / `40015`, `40200` (server upgrade), `40725` (service error), `40908`-`40910` (concurrent ops), `45043` (settlement), `50031` (system error), `50066` (position closing) | Network | retryable with backoff |
| Exchange — business rejection | `40754`-`40758` (balance/position locks), `40798`-`40800` (margin/contract balance), `43012` (insufficient balance), `45002`-`45009` (asset/position/risk), `50020` / `50067` | Exchange | desk decides |

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

  **`timestamp` is in SECONDS** (10 digits, e.g. `"1538054050"`) — Bitget V2
  WS deviates from its own REST convention (REST uses milliseconds). Sending
  ms made the server silently drop the login frame, the client hit the read
  deadline, reconnected, re-logged-in, timed out again, ad infinitum. The
  SDK uses `Signer.SecondsTimestamp` for the WS login path and
  `Signer.MillisTimestamp` for REST — fixed in v1.0.2 (see CHANGELOG).

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
