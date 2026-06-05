/*
FILE: broker/types/agent.go

DESCRIPTION:
Domain types for the AGENT (affiliate / referral) reporting endpoints
(/api/v2/broker/customer-*, sub-customer-list, agent-commission). Field
mapping verified against the Bitget V2 broker docs and the tiagosiebler
reference types (AgentCustomerCommission* / AgentSubCustomer* /
AgentCustomerTradeVolume* / AgentCustomerList* / AgentCustomerKyc* /
AgentCustomerDeposit* / AgentCustomerAsset* / AgentCommissionDetail*).
*/

package types

import "github.com/shopspring/decimal"

// AgentCustomerCommission — one row of GET broker/customer-commissions.
type AgentCustomerCommission struct {
	UID                    string
	Date                   string
	Coin                   string
	Symbol                 string
	ProductType            string
	DealAmount             decimal.Decimal
	Fee                    decimal.Decimal
	FeeDeduction           decimal.Decimal
	ActivityBonusDeduct    decimal.Decimal
	SpotCouponDeduct       decimal.Decimal
	FuturesCouponDeduct    decimal.Decimal
	SpotFeeDiscountDeduct  decimal.Decimal
	NegativeMakerFeeDeduct decimal.Decimal
	FeePaid                decimal.Decimal
	RebateAmount           decimal.Decimal
	UserTotalRebateAmount  decimal.Decimal
	DayTotalRebateAmount   decimal.Decimal
	TotalRebateAmount      decimal.Decimal
}

// AgentSubCustomer — one row of GET broker/sub-customer-list.
type AgentSubCustomer struct {
	UID            string
	RegisterTimeMs int64
}

// AgentCustomerTradeVolume — one row of POST broker/customer-trade-volume.
// (The Volume field maps the venue's misspelled "volumn".)
type AgentCustomerTradeVolume struct {
	UID           string
	Volume        decimal.Decimal
	SpotVolume    decimal.Decimal
	FuturesVolume decimal.Decimal
	TimeMs        int64
}

// AgentCustomerListItem — one row of POST broker/customer-list.
type AgentCustomerListItem struct {
	UID            string
	RegisterTimeMs int64
}

// AgentCustomerKyc — one row of GET broker/customer-kyc-result.
//
//	KycResult: passed | not_passed
type AgentCustomerKyc struct {
	UID       string
	KycResult string
}

// AgentCustomerDeposit — one row of POST broker/customer-deposit.
type AgentCustomerDeposit struct {
	OrderID       string
	UID           string
	DepositTimeMs int64
	DepositCoin   string
	DepositAmount decimal.Decimal
}

// AgentCustomerAsset — one row of POST broker/customer-asset.
type AgentCustomerAsset struct {
	UID     string
	Balance decimal.Decimal
	UTimeMs int64
	Remark  string
}

// AgentCommissionDetail — one row of GET broker/agent-commission.
//
//	BizType: spot | futures
//	Status:  settled | unsettled | notIssued
type AgentCommissionDetail struct {
	UID                     string
	BizType                 string
	SubBizType              string
	Symbol                  string
	Coin                    string
	Fee                     decimal.Decimal
	Volume                  decimal.Decimal
	ActivityBonusDeduct     decimal.Decimal
	SpotCouponDeduct        decimal.Decimal
	FuturesCouponDeduct     decimal.Decimal
	SpotFeeDiscountDeduct   decimal.Decimal
	NegativeMakerFeeDeduct  decimal.Decimal
	FeePaid                 decimal.Decimal
	DirectCommission        decimal.Decimal
	SubCommission           decimal.Decimal
	PartnerCommission       decimal.Decimal
	PartnerActualCommission decimal.Decimal
	TraderType              string
	APIType                 string
	Status                  string
	StartCalculationTimeMs  int64
	EndCalculationTimeMs    int64
}
