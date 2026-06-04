# Changelog

All notable changes to `github.com/tonymontanov/go-bitget/v2` are documented
here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v2.0.0 — 2026-06-04 (SPOT GA roll-up)

General-availability cut of the **v2.0 SPOT** profile. No new REST/WS
surface beyond `v2.0.0-m6` — this release folds milestones `m1`–`m6`
into a stable line and closes the production-readiness gaps the
milestones left open (runnable spot examples, a live smoke harness, and
an error-code audit). Validated live on `PARTIUSDT` (Chase / CQB Scale)
end-to-end: place → amend → cancel with correct position tracking.

### Fixed

- **`spot.Trading.ModifyOrder` / `ModifyBatchOrders` returned a stale
  `clientOid` after a cancel-replace (live "hanging orders").** Bitget
  spot `cancel-replace-order` / `batch-cancel-replace-order` is a native
  cancel-replace that **rotates the `clientOid`**: the surviving order
  adopts the `newClientOid` we send. The HTTP-200 response, however,
  returns an **empty `orderId`** and **echoes the OLD requested
  `clientOid`** (`{"orderId":"","clientOid":"<old>","success":"success"}`),
  so neither field in the response identifies the live order. The SDK
  selected the echoed old id via `ChooseClientOid`, so callers kept
  addressing a stale id; the next amend/cancel missed the live order
  (venue replied `该订单已成交或者已撤单` / `43001 订单不存在`) and the
  order hung on the book. `ModifyOrder` now returns the `newClientOid`
  and `ModifyBatchOrders` returns `resolvedNewOid[i]` as the result
  `ClientOrderID` — the real post-rotation id — so callers can address
  the order on the next amend/cancel. Verified live on `PARTIUSDT`;
  pinned by `TestContract_Spot_ModifyOrder_ReturnsRotatedClientOid`.

- **`spot.Trading.ModifyBatchOrders` response decoding (parse panic
  "expect { but found [").** `batch-cancel-replace-order` returns a
  **flat array** of per-row outcomes in request order
  (`[{orderId,clientOid,success,msg}]`), NOT the `{successList,
  failureList}` envelope used by `batch-orders` / `batch-cancel-order`.
  Decoding it as `BatchEnvelope` failed every batch modify. Added a
  dedicated array row type + positional collation, and a per-row
  `success":"failure"` now surfaces as a typed row error. `ModifyOrder`
  (single) likewise now inspects the `success` flag so a venue-rejected
  amend on an HTTP-200 envelope returns an error instead of a phantom
  success. Production regression observed on `PARTIUSDT`.
- **`spot.Trading.ModifyOrder` / `ModifyBatchOrders` wire field names
  (code=400172 / 40019).** The cancel-replace bodies serialized the new
  amount/price under `newSize` / `newPrice`, but Bitget V2 spot
  cancel-replace-order (and batch-cancel-replace-order) carries the new
  values under the **bare `size` / `price`** keys — the request is a
  full re-placement. The venue silently ignored the `new*` keys and
  rejected every modify with `code=400172 size...empty / price...empty`
  (single) or `code=40019 size cannot be empty` (batch). Renamed the
  wire fields to `size` / `price`; both are now mandatory and
  `validateModifyOrderRequest` rejects a partial modify locally (was
  "at least one"), so a re-price-only caller gets a clear local error
  instead of a venue round-trip. MIX is unaffected — its modify
  endpoint genuinely uses `newSize` / `newPrice`. Production regression
  observed on `PARTIUSDT`. Contract tests had pinned the wrong keys
  (`newSize` / `newPrice`); they now assert `size` / `price` and that
  the `new*` keys are absent.
- **`spot.Trading.CancelBatchOrders` endpoint path (HTTP 404).** The
  method POSTed to `/api/v2/spot/trade/cancel-batch-orders`, which does
  not exist — Bitget V2 spot batch cancellation lives at
  `/api/v2/spot/trade/batch-cancel-order`. Every batch cancel returned
  `404 Not Found`, so ladder/scale strategies could not tear down their
  open orders in bulk (production regression observed on `PARTIUSDT`:
  desk "orders not cancelled in batch"). Corrected the path; the
  request/response shape (`symbol` + `orderList`, `{successList,
  failureList}` envelope) was already correct. The contract test had
  pinned the wrong path; it now asserts `batch-cancel-order`.
- **`spot.MarketData.GetHistoricalCandles` granularity (code=400171).**
  The spot `/api/v2/spot/market/candles` endpoint rejects the
  `roottypes.Timeframe.Wire()` tokens that MIX uses (`1m` / `1H` / `1D`)
  with `code=400171 Parameter verification failed k-line time range`.
  SPOT requires a different alphabet (`1min` / `1h` / `1day` / `1week`,
  `1M` unchanged). Added a spot-local `spotCandleGranularity` mapping and
  routed `GetHistoricalCandles` through it; MIX is untouched (still uses
  `Wire()`), and the WS candle channel (`candle1m` / `candle1H`) is
  unaffected — it uses the `Wire()` tokens on both profiles. Production
  regression observed on `PARTIUSDT` (desk `candles_updater` failed to
  load initial candles). The existing contract test had pinned the wrong
  expectation (`1m`); it now asserts `1min`, and a new
  `TestSpotCandleGranularity_FullTable` pins the whole alphabet.

### Added

- **Runnable SPOT examples** (mirror the existing MIX demos one-to-one):
  - `examples/spot-marketdata` — public REST (`GetSymbolInfo` /
    `GetMarketTicker` / `GetOrderBook`) + public WS `WatchOrderbook`
    with the shared CRC32 orderbook engine. No credentials required.
  - `examples/spot-place-order` — signed REST trading round-trip: a
    deep post-only LIMIT BUY (`0.95*ask`, cannot cross) →
    `GetOrderDetail` → `CancelOrder`.
  - `examples/spot-private-stream` — signed WS `WatchOrders` /
    `WatchAccount` / `WatchFills` (no positions — cash-only).
  - All signed examples read the section-specific `BITGET_SPOT_*`
    credentials, falling back to the generic `BITGET_*` triple.

- **`examples/spot-smoke` — production-readiness smoke harness.** Runs
  the full go-live checklist against the LIVE API in one shot
  (public REST, public WS, signed REST `GetAccountInfo` / `GetAccount`,
  private WS login, and a post-only trading round-trip) and prints a
  `PASS` / `FAIL` / `SKIP` summary, exiting non-zero on any failure.
  `-read-only` skips the order placement; with no credentials it runs
  the public surface only (useful as a CI connectivity probe). This is
  the operator hand-off point — the SDK never runs live trades on the
  operator's behalf.

### Audited (no change)

- **Error-code coverage (`internal/bgerr`)** — audited against the spot
  REST surface (order lifecycle `43xxx`, balance/wallet `50xxx`,
  risk/quantity `45xxx`, dup-clientOid `50060`). Every code mapped in
  `codes.go` is pinned by a row in `codes_test.go`, and unlisted codes
  fall back to `ErrorKindExchange` by design. No additions were needed.

- **SPOT ↔ MIX API parity** — confirmed: Trading (7 methods),
  MarketData (5), public WS (4) match; the only differences are
  intentional (spot has no positions / leverage / position-mode /
  `ClosePosition`, and adds `GetAccountInfo` / `GetOrderHistory` /
  `GetFills`).

## v2.0.0-m6 — 2026-06-03

Sixth and final milestone of the **v2.0 SPOT** profile. Closes the
mix↔spot symmetry on the private WebSocket surface: spot picks up
the two private channels deferred from M5 (`account`, `fill`) and
mix gains the channel that was missing since v1.0 (`fill`). After
M6 the private surfaces are fully symmetric except `WatchPositions`,
which is mix-only by venue contract (cash-only spot has no positions).

### Added

- **`spot.StreamClient.WatchAccount(ctx, coin, handler, errHandler)`** —
  subscribes to the spot `account` private channel.
  - Wire-level the SDK ALWAYS subscribes with `coin="default"` —
    the only value Bitget V2's spot account channel accepts ("Only
    default is supported now" per the official docs). Per-coin
    semantics are preserved client-side: pass any concrete coin
    (e.g. `"USDT"`) to receive only its rows; pass empty string or
    `"default"` to receive every asset on the account.
  - The handler receives `spottypes.AccountUpdate` — a profile-local
    per-asset shape (`Coin` / `Available` / `Frozen` / `Locked` /
    `LimitAvailable` / `UpdatedAtMs`). Distinct from `mix.WatchAccount`'s
    per-margin-coin `roottypes.Balance`: spot has no positions or
    unrealized PnL, so a shared shape would always-zero half of mix's
    fields or force spot users to ignore meaningless columns. Same
    anti-pattern we avoided for ticker in M4.

- **`spot.StreamClient.WatchFills(ctx, symbol, handler, errHandler)`** —
  subscribes to the spot `fill` private channel.
  - Subscribes with `instId="default"` (per-symbol rejected with
    code=30001, same as orders); per-symbol semantics preserved
    client-side via the dispatcher filter.
  - Handler receives `spottypes.FillUpdate` (`OrderID` / `TradeID` /
    `Symbol` / `Side` / `OrderType` / `PriceAvg` / `Size` / `Amount` /
    `TradeScope` / `FeeDetail[]` / `CreatedAtMs` / `UpdatedAtMs`).
  - **`clientOid` is NOT shipped on spot fills** (verified against
    Bitget V2 docs). Callers that need to correlate fills with the
    desk's idempotency keys MUST go through the `orders` push (which
    DOES carry clientOid) and join on OrderID. This is a venue-level
    invariant — the SDK does not invent a stateful cross-channel join.

- **`mix.StreamClient.WatchFills(ctx, symbol, handler, errHandler)`** —
  subscribes to the mix `fill` private channel.
  - `instId="default"` + client-side symbol filter, identical
    contract to existing mix orders / positions.
  - Handler receives `mixtypes.FillUpdate` — distinct from spot's
    counterpart: mix DOES carry `clientOid` (useful for joining
    fills to the desk's idempotency cache without going through
    orders), plus derivatives-only fields (`PosMode` / `TradeSide` /
    `Profit`). Wire field names are different too (`price` /
    `baseVolume` / `quoteVolume` vs spot's `priceAvg` / `size` /
    `amount`).

- **Profile-local `FillUpdate` types**:
  - `spot/types/fill-update.go` → `spottypes.FillUpdate`
  - `mix/types/fill-update.go` → `mixtypes.FillUpdate`
  - Distinct from the existing REST `Fill` shape on spot
    (`spot/types/fill.go`): the WS push ships a `feeDetail[]` ARRAY
    so a single execution can carry both a primary fee and a BGB-
    deduction credit. REST collapses that into singletons.

- **Profile-local `spottypes.AccountUpdate`** (`spot/types/account-
  update.go`) — per-asset balance shape distinct from mix's per-
  margin-coin `roottypes.Balance`.

### Internal

- **`internal/bgcommon/wsfee.go`** — single source of truth for the
  `feeDetail[]` array on the V2 fill channel. Wire row
  (`WSFeeDetailRow`) uses `bgcommon.FlexString` on every numeric
  field; typed shape (`WSFeeDetail`) maps to `decimal.Decimal`.
  `ParseFeeDetail` / `ParseFeeDetailList` consumed by both
  `spot.convertWSFillRow` and `mix.convertWSFillRow` — the only
  piece of the fill push that has byte-identical wire across both
  profiles, so it lives in shared infrastructure rather than
  parallel-copy-pasting.

- **mix audit pass — back-port of M5 discipline**: `mix/stream-
  private.go` already implemented the M5 contract (errInvalidRequest
  on validation, FlexString on every numeric, detachPrivateOnContext
  Done, surfaceError on parse errors, instId="default"+filter). The
  audit therefore focused on test-coverage parity with the spot M5
  suite, adding three regression guards that mix lacked:
  - `TestContract_WatchOrders_FilterDropsForeignSymbol` — the per-
    symbol filter for orders (mix had it for positions only).
  - `TestContract_WatchOrders_DefaultSymbolReceivesAll` — opt-out
    via `symbol="default"` for orders.
  - `TestContract_WatchOrders_AcceptsNumericFields` — flexString
    regression guard on orders (mix had `WatchPositions_AcceptsNumeric
    Leverage` only).

### Contract tests

- **`spot/stream_private_contract_test.go`** — six new tests:
  - `TestContract_Spot_WatchAccount_FieldMapping` — happy-path per-
    asset row mapping; subscribe arg pinned to `coin="default"`.
  - `TestContract_Spot_WatchAccount_FilterDropsForeignCoin` — the
    client-side per-coin filter on a multi-asset push.
  - `TestContract_Spot_WatchFills_FieldMapping` — happy-path with
    a one-element feeDetail (typical: single fee, no BGB deduction).
  - `TestContract_Spot_WatchFills_FilterDropsForeignSymbol` — same
    per-symbol contract as orders.
  - `TestContract_Spot_WatchFills_AcceptsNumericFields` — pre-emptive
    flexString regression guard on fills.
  - Extended `TestContract_Spot_PrivateChannels_RequireSigner` and
    `TestContract_Spot_StreamPrivateValidation` to table-driven over
    every M5+M6 channel.

- **`mix/stream_private_contract_test.go`** — three new fills tests
  (FieldMapping / FilterDropsForeignSymbol / AcceptsNumericFields)
  plus extended Auth / Validation tables to cover `WatchFills`.

- **`internal/bgcommon/wsfee_test.go`** — unit tests for
  `ParseFeeDetail` / `ParseFeeDetailList`: happy path, JSON-number-
  vs-string acceptance, empty-input contract, error propagation.

### Roadmap

  - **v2.0.0-m6** (this tag): mix↔spot private-WS symmetry. Closes
                  the deferred items from M5 (`spot.WatchAccount`,
                  `spot.WatchFills`) plus the mix gap
                  (`mix.WatchFills`). Audit pass on
                  `mix/stream-private.go` extended the contract-test
                  coverage to M5 levels.
  - **v2.0.0**:    aggregate release — the v2.0 spot profile is now
                  feature-complete on REST + public WS + private WS.
  - **v2.5**:      `uta/` profile (V3 unified trading account, hedge
                  mode, demo / testnet hosts).

## v2.0.0-m5 — 2026-06-03

Fifth milestone of the **v2.0 SPOT** profile. Wires the private
WebSocket surface — specifically the `orders` channel that lifts
`WatchOpenOrders` in `market-making-desk-core`. Other private
channels available on Bitget spot (`account`, `fills`) are
intentionally deferred (see "Not included" below).

### Added

- **`spot.StreamClient.WatchOrders(ctx, symbol, handler, errHandler)`** —
  full order lifecycle feed (place / partial-fill / filled / cancel /
  reject) over the private WS connection.
  - Lazily-constructed signed `*ws.Conn` (`cfg.WS.PrivateURL`),
    separate from the public conn spun up in M4. The supervisor
    performs the V2 login op (`ACCESS-KEY` / `passphrase` /
    `timestamp` / sign over `GET /user/verify` in base64-HMAC,
    `internal/auth.SignWS`) before issuing any subscribe op.
  - On the wire the SDK ALWAYS subscribes with
    `instType="SPOT", channel="orders", instId="default"`. Bitget V2
    rejects per-symbol `orders` subscriptions with `code=30001
    "instId:<sym> doesn't exist"` (regression captured on mix in
    v1.0.4 and now codified for spot too). The per-symbol semantics
    callers expect are preserved client-side via the InstID filter
    inside the dispatcher: pass any concrete symbol to receive only
    its rows; pass `"default"` to opt out of the filter and receive
    every order on the account.
  - Wire row uses `bgcommon.FlexString` for every numeric field —
    Bitget has been seen to ship the same field as a quoted string
    on one push and a JSON number on the next. The PARTIUSDT
    regression that broke mix in May 2026 is regression-tested for
    spot too (`TestContract_Spot_WatchOrders_AcceptsNumericFields`).
  - On reconnect `ws.Conn` re-logins and re-subscribes transparently;
    `StreamClient` never observes a transport reset.

- **Wire row struct `wsOrderRow` (spot)** — distinct from
  `mix.wsOrderRow`: omits `tradeSide` / `posSide` / `marginCoin` /
  `marginMode` / `leverage` / `reduceOnly` (cash-only spot, no
  margin, no positions). Reuses `bgcommon.FlexString`,
  `ParseDecimalOrZero`, `ParseInt64OrZero` — no copy-paste with mix
  past the JSON-tag declarations.

- **Contract tests** extending the M4 mock with a login handler:
  - `TestContract_Spot_WatchOrders_FieldMapping` — happy-path
    field-by-field on a single row.
  - `TestContract_Spot_WatchOrders_FilterDropsForeignSymbol` — two
    rows in one push (`ETHUSDT` + `BTCUSDT`), only the requested
    symbol surfaces to the handler.
  - `TestContract_Spot_WatchOrders_DefaultSymbolReceivesAll` —
    `symbol="default"` opts out of the filter, both rows reach the
    handler.
  - `TestContract_Spot_WatchOrders_AcceptsNumericFields` — flex-
    string regression guard: every numeric field shipped as a JSON
    number instead of a quoted string still decodes cleanly.
  - `TestContract_Spot_PrivateChannels_RequireSigner` — typed
    `ErrorKindAuth` when API credentials are missing.
  - `TestContract_Spot_StreamPrivateValidation` — empty symbol /
    nil handler return `ErrorKindInvalidRequest` BEFORE any network
    activity.

### Not included (intentional, deferred)

- **`WatchPositions`** — spot is cash-only, the channel does not
  exist on Bitget spot. Mix exposes it; spot never will.
- **`WatchAccount`** — per-asset balance pushes ARE shipped by
  Bitget spot, but the wire shape diverges from mix (mix is per-
  margin-coin and bundles unrealized PnL / margin metrics; spot is
  per-asset and bundles only available/frozen). Wiring it cleanly
  requires a profile-local spot `AccountUpdate` shape; deferred to
  v2.0.0-m6.
- **`WatchFills`** — real-time trade fills feed (per-execution).
  Useful for fee accounting and trade-by-trade PnL attribution
  on top of order lifecycle. Both spot and mix expose it; deferred
  to v2.0.0-m6.

### Internal

- **`spot.StreamClient.privateState`** — embedded `privateConnState`
  bundle (lazy `*ws.Conn` + own mutex + `closeOnce`), structurally
  identical to the mix counterpart. Public-side fields stay
  decoupled.
- **`StreamClient.Close()`** now closes both the public and the
  private connection (idempotent).

### Roadmap

  - **v2.0.0-m5** (this tag): spot private WS — `WatchOrders`.
  - **v2.0.0-m6**: close the private-WS gaps —
                   `spot.WatchAccount` (profile-local per-asset
                   `AccountUpdate`), `spot.WatchFills`, plus
                   `mix.WatchFills` for full mix↔spot symmetry on
                   the private surface. Includes an audit pass on
                   `mix/stream-private.go` to back-port the M5
                   discipline (FlexString on every numeric, ctx-
                   cancel coverage, fail-fast input validation).
  - **v2.0.0**:    aggregate release once M6 lands.

## v2.0.0-m4 — 2026-05-28

Fourth milestone of the **v2.0 SPOT** profile. Wires the public
WebSocket surface — the high-throughput feed every market-making
strategy depends on.

### Added

- **`spot.StreamClient`** — four `Watch*` primitives, all multiplexed
  on a single lazily-constructed `*ws.Conn` (`cfg.WS.PublicURL`,
  shared with mix and future uta):
  - `WatchOrderbook(ctx, symbol, handler, errHandler)` →
    channel `books`. Full-depth feed driven by the shared
    `internal/bgcommon/orderbook.Engine` — Bitget CRC32 is
    validated on every applied delta. On checksum mismatch the
    SDK surfaces `ErrChecksum` to `errHandler` and schedules an
    Unsubscribe→Subscribe round-trip in the background; the
    `*ws.Subscription` is reused so the user handler keeps
    receiving frames seamlessly after the resync.
  - `WatchTicker(ctx, symbol, handler, errHandler)` → channel
    `ticker`. Decodes the spot-specific 24h roll-up fields
    (`open24h` / `high24h` / `low24h` / `openUtc` / `change24h` /
    `changeUtc24h` / `baseVolume` / `quoteVolume` / `usdtVolume`)
    plus best bid/ask price + size. Unlike the mix shape there
    are no `markPrice` / `indexPrice` / `fundingRate` fields —
    spot has no equivalent.
  - `WatchTrades(ctx, symbol, handler, errHandler)` → channel
    `trade`. Bitget ships trade batches; the SDK fans them out
    so the handler sees one `roottypes.TradeUpdate` per fill.
    Buy/sell sides are normalised to `roottypes.SideTypeBuy` /
    `SideTypeSell`; unknown values pass through verbatim
    (forward-compat).
  - `WatchKline(ctx, symbol, timeframe, handler, errHandler)` →
    channel `candle{tf}`. 7-element row decoder is shared with
    mix via `bgcommon.ParseCandleRow`. The wire does not flag
    closed bars — the SDK ships `Confirmed=false` uniformly and
    consumers detect closure by comparing `StartMs`.

- **Spot WS subscribe arg pins `instType="SPOT"`** for every
  channel (`books` / `ticker` / `trade` / `candle{tf}`). The
  contract-test suite asserts this on every `Watch*` to prevent
  a future hand-edit from copy-pasting the mix product type.

- **Contract tests** on a local `httptest.Server` upgrading to a
  TEXT-frame WebSocket:
  - `WatchOrderbook` snapshot + delta;
  - checksum-mismatch → unsubscribe + resubscribe round-trip;
  - ticker 24h roll-up field mapping (regression guard against
    accidental mix-shape contamination);
  - trade fan-out + side normalisation;
  - kline row decoding;
  - fail-fast validation on every `Watch*` (empty symbol, nil
    handler, empty timeframe).

### Internal (no public API change)

- **WS wire shapes extracted from `mix/`** ahead of M4 (committed
  separately in `refactor(internal/bgcommon): extract WS books / trade / candle frames`):
  - `bgcommon.OrderbookFrame` — books-channel row decoder.
  - `bgcommon.TradeFrame` — trade-channel row decoder.
  - `bgcommon.ParseTradeFrame(symbol, frame) → TradeUpdate` — generic.
  - `bgcommon.ParseCandleRow(symbol, tf, row) → KlineUpdate` — generic.
  - `mix/stream.go` rewired through them; the `tickerFrame` shape
    stays profile-local because mix carries `markPrice` /
    `indexPrice` / `fundingRate` while spot carries 24h roll-ups
    — a shared shape would force always-zero fields on one side.

### Roadmap

  - **v2.0.0-m4** (this tag): public WebSocket — books / ticker /
                  trade / candles — on `instType="SPOT"`.
  - **v2.0.0-m5**: private WebSocket (account / orders / fills)
                  with login + auto-resub via `internal/ws.Conn`.
  - **v2.0.0**:    aggregate release once M5 lands.

## v2.0.0-m3 — 2026-05-28

Third milestone of the **v2.0 SPOT** profile. Wires the authenticated
account / history REST endpoints. The new `spot.AccountClient` mirrors
the `mix.AccountClient` shape minus the position / leverage surface
(spot has no positions); `mix/` is byte-stable on the wire.

### Added

- **`spot/types/`** — two new namespace structs:
  - `AccountInfo` — meta about the API key's owning account (UserID,
    InviterID, IPs, Authorities, ParentID, TraderType, ChannelCode,
    RegisTimeMs). Used by the desk for boot-time health checks.
  - `Fill` — one trade execution from `/spot/trade/fills` (OrderID,
    TradeID, Symbol, Side, OrderType, FillPrice, Size, Amount,
    TotalFee, FeeCoin, TradeScope, CreatedAtMs).

- **`spot.AccountClient`** — six REST endpoints, all signed:
  - `GetAccountInfo()` → `GET /api/v2/spot/account/info`. Surfaces
    granted authorities + whitelisted IPs for boot-time validation.
  - `GetAccount()` → `GET /api/v2/spot/account/assets`. Returns every
    funded coin in `roottypes.Balance.Coins[]`. Aggregate fields
    (`TotalEquity` / `AvailableBalance`) stay zero because spot
    does not expose them; per-coin `Equity` is synthesised as
    `available + frozen + locked`.
  - `GetOpenOrders(symbol)` → `GET /api/v2/spot/trade/unfilled-orders`.
    Cursor-paginated through `bgcommon.PaginateByCursor` (cursor =
    last `orderId` on the page). `symbol == ""` returns ALL open
    orders for the API key (full-account reconciliation path).
  - `GetOrderDetail(symbol, orderID, clientOID)` →
    **POST** `/api/v2/spot/trade/orderInfo` (the only spot account
    endpoint that uses POST). Symbol is required client-side for
    symmetry with mix and so the rate-limit observer sees a typed
    Symbols list. Either OrderID or ClientOrderID is required;
    OrderID wins when both supplied.
  - `GetOrderHistory(symbol, startMs, endMs)` →
    `GET /api/v2/spot/trade/history-orders`. Cursor-paginated;
    `symbol == ""` returns history across all symbols; `startMs ==
    0` / `endMs == 0` leaves the corresponding bound off (Bitget
    falls back to its default look-back, ~90 days).
  - `GetFills(symbol, orderID, startMs, endMs)` →
    `GET /api/v2/spot/trade/fills`. Cursor-paginated by `tradeId`
    (NOT `orderId`, on this endpoint specifically). `orderID == ""`
    returns fills across all orders for the symbol; passing an
    explicit orderID restricts the query.

- **Contract tests** on a local `httptest.Server`:
  - happy-path parsing for every endpoint;
  - 250-row multi-page pagination test that pins the cursor protocol
    (3 pages: 100 + 100 + 50 rows; verifies `idLessThan` echoes the
    last `orderId` of the previous page);
  - regression guards: `productType` / `marginCoin` / `marginMode` /
    `holdSide` / `tradeSide` MUST NOT appear on any spot account
    request;
  - POST-vs-GET shape pin for `orderInfo` (the SDK never silently
    flips it back to GET);
  - fail-fast validation for `GetOrderDetail` (empty symbol /
    no identifier).

### Internal (no public API change)

- **Pagination helper extracted from `mix/`** ahead of M3 (committed
  separately in `refactor(internal/bgcommon): extract cursor-pagination helper`):
  - `bgcommon.OrdersPageLimit = 100`
  - `bgcommon.OrdersMaxPages = 10`
  - `bgcommon.PaginateByCursor[T any](ctx, label, fetch) ([]T, error)`
    — generic cursor walker shared by mix open-orders and the new
    spot open-orders / order-history / fills endpoints.
  - `mix/account.go::GetOpenOrders` rewired to use the helper; the
    ceiling error message stays byte-stable
    (`"mix.Account.GetOpenOrders: pagination ceiling hit ..."`)
    so existing tests / log greps remain valid.

### Roadmap

  - **v2.0.0-m3** (this tag): Account + history REST.
  - **v2.0.0-m4**: public WebSocket (books with CRC32 resync via
                   the shared `internal/bgcommon/orderbook` engine,
                   ticker, trade, candles).
  - **v2.0.0-m5**: private WebSocket (account / orders / fills)
                   with login + auto-resub via `internal/ws.Conn`.
  - **v2.0.0**:    aggregate release once M4..M5 land.

## v2.0.0-m2 — 2026-05-28

Second milestone of the **v2.0 SPOT** profile. Wires the REST market-
data and trading endpoints. `mix/` is unaffected on the wire; an
internal refactor (see "Internal" below) lifts a few profile-agnostic
helpers into `internal/bgcommon` so that `spot/` and `mix/` share a
single source of truth instead of running parallel copy-paste.

### Added

- **`spot/types/` request/response shapes** wired for M2:
  - `SymbolInfo` — instrument spec (no productType / leverage; adds
    `QuoteStep` for market-buy quote-side denominated size).
  - `MarketTicker` — composite price snapshot (no markPrice /
    indexPrice / fundingRate; adds 24h roll-ups: high24h, low24h,
    open, openUtc, baseVolume, quoteVolume, usdtVolume, change24h,
    changeUtc24h).
  - `CreateOrderRequest` — no TradeSide / ReduceOnly. The doc
    comment pins the spot quirk that `Quantity` is QUOTE-side for
    market BUYs and BASE-side for everything else.
  - `ModifyOrderRequest` — same identification shape as mix; SDK
    auto-fills `NewClientOrderID` with `s-<32-hex>` when empty
    (mix uses `m-<32-hex>` — both share `bgcommon.GenClientOid`).
  - `OrderInfo` — no HoldSide / TradeSide / leverage.
  - `BatchOrderResult` — same shape as mix's, profile-local because
    `Order` is `*spot.types.OrderInfo`.

- **`spot.MarketDataClient` (REST market data)** — wires four
  endpoints. None require auth; all run through the shared rate-
  limited `bgcommon.RestDoer`.
  - `GetSymbolInfo(symbol)` → `GET /api/v2/spot/public/symbols`.
    Empty / not-found symbols surface as `ErrorKindInvalidRequest`.
    Derives `PriceTick` / `SizeStep` / `QuoteStep` from precision
    counts (`10^-precision`, no `priceEndStep` multiplier — that
    only exists on mix).
  - `GetOrderBook(symbol, depth)` → `GET /api/v2/spot/market/orderbook`.
    Numeric `limit` (1..150); `depth ≤ 0 → 50`. `type=step0` is
    pinned (native tick precision).
  - `GetMarketTicker(symbol)` → `GET /api/v2/spot/market/tickers`.
    Maps every 24h roll-up the venue exposes.
  - `GetHistoricalCandles(symbol, timeframe, length)` and
    `GetHistoricalCandles1m(symbol, length)` →
    `GET /api/v2/spot/market/candles`. `length ≤ 0 → 100`,
    capped at 200 (spot ships an 8-element row; the shared
    `bgcommon.ParseCandles` ignores the optional 8th column so
    one parser handles both mix and spot).

- **`spot.TradingClient` (REST trading)** — wires every single +
  batch place / amend / cancel endpoint:
  - `CreateOrder(req)` → `POST /api/v2/spot/trade/place-order`.
  - `CreateBatchOrders(reqs)` → `POST /api/v2/spot/trade/batch-orders`.
    Per-symbol (homogeneous symbol, validated client-side); collates
    `successList` / `failureList` into a slice ordered to match
    the request slice (matched by `clientOid`, positional fallback
    when `clientOid` is empty).
  - `ModifyOrder(req)` → `POST /api/v2/spot/trade/cancel-replace-order`.
    Same Bitget quirk as mix: `newClientOid` is REQUIRED and MUST
    differ from the existing `clientOid`. SDK auto-fills with
    `bgcommon.GenClientOid("s-")` when the caller leaves it empty
    and rejects accidental duplicates client-side
    (`ErrorKindInvalidRequest`) before they round-trip into a
    code=40786 from the venue.
  - **`ModifyBatchOrders(reqs)` → `POST /api/v2/spot/trade/batch-cancel-replace-order`**.
    **NATIVE endpoint on spot** — single REST RPC, no client-side
    fan-out (mix has no batch-modify endpoint and falls back to N
    `ModifyOrder` calls; spot does not). Spot's batch-cancel-replace
    accepts MIXED symbols, so the SDK does not enforce homogeneous
    symbol on this method.
  - `CancelOrder(req)` → `POST /api/v2/spot/trade/cancel-order`.
  - `CancelBatchOrders(reqs)` → `POST /api/v2/spot/trade/cancel-batch-orders`.
  - **`CancelAllOrders(symbol)` → `POST /api/v2/spot/trade/cancel-symbol-order`**.
    Bitget V2 spot has NO cross-symbol cancel-all endpoint (mix
    does); empty `symbol` surfaces as `ErrorKindInvalidRequest`.

- **Contract tests** on a local `httptest.Server` for every wired
  endpoint:
  - `spot/contract_test.go` — market-data parsing, depth clamp,
    ASC-order kline preservation, "no productType on the wire"
    regression guards.
  - `spot/trading_contract_test.go` — body shape (no
    productType / marginMode / marginCoin / tradeSide), happy-
    path orderId echo, batch collation (mixed success / failure),
    heterogeneous-symbol rejection, native batch-cancel-replace
    is hit EXACTLY ONCE per call (the architectural promise vs.
    mix's fan-out), `s-` prefix on auto-filled `newClientOid`,
    fail-fast validation across every method.

### Internal (no public API change)

- **Profile-agnostic batch + clientOid helpers** lifted from `mix/`
  into `internal/bgcommon`:
  - `bgcommon.GenClientOid(prefix)` — collision-resistant
    `<prefix><32-hex>` token via `crypto/rand`. `mix.genNewClientOid`
    and `spot.genNewClientOid` are now thin wrappers (`m-` and
    `s-` prefixes).
  - `bgcommon.ChooseClientOid(fromVenue, fromRequest)` — pick the
    venue-echoed value with graceful fallback to the request's
    own. Used everywhere both profiles collate batch responses.
  - `bgcommon.MaxBatchSize`, `bgcommon.BatchEnvelope`,
    `bgcommon.BatchSuccessRow`, `bgcommon.BatchFailureRow`,
    `bgcommon.ValidateBatchSize(label, n)` — the Bitget V2 batch
    response envelope and 50-row cap apply uniformly across mix /
    spot / future uta. `mix/trading.go` was rewired through these
    types; the legacy `validateBatchSize` is now a thin wrapper that
    preserves the existing error-message prefix
    (`"mix.Trading.<Method>"`) so logs and tests stay byte-stable.
- `mix/account.go::ClosePosition` was rewired through
  `bgcommon.BatchEnvelope` / `BatchFailureRow` (it already decoded
  the same shape; no behaviour change).

### Roadmap

  - **v2.0.0-m2** (this tag): MarketData + Trading REST.
  - **v2.0.0-m3**: Account (balance / info) + history queries
                   (open orders / order history / fills).
  - **v2.0.0-m4**: public WebSocket (books with CRC32 resync via
                   the shared `internal/bgcommon/orderbook` engine,
                   ticker, trade, candles).
  - **v2.0.0-m5**: private WebSocket (account / orders / fills)
                   with login + auto-resub via `internal/ws.Conn`.
  - **v2.0.0**:    aggregate release once M3..M5 land.

## v2.0.0-m1 — 2026-05-28

First milestone of the **v2.0 SPOT** profile. Functional behaviour does
not change — `mix/` is unaffected and the new `spot/` package only
exposes scaffolding. The milestone tag exists so the desk-side
connector can pin the SDK while later milestones (M2..M5) land.

### Added

- **`spot/` package** — Bitget V2 SPOT profile root sub-client.
  - `spot.Client` mirrors the `mix.Client` shape (Trading / Account /
    MarketData / Stream sub-clients) but drops every mix-specific
    pin (no productType, no marginMode, no marginCoin, no
    holdSide, no tradeSide, no positions, no leverage).
  - `spot.NewClient(parent)` constructs all four sub-clients eagerly;
    every `Client.Trading() / Account() / MarketData() / Stream()`
    getter returns non-nil immediately. M1 ships only struct +
    constructor for each sub-client; the REST / WS endpoint methods
    land in M2..M5.
  - `init()` registers a factory in the root package, so callers can
    reach the spot client through `bitget.Client.Spot().(*spot.Client)`
    once `_ "github.com/tonymontanov/go-bitget/v2/spot"` is imported.
  - `spot.SpotInstType = "SPOT"` constant — the literal Bitget V2
    expects in WebSocket subscription `instType` for every spot
    channel.
- **`spot/types/` namespace** — placeholder for spot-specific request
  / response shapes (filled in M2..M5).
- **Smoke tests** — pin the M1 contracts so future milestones cannot
  regress them silently:
  - `NewClient(nil)` returns nil.
  - `NewClient(parent)` builds Trading / Account / MarketData / Stream.
  - `bitget.Client.Spot()` factory wiring works once the spot package
    is imported.

### Roadmap

  - **v2.0.0-m1** (this tag): scaffolding only.
  - **v2.0.0-m2**: MarketData (symbols / tickers / orderbook /
                   candles / fills) + Trading (place / amend /
                   cancel; single + batch). REST only.
  - **v2.0.0-m3**: Account (balance / info) + Trading history
                   (open / history / fills).
  - **v2.0.0-m4**: public WebSocket (books with CRC32 resync via the
                   shared `internal/bgcommon/orderbook` engine,
                   ticker, trade, candles).
  - **v2.0.0-m5**: private WebSocket (account / orders / fills) with
                   login + auto-resub via `internal/ws.Conn`.
  - **v2.0.0**:    aggregate release once M2..M5 land.

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
