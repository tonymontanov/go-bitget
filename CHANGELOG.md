# Changelog

All notable changes to `github.com/tonymontanov/go-bitget/v2` are documented
here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v1.2.2 — 2026-05-28

### Changed (internal — no public API change)

- **Profile-agnostic infrastructure extracted from `mix/` into the
  shared `internal/bgcommon` layer** ahead of the v2.0 spot profile.
  The two-layer architecture rule for this SDK is "no parallel
  copy-paste": every helper that is the same on the wire across
  profiles lives in one place and is consumed by mix/, spot/, uta/
  via direct import — never via cross-profile delegation.

  Moved:

  - **`internal/bgcommon/numeric.go`**: `ParseDecimalOrZero`,
    `ParseInt64OrZero`, `ParseIntOrZero`. Bitget V2 ships every
    numeric scalar as a JSON string with empty-as-zero semantics —
    one parser, all profiles.
  - **`internal/bgcommon/restdoer.go`**: `RestDoer` interface
    (the test seam over `*rest.Client.Do`). Was duplicated as
    `mix.restDoer`; spot/uta would have duplicated it again.
  - **`internal/bgcommon/flexstring.go`**: `FlexString` type for
    JSON fields that wire as either quoted string or bare number
    (the `leverage:5` regression we shipped v1.2.1 to fix on the
    `positions` channel — same shape exists on spot account /
    fills, so the type belongs in shared infrastructure).
  - **`internal/bgcommon/orderbook/`** (new sub-package):
    `Engine`, `Level`, `ParseLevels`, `ComputeCRC`, `ErrChecksum`,
    `ErrDirty`. The Bitget V2 "books" CRC32 protocol is identical
    on mix and spot; the engine is now the single source of
    truth, with profile-specific stream wiring built on top.

  `mix/parse-helpers.go`, `mix/rest-doer.go`,
  `mix/orderbook-engine.go`, and the `flexString` definition
  inside `mix/stream-private.go` were deleted in favour of the
  shared symbols. `mix/orderbook_engine_test.go` moved to
  `internal/bgcommon/orderbook/engine_test.go`. All other
  contract-tests pass without modification.

  External callers see no change — every `mix.*` exported symbol
  keeps its name and signature.

### Why patch (not minor)

Public API is unchanged, behaviour is unchanged, wire format is
unchanged. Only the internal layout was refactored, so this is a
patch release per SemVer.

## v1.2.1 — 2026-05-27

### Fixed (high-impact)

- **Private `positions` / `orders` / `account` channels now decode
  numeric-as-number fields** (e.g. `"leverage":5` instead of the
  documented `"leverage":"5"`). Production app.log on PARTIUSDT
  showed every positions push being aborted with
  `mix.wsPositionRow.Leverage: ReadString: expects " or n, but found 5`,
  silently downgrading inventory updates to REST polling (the
  high-frequency desk then logged
  `Too many reconnection attempts, will retry after periodic refresh`).

  The fix mirrors the v1.1.0 `flexCode` strategy: a new `flexString`
  type accepts both quoted-string and JSON-number wire shapes,
  canonicalises to the decimal string the existing
  `parseDecimalOrZero` / `parseInt*OrZero` helpers expect, and is
  applied to **every** numeric / timestamp field on `wsOrderRow`,
  `wsPositionRow`, `wsAccountRow`. Identifier fields (`instId`,
  `orderId`, `clientOid`, `side`, `marginMode`, …) stay strict
  `string` so genuine wire bugs are not masked.

- Test:
  - `TestContract_WatchPositions_AcceptsNumericLeverage` pins the
    exact PARTIUSDT wire shape captured from prod (every numeric
    field — `total`, `available`, `markPrice`, `openPriceAvg`,
    `unrealizedPL`, `leverage`, `cTime`, `uTime` — sent as JSON
    number).

## v1.2.0 — 2026-05-26

### Fixed (high-impact)

- **Private `orders` / `positions` channels now subscribe with
  `instId="default"`, not the symbol.** Bitget V2 ONLY accepts
  `default` for these channels; any actual symbol is rejected with
  `code=30001 "instType:USDT-FUTURES,channel:positions,instId:<sym>,
  precision:null doesn't exist"` (regression seen in PARTIUSDT field
  log under v1.0.4 right after the new login fix surfaced this older
  subscribe bug). Confirmed against
  https://www.bitget.com/api-doc/classic/best-practices and
  `tiagosiebler/bitget-api` (`coin: string = 'default'`).

  The per-symbol public API (`WatchOrders(ctx, symbol, h, eh)` /
  `WatchPositions(ctx, symbol, h, eh)`) is preserved verbatim — the
  SDK now subscribes globally and filters rows client-side inside
  `handleOrdersFrame` / `handlePositionsFrame` by `row.InstID ==
  symbol`. Pass `symbol="default"` to receive every row unfiltered
  (useful for desks fanning out by symbol on their own).

### Added

- **`ModifyBatchOrders` is now a real batch-modify** (was a
  fail-fast stub in v1.1.0). The SDK fans the batch out to single
  `ModifyOrder` RPCs with bounded concurrency
  (`modifyFanOutConcurrency = 5`) and returns a per-row
  `BatchOrderResult` slice in input order — same external contract
  as `CreateBatchOrders` / `CancelBatchOrders`. The wire-level
  endpoint `/api/v2/mix/order/batch-modify-order` still does not
  exist on Bitget V2 (only on V3 / UTA, see
  `/api/v3/trade/batch-modify-order` in `tiagosiebler/bitget-api`),
  but callers no longer need to write the loop themselves. The V2/V3
  cutover will swap the implementation while preserving the
  contract.

  Per-row failure semantics:
  - `results[i].Order != nil` → row succeeded;
  - `results[i].Err != nil` → row failed (typed `*bitget.Error`,
    works with `IsRateLimit` / `IsExchange` / etc. for retry
    decisions);
  - `results[i].ClientOrderID` echoes the request's existing
    clientOid (helpful for mapping results back to the caller's
    idempotency cache).
  - The function-level error is non-nil ONLY for pre-flight
    problems (empty batch, heterogeneous symbols, per-row
    validation).

- Tests:
  - `TestContract_ModifyBatchOrders_FanOutSucceeds` — all-row
    success path + input-order preservation.
  - `TestContract_ModifyBatchOrders_PerRowFailureIsolated` — one
    bad row doesn't poison its neighbours.
  - `TestContract_WatchPositions_FilterDropsForeignSymbol` — locks
    down the per-symbol filter.
  - `TestContract_WatchPositions_DefaultSymbolReceivesAll` —
    unfiltered opt-out.
  - `TestContract_WatchOrders_FieldMapping` and
    `TestContract_WatchPositions_FieldMapping` updated to assert
    `instId="default"` on the wire.

## v1.1.0 — 2026-05-26

### Fixed (high-impact)

- **`ModifyOrder` no longer reuses the existing `clientOid` as the
  `newClientOid`.** The previous behaviour reproduced `code=40786
  Duplicate clientOid` on every modify in production (PARTIUSDT
  field session, v1.0.3). Bitget V2's modify-order endpoint
  implements modify as cancel-replace at the matcher, so the
  resulting order needs a *fresh* customer ID; reusing the old one
  is a guaranteed reject.
  - New field `ModifyOrderRequest.NewClientOrderID` for callers
    that want to own the ID space (idempotency, parent-strategy
    correlation).
  - When left empty, the SDK auto-fills a `m-<32-hex>` token via
    `crypto/rand` so the modify always succeeds without forcing
    every caller to bring their own UUID generator.
  - Caller misuse (passing the same value for both fields)
    short-circuits client-side with `ErrorKindInvalidRequest`
    rather than burning an RTT on a known-doomed request.

- **`ModifyBatchOrders` is now a fast-fail stub on V2.** The
  endpoint `/api/v2/mix/order/batch-modify-order` does not exist
  on Bitget V2 (HTTP 404 / `code=40404 Request URL NOT FOUND`,
  verified in production and against
  https://www.bitget.com/api-doc/contract/trade/Modify-Order which
  lists batch-place / batch-cancel only — no batch-modify). The
  method now returns `ErrorKindInvalidRequest` with remediation
  hints (loop ModifyOrder per row, or cancel-then-place) and does
  NOT issue a doomed HTTP request. Method signature kept stable so
  the caller-side connector interface survives the V2/V3 cutover.

### Removed

- Internal helper `collateBatchResultsFromModify` (dead code after
  the batch-modify stub-out). Restore from git history when Bitget
  ships a real batch-modify endpoint or when the SDK adds the V3
  trade client.

### Added

- `mix/trading.go::genNewClientOid()` — collision-resistant token
  generator (`m-<32-hex>`, crypto/rand, with ns-timestamp fallback
  on RNG failure so modify never silently aborts).
- Test coverage in `mix/trading_contract_test.go`:
  - `TestContract_ModifyOrder_Happy` — explicit NewClientOrderID
    path; asserts `clientOid` and `newClientOid` reach the wire as
    *different* values.
  - `TestContract_ModifyOrder_AutoGeneratedNewClientOid` — empty
    NewClientOrderID + non-empty ClientOrderID; SDK auto-generates
    a distinct token.
  - `TestContract_ModifyOrder_RejectsDuplicateClientOid` — explicit
    misuse path returns InvalidRequest before touching the wire.
  - `TestContract_ModifyBatchOrders_NotSupportedByVenue` — fast-fail
    semantics + remediation message.
  - `TestContract_ModifyBatchOrders_StillValidatesInputs` — empty
    batch keeps validating.

### Migration

- `ModifyOrderRequest`: existing fields unchanged. The new
  `NewClientOrderID` is optional; existing callers continue to
  work (auto-generated token instead of duplicating the old ID).
- `ModifyBatchOrders`: callers that issued batch modifies must
  switch to per-row `ModifyOrder` (or `CancelBatchOrders` +
  `CreateBatchOrders`). The previous code path was already failing
  in production; this only changes WHERE the failure surfaces.

## v1.0.4 — 2026-05-26

### Fixed

- **WS envelope `code` field now accepts JSON-number form.** This was
  the actual reason private-WS login still timed out after v1.0.2 —
  not the timestamp, not the network. The Bitget V2 docs show the
  field as a quoted string (`"code":"0"`), but the live server
  emits it as a JSON number on login and subscribe acks
  (`tiagosiebler/bitget-api` confirms this with a `typeof code ===
  'number'` switch). Our `Envelope.Code string` declaration made
  jsoniter reject the number form; the entire ack envelope failed
  to parse, the dispatcher dropped it as garbage, and the supervisor
  blocked on its read deadline waiting for an ack that had already
  arrived ~300ms after the login op (98-byte frame, observed in
  the field log as `ws: unparseable frame during login wait`).

  New `flexCode` type accepts both shapes and canonicalises to a
  decimal string. The rest of the dispatcher keeps its
  `switch env.Code { case "0": ... }` ergonomics. Push frames
  (which don't carry a code) still parse cleanly.

- **Diagnostic body sample on parse failure.** The
  `ws: unparseable frame during login wait` debug log now includes
  a 200-byte truncated body sample plus the underlying jsoniter
  error, so future schema drift surfaces with the actual wire
  bytes instead of just a length.

### Added

- **Test coverage in `internal/ws/protocol_test.go`** for both
  documented (string) and live (number) shapes of `code`, including
  a numeric error code (30005), the `null` literal, and a push-frame
  smoke test guarding against accidentally breaking the data path
  with the type change.

## v1.0.3 — 2026-05-26

### Changed

- **Default `WS.LoginTimeout` raised from 15s to 30s.** Even after the
  v1.0.2 seconds-precision fix, a small fraction of WARP/VPN sessions
  observed a slower-than-expected login ack (likely first-frame
  buffering through the overlay). 30s is still safe — it only delays
  the reconnect cascade on a genuinely dead route, and the normal
  case lands in <300ms.

### Added

- **Diagnostic log on every login op.** `ws: sending login` now
  records the timestamp string length (10 = seconds = v1.0.2+,
  13 = milliseconds = pre-v1.0.2 binary), the signature length, and
  the expected timestamp length. Operators who suspect the
  application binary is stale can grep one log line to confirm the
  fix is actually present — no need to inspect the wire.
- **Diagnostic logs during the login-ack wait.** Pong frames,
  unparseable frames, and non-login envelopes that arrive between
  the login op and the ack are now traced at debug level. When the
  read deadline expires WITHOUT a single frame seen since connect,
  the wrapped error explicitly calls out the overlay-network drop
  case ("no frames seen since connect ... overlay-network likely
  dropping post-upgrade frames"). This separates "Bitget rejected
  the login" (frames arrive) from "Cloudflare WARP ate the login"
  (no frames arrive) without operator help.

## v1.0.2 — 2026-05-26

### Fixed

- **WS login timestamp now uses SECONDS, not milliseconds.** Production
  logs from the `PARTIUSDT` MIX session showed the private-WS supervisor
  in an unbreakable reconnect loop: every connect succeeded, every
  `op:login` was sent, and every login deadline expired (`login ack not
  received within 15s`). Bitget V2's WS login server hashes the
  pre-image `timestamp + "GET" + "/user/verify"` with **seconds-precision
  timestamps** (per the official docs Java example
  `Long ts = System.currentTimeMillis()/1000;` and the canonical
  `"1538054050"` sample value — 10 digits, not 13). When we sent the
  13-digit ms timestamp the server's HMAC compare failed silently — it
  did **not** return `{"event":"login","code":!=0}`, it just dropped the
  frame. That made the failure indistinguishable from packet loss to
  the client, which then timed out and reconnected forever, so no
  private push (`orders` / `positions` / `account`) ever reached the
  caller. Two symptoms surfaced together for affected operators:
  positions opened in another app didn't appear in WatchPositions, and
  `app.log` was carpeted with `login ack not received within 15s` warns.
  Fix: new `Signer.SecondsTimestamp(now time.Time)` returns the
  10-digit Unix-seconds string, and `internal/ws/conn.go::performLogin`
  switched from `MillisTimestamp` to `SecondsTimestamp`. REST signing
  is unaffected — REST still uses ms per Bitget docs (the WS/REST
  units differ in the official spec, this was the trap).

### Added

- **`Signer.SecondsTimestamp`** — helper for the WS login path. The
  REST helper `MillisTimestamp` remains unchanged. Both helpers have
  cross-references in their godoc.
- **Regression test** `TestSecondsTimestamp` asserting 10-digit
  output and the sub-second-truncation behaviour, so the seconds /
  milliseconds split can't silently re-regress.

## v1.0.1 — 2026-05-26

### Changed

- **Default `WS.LoginTimeout` raised from 5s to 15s.** Production
  logs from operators routing through Cloudflare WARP / VPN
  split-tunnels (where the egress IP lands in 198.18.0.0/15 TEST-NET-2
  ranges) showed the private-WS login ack regularly arriving
  6-9 seconds after the request. The previous 5s default produced
  pathological reconnect loops: every attempt timed out at the read
  deadline, the supervisor reconnected, login was re-sent, timed out
  again, and so on. 15s leaves headroom for one full RTT-doubling on
  overlay networks without slowing down direct-route clients (where
  the ack lands in <300ms). The field is still individually
  configurable.

### Fixed

- **Login-timeout error message clarified.** Previously a login
  read-deadline expiration surfaced as `login: read tcp ...: i/o
  timeout`, indistinguishable from a generic socket failure.
  `performLogin` now detects `net.Error.Timeout() == true` and wraps
  the error with explicit `login ack not received within <duration>
  (raise WS.LoginTimeout or check network/VPN routing)`, so operators
  can immediately tell that the problem is overlay-network latency,
  not bad credentials.

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
