/*
FILE: common/types/tax.go

DESCRIPTION:
Domain types for the COMMON tax transaction records (/api/v2/tax/*). Field
mapping verified against the Bitget V2 docs and the tiagosiebler reference
types (SpotTransactionRecordV2 / FuturesTransactionRecordV2 /
MarginTransactionRecordV2 / P2PMerchantOrdersV2).
*/

package types

import "github.com/shopspring/decimal"

// SpotTaxRecord — one row of GET tax/spot-record.
type SpotTaxRecord struct {
	ID      string
	Coin    string
	TaxType string // spotTaxType
	Amount  decimal.Decimal
	Fee     decimal.Decimal
	Balance decimal.Decimal
	TimeMs  int64
}

// FuturesTaxRecord — one row of GET tax/future-record.
type FuturesTaxRecord struct {
	ID         string
	Symbol     string
	MarginCoin string
	TaxType    string // futureTaxType
	Amount     decimal.Decimal
	Fee        decimal.Decimal
	TimeMs     int64
}

// MarginTaxRecord — one row of GET tax/margin-record.
type MarginTaxRecord struct {
	ID      string
	Coin    string
	Symbol  string
	TaxType string // marginTaxType
	Amount  decimal.Decimal
	Fee     decimal.Decimal
	Total   decimal.Decimal
	TimeMs  int64
}

// P2PTaxRecord — one row of GET tax/p2p-record.
type P2PTaxRecord struct {
	ID      string
	Coin    string
	TaxType string // p2pTaxType
	Total   decimal.Decimal
	TimeMs  int64
}
