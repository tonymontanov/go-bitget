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
Last tagged milestone: **`v2.0.0-m6`** (latest stable line: **`v1.2.2`**).

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

  uta/                 # v2.5 — Unified Trading Account (planned, not started)
  examples/            # runnable demos: marketdata / place-order / private-stream
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

Desk-core (`bitget-connector` branch): MIX connector cycle (B3–B5) and Spot
connector cycle (T2b account/history, T2c public WS, `WatchOpenOrders` over
SDK m5 private streams) are implemented and tested against the local SDK.

### 🔧 In progress / open decision

- No active coding task. Last completed: `v2.0.0-m6` tag + this handoff.
- Pending product decision on next step (options previously surfaced):
  desk-core T3 follow-up, a `v2.0` GA release cut, a live smoke-test, or
  starting `v2.5` UTA.

### 📋 Planned

- **`v2.0` GA** — fold the m1–m6 milestones into a stable spot release
  (final error-code audit, examples, CHANGELOG roll-up).
- **`v2.5` — `uta/` profile** — Bitget V3 Unified Trading Account: hedge
  mode, demo / testnet hosts (URL constants intentionally NOT shipped
  yet). `WatchPositions` stays mix-only by venue contract; UTA reintroduces
  unified positions.

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
