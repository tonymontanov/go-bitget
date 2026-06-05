/*
FILE: convert/types/convert.go

DESCRIPTION:
Domain types for the CONVERT profile. Field mapping verified against the
Bitget V2 convert docs and the tiagosiebler reference types
(ConvertCurrencyV2 / ConvertQuotedPriceV2 / ConvertTradeResponseV2 /
ConvertRecordV2 / BGBConvertCoinV2 / ConvertBGBResponseV2 /
BGBConvertHistoryV2).
*/

package types

import "github.com/shopspring/decimal"

// ConvertCurrency — one row of GET convert/currencies. MaxAmount /
// MinAmount are the per-call bounds: as a fromCoin they cap the
// consumable amount, as a toCoin the redeemable amount.
type ConvertCurrency struct {
	Coin      string
	Available decimal.Decimal
	MaxAmount decimal.Decimal
	MinAmount decimal.Decimal
}

// QuotedPrice — response of GET convert/quoted-price (an RFQ). TraceID +
// CnvtPrice must be echoed back to Trade within the quote TTL (~8s).
// CnvtPrice = fromCoin price / toCoin price.
type QuotedPrice struct {
	TraceID      string
	FromCoin     string
	ToCoin       string
	FromCoinSize decimal.Decimal
	ToCoinSize   decimal.Decimal
	CnvtPrice    decimal.Decimal
	Fee          decimal.Decimal
}

// TradeRequest — input to POST convert/trade. All fields are required;
// FromCoinSize / ToCoinSize / CnvtPrice / TraceID come from the matching
// QuotedPrice. Sizes/price are strings to forward the exact RFQ values
// without re-rounding.
type TradeRequest struct {
	FromCoin     string
	ToCoin       string
	FromCoinSize string
	ToCoinSize   string
	CnvtPrice    string
	TraceID      string
}

// TradeResult — response of POST convert/trade.
type TradeResult struct {
	ToCoin     string
	ToCoinSize decimal.Decimal
	CnvtPrice  decimal.Decimal
	TimeMs     int64
}

// ConvertRecord — one row of GET convert/convert-record.
type ConvertRecord struct {
	ID           string
	FromCoin     string
	ToCoin       string
	FromCoinSize decimal.Decimal
	ToCoinSize   decimal.Decimal
	CnvtPrice    decimal.Decimal
	Fee          decimal.Decimal
	TimeMs       int64
}

// BGBFeeTier — one fee-rate row of a BGBConvertCoin.
type BGBFeeTier struct {
	FeeRate decimal.Decimal
	Fee     decimal.Decimal
}

// BGBConvertCoin — one row of GET convert/bgb-convert-coin-list: a coin
// eligible for small-balance conversion to BGB. BGBEstAmount is the
// estimated BGB you would receive for the full Available balance.
type BGBConvertCoin struct {
	Coin         string
	Available    decimal.Decimal
	BGBEstAmount decimal.Decimal
	Precision    string
	FeeDetail    []BGBFeeTier
	TimeMs       int64
}

// BGBConvertOrder — one row of the POST convert/bgb-convert result.
type BGBConvertOrder struct {
	Coin    string
	OrderID string
}

// BGBFeeDetail — one fee row of a BGBConvertHistory entry.
type BGBFeeDetail struct {
	FeeCoin string
	Fee     decimal.Decimal
}

// BGBConvertHistory — one row of GET convert/bgb-convert-records.
type BGBConvertHistory struct {
	OrderID       string
	FromCoin      string
	ToCoin        string
	FromAmount    decimal.Decimal
	ToAmount      decimal.Decimal
	FromCoinPrice decimal.Decimal
	ToCoinPrice   decimal.Decimal
	FeeDetail     []BGBFeeDetail
	Status        string
	TimeMs        int64
}
