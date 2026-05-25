/*
Package mix implements the Bitget legacy V2 MIX profile —
USDT-margined perpetual futures (productType=USDT-FUTURES on the wire).

# What "MIX" means

"MIX" is Bitget's own term for the legacy V2 contracts API that lives
under /api/v2/mix/* (REST) and the public/private V2 WebSocket channels
on wss://ws.bitget.com/v2/ws/{public,private}. The endpoint family
covers USDT-margined, USDC-margined and coin-margined perpetuals; the
SDK parameterises the wire payload by ProductType but in v1.0 only
USDT-FUTURES is exercised by the desk.

UTA / V3 endpoints (/api/v3/*) are intentionally NOT covered by this
package. They will land in a separate `uta/` profile in v2.5.

# Architecture

The profile follows the same two-layer pattern as go-bybit / go-okx:

  - Root client (github.com/tonymontanov/go-bitget/v2.Client) owns the
    REST transport, signer, logger and config. It exposes Mix() any,
    which is wired by this package's init() via
    bitget.RegisterMixFactory.
  - mix.Client is a thin coordinator that holds a reference to the root
    client and exposes four domain sub-clients:
    Trading() / Account() / MarketData() / Stream().

# v1.0 status

  - MarketData : production-ready in M1 (REST). Implements
    GetSymbolInfo, GetOrderBook, GetMarketTicker,
    GetHistoricalCandles, GetHistoricalCandles1m.
  - Trading    : stubs in M1; full implementation lands in M2 along
    with REST place/amend/cancel + batch endpoints.
  - Account    : stubs in M1; full implementation lands in M3
    (positions, leverage, position-mode, close).
  - Stream     : stubs in M1; full implementation lands in M4
    (public WS + order-book engine) and M5 (private WS).

# Numeric handling

All wire numbers (prices, sizes, balances, funding rates) are strings
on the Bitget side. They are decoded into shopspring/decimal at the
SDK boundary and exposed to callers as decimal.Decimal. Floating-point
conversion happens only at the very edge (desk adapter) via
decimal.InexactFloat64 — never inside the SDK.

# Symbols and product type

For productType=USDT-FUTURES Bitget V2 uses the bare base+quote symbol
with no suffix (e.g. "BTCUSDT", "ETHUSDT"). The legacy "_UMCBL" suffix
was removed in V2; the SDK accepts and returns symbols verbatim.

# Concurrency

mix.Client and every sub-client are safe for concurrent use by
multiple goroutines.
*/
package mix
