/*
FILE: spot/types/symbol-info.go

DESCRIPTION:
SymbolInfo — instrument specification for a single SPOT symbol,
returned by GET /api/v2/spot/public/symbols.

WHY THESE FIELDS:
The desk treats SymbolInfo as the authoritative source of price/size
filters when validating quotes:

  - PriceTick      → snap quote prices to the venue's price step.
  - SizeStep       → snap quote sizes to the venue's lot size.
  - QuoteStep      → spot-specific: market BUY orders use quote-side
                     size (USDT), so the increment lives at quote
                     precision rather than base precision.
  - MinTradeAmount → reject quotes whose base quantity is below the
                     venue minimum (Bitget rejects with
                     code=43005 / 43009 otherwise).
  - MinTradeUSDT   → reject quotes whose notional is below the venue
                     quote-side minimum.
  - MaxTradeAmount → upper cap on a single order; rarely binding for
                     market makers but pinned anyway for symmetry
                     with the desk's pre-trade validators.
  - PricePrecision /
    QuantityPrecision /
    QuotePrecision → reserved for the desk's float-formatting layer.
  - MakerFeeRate /
    TakerFeeRate   → reported in absolute decimal (0.0008 = 0.08%).
                     The desk uses these to model effective spread.

DIFFERENCES FROM mix/types.SymbolInfo:

  - No ProductType (spot is the product).
  - No MaxLever / MinLever (no leverage on plain spot).
  - QuotePrecision / QuoteStep are present (mix has none — every
    mix order is denominated in BASE).
  - Status is venue-specific: "online", "offline", "halt", etc.
    Stored as a raw string because the set is open-ended.

NUMERIC HANDLING:
All numeric fields stay in shopspring/decimal. The desk converts them
to float64 with InexactFloat64 at the very edge.
*/

package types

import "github.com/shopspring/decimal"

// SymbolInfo — instrument specification for one spot symbol.
type SymbolInfo struct {
	// Symbol — e.g. "BTCUSDT".
	Symbol string
	// BaseCoin — e.g. "BTC".
	BaseCoin string
	// QuoteCoin — e.g. "USDT".
	QuoteCoin string

	// PriceTick — minimum price increment, derived from PricePrecision.
	PriceTick decimal.Decimal
	// SizeStep — minimum base-side size increment, derived from
	// QuantityPrecision.
	SizeStep decimal.Decimal
	// QuoteStep — minimum quote-side size increment, used when placing
	// market BUY orders (their `size` field is in quote currency).
	QuoteStep decimal.Decimal

	// MinTradeAmount — minimum order quantity in BASE asset.
	MinTradeAmount decimal.Decimal
	// MaxTradeAmount — maximum order quantity in BASE asset (per order).
	MaxTradeAmount decimal.Decimal
	// MinTradeUSDT — minimum order notional in QUOTE asset.
	MinTradeUSDT decimal.Decimal

	// MakerFeeRate / TakerFeeRate — absolute decimal (0.0008 = 0.08%).
	MakerFeeRate decimal.Decimal
	TakerFeeRate decimal.Decimal

	// PricePrecision — digits after the decimal point for price.
	PricePrecision int
	// QuantityPrecision — digits after the decimal point for base-side
	// size (limit / market-sell quantity).
	QuantityPrecision int
	// QuotePrecision — digits after the decimal point for quote-side
	// size (market-buy quantity).
	QuotePrecision int

	// Status — Bitget reports "online", "offline", "halt", etc. Stored
	// as a raw string because the set is open-ended; the SDK does not
	// act on it (callers can compare against literals if needed).
	Status string
}
