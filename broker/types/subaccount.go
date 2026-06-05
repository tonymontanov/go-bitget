/*
FILE: broker/types/subaccount.go

DESCRIPTION:
Domain types for the BROKER sub-account lifecycle (/api/v2/broker/account/...
plus the broker/subaccount-{deposit,withdrawal} and
broker/all-sub-deposit-withdrawal records). Field mapping verified against
the Bitget V2 broker docs and the tiagosiebler reference types
(CreateSubaccountResponseV2 / BrokerSubaccountV2 / SubaccountEmailV2 /
BrokerSubaccount{Spot,Future}AssetV2 / CreateSubaccountDepositAddressV2 /
SubaccountDepositV2 / BrokerSubaccountWithdrawalV2 /
AllSubDepositWithdrawalRecordV2).
*/

package types

import "github.com/shopspring/decimal"

// BrokerInfo — GET broker/account/info: sub-account quota for the broker.
type BrokerInfo struct {
	SubAccountSize    int64
	MaxSubAccountSize int64
	UTimeMs           int64
}

// SubAccount — a managed broker sub-account (subaccount-list /
// create-subaccount / modify-subaccount). Create omits Language/UTimeMs.
//
//	Status:  normal | freeze | del
//	PermList: withdraw | transfer | spot_trade | contract_trade | read |
//	          deposit | margin_trade
type SubAccount struct {
	SubUID         string
	SubaccountName string
	Status         string
	Label          string
	Language       string
	PermList       []string
	CTimeMs        int64
	UTimeMs        int64
}

// SubAccountEmail — GET broker/account/subaccount-email.
type SubAccountEmail struct {
	SubUID          string
	SubaccountName  string
	SubaccountEmail string
	CTimeMs         int64
	UTimeMs         int64
}

// SubAccountSpotAsset — one row of broker/account/subaccount-spot-assets.
type SubAccountSpotAsset struct {
	Coin      string
	Available decimal.Decimal
	Frozen    decimal.Decimal
	Locked    decimal.Decimal
	UTimeMs   int64
}

// SubAccountFuturesAsset — one row of
// broker/account/subaccount-future-assets (per marginCoin).
type SubAccountFuturesAsset struct {
	MarginCoin           string
	Available            decimal.Decimal
	Frozen               decimal.Decimal
	Locked               decimal.Decimal
	CrossedMaxAvailable  decimal.Decimal
	IsolatedMaxAvailable decimal.Decimal
	MaxTransferOut       decimal.Decimal
	AccountEquity        decimal.Decimal
	USDTEquity           decimal.Decimal
	BTCEquity            decimal.Decimal
	UTimeMs              int64
}

// SubAccountDepositAddress — POST broker/account/subaccount-address.
type SubAccountDepositAddress struct {
	SubUID  string
	Coin    string
	Address string
	Chain   string
	Tag     string
	URL     string
	CTimeMs int64
}

// SubWithdrawalResult — POST broker/account/subaccount-withdrawal.
type SubWithdrawalResult struct {
	OrderID   string
	ClientOid string
}

// SubDepositRecord — one row of GET broker/subaccount-deposit (ND broker).
type SubDepositRecord struct {
	OrderID     string
	TxID        string
	Coin        string
	Type        string
	Dest        string
	Amount      decimal.Decimal
	Status      string
	FromAddress string
	ToAddress   string
	Fee         decimal.Decimal
	Chain       string
	Confirm     string
	Tag         string
	CTimeMs     int64
	UTimeMs     int64
}

// SubWithdrawalRecord — one row of GET broker/subaccount-withdrawal
// (ND broker). Same as SubDepositRecord plus the initiating UserID.
type SubWithdrawalRecord struct {
	OrderID     string
	TxID        string
	Coin        string
	Type        string
	Dest        string
	Amount      decimal.Decimal
	Status      string
	FromAddress string
	ToAddress   string
	Fee         decimal.Decimal
	Chain       string
	Confirm     string
	Tag         string
	UserID      string
	CTimeMs     int64
	UTimeMs     int64
}

// AllSubRecord — one row of GET broker/all-sub-deposit-withdrawal.
//
//	Type:    deposit | withdrawal
//	SubType: onchain | internal | fast
//	Status:  pending | fail | success
type AllSubRecord struct {
	UID     string
	TxID    string
	Type    string
	SubType string
	Coin    string
	Amount  decimal.Decimal
	Status  string
	TimeMs  int64
}

// ModifySubAccountRequest — POST broker/account/modify-subaccount.
// PermList and Status are required; Language is optional.
type ModifySubAccountRequest struct {
	SubUID   string
	PermList []string
	Status   string
	Language string
}

// SubWithdrawalRequest — POST broker/account/subaccount-withdrawal.
// SubUID, Coin, Dest, Address and Amount are required; Dest is one of
// "on_chain" | "internal_transfer". Chain is required for on_chain.
type SubWithdrawalRequest struct {
	SubUID    string
	Coin      string
	Dest      string
	Chain     string
	Address   string
	Amount    string
	Tag       string
	ClientOid string
}
