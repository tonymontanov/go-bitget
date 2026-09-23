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

	// PriceMultiplier / QuantityMultiplier — the REAL price tick and
	// quantity step. They are NOT always 10^-precision: on 2026-09-21
	// 22 USDT-FUTURES symbols had a quantity step above it (SHIBUSDT:
	// quantityPrecision=0 but quantityMultiplier=10000; PEPEUSDT 1000;
	// NOT / ATH / SUN 10 ...). The venue silently floors an order's qty
	// to the step, so sizing by precision alone sends a different
	// quantity than intended. Zero when the venue omits the field
	// (fall back to 10^-precision).
	PriceMultiplier    decimal.Decimal
	QuantityMultiplier decimal.Decimal

	MinOrderQty       decimal.Decimal
	MaxOrderQty       decimal.Decimal
	MaxMarketOrderQty decimal.Decimal
	MinOrderAmount    decimal.Decimal

	BuyLimitPriceRatio  decimal.Decimal
	SellLimitPriceRatio decimal.Decimal

	// Futures-specific (zero for spot / margin).
	MakerFeeRate decimal.Decimal
	TakerFeeRate decimal.Decimal
	// SymbolType is the venue's asset class ("crypto", ...) — NOT the
	// contract kind. Live 2026-09-23: every USDT-FUTURES row carries
	// symbolType="crypto"; the perpetual/delivery distinction is in Type.
	SymbolType string
	// Type is the contract kind: "perpetual" or "delivery" for futures,
	// empty for spot / margin (the venue omits the field there).
	Type         string
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
