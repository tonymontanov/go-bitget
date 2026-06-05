/*
FILE: broker/types/copy.go

DESCRIPTION:
Domain types for the copy-trading BROKER reads
(/api/v2/copy/mix-broker/{query-traders,query-history-traces,
query-current-traces}). These endpoints are typed as `any` in the
tiagosiebler reference and the official broker docs are sparse; the field
mapping below follows the documented copy-trading "Get My Traders" shape
and the copy-order trace schema used elsewhere in this SDK. Decoding is
lenient — unknown fields are ignored — so extra venue fields will not
break callers. (Open item for live confirmation.)
*/

package types

import "github.com/shopspring/decimal"

// BrokerTrader — one row of GET copy/mix-broker/query-traders: a trader
// visible under the broker, with follow limits and aggregate copy stats.
type BrokerTrader struct {
	TraderID          string
	TraderName        string
	CertificationType string // Certified | Uncertified

	MaxFollowLimit    int64
	BGBMaxFollowLimit int64
	FollowCount       int64
	BGBFollowCount    int64

	TraceTotalMarginAmount decimal.Decimal
	TraceTotalNetProfit    decimal.Decimal
	TraceTotalProfit       decimal.Decimal

	CurrentTradingPairs []string
	FollowerTimeMs      int64
}

// BrokerOrderTrace — one copy-order trace row of
// query-history-traces (closed) or query-current-traces (live). The close
// / profit fields are populated only for history traces.
type BrokerOrderTrace struct {
	TrackingNo   string
	TraderID     string
	TraderName   string
	Symbol       string
	PosSide      string
	OpenOrderID  string
	CloseOrderID string

	OpenLeverage decimal.Decimal
	OpenPriceAvg decimal.Decimal
	OpenSize     decimal.Decimal
	OpenFee      decimal.Decimal
	OpenTimeMs   int64

	ClosePriceAvg decimal.Decimal
	CloseSize     decimal.Decimal
	CloseFee      decimal.Decimal
	CloseTimeMs   int64

	NetProfit  decimal.Decimal
	ProfitRate decimal.Decimal
	AchievedPL decimal.Decimal
}
