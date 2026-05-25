# Changelog

All notable changes to `github.com/tonymontanov/go-bitget/v2` are documented
here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v1.0.0 — 2026-05-26

First production-grade release of the SDK. The **MIX (USDT-margined
perpetuals)** category is feature-complete; spot and UTA are deferred
to v2.0 / v2.5.

### Added

- **REST market-data** (`mix.MarketDataClient`): `GetSymbolInfo`,
  `GetOrderBook`, `GetMarketTicker`, `GetHistoricalCandles`
  (+ 1m shortcut). All endpoints exposed under `client.Mix().MarketData()`.
- **REST trading** (`mix.TradingClient`): `CreateOrder`, `ModifyOrder`,
  `CancelOrder`, batch place / modify / cancel (≤50 rows),
  `CancelAllOrders` (per productType + marginCoin), `CancelForgottenOrders`
  (forced cleanup using server-side state). Client-side validation
  (size > 0, price > 0 on limit, identifier required for amend / cancel),
  per-row clientOid pairing in batches, RateLimitEvent meta filled with
  category + OrderCount.
- **REST account / position** (`mix.AccountClient`): `GetAccount`,
  `GetPosition` (single-leg, zero-row filter), `GetOpenOrders` (cursor
  pagination, hard ceiling 10 × 100 rows), `GetOrderDetail` (orderId xor
  clientOid), `ClosePosition` (one-way mode, market close), `SetLeverage`,
  `SetPositionMode`.
- **Public WebSocket** (`mix.StreamClient`): `WatchOrderbook` (full L2
  book maintained locally with top-25 CRC32 validation, dirty-on-mismatch
  + auto re-subscribe round-trip), `WatchTicker`, `WatchTrades` (per-tick
  fan-out), `WatchKline` (`candle{tf}` channel). Single lazy-init `*ws.Conn`
  multiplexes every public subscription; per-`Watch*` `ctx` scopes the
  subscription, not the connection.
- **Private WebSocket** (`mix.StreamClient`): `WatchOrders`,
  `WatchPositions`, `WatchAccount`. Lazily-dialed signed `*ws.Conn`,
  per-row fan-out, auth pre-flight returning `ErrorKindAuth` when the
  signer has no credentials. Reconnect, re-login and re-subscribe are
  transparent.
- **Error mapping** (`internal/bgerr/codes.go`): ~115 Bitget V2 codes
  mapped to `Auth` / `InvalidRequest` / `RateLimit` / `Network` /
  `Exchange`. Covers full lifecycles (auth, IP, passphrase, account
  state, derivative param formatting, order CRUD, amend ergonomics,
  position-mode lock, leverage validation, risk / quantity / price /
  value caps, transient maintenance, withdrawal-adjacent rate-limit).
- **Examples** under [`examples/`](./examples):
  - `marketdata` — public REST + WS depth printer (no creds).
  - `place-order` — signed REST: place a post-only LIMIT 5 % below ask,
    inspect, cancel. Demonstrates typed-error branching.
  - `private-stream` — signed WS: orders / positions / account pushes
    for a symbol.

### Changed

- **BREAKING (vs. pre-v1 dev branch):** Bitget code `40037` is now
  classified as `ErrorKindAuth` (`"Apikey does not exist"`) instead of
  `ErrorKindInvalidRequest`. The previous mapping was a documentation
  bug — the canonical "Order does not exist" code is `40768`. Callers
  that branched on `IsInvalidRequest` for `40037` will now see `IsAuth`
  fire instead, which is the correct behaviour (a missing API key is an
  auth failure, not a request validation issue).

### Notes for downstream integrators

- `mix.Client` requires a parent `bitget.Client` and is constructed by
  `NewClientWithSettings` (or the convenience `client.Mix()` accessor,
  which uses the SDK defaults: `USDT-FUTURES` / crossed / `USDT`).
- All SDK methods accept `context.Context` as the first parameter.
- `RateLimitEventObserver` fires synchronously after every REST request
  (success or exchange rejection). Implementations must be O(1).
- Idempotent-success codes (`22002` / `40814` / `45054` "no change in
  leverage") are deliberately mapped to `ErrorKindInvalidRequest`. The
  SDK does NOT silently turn errors into successes — callers that want
  idempotent semantics must inspect `BitgetCode` explicitly.

### Tested with

- Go 1.24
- `github.com/gorilla/websocket` v1.5.3
- `github.com/json-iterator/go` v1.1.12
- `github.com/shopspring/decimal` v1.4.0

### Roadmap

- **v2.0** — `spot/` profile (Trading / Account / MarketData / Stream
  mirroring `mix/`).
- **v2.5** — `uta/` profile (V3 Unified Trading Account, hedge mode,
  demo / testnet endpoints).
