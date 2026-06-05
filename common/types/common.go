/*
FILE: common/types/common.go

DESCRIPTION:
Domain types for the COMMON public + account-wide endpoints. Field mapping
verified against the Bitget V2 docs and the tiagosiebler reference types
(AnnouncementV2 / FundingAssetV2 / BotAssetV2 + the trade-rate /
all-account-balance inline shapes).
*/

package types

import "github.com/shopspring/decimal"

// Announcement — one row of GET public/annoucements.
type Announcement struct {
	AnnID    string
	AnnTitle string
	AnnDesc  string
	AnnURL   string
	Language string
	CTimeMs  int64
}

// FundingAsset — one row of GET account/funding-assets (the P2P / funding
// wallet balance per coin).
type FundingAsset struct {
	Coin      string
	Available decimal.Decimal
	Frozen    decimal.Decimal
	USDTValue decimal.Decimal
}

// BotAsset — one row of GET account/bot-assets (strategy-bot wallet).
type BotAsset struct {
	Coin      string
	Available decimal.Decimal
	Equity    decimal.Decimal
	Bonus     decimal.Decimal
	Frozen    decimal.Decimal
	USDTValue decimal.Decimal
}

// AccountBalance — one row of GET account/all-account-balance: the USDT
// value held in each account type (spot / futures / margin / earn / ...).
type AccountBalance struct {
	AccountType string
	USDTBalance decimal.Decimal
}

// TradeRate — GET common/trade-rate: the caller's maker / taker fee rate
// for a symbol + business type.
type TradeRate struct {
	MakerFeeRate decimal.Decimal
	TakerFeeRate decimal.Decimal
}
