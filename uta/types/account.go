/*
FILE: uta/types/account.go

DESCRIPTION:
Domain types for the V3 UTA unified-account surface (core slice): assets,
funding assets, account info / settings, fee rate, max-transferable,
financial records and the account-level open-interest limit. Field
mapping verified against the Bitget V3 docs and the tiagosiebler reference
types. decimal for monetary fields, int64 epoch-ms for timestamps.
*/

package types

import "github.com/shopspring/decimal"

// AccountAsset — one per-coin row of the unified account equity.
type AccountAsset struct {
	Coin      string
	Equity    decimal.Decimal
	USDValue  decimal.Decimal
	Balance   decimal.Decimal
	Available decimal.Decimal
	Debt      decimal.Decimal
	Locked    decimal.Decimal
}

// AccountAssets — GET account/assets: the unified-account equity overview
// plus the per-coin breakdown.
type AccountAssets struct {
	AccountEquity     decimal.Decimal
	USDTEquity        decimal.Decimal
	BTCEquity         decimal.Decimal
	UnrealisedPnl     decimal.Decimal
	USDTUnrealisedPnl decimal.Decimal
	BTCUnrealisedPnl  decimal.Decimal
	EffEquity         decimal.Decimal
	MMR               decimal.Decimal
	IMR               decimal.Decimal
	MgnRatio          decimal.Decimal
	PositionMgnRatio  decimal.Decimal
	Assets            []AccountAsset
}

// FundingAsset — one row of GET account/funding-assets (V3 funding wallet).
type FundingAsset struct {
	Coin      string
	Available decimal.Decimal
	Frozen    decimal.Decimal
	Balance   decimal.Decimal
}

// AccountInfo — GET account/info: account metadata.
type AccountInfo struct {
	UserID      string
	InviterID   string
	ParentID    string
	ChannelCode string
	Channel     string
	IPs         string
	PermType    string
	Permissions []string
	RegisTimeMs int64
}

// AccountSymbolConfig — per-symbol margin/leverage config.
type AccountSymbolConfig struct {
	Category   string
	Symbol     string
	MarginMode string
	Leverage   decimal.Decimal
}

// AccountCoinConfig — per-coin leverage config.
type AccountCoinConfig struct {
	Coin     string
	Leverage decimal.Decimal
}

// AccountSettings — GET account/settings.
type AccountSettings struct {
	UID           string
	AccountMode   string
	AssetMode     string
	AccountLevel  string
	HoldMode      string
	STPMode       string
	SymbolConfigs []AccountSymbolConfig
	CoinConfigs   []AccountCoinConfig
}

// FeeRate — GET account/fee-rate.
type FeeRate struct {
	MakerFeeRate decimal.Decimal
	TakerFeeRate decimal.Decimal
}

// MaxTransferable — GET account/max-transferable.
type MaxTransferable struct {
	Coin              string
	MaxTransfer       decimal.Decimal
	BorrowMaxTransfer decimal.Decimal
}

// FinancialRecord — one row of GET account/financial-records.
type FinancialRecord struct {
	Category string
	ID       string
	Symbol   string
	Coin     string
	Type     string
	Amount   decimal.Decimal
	Fee      decimal.Decimal
	Balance  decimal.Decimal
	TimeMs   int64
}

// AccountOpenInterestLimit — GET account/open-interest-limit (per-symbol
// notional caps for this account / its sub-accounts / market-maker tier).
type AccountOpenInterestLimit struct {
	Symbol           string
	SingleUserLimit  decimal.Decimal
	MasterSubLimit   decimal.Decimal
	MarketMakerLimit decimal.Decimal
}
