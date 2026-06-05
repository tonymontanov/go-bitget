/*
FILE: earn/types/sharkfin.go

DESCRIPTION:
Domain types for the EARN Shark Fin (structured product) category. Field
mapping verified against the Bitget V2 earn/sharkfin docs and the
tiagosiebler reference types (EarnSharkfinProductV2 /
EarnSharkfinAccountV2 / EarnSharkfinAssetV2 / EarnSharkfinRecordV2 /
EarnSharkfinSubscriptionDetailV2). *Time fields are Unix-ms timestamps.
*/

package types

import "github.com/shopspring/decimal"

// SharkFinProduct — one row of GET earn/sharkfin/product.
type SharkFinProduct struct {
	ProductID           string
	ProductName         string
	ProductCoin         string
	SubscribeCoin       string
	Status              string
	Period              string
	LowerRate           decimal.Decimal
	DefaultRate         decimal.Decimal
	UpperRate           decimal.Decimal
	MinAmount           decimal.Decimal
	LimitAmount         decimal.Decimal
	SoldAmount          decimal.Decimal
	FarmingStartTimeMs  int64
	FarmingEndTimeMs    int64
	InterestStartTimeMs int64
	StartTimeMs         int64
	EndTimeMs           int64
}

// SharkFinAccount — response of GET earn/sharkfin/account.
type SharkFinAccount struct {
	BTCSubscribeAmount   decimal.Decimal
	USDTSubscribeAmount  decimal.Decimal
	BTCHistoricalAmount  decimal.Decimal
	USDTHistoricalAmount decimal.Decimal
	BTCTotalEarning      decimal.Decimal
	USDTTotalEarning     decimal.Decimal
}

// SharkFinAsset — one held shark-fin position (GET earn/sharkfin/assets).
type SharkFinAsset struct {
	ProductID           string
	ProductCoin         string
	SubscribeCoin       string
	Trend               string
	ProductStatus       string
	InterestStartTimeMs int64
	InterestEndTimeMs   int64
	SettleTimeMs        int64
	InterestAmount      decimal.Decimal
}

// SharkFinRecord — one row of GET earn/sharkfin/records.
type SharkFinRecord struct {
	OrderID string
	Product string
	Period  string
	Type    string
	Amount  decimal.Decimal
	TimeMs  int64
}

// SharkFinSubscribeInfo — response of GET earn/sharkfin/subscribe-info.
type SharkFinSubscribeInfo struct {
	ProductCoin        string
	SubscribeCoin      string
	Period             string
	ProfitPrecision    string
	SubscribePrecision string
	InterestTimeMs     int64
	ExpirationTimeMs   int64
	MinPrice           decimal.Decimal
	CurrentPrice       decimal.Decimal
	MaxPrice           decimal.Decimal
	MinRate            decimal.Decimal
	DefaultRate        decimal.Decimal
	MaxRate            decimal.Decimal
	ProductMinAmount   decimal.Decimal
	AvailableBalance   decimal.Decimal
	UserAmount         decimal.Decimal
	RemainingAmount    decimal.Decimal
}
