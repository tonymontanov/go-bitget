/*
FILE: uta/types/public.go

DESCRIPTION:
Domain types for the V3 UTA public market-data surface (core slice:
instruments, tickers, orderbook, candles, public fills). Field mapping
verified against the Bitget V3 docs and the tiagosiebler reference types
(InstrumentV3 / TickerV3 / OrderBookV3 / CandlestickV3 / PublicFillV3).
Money / sizes / ratios use decimal; precisions are int32; timestamps are
int64 epoch-ms.
*/

package types

import "github.com/shopspring/decimal"

// Instrument — one row of GET market/instruments. Spot / margin / futures
// share the struct; product-specific fields are zero when not applicable.
type Instrument struct {
	Symbol    string
	Category  Category
	BaseCoin  string
	QuoteCoin string
	Status    string

	PricePrecision    int32
	QuantityPrecision int32
	QuotePrecision    int32

	MinOrderQty       decimal.Decimal
	MaxOrderQty       decimal.Decimal
	MaxMarketOrderQty decimal.Decimal
	MinOrderAmount    decimal.Decimal

	BuyLimitPriceRatio  decimal.Decimal
	SellLimitPriceRatio decimal.Decimal

	// Futures-specific (zero for spot / margin).
	MakerFeeRate decimal.Decimal
	TakerFeeRate decimal.Decimal
	SymbolType   string
	MinLeverage  decimal.Decimal
	MaxLeverage  decimal.Decimal
	FundInterval string
	LaunchTimeMs int64
	DeliveryTime int64
}

// Ticker — one row of GET market/tickers.
type Ticker struct {
	Category     Category
	Symbol       string
	LastPrice    decimal.Decimal
	OpenPrice24h decimal.Decimal
	HighPrice24h decimal.Decimal
	LowPrice24h  decimal.Decimal
	Ask1Price    decimal.Decimal
	Ask1Size     decimal.Decimal
	Bid1Price    decimal.Decimal
	Bid1Size     decimal.Decimal
	Price24hPcnt decimal.Decimal
	Volume24h    decimal.Decimal
	Turnover24h  decimal.Decimal

	// Futures-specific (zero for spot).
	IndexPrice   decimal.Decimal
	MarkPrice    decimal.Decimal
	FundingRate  decimal.Decimal
	OpenInterest decimal.Decimal
}

// PriceLevel — one [price, size] book level.
type PriceLevel struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}

// OrderBook — GET market/orderbook depth snapshot.
type OrderBook struct {
	Asks   []PriceLevel
	Bids   []PriceLevel
	TimeMs int64
}

// Candle — one candlestick of GET market/candles | history-candles.
type Candle struct {
	TimeMs   int64
	Open     decimal.Decimal
	High     decimal.Decimal
	Low      decimal.Decimal
	Close    decimal.Decimal
	Volume   decimal.Decimal
	Turnover decimal.Decimal
}

// PublicFill — one row of GET market/fills (recent public trades).
type PublicFill struct {
	ExecID string
	Price  decimal.Decimal
	Size   decimal.Decimal
	Side   string
	TimeMs int64
}
