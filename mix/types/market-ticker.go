/*
FILE: mix/types/market-ticker.go

DESCRIPTION:
MarketTicker — last/mark/index/funding payload returned by
GET /api/v2/mix/market/ticker. Bitget bundles every price the desk
typically polls into a single response, so the SDK exposes one
type instead of three.

USES:
  - WatchMarkPrice / WatchIndexPrice / WatchLastPrice (M4) call this
    endpoint as a fallback when the WS stream has not yet pushed an
    update.
  - The desk's pre-trade validators read MarkPrice / IndexPrice for
    safety checks.

NUMERIC HANDLING:
Same conventions as SymbolInfo — shopspring/decimal everywhere,
float conversion only at the edge.
*/

package types

import "github.com/shopspring/decimal"

// MarketTicker — composite price snapshot for one symbol.
type MarketTicker struct {
	// Symbol — e.g. "BTCUSDT".
	Symbol string
	// LastPrice — most recent trade price.
	LastPrice decimal.Decimal
	// MarkPrice — exchange-published fair price (drives liquidations).
	MarkPrice decimal.Decimal
	// IndexPrice — composite spot index used by Bitget.
	IndexPrice decimal.Decimal
	// AskPrice — best ask on the order book at fetch time.
	AskPrice decimal.Decimal
	// AskSize — size resting at AskPrice. 0 when Bitget did not
	// populate the field (some pre-launch contracts).
	AskSize decimal.Decimal
	// BidPrice — best bid on the order book at fetch time.
	BidPrice decimal.Decimal
	// BidSize — size resting at BidPrice. 0 when Bitget did not
	// populate the field.
	BidSize decimal.Decimal
	// FundingRate — most recent settled funding rate (next-period rate
	// is published separately by /current-fund-rate; M1 ships only the
	// one returned by the ticker endpoint).
	FundingRate decimal.Decimal
	// NextFundingTimeMs — predicted timestamp of the next funding
	// settlement (ms since epoch). 0 when Bitget did not include the
	// field on the response (some symbols).
	NextFundingTimeMs int64
	// TsMs — Bitget publish timestamp for this ticker (ms).
	TsMs int64
}
