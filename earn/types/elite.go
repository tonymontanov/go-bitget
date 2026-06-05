/*
FILE: earn/types/elite.go

DESCRIPTION:
Domain types for the EARN On-Chain Elite category. Field mapping verified
against the Bitget V2 earn/elite reference types (EarnEliteProductV2 /
EarnEliteAssetV2 / EarnEliteRecordV2 / EarnEliteSubscribeInfoV2 /
EarnEliteRedeemInfoV2 and friends). The redeemType / paymentAccount
fields arrive as either a string or a string array on the wire and are
normalised to a []string here.
*/

package types

import "github.com/shopspring/decimal"

// EliteSubscriptionCoin — one accepted subscription coin of an elite
// product (subscriptionCoinList).
type EliteSubscriptionCoin struct {
	SubscriptionCoin string
	Precision        string
	FeeRate          decimal.Decimal
	ExchangeRate     decimal.Decimal
	RemainQuota      decimal.Decimal
	MinAmount        decimal.Decimal
}

// EliteProduct — one row of GET earn/elite/product.
type EliteProduct struct {
	ProductID            string
	Coin                 string
	MinApr               decimal.Decimal
	MaxApr               decimal.Decimal
	SellOut              string // "YES" / "NO"
	SubscriptionCoinList []EliteSubscriptionCoin
}

// EliteAsset — one held elite position (GET earn/elite/assets).
type EliteAsset struct {
	ProductID         string
	ProductCoin       string
	SubscriptionCoin  string
	InterestCoin      string
	HoldingAmount     decimal.Decimal
	USDTHoldingAmount decimal.Decimal
	ExchangeRate      decimal.Decimal
	Apr               decimal.Decimal
	MinApy            decimal.Decimal
	MaxApy            decimal.Decimal
	ExchangeAmount    decimal.Decimal
	UnsettledBGPoints decimal.Decimal
	TotalProfit       decimal.Decimal
	ProjectList       []string // projectName values
}

// EliteRecord — one row of GET earn/elite/records.
type EliteRecord struct {
	RecordID               string
	ProductID              string
	Coin                   string
	Status                 string
	ReceivedCoin           string
	ReceivingAccount       string
	ActualReceivingAccount string
	ExchangeRate           decimal.Decimal
	ReceivedAmount         decimal.Decimal
	InvestAmount           decimal.Decimal
	FeeRate                decimal.Decimal
	SettlePoints           decimal.Decimal
	Fee                    decimal.Decimal
	RedeemType             []string
	PaymentAccount         []string
}

// EliteSubscribeInfo — response of GET earn/elite/subscribe-info.
type EliteSubscribeInfo struct {
	ProductSubID         string
	ProductCoin          string
	Precision            string
	InterestTime         string
	SettleTime           string
	MinAmount            decimal.Decimal
	RemainQuota          decimal.Decimal
	ExchangeRate         decimal.Decimal
	FeeRate              decimal.Decimal
	SubscriptionCoinList []EliteSubscriptionCoin
}

// EliteSubscribeRequest — input to POST earn/elite/subscribe.
// ProductSubID and Amount are required; Coin and PaymentAccount
// ("spot" | "unified") are optional.
type EliteSubscribeRequest struct {
	ProductSubID   string
	Amount         string
	Coin           string
	PaymentAccount string
}

// EliteRedeemRequest — input to POST earn/elite/redeem. ProductID,
// ProductSubID, RedeemType ("fast" | "standard"), Amount and
// ReceiveAccount ("spot" | "unified") are required; AdvancedSettle
// ("yes" | "no") and Coin are optional.
type EliteRedeemRequest struct {
	ProductID      string
	ProductSubID   string
	RedeemType     string
	Amount         string
	ReceiveAccount string
	AdvancedSettle string
	Coin           string
}

// EliteBgusdReceiveCoin — one BGUSD receive-coin option of a redeem-info.
type EliteBgusdReceiveCoin struct {
	BgusdReceiveCoin  string
	BgusdExchangeRate decimal.Decimal
}

// EliteRedeemMode — one redemption mode of a redeem-info.
type EliteRedeemMode struct {
	RedeemType      string // "fast" / "standard"
	RedeemFeeRate   decimal.Decimal
	RemainQuota     decimal.Decimal
	RedeemScale     decimal.Decimal
	MinRedeemAmount decimal.Decimal
	RedeemDelayDate string
	RedeemTime      string
}

// EliteRedeemInfo — response of GET earn/elite/redeem-info.
type EliteRedeemInfo struct {
	ProductID                string
	ProductSubID             string
	ProductCoin              string
	SubscriptionCoin         string
	ProfitCoin               string
	ReceivedCoin             string
	ExchangeRate             decimal.Decimal
	TotalUnPayInterestAmount decimal.Decimal
	PreSettleApr             decimal.Decimal
	UnsettledPoints          decimal.Decimal
	BgusdReceiveCoinList     []EliteBgusdReceiveCoin
	RedeemModeList           []EliteRedeemMode
}
