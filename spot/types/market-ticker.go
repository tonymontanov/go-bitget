/*
FILE: spot/types/market-ticker.go

DESCRIPTION:
MarketTicker — composite price snapshot returned by
GET /api/v2/spot/market/tickers.

DIFFERENCES FROM mix/types.MarketTicker:

  - No MarkPrice / IndexPrice / FundingRate / NextFundingTimeMs —
    those concepts don't exist on plain spot.
  - Best bid/ask sizes are present (`bidSz` / `askSz`) — same as mix.
  - Adds 24h roll-up metrics (high24h, low24h, openUtc, change24h,
    changeUtc24h) that the spot endpoint bundles into the ticker
    response while mix exposes them under a different family
    (/market/ticker doesn't return change24h).

NUMERIC HANDLING:
Same conventions as elsewhere in the SDK — shopspring/decimal
everywhere, float conversion only at the edge.
*/

package types

import "github.com/shopspring/decimal"

// MarketTicker — composite price snapshot for one spot symbol.
type MarketTicker struct {
	// Symbol — e.g. "BTCUSDT".
	Symbol string

	// LastPrice — most recent trade price.
	LastPrice decimal.Decimal
	// AskPrice — best ask on the book at fetch time.
	AskPrice decimal.Decimal
	// AskSize — size resting at AskPrice. 0 when Bitget did not
	// populate the field.
	AskSize decimal.Decimal
	// BidPrice — best bid on the book at fetch time.
	BidPrice decimal.Decimal
	// BidSize — size resting at BidPrice. 0 when Bitget did not
	// populate the field.
	BidSize decimal.Decimal

	// 24-hour roll-ups.

	// High24h — highest trade price in the trailing 24h.
	High24h decimal.Decimal
	// Low24h — lowest trade price in the trailing 24h.
	Low24h decimal.Decimal
	// Open — open price 24h ago (rolling window).
	Open decimal.Decimal
	// OpenUtc — open price at the most recent UTC midnight.
	OpenUtc decimal.Decimal
	// BaseVolume — traded volume in BASE over the trailing 24h.
	BaseVolume decimal.Decimal
	// QuoteVolume — traded volume in QUOTE over the trailing 24h.
	QuoteVolume decimal.Decimal
	// UsdtVolume — traded volume in USDT (Bitget computes this for
	// non-USDT-quote pairs as a convenience).
	UsdtVolume decimal.Decimal
	// Change24h — fractional price change vs the price 24h ago
	// (0.01 = 1%).
	Change24h decimal.Decimal
	// ChangeUtc24h — fractional price change vs OpenUtc.
	ChangeUtc24h decimal.Decimal

	// TsMs — Bitget publish timestamp for this ticker (ms).
	TsMs int64
}
