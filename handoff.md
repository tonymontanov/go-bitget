# handoff.md — go-bitget SDK

> Session-handoff document. Its purpose is to preserve context between
> working sessions: who this project is for, how it is laid out, what is
> done / in flight / planned, the code-style contract, and the
> integration surface (config paths, env vars, external APIs, deps).
>
> **Update this file after every significant task** (architecture change,
> new module, refactor, milestone tag). Keep it factual — do not invent
> internal context.

Module path: `github.com/tonymontanov/go-bitget/v2`
Last tagged release: **`v2.0.0`** (SPOT GA). Active dev branch: **`v2.5`**
(full-exchange coverage; see Roadmap).

---

## 1. Role & stack

**Role.** A dedicated, high-performance Go SDK for the **Bitget** exchange,
built for HFT / algorithmic trading (low-latency, high-throughput, minimal
allocations on hot paths). It is a *single-exchange* SDK: it covers
Bitget's REST + WebSocket API and exposes idiomatic Go types. It is **not**
a multi-exchange abstraction — cross-exchange unification lives in the
consumer.

**Consumer.** The SDK is integrated into the
trading engine via the `internal/connectors/bitget/{common,mix,spot}`
connector (branch `bitget-connector`, based on `qa`). The connector adapts
SDK types to the desk's unified `ExchangeConnector` interface. Changes in
the engine are intentionally localised to the new connector to avoid
destabilising the tested core.

**Design references** (sibling SDKs by the same author, same style):
`go-okx`, `go-bybit`, and the `go-binance` fork. API ergonomics follow
adshao/go-binance v2.8.x.

**Stack.**

- Language: **Go 1.24**.
- Direct dependencies (intentionally minimal — same set as `go-bybit` /
  `go-okx`):
  - `github.com/gorilla/websocket v1.5.3` — WS transport.
  - `github.com/json-iterator/go v1.1.12` — fast JSON on hot paths.
  - `github.com/shopspring/decimal v1.4.0` — exact numerics for all
    price/size fields (Bitget delivers them as JSON strings).
- Indirect: `modern-go/concurrent`, `modern-go/reflect2` (jsoniter deps).
- Standard library for everything else (HTTP, crypto/hmac, sync, context).

---

## 2. Architecture

### Naming convention (hard rule)

Trading sections are named **identically to Bitget's own naming**. Bitget's
USDT-margined perpetual futures are called **MIX**, so the SDK package is
`mix/` (and the desk section is `bitget_mix`). Spot is `spot/`. The future
V3 Unified Trading Account is `uta/`. Do not rename or "improve" these.

### Two-layer architecture (hard rule)

**No parallel copy-paste, no cross-section function reuse.** Shared logic
lives in a common layer and is *composed* by each profile with its own
specifics. Concretely:

- Domain-agnostic helpers live in `internal/bgcommon/` and are consumed by
  `mix/`, `spot/` (and later `uta/`).
- Profile-specific wire shapes / semantics stay profile-local.

```
WRONG: spotRequestFunc() { futuresRequestFunc(...) }   // section reuses section
RIGHT: requestFunc()      // unified
       futuresRequestFunc() { requestFunc(...) }        // specialise
       spotRequestFunc()    { requestFunc(...) }         // specialise
```

### Folder structure & module interaction

```
go-bitget/
  client.go            # root *Client: shared REST transport + signer + lazy sub-client cache
  config.go            # public Config (REST/WS endpoints, timeouts, reconnect, orderbook)
  doc.go / errors.go / logger.go / metrics.go / rate-limit-event.go

  internal/            # hidden from SDK users; shared across profiles
    auth/      — HMAC-SHA256 (base64) signing for REST + WS login
    bgerr/     — Error / ErrorKind / MapBitgetCode / MapHTTPStatus (~115 V2 codes)
    bglog/     — Logger interface, Field, NoopLogger
    bgmet/     — Counter / CounterFactory, NoopMetrics
    codec/     — jsoniter wrappers + ParseDecimal / ParseInt64 / RawJSON
    rest/      — low-level HTTP client; Bitget envelope {code,msg,data,requestTime}; ACCESS-* headers; rate-limit observers
    ws/        — Conn: connect / login / plain-text ping / reconnect+jitter / resubscribe / dispatch
    bgcommon/  — domain-agnostic helpers shared by profiles:
                   pagination.go  (PaginateByCursor[T] — idLessThan cursor protocol)
                   batch.go       (BatchEnvelope / ValidateBatchSize)
                   clientoid.go   (GenClientOid / ChooseClientOid)
                   numeric.go     (ParseDecimalOrZero / ParseInt64OrZero)
                   flexstring.go  (FlexString — accepts JSON number OR quoted string)
                   parse.go / restdoer.go
                   orderbook/     (Engine — snapshot+delta, top-25 CRC32 validation, resync)
                   wsframes.go    (profile-agnostic books/trade/candle wire rows + parsers)
                   wsfee.go       (WSFeeDetail / ParseFeeDetailList — shared feeDetail[] in fill push)

  types/               # protocol-common domain types (Side, OrderType, TIF, OrderStatus,
                       # ProductType, PositionMode, MarginMode, OrderBookLevel/Snapshot,
                       # Candle, Timeframe, TradeUpdate, KlineUpdate, CancelOrderRequest, Balance)

  mix/                 # v1.0 — MIX (USDT-margined perps)
    client.go          — *mix.Client + RegisterMixFactory (init)
    market.go          — REST market-data
    trading.go         — REST trading
    account.go         — REST account / position
    stream.go          — public WS + orderbook engine wiring
    stream-private.go  — private WS (orders / positions / account / fills)
    types/             — MIX-only domain types (incl. PositionInfo, FillUpdate)

  spot/                # v2.0 — Bitget spot
    client.go          — *spot.Client + RegisterSpotFactory (init)
    market.go / trading.go / account.go
    stream.go          — public WS
    stream-private.go  — private WS (orders / account / fills; NO positions — cash-only)
    types/             — spot-only domain types (AccountInfo, Fill, AccountUpdate, FillUpdate)

  margin/              # v2.5 — Bitget margin (crossed + isolated)
    client.go          — *margin.Client + ClientSettings{Mode} + RegisterMarginFactory (init)
    trading.go         — place / batch-place / cancel / batch-cancel (no amend on margin)
    account.go         — assets / borrow / repay / max-borrowable / max-transfer-out /
                         orders / fills / paged history records (cursor pagination)
    public.go          — Currencies (margin-coin reference; mode-agnostic)
    stream.go          — private WS: WatchOrders (orders-<mode>) / WatchAccount (account-<mode>)
    types/             — margin-only domain types (CreateOrderRequest, OrderInfo, LoanType,
                         STPMode, MarginAsset, AccountUpdate, records: Borrow/Repay/Interest/
                         Liquidation/Financial, Currency, MaxBorrowable, MaxTransferOut)

  uta/                 # v2.5 — V3 Unified Trading Account (core done: Public/Account/Trade/Position/Strategy)
  examples/            # runnable demos. MIX: marketdata / place-order /
                       #   private-stream. SPOT: spot-marketdata /
                       #   spot-place-order / spot-private-stream /
                       #   spot-smoke (production-readiness harness)
  docs/                # TS-SINGLE-EXCHANGE-SDK*.md (technical spec)
```

**Interaction chain (user → SDK → exchange):**

```
bitget.NewClient(cfg)            // root client: builds signer + shared rest.Client
   └─ client.Mix() / Spot()      // lazy, factory registered by the profile package's init()
        └─ Trading() / Account() / MarketData() / Stream()   // four "fat" domain sub-clients
             └─ internal/rest (REST)  /  internal/ws (WS)     // shared transport
                  └─ internal/auth (signing) , internal/bgerr (typed errors)
```

The root package never imports profile packages (avoids an import cycle:
profiles import root for `Config`/`Error`). Profiles register a factory at
`init()`; `client.Mix()` / `client.Spot()` return `any`, the caller casts
to `*mix.Client` / `*spot.Client`. Import the profile package (anonymously
is fine) to wire its factory.

**Desk integration chain:**

```
internal/connectors/bitget/common/   # BuildSDKConfig, id-mapping, forgotten-orders,
                                      # sync-mappings, order-status, rate-limit-observer,
                                      # logger-adapter (zerolog → bitget.Logger)
   ├─ bitget/mix/    (connector.go, account.go, trading.go, market.go, stream.go, stream-fanout.go)
   └─ bitget/spot/   (connector.go, account.go, trading.go, market.go, stream.go, stream-fanout.go)
```

The connector's `common/` layer mirrors the SDK's two-layer discipline:
profile-agnostic helpers shared, profile-specific fan-out (`stream-fanout.go`)
local. Fan-out multiplexes several desk `Watch*` calls over one SDK
subscription (e.g. `WatchLastPrice` + `WatchSpread` both ride `WatchTicker`).

---

## 3. Roadmap (state)

### ✅ Done

| Tag | Scope |
| --- | --- |
| M0 | scaffolding: root client/config/errors/logger/metrics/rate-limit-event; `internal/auth`, `internal/bgerr` (~115 V2 codes), `internal/rest`, `internal/ws` |
| `v1.0` / M1–M5 | **MIX** end-to-end: REST market-data, REST trading (+batch, CancelAll), REST account/position (positions, open orders, leverage, position-mode, ClosePosition), public WS (orderbook engine + CRC32 resync, ticker, trades, kline), private WS (orders / positions / account) |
| `v1.0.x` / `v1.1` / `v1.2.x` | hardening: extended error-code coverage, examples, WS login `timestamp` in **seconds** fix (v1.0.2), PARTIUSDT private-stream regressions fixed (v1.2.x) |
| `v2.0.0-m1` | `spot/` scaffolding: Client + Trading/Account/MarketData/Stream stubs, factory wired |
| `v2.0.0-m2` | spot MarketData + Trading REST (native batch-cancel-replace; `s-` clientOid prefix); lifted batch/clientOid helpers into `bgcommon`, rewired mix through them |
| `v2.0.0-m3` | spot Account + history REST; new `bgcommon.PaginateByCursor[T]` (idLessThan cursor), mix `GetOpenOrders` rewired through it |
| `v2.0.0-m4` | spot public WebSocket (orderbook via shared `bgcommon/orderbook.Engine`, 24h-rollup ticker, trades, kline); lifted `bgcommon/wsframes.go` |
| `v2.0.0-m5` | spot private WS `WatchOrders` (instId="default" + client-side symbol filter; auth pre-flight → `ErrorKindAuth`) |
| `v2.0.0-m6` | mix↔spot private-WS symmetry: `spot.WatchAccount` (per-asset) + `spot.WatchFills` + `mix.WatchFills`; shared `bgcommon.WSFeeDetail`; audited mix private streams to M5 test parity |
| **`v2.0.0`** | **SPOT GA** — folded m1–m6 + four live PARTIUSDT wire fixes (candle granularity, batch-cancel path, cancel-replace `size`/`price`, rotated `newClientOid`) into a stable release; CHANGELOG roll-up; desk-core consumes the public tag |

Desk-core (`bitget-connector` branch): MIX connector cycle (B3–B5) and Spot
connector cycle (T2b account/history, T2c public WS, `WatchOpenOrders` over
SDK m5 private streams) are implemented and tested against the local SDK.

### 🔧 In progress — `v2.5` (full-exchange coverage)

**Active line: `v2.5` on branch `v2.5`** (cut from `main` at the
`v2.0.0` GA). Goal agreed with the owner: cover the WHOLE Bitget
exchange, including the V3 / UTA profile, done in stepwise phases with
an owner review pause after each. Constraints reaffirmed: SDK-only (the
desk connector is NOT touched in this line), strict two-layer
discipline (lift shared into `bgcommon`, never cross-section reuse),
package names mirror Bitget's own section naming, English GoDoc,
contract tests at parity. **Definition of done = REST+WS coverage +
unit/contract tests (httptest / mock WS); the owner validates live.**

Agreed phase order:

| Phase | Scope | State |
| --- | --- | --- |
| 0 | branch `v2.5` from `main` | ✅ |
| 1 | **Futures completeness** — validate COIN-FUTURES / USDC-FUTURES across `mix/` and pin the wire deltas | ✅ (this session) |
| 2 | `margin/` — cross + isolated (one package parameterised by mode) | ✅ M1–M4 done (this session) |
| 3 | Copy Trading — futures + spot | ✅ M1–M5 done (this session) |
| 4 | `earn/` + `convert/` | ✅ done (this session) |
| 5 | `broker/` (Agent) | ✅ done (this session) |
| 6 | Common / public utilities round-out | ✅ done (this session) |
| 7 | `uta/` — V3 Unified Trading Account (core: Public+Account+Trade+Position+Strategy; hedge mode, demo header) | ✅ done (this session) |

**Phase 1 — done (futures completeness).** Audit confirmed every `mix/`
REST/WS path already routes `productType` / `marginCoin` through the
pinned `ClientSettings` (no hard-coded `USDT-FUTURES` in logic — only
in comments/defaults), and the `ProductType` enum is complete
(USDT/COIN/USDC + demo SUSDT/SCOIN/SUSDC). The gap was test coverage:
no contract test exercised COIN/USDC. Added
`mix/producttype_contract_test.go` — a product-type matrix
(USDT/USDC/COIN) pinning the venue-visible deltas on place / cancel /
batch-place / account / single-position / public-WS-subscribe:

- `productType` on the wire matches the pinned setting;
- `marginCoin` = `USDT` / `USDC`, and is **OMITTED** for COIN-FUTURES
  (coin-margined contracts use a per-symbol coin; the SDK lets Bitget
  infer it from the symbol — `defaultMarginCoinFor` returns "");
- public-WS subscribe `instType` follows the product type.

Plus `defaultMarginCoinFor` unit coverage for all six product types and
a demo-product-type construction test. `examples/marketdata` gained a
`-product-type` flag (read-only) so coin/usdc can be exercised live.
No production-code change was required — the routing was already
correct; the COIN-FUTURES `marginCoin`-omission is the SDK's documented
assumption, to be confirmed by the owner's live smoke run.

**Phase 2 — done (`margin/`, crossed + isolated).** New top-level
package `margin/`. Design agreed with the owner: ONE package
parameterised by **mode** (`crossed` / `isolated`) pinned at
construction via `margin.ClientSettings{Mode}` (reusing the existing
`roottypes.MarginMode` — no new enum). The mode drives the URL path
segment on every endpoint (`/api/v2/margin/<mode>/...`); the literal
segment is `crossed` / `isolated` (NOT `cross` — the V2 release-note
column is stale; confirmed against the live Cross-Place-Order doc and
`assertMarginType: crossed|isolated`). `bitget.Client.Margin()` returns
the crossed default; isolated callers use `NewClientWithSettings`.
Margin trades SPOT instruments, so there is **no margin market-data** —
prices/books/candles come from the `spot` profile; the only public
margin endpoint is `/api/v2/margin/currencies` (M3). **No public margin
WS** — only private `account-<mode>` / `orders-<mode>` on
instType=`MARGIN` (M4).

Milestone state:

- **M1 (scaffold) — done.** Root `Margin()` factory; `margin.Client` +
  `ClientSettings{Mode}` + `Trading`/`Account`/`Public`/`Stream`
  sub-clients; `margin/types` (`CreateOrderRequest` with
  `BaseSize`/`QuoteSize`/`LoanType`/`STPMode`, `OrderInfo`,
  `BatchOrderResult`, `LoanType`/`STPMode` enums). Account/Public/Stream
  are stubs.
- **M2 (Trading REST) — done.** `CreateOrder` / `CreateBatchOrders` /
  `CancelOrder` / `CancelBatchOrders`. **No amend endpoint exists on
  margin** (Bitget ships none). Pinned by `margin/trading_contract_test.go`:
  crossed/isolated path matrix; `loanType` always on the wire (default
  `normal`); side-dependent size (`baseSize` limit/market-sell,
  `quoteSize` market-buy, other omitted); `force` limit-only; optional
  `stpMode`; per-symbol batch + `{successList,failureList}` collation.
  Shared helpers reused from `bgcommon` (no `spot/` reuse, no copy-paste).
- **M3 (Account/assets + borrow/repay/records + currencies) — done.**
  `Account()`: assets, borrow/repay, max-borrowable, max-transfer-out,
  open/history orders, fills, and paged history records (borrow / repay
  / interest / liquidation / financial) over `bgcommon.PaginateByCursor`.
  `Public().Currencies` (mode-agnostic margin-coin reference). Pinned by
  `margin/account_contract_test.go`: isolated borrow/repay REQUIRE
  `symbol` (crossed omits); margin order queries REQUIRE `symbol`;
  `GetMaxBorrowable` parses both crossed (`coin`) and isolated
  (`baseCoin`/`quoteCoin`) shapes. Advanced endpoints (interest-rate-and-
  limit, tier-data, risk-rate, flash-repay, liquidation-order) documented
  and deferred — not on the desk hot path.
- **M4 (private WS `account-<mode>` / `orders-<mode>`) — done.**
  `Stream().WatchOrders` / `WatchAccount` on instType=`MARGIN`, lazy
  login-gated conn mirroring spot's private side. Pinned by
  `margin/stream_contract_test.go`: mode-suffixed channel names;
  `instId="default"` (orders) / `coin="default"` (account) with
  CLIENT-side per-symbol/per-coin filter (`"default"` opts out);
  field mapping (loanType / baseVolume / fee aggregation / balances);
  `ErrorKindAuth` without credentials; `FlexString` numeric tolerance.
- **Example + tests.** `examples/margin` (signed place→list→cancel with
  `-mode` flag, prices via spot). Full suite green incl. `-race`.

**Phase 3 — done (`copytrading/`, futures + spot, trader + follower).**
New top-level package `copytrading/` for the V2 copy-trading surface
(`/api/v2/copy/...`). Design agreed with the owner: a 2×2 of product
(futures / spot) × role (lead trader / follower) as four sub-clients off
`bitget.Client.CopyTrading()` (lazy factory like `Mix`/`Spot`/`Margin`).
**REST-only** — copy trading ships no dedicated WS. Futures **product
type** pinned at construction via `copytrading.ClientSettings` and sent
on every futures call; **spot ignores it** (spot is not product-type
scoped — pinned by a no-`productType` invariant test). Broker/agent
endpoints deferred to Phase 5.

Milestone state:

- **M1 (scaffold) — done.** Root `CopyTrading()` factory; `Client` +
  `ClientSettings{ProductType}` + the four accessors; shared helpers
  (`errInvalid`/`errParse`/`queryMeta`).
- **M2 + M2b (futures follower) — done.** Core (`query-traders`,
  `query-current-orders`, `query-history-orders`, `close-positions`,
  `cancel-trader`) + config (`settings`, `query-settings`,
  `setting-tpsl`, `query-quantity-limit`). Cursor pagination;
  `openPriceAvg`/`openAvgPrice` spelling fallback; TP/SL prices as
  strings for empty/`"0"`/`>0`; `followerEnable` → `Following` bool.
- **M3 (futures lead trader, 14 endpoints) — done.** M3a orders / M3b
  config / M3c profit across `mix-trader/*`. Added the generic
  `paginateByPageNo` helper (page-number pagination + hard page ceiling);
  `TotalPL` kept raw (string) for the currency prefix.
- **M4a (spot lead trader, 12 endpoints) — done.** `spot-trader/*`
  orders + config (`config-query-settings` single blob, not the futures
  symbols+base split) + profit (`profit-summarys` /
  `profit-history-details` / `profit-details`). No `productType`.
- **M4b (spot follower, 10 endpoints) — done.** `spot-follower/*`:
  query-traders (page-no), query-trader-symbols, query-settings (active
  rows + venue bounds), settings, setting-tpsl, current/history orders
  (cursor), order-close-tracking, stop-order, cancel-trader. ≤50 batch
  guard; empty `platsk*` tier limits → zero.
- **M5 (example + docs) — done.** `examples/copytrading`: read-only demo
  across all four roles (lists followed traders futures+spot; with
  `-trader` reads the lead summaries, treating the eligibility 4xx as an
  expected note for non-elite accounts; `-product-type` flag). Wire
  shapes throughout sourced from the Bitget V2 docs (CoinTR mirror for
  the spot slugs). Full suite green.

**Phase 4 — done (`earn/` + `convert/`).** Two new top-level packages for
Bitget's account-level yield + swap surfaces. Both **REST-only** and
**not** product-type scoped (no `productType`, no WS); lazy
`bitget.Client.Earn()` / `.Convert()` factories like the other profiles.
Wire shapes + request params sourced from the V2 docs and the
tiagosiebler / tty666 reference clients (the official doc slugs were
flaky; the reference TS request/response types filled the gaps). Owner
validates live.

Milestone state:

- **P4-C (`convert/`) — done.** 7 endpoints: `currencies`,
  `quoted-price` (RFQ), `trade`, `convert-record` (cursor), and the BGB
  small-balance trio (`bgb-convert-coin-list` / `bgb-convert` /
  `bgb-convert-records`). Two-step swap: `GetQuotedPrice` → `traceId` +
  `cnvtPrice` (TTL ~8s) fed into `Trade`. **Naming delta to confirm
  live:** the GET quoted-price uses `fromCoinSz`/`toCoinSz` query keys
  while the POST trade body uses `fromCoinSize`/`toCoinSize` (the doc
  curl + param description disagree with the column header; chose the
  curl/description spelling). `bgb-convert` body is a JSON array
  `coinList` per the doc curl.
- **P4-E1 (`earn/` scaffold + Savings + Account) — done.** Category
  sub-clients off `earn.Client` (Account/Savings/SharkFin/Elite/Loan).
  Account `assets`; Savings `product`/`account`/`assets`(cursor)/
  `records`(cursor)/`subscribe-info`/`subscribe`/`subscribe-result`/
  `redeem`/`redeem-result`. `assets`/`records` require a `periodType`
  (`flexible`|`fixed`); subscribe/redeem-result return `{result,msg}`.
- **P4-E2 (Shark Fin + On-Chain Elite) — done.** Shark Fin (7): product
  (cursor), account, assets (by status, cursor), records (by type,
  cursor), subscribe-info, subscribe, subscribe-result. Elite (8):
  product, assets (single `resultList`), records (cursor over
  `recordList` via `cursor`/`endId`), subscribe-info, subscribe,
  subscribe-result (status), redeem-info, redeem. Elite `redeemType` /
  `paymentAccount` decode from string-or-array via a `flexStringList`.
- **P4-E3 (Crypto Loan, 11) — done.** `public/coinInfos` +
  `public/hour-interest` are **unsigned**; `borrow` (exactly-one-of
  pledgeAmount/loanAmount), `repay`, `revise-pledge`, `ongoing-orders`,
  `debts`, plus the page-number paginated `repay-history`,
  `revise-history`, `borrow-history`, `reduces` (all require a
  `[startTime,endTime]` window). Profile-local `paginateByPageNo`
  (mirrors the copytrading helper) with a hard page ceiling.
- **P4-E4 (example + docs) — done.** `examples/earn`: read-only demo
  across both profiles (earn account overview; savings / shark-fin /
  elite products + held positions; loan currency table + ongoing orders;
  convert currency list + a sample RFQ quote — no funds moved). Full
  suite green (`go test -race ./...`).

**Phase 5 — done (`broker/`, broker + agent).** New top-level package
`broker/` for the two programs that share the broker namespace: the
institutional **broker** (managed sub-accounts + commission reporting) and
the **agent** (affiliate / referral) program. **REST-only**, account-level,
**not** product-type scoped (except `subaccount-future-assets`); no WS.
Lazy `bitget.Client.Broker()` factory; five sub-clients
(`SubAccounts`/`APIKeys`/`Stats`/`Agent`/`CopyBroker`), 34 endpoints. All
signed; most require an approved broker/agent account (a non-eligible key
gets a 4xx). Wire shapes from the V2 docs + the changelog legacy-endpoint
mapping + the tiagosiebler reference (request/response types). Owner
validates live.

Milestone state:

- **P5-M1 (scaffold + Sub-account mgmt, 14) — done.** `account/info`,
  `create-subaccount`, `subaccount-list` (hasNextPage/idLessThan paged),
  `modify-subaccount`, modify/get `subaccount-email`, spot/future assets
  (future-assets is the one productType-scoped call), `subaccount-address`,
  `subaccount-withdrawal`, `set-subaccount-autotransfer`, plus the
  ND-broker `subaccount-deposit` / `subaccount-withdrawal` /
  `all-sub-deposit-withdrawal` record feeds (idLessThan/endId cursor).
  Guards on the mutating calls (incl. `on_chain` → `chain` required).
- **P5-M2 (API keys, 3) — done.** `create-subaccount-apikey` (returns
  `secretKey` once), `subaccount-apikey-list`, `modify-subaccount-apikey`.
  ipList/permList sent as JSON arrays.
- **P5-M3 (broker reporting, 6) — done.** `subaccounts`/`commissions`/
  `trade-volume` (pageNo/pageSize, fully walked), `total-commission`
  (daily slice, nested spot/futures breakdown), `order-commission`
  (idLessThan/endId cursor), `rebate-info` (per-uid).
- **P5-M4 (agent reporting, 8) — done.** GET cursor reads
  (`customer-commissions`, `sub-customer-list` minId cursor,
  `customer-kyc-result`, `agent-commission`) + **POST** pageNo/pageSize
  reads (`customer-trade-volume`, `customer-list`, `customer-deposit`,
  `customer-asset`). Maps the venue's misspelled `volumn` → `Volume`.
- **P5-M5 (copy mix-broker + docs) — done.** `query-traders`,
  `query-history-traces`, `query-current-traces` (deferred from Phase 3;
  pageNo/pageSize; current-traces ignores the time window). **Open item:**
  upstream types these as `any`; the row shapes follow the documented copy
  trader / order-trace schema and decode leniently — confirm field names
  on the live smoke run. `examples/broker`: read-only demo across all four
  groups. Full suite green (`go test -race ./...`).

**Phase 6 — done (`common/`, public utilities round-out).** New top-level
package `common/` for the market-agnostic, account-level surface not tied
to a single trading profile. **REST-only**, **not** product-type scoped,
no WS. Lazy `bitget.Client.Common()` factory; five sub-clients
(`Public`/`Account`/`Tax`/`P2P`/`Users`), 21 endpoints. Wire shapes from
the V2 docs + the tiagosiebler reference. Owner validates live.

Milestone state:

- **P6-M1 (scaffold + Public + Account, 6) — done.** `Public` is UNSIGNED
  (no `ACCESS-SIGN`): `GetServerTime` (`public/time`), `GetAnnouncements`
  (`public/annoucements`, language required). `Account` (signed):
  `GetFundingAssets`, `GetBotAssets`, `GetAllAccountBalance`, `GetTradeRate`
  (`common/trade-rate`, symbol+businessType guards).
- **P6-M2 (Tax + P2P, 8) — done.** Tax (4): spot/future/margin/p2p records,
  each window-required, flat array stitched by `idLessThan` cursor (next =
  last row id) via `bgcommon.PaginateByCursor`. P2P (4): merchantList /
  merchantInfo / orderList / advList; list reads walk the
  `idLessThan`/`min*Id` cursor; nested order paymentInfo + ad
  userLimit/paymentMethods/certified decode into typed structs.
- **P6-M3 (virtual sub-accounts + API keys, 7) — done.** MAIN-account user
  management (`user/...`), distinct from broker sub-accounts.
  Create/Modify subaccount, BatchCreateSubAccountAndAPIKey, GetSubAccounts
  (`idLessThan`/`endId` cursor), Create/Modify/Get API keys. Writes mint
  real credentials → client-side guards; `SecretKey` surfaced once. **Open
  item:** the create-subaccount response uses the venue's `subaAccount*`
  spelling — decoded defensively (both spellings); confirm on live run.
- **P6-M4 (example + docs) — done.** `examples/common`: unsigned public
  section runs with no creds; read-only signed sections (account assets /
  trade-rate, tax, P2P, virtual sub-accounts). CHANGELOG/README/handoff
  updated. Full suite green (`go test -race ./...`).

**Phase 7 — done (`uta/`, V3 Unified Trading Account — CORE).** New
top-level package `uta/` for the V3 UTA. V3 keys on a per-call `category`
(`SPOT`/`MARGIN`/`USDT|COIN|USDC-FUTURES`), not product-pinned clients;
signing reuses the V2 `ACCESS-*` scheme unchanged. Lazy
`bitget.Client.UTA()` factory; five sub-clients
(`Public`/`Account`/`Trade`/`Position`/`Strategy`), ~55 endpoints. REST
only. Wire shapes from the V3 docs + the tiagosiebler `rest-client-v3`.
Owner validates live.

- **Demo.** New `bitget.Config.Demo` → REST transport adds `paptrading: 1`
  on every request (production host + a Demo API Key). WS demo URL consts
  (`wspap...`) reserved, not wired.
- **P7-M1 (scaffold + Public core, 7) — done.** UNSIGNED: server-time,
  instruments, tickers, orderbook, candles/history-candles, public fills.
- **P7-M2 (Public extras, 14) — done.** fee-group, score-weights,
  proof-of-reserves, open-interest, funding (current/history), risk-reserve
  (+hour +all), discount-rate, margin-loans, position-tier, oi-limit,
  index-components.
- **P7-M3 (Account core, 12) — done.** assets, funding-assets, info,
  settings, set-leverage, set-hold-mode (one_way/hedge), fee-rate,
  max-transferable, financial-records (cursor), open-interest-limit,
  switch + switch-status. Shared `Client.callSigned` helper.
- **P7-M4 (Trade, 13) — done.** place/modify/cancel (+batch×3),
  cancel-symbol, close-positions, countdown-cancel-all, order-info,
  unfilled/history, fills. **Batch body = top-level JSON array**; responses
  decoded leniently (bare array OR `{list|successList|failureList}`).
- **P7-M5 (Position, 4) — done.** current/history position, max-open-
  available (POST), adlRank. `holdMode`/`posSide` surfaced.
- **P7-M6 (Strategy/plan orders, 5) — done.** place/modify/cancel +
  unfilled/history; TP/SL (full/partial) and trigger orders.
- **P7-M7 (example + docs) — done.** `examples/uta` (read-only; `BITGET_DEMO=1`
  for paper). CHANGELOG/README/handoff updated. Full suite green.

**Open items for live confirmation (Phase 7):** (1) the V3 batch
place/modify response shape — decoded leniently, confirm whether it is a
bare array or `{successList,failureList}` on a real key; (2) the exact
`paptrading` casing accepted by the venue (sent lowercase, docs show both);
(3) `account/info` (`GetInfo`) field set; (4) candle turnover column
presence (index 6, decoded if present).

### 📋 Planned

- **`v2.5` follow-ups** — the V3 re-issues not in the core: wallet /
  transfer / deposit / withdraw, V3 user sub-accounts, V3 tax, V3 broker,
  ins-loan, V3 crypto-loan, V3 earn-elite, V3 copy-futures — plus the V3
  **WebSocket** (public/private streams + WS trading, incl. the `wspap`
  demo hosts). Each: two-layer (lift shared into `bgcommon`), contract
  tests at parity, then a review pause. `uta/` is additive and must not
  change V2 behaviour; `WatchPositions` stays mix-only by venue contract
  until UTA reintroduces unified positions over WS.

---

## 4. Rules & code-style (contract)

- **Language of comments/docs: English** (public project).
- **Explicit variable declarations:** `var name type = value` — always,
  even where `:=` would compile. Same for constants.
- **`camelCase`** for local/unexported identifiers, **`PascalCase`** for
  exported ones.
- **GoDoc on every exported symbol** (functions, types, methods, consts).
  File-level header comment describing the file's role.
- **JSON:** `jsoniter` via `internal/codec` on hot paths; **do not** import
  `encoding/json` directly.
- **Numerics:** `shopspring/decimal` for every price/size; parse exchange
  string fields via `bgcommon.ParseDecimalOrZero` / `ParseInt64OrZero`
  (empty string → zero, never an error). Use `bgcommon.FlexString` for WS
  fields that may arrive as a JSON number *or* a quoted string.
- **Context:** every method takes `context.Context` as the first param;
  calling `context.Background()` inside a method that already has a `ctx` is
  forbidden.
- **Performance (HFT):** target ≤ 100 µs software latency on critical paths
  (parse one WS message, apply one orderbook delta). Avoid allocations in
  hot paths — reuse buffers, prefer `sync.Pool` where it measurably helps,
  keep per-message work allocation-light. Rate-limit observer callbacks must
  be O(1) (non-blocking send to a buffered channel).
- **Errors:** all public methods return `*bitget.Error` with a `Kind`
  (`Network` / `RateLimit` / `Auth` / `InvalidRequest` / `Exchange` /
  `Unknown`); preserve the raw Bitget code in `Error.BitgetCode`. Use
  `bgerr.New`. WS decode errors go to the caller's `errHandler`; REST errors
  are returned. No retry on `Auth` / `InvalidRequest`.
- **Secrets are never logged.** Sanitise sensitive fields.
- **Testing:** unit tests for parsing/mapping/orderbook; **contract tests**
  against a local `httptest.Server` / mock WS pin every wired endpoint and
  guard regressions (e.g. "no productType/marginMode on the spot wire",
  cursor-pagination protocol, WS subscribe-arg shapes). New features land
  with contract tests at parity with the sibling profile.
- **Two-layer discipline:** before adding profile code, check whether the
  logic belongs in `bgcommon`. Lift shared logic; keep profile-specific
  wire shapes local. Never have one section call another section's func.

---

## 5. Integration: config paths, env vars, external APIs, deps

> No real keys live in this repo or in this document.

### SDK configuration (`config.go`)

`bitget.DefaultConfig()` returns production-ready defaults; override fields
and pass to `bitget.NewClient`. Credentials are supplied via the `Config`
struct (`APIKey` / `SecretKey` / `Passphrase`) — the SDK itself does not
read env vars; the *consumer* sources them.

**Default endpoints (production):**

| Purpose | URL |
| --- | --- |
| REST | `https://api.bitget.com` |
| WS public | `wss://ws.bitget.com/v2/ws/public` |
| WS private (login required) | `wss://ws.bitget.com/v2/ws/private` |

A single private WS endpoint serves all contract types (auth is per-UID,
not per-product). Testnet/demo hosts are **not** shipped — deferred to v2.5.
Endpoint vars are overridable (tests point them at a mock server).

**Key tunables (HFT-relevant defaults):** REST timeout 10s; WS read 35s /
write 5s / ping 20s / login 30s; reconnect backoff 200ms→10s with 0.2
jitter; WS read/write buffers 64KB/16KB; orderbook max depth 200.

### Auth / signing

- REST: `ACCESS-KEY` / `ACCESS-SIGN` / `ACCESS-TIMESTAMP` (**milliseconds**)
  / `ACCESS-PASSPHRASE` headers; `sign = base64(HMAC_SHA256(secret,
  timestamp + method + requestPath + body))`.
- WS login (**timestamp in SECONDS**, 10 digits — Bitget V2 WS deviates
  from its own REST ms convention):
  `sign = base64(HMAC_SHA256(secret, timestamp + "GET" + "/user/verify"))`.

### Consumer env vars

Credentials are read by the desk per section (the SDK receives them via
`common.BuildSDKConfig`). Values are placeholders here:

```
BITGET_MIX_API_KEY=...
BITGET_MIX_SECRET_KEY=...
BITGET_MIX_PASSPHRASE=...

# Spot uses its OWN section keys (separate API key with spot permissions):
BITGET_SPOT_API_KEY=...
BITGET_SPOT_SECRET_KEY=...
BITGET_SPOT_PASSPHRASE=...
```

Referenced in desk-core: `.env.example`, `docker-compose.yml`,
`internal/connectors/common/credentials.go`,
`internal/connectors/bitget/common/sdk-config.go` (`BuildSDKConfig`),
`docs/deployment/docker.md`.

> Note: the desk README currently states Spot may reuse the MIX triple if
> the key has spot permissions; the credentials resolver defines distinct
> `BITGET_SPOT_*` vars. Prefer dedicated `BITGET_SPOT_*` keys.

### External APIs / reference docs

- Bitget API docs: `https://www.bitget.com/api-doc/`
  (UTA/V3 intro: `https://www.bitget.com/api-doc/uta/intro`).
- Internal technical spec: `docs/TS-SINGLE-EXCHANGE-SDK.md` (EN) and
  `docs/TS-SINGLE-EXCHANGE-SDK-RU.md`.
- Sibling reference SDKs: `tonymontanov/go-okx`, `tonymontanov/go-bybit`,
  `khanbekov/go-binance` (fork).

### Dependencies

```
github.com/gorilla/websocket   v1.5.3
github.com/json-iterator/go    v1.1.12
github.com/shopspring/decimal  v1.4.0
```

---

## Quick references

- Release notes / per-milestone detail: [`CHANGELOG.md`](./CHANGELOG.md).
- Full status table + usage snippets: [`README.md`](./README.md).
- Error-code table: [`internal/bgerr/codes.go`](./internal/bgerr/codes.go).
- Runnable demos: [`examples/`](./examples) (`go run ./examples/<name>`).
