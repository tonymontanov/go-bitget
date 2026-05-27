/*
FILE: spot/doc.go

DESCRIPTION:
Bitget V2 SPOT profile client. Mirrors the architectural shape of the
mix/ profile (see mix/doc.go) but targets the plain spot market —
non-margin, no positions, no leverage, no productType/marginMode/
marginCoin pinning. Sub-clients (Trading, Account, MarketData, Stream)
delegate REST work to the parent bitget.Client.REST() and use the
shared internal/bgcommon helpers for everything that is wire-identical
across profiles (numeric parsers, FlexString, RestDoer interface,
orderbook engine).

ENDPOINT FAMILIES (Bitget V2 SPOT, https://www.bitget.com/api-doc/spot):

	REST:
	  - GET    /api/v2/spot/public/symbols           (M2)
	  - GET    /api/v2/spot/public/coins             (M2/M3)
	  - GET    /api/v2/spot/market/tickers           (M2)
	  - GET    /api/v2/spot/market/orderbook         (M2)
	  - GET    /api/v2/spot/market/candles           (M2)
	  - GET    /api/v2/spot/market/history-candles   (M2)
	  - GET    /api/v2/spot/market/fills             (M2)
	  - GET    /api/v2/spot/market/fills-history     (M2)
	  - POST   /api/v2/spot/trade/place-order        (M2)
	  - POST   /api/v2/spot/trade/batch-orders       (M2)
	  - POST   /api/v2/spot/trade/cancel-order       (M2)
	  - POST   /api/v2/spot/trade/cancel-symbol-order (M2)
	  - POST   /api/v2/spot/trade/cancel-replace-order (M2)
	  - POST   /api/v2/spot/trade/batch-cancel-replace-order (M2)
	  - POST   /api/v2/spot/trade/orderInfo          (M2)
	  - GET    /api/v2/spot/trade/unfilled-orders    (M3)
	  - GET    /api/v2/spot/trade/history-orders     (M3)
	  - GET    /api/v2/spot/trade/fills              (M3)
	  - GET    /api/v2/spot/account/info             (M3)
	  - GET    /api/v2/spot/account/assets           (M3)

	WebSocket (public  /v2/ws/public, instType="SPOT"):
	  - books   / books5 / books15 / books50         (M4)
	  - ticker                                       (M4)
	  - trade                                        (M4)
	  - candle{interval}                             (M4)

	WebSocket (private /v2/ws/private, instType="SPOT"):
	  - account                                      (M5)
	  - orders                                       (M5)
	  - fills                                        (M5)

DIFFERENCES FROM mix/:

  - No ProductType / MarginMode / MarginCoin pinning. Spot symbols are
    self-contained ("BTCUSDT" carries the base/quote pair, no margin
    coin is sent on the wire).
  - No HoldSide / TradeSide. Spot is one-way only — every order is
    side=buy or side=sell with no open/close leg semantics.
  - No SetLeverage / SetPositionMode / GetPosition / ClosePosition.
    Spot has no positions; the AccountClient instead exposes balance
    snapshots per coin.
  - The "fills" channel is the spot equivalent of mix' execution
    stream; private orders/account topics work the same way once
    instType is switched to "SPOT".

TWO-LAYER ARCHITECTURE:

Anything that is identical on the wire between mix and spot lives in
internal/bgcommon (parse helpers, RestDoer, FlexString) or
internal/bgcommon/orderbook (V2 books CRC32 engine). The spot/
package consumes these directly — never via cross-profile delegation
— mirroring the rule already enforced for mix/.

ROADMAP:

  - M1 (this milestone) : root sub-client + factory registration +
                          empty stubs. No functional REST/WS yet.
  - M2                  : MarketData (tickers/orderbook/candles/fills/
                          symbols) + Trading (place/modify/cancel/
                          batch). REST only.
  - M3                  : Account (balance/info/sync) + Trading
                          history (open/history/fills).
  - M4                  : public WebSocket (books with CRC32 resync,
                          ticker, trade, candles).
  - M5                  : private WebSocket (account/orders/fills) with
                          login + auto-resub.

Aggregate release tag: v2.0.0 once M1..M5 land. Each milestone is
tagged separately (v2.0.0-m1, v2.0.0-m2, ...) to let the desk-side
connector integrate phase-by-phase, mirroring the mix/ release
cadence (v1.0).
*/

package spot
