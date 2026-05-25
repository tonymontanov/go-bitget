/*
FILE: mix/types/symbol-info.go

DESCRIPTION:
SymbolInfo — instrument specification for a single MIX symbol, returned
by GET /api/v2/mix/market/contracts.

WHY THESE FIELDS:
The desk treats SymbolInfo as the authoritative source of price/size
filters when validating quotes:

  - PriceTick      → snap quote prices to the venue's price step.
  - SizeStep       → snap quote sizes to the venue's lot size.
  - MinTradeNum    → reject quotes below the minimum order quantity.
  - MinTradeUSDT   → reject quotes whose notional is below the venue
    minimum (Bitget enforces this server-side and
    rejects with 40808 / 40909 otherwise).
  - PricePrecision /
    SizePrecision  → reserved for the desk's float-formatting layer.

NUMERIC HANDLING:
All numeric fields stay in shopspring/decimal. The desk converts them
to float64 with InexactFloat64 at the very edge.

WIRE NOTES:
Bitget returns numeric filters as plain decimal strings ("0.1", "0.001",
"5"). Empty strings (which Bitget occasionally emits for not-yet-live
contracts) are normalised to zero by the parser.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// SymbolInfo — instrument specification.
type SymbolInfo struct {
	// Symbol — e.g. "BTCUSDT".
	Symbol string
	// BaseCoin — e.g. "BTC".
	BaseCoin string
	// QuoteCoin — e.g. "USDT".
	QuoteCoin string
	// ProductType — USDT-FUTURES / USDC-FUTURES / COIN-FUTURES.
	ProductType roottypes.ProductType
	// PriceTick — minimum price increment.
	PriceTick decimal.Decimal
	// SizeStep — minimum size (lot) increment.
	SizeStep decimal.Decimal
	// MinTradeNum — minimum order quantity in BASE asset.
	MinTradeNum decimal.Decimal
	// MinTradeUSDT — minimum order notional in QUOTE asset.
	MinTradeUSDT decimal.Decimal
	// PricePrecision — digits after the decimal point for price (info only).
	PricePrecision int
	// SizePrecision — digits after the decimal point for size (info only).
	SizePrecision int
	// MaxLever — maximum leverage allowed by Bitget for this symbol
	// (raw integer, e.g. 125 for 125x).
	MaxLever int
	// MinLever — minimum leverage (usually 1).
	MinLever int
	// SymbolStatus — Bitget reports "normal", "off", "pre-launch" etc.
	// Stored as a raw string because the set is open-ended; the SDK does
	// not act on it (callers can compare against literals if needed).
	SymbolStatus string
}
