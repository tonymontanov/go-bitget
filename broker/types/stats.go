/*
FILE: broker/types/stats.go

DESCRIPTION:
Domain types for the institutional-broker reporting endpoints
(/api/v2/broker/{subaccounts,commissions,trade-volume,total-commission,
order-commission,rebate-info}). Field mapping verified against the Bitget
V2 broker docs and the tiagosiebler reference types (BrokerSubaccountInfoV2
/ BrokerCommissionV2 / BrokerTradeVolumeV2 / BrokerTotalCommissionV2 /
BrokerOrderCommissionV2 / BrokerRebateInfoV2).
*/

package types

import "github.com/shopspring/decimal"

// BrokerSubaccountInfo — one row of GET broker/subaccounts.
type BrokerSubaccountInfo struct {
	UID                string
	Asset              decimal.Decimal
	FirstTimeDepositMs int64
	FirstTimeTradeMs   int64
	RegisterTimeMs     int64
}

// BrokerCommission — one row of GET broker/commissions.
type BrokerCommission struct {
	UID             string
	Coin            string
	Symbol          string
	DealtAmount     decimal.Decimal
	TotalFee        decimal.Decimal
	DeductedFee     decimal.Decimal
	PaidFee         decimal.Decimal
	MarkUpFee       decimal.Decimal
	TotalCommission decimal.Decimal
}

// BrokerTradeVolume — one row of GET broker/trade-volume.
type BrokerTradeVolume struct {
	UID          string
	Volume       decimal.Decimal
	SpotVolume   decimal.Decimal
	FutureVolume decimal.Decimal
}

// BrokerSegmentCommission — the spot / futures breakdown nested in
// BrokerTotalCommission.
type BrokerSegmentCommission struct {
	TradingVolume  decimal.Decimal
	TradingFee     decimal.Decimal
	PureTradingFee decimal.Decimal
	Commission     decimal.Decimal
}

// BrokerTotalCommission — one daily row of GET broker/total-commission.
type BrokerTotalCommission struct {
	Date               string
	TotalTradingVolume decimal.Decimal
	TotalActiveTraders int64
	TotalCommission    decimal.Decimal
	Spot               BrokerSegmentCommission
	Futures            BrokerSegmentCommission
}

// BrokerOrderCommissionItem — one row of GET broker/order-commission.
//
//	BizType:    spot | futures
//	SubBizType: spot_trade | spot_margin | usdt_futures | coin_futures | usdc_futures
type BrokerOrderCommissionItem struct {
	FillID       string
	OrderID      string
	TimeMs       int64
	ClientOid    string
	BizType      string
	SubBizType   string
	Symbol       string
	Volume       decimal.Decimal
	Fee          decimal.Decimal
	PureFee      decimal.Decimal
	RebateAmount decimal.Decimal
}

// BrokerRebateInfo — GET broker/rebate-info.
//
//	AffiliationType: affiliate | official
type BrokerRebateInfo struct {
	AffiliationType          string
	UserLevel                string
	ClientSpotRebateRatio    decimal.Decimal
	ClientFuturesRebateRatio decimal.Decimal
}
