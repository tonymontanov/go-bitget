/*
FILE: uta/account.go

DESCRIPTION:
Account sub-client — the V3 UTA unified-account surface (core slice):
assets, funding assets, account info / settings, set-leverage,
set-hold-mode (one-way / hedge), fee rate, max-transferable, financial
records, account open-interest limit and the classic-account
downgrade + its status.

All calls are SIGNED. Request params verified against the Bitget V3 docs
and the tiagosiebler reference client.
*/

package uta

import (
	"context"
	"net/url"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// AccountClient — unified-account sub-client.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// ---------------------------------------------------------------------
// GetAssets — account/assets.
// ---------------------------------------------------------------------

type accountAssetsRow struct {
	AccountEquity     string `json:"accountEquity"`
	USDTEquity        string `json:"usdtEquity"`
	BTCEquity         string `json:"btcEquity"`
	UnrealisedPnl     string `json:"unrealisedPnl"`
	USDTUnrealisedPnl string `json:"usdtUnrealisedPnl"`
	BTCUnrealisedPnl  string `json:"btcUnrealizedPnl"`
	EffEquity         string `json:"effEquity"`
	MMR               string `json:"mmr"`
	IMR               string `json:"imr"`
	MgnRatio          string `json:"mgnRatio"`
	PositionMgnRatio  string `json:"positionMgnRatio"`
	Assets            []struct {
		Coin      string `json:"coin"`
		Equity    string `json:"equity"`
		USDValue  string `json:"usdValue"`
		Balance   string `json:"balance"`
		Available string `json:"available"`
		Debt      string `json:"debt"`
		Locked    string `json:"locked"`
	} `json:"assets"`
}

// GetAssets returns the unified-account equity overview and per-coin
// breakdown.
func (a *AccountClient) GetAssets(ctx context.Context) (utatypes.AccountAssets, error) {
	var out utatypes.AccountAssets
	var row accountAssetsRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/assets", Meta: queryMeta(),
	}, "Account.GetAssets", &row); err != nil {
		return out, err
	}
	if err = decMany("Account.GetAssets", []decPair{
		{&out.AccountEquity, row.AccountEquity}, {&out.USDTEquity, row.USDTEquity}, {&out.BTCEquity, row.BTCEquity},
		{&out.UnrealisedPnl, row.UnrealisedPnl}, {&out.USDTUnrealisedPnl, row.USDTUnrealisedPnl}, {&out.BTCUnrealisedPnl, row.BTCUnrealisedPnl},
		{&out.EffEquity, row.EffEquity}, {&out.MMR, row.MMR}, {&out.IMR, row.IMR},
		{&out.MgnRatio, row.MgnRatio}, {&out.PositionMgnRatio, row.PositionMgnRatio},
	}); err != nil {
		return out, err
	}
	var i int
	out.Assets = make([]utatypes.AccountAsset, 0, len(row.Assets))
	for i = 0; i < len(row.Assets); i++ {
		var as utatypes.AccountAsset = utatypes.AccountAsset{Coin: row.Assets[i].Coin}
		if err = decMany("Account.GetAssets", []decPair{
			{&as.Equity, row.Assets[i].Equity}, {&as.USDValue, row.Assets[i].USDValue}, {&as.Balance, row.Assets[i].Balance},
			{&as.Available, row.Assets[i].Available}, {&as.Debt, row.Assets[i].Debt}, {&as.Locked, row.Assets[i].Locked},
		}); err != nil {
			return out, err
		}
		out.Assets = append(out.Assets, as)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetFundingAssets — account/funding-assets.
// ---------------------------------------------------------------------

type fundingAssetRow struct {
	Coin      string `json:"coin"`
	Available string `json:"available"`
	Frozen    string `json:"frozen"`
	Balance   string `json:"balance"`
}

// GetFundingAssets returns the funding-wallet balances. coin is optional
// (filters to one).
func (a *AccountClient) GetFundingAssets(ctx context.Context, coin string) ([]utatypes.FundingAsset, error) {
	var query url.Values = url.Values{}
	if coin != "" {
		query.Set("coin", coin)
	}
	var rows []fundingAssetRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/funding-assets", Query: query, Meta: queryMeta(),
	}, "Account.GetFundingAssets", &rows); err != nil {
		return nil, err
	}
	var out []utatypes.FundingAsset = make([]utatypes.FundingAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var fa utatypes.FundingAsset = utatypes.FundingAsset{Coin: rows[i].Coin}
		if err = decMany("Account.GetFundingAssets", []decPair{
			{&fa.Available, rows[i].Available}, {&fa.Frozen, rows[i].Frozen}, {&fa.Balance, rows[i].Balance},
		}); err != nil {
			return nil, err
		}
		out = append(out, fa)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetInfo — account/info.
// ---------------------------------------------------------------------

type accountInfoRow struct {
	UserID      string   `json:"userId"`
	InviterID   string   `json:"inviterId"`
	ParentID    string   `json:"parentId"`
	ChannelCode string   `json:"channelCode"`
	Channel     string   `json:"channel"`
	IPs         string   `json:"ips"`
	PermType    string   `json:"permType"`
	Permissions []string `json:"permissions"`
	RegisTime   string   `json:"regisTime"`
}

// GetInfo returns account metadata (UID, inviter, parent, permissions).
func (a *AccountClient) GetInfo(ctx context.Context) (utatypes.AccountInfo, error) {
	var out utatypes.AccountInfo
	var row accountInfoRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/info", Meta: queryMeta(),
	}, "Account.GetInfo", &row); err != nil {
		return out, err
	}
	out = utatypes.AccountInfo{
		UserID: row.UserID, InviterID: row.InviterID, ParentID: row.ParentID,
		ChannelCode: row.ChannelCode, Channel: row.Channel, IPs: row.IPs,
		PermType: row.PermType, Permissions: row.Permissions, RegisTimeMs: i64(row.RegisTime),
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetSettings — account/settings.
// ---------------------------------------------------------------------

type accountSettingsRow struct {
	UID              string `json:"uid"`
	AccountMode      string `json:"accountMode"`
	AssetMode        string `json:"assetMode"`
	AccountLevel     string `json:"accountLevel"`
	HoldMode         string `json:"holdMode"`
	STPMode          string `json:"stpMode"`
	SymbolConfigList []struct {
		Category   string `json:"category"`
		Symbol     string `json:"symbol"`
		MarginMode string `json:"marginMode"`
		Leverage   string `json:"leverage"`
	} `json:"symbolConfigList"`
	CoinConfigList []struct {
		Coin     string `json:"coin"`
		Leverage string `json:"leverage"`
	} `json:"coinConfigList"`
}

// GetSettings returns the account settings (mode, level, hold mode, STP,
// per-symbol / per-coin leverage config).
func (a *AccountClient) GetSettings(ctx context.Context) (utatypes.AccountSettings, error) {
	var out utatypes.AccountSettings
	var row accountSettingsRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/settings", Meta: queryMeta(),
	}, "Account.GetSettings", &row); err != nil {
		return out, err
	}
	out.UID = row.UID
	out.AccountMode = row.AccountMode
	out.AssetMode = row.AssetMode
	out.AccountLevel = row.AccountLevel
	out.HoldMode = row.HoldMode
	out.STPMode = row.STPMode
	var i int
	out.SymbolConfigs = make([]utatypes.AccountSymbolConfig, 0, len(row.SymbolConfigList))
	for i = 0; i < len(row.SymbolConfigList); i++ {
		var sc utatypes.AccountSymbolConfig = utatypes.AccountSymbolConfig{
			Category: row.SymbolConfigList[i].Category, Symbol: row.SymbolConfigList[i].Symbol, MarginMode: row.SymbolConfigList[i].MarginMode,
		}
		if err = decMany("Account.GetSettings", []decPair{{&sc.Leverage, row.SymbolConfigList[i].Leverage}}); err != nil {
			return out, err
		}
		out.SymbolConfigs = append(out.SymbolConfigs, sc)
	}
	out.CoinConfigs = make([]utatypes.AccountCoinConfig, 0, len(row.CoinConfigList))
	for i = 0; i < len(row.CoinConfigList); i++ {
		var cc utatypes.AccountCoinConfig = utatypes.AccountCoinConfig{Coin: row.CoinConfigList[i].Coin}
		if err = decMany("Account.GetSettings", []decPair{{&cc.Leverage, row.CoinConfigList[i].Leverage}}); err != nil {
			return out, err
		}
		out.CoinConfigs = append(out.CoinConfigs, cc)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// SetLeverage — account/set-leverage (POST).
// ---------------------------------------------------------------------

// SetLeverageRequest — parameters for SetLeverage. Category and Leverage
// are required; Symbol / Coin / PosSide are conditional on the product and
// hold mode (PosSide is only meaningful in hedge mode).
type SetLeverageRequest struct {
	Category utatypes.Category `json:"category"`
	Symbol   string            `json:"symbol,omitempty"`
	Coin     string            `json:"coin,omitempty"`
	Leverage string            `json:"leverage"`
	PosSide  string            `json:"posSide,omitempty"`
}

// SetLeverage sets the leverage for a category / symbol (and posSide in
// hedge mode). Category and Leverage are required.
func (a *AccountClient) SetLeverage(ctx context.Context, req SetLeverageRequest) error {
	switch {
	case req.Category == "":
		return errInvalid("Account.SetLeverage", "category is required")
	case req.Leverage == "":
		return errInvalid("Account.SetLeverage", "leverage is required")
	}
	return a.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/account/set-leverage", Body: req, Meta: queryMeta(),
	}, "Account.SetLeverage", nil)
}

// ---------------------------------------------------------------------
// SetHoldMode — account/set-hold-mode (POST).
// ---------------------------------------------------------------------

type setHoldModeBody struct {
	HoldMode string `json:"holdMode"`
}

// SetHoldMode switches the futures position mode between one-way and
// hedge. mode is required.
func (a *AccountClient) SetHoldMode(ctx context.Context, mode utatypes.HoldMode) error {
	if mode == "" {
		return errInvalid("Account.SetHoldMode", "holdMode is required")
	}
	return a.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/account/set-hold-mode", Body: setHoldModeBody{HoldMode: string(mode)}, Meta: queryMeta(),
	}, "Account.SetHoldMode", nil)
}

// ---------------------------------------------------------------------
// GetFeeRate — account/fee-rate.
// ---------------------------------------------------------------------

type feeRateRow struct {
	MakerFeeRate string `json:"makerFeeRate"`
	TakerFeeRate string `json:"takerFeeRate"`
}

// GetFeeRate returns the trading fee rate for a category / symbol. Both are
// required.
func (a *AccountClient) GetFeeRate(ctx context.Context, category utatypes.Category, symbol string) (utatypes.FeeRate, error) {
	var out utatypes.FeeRate
	switch {
	case category == "":
		return out, errInvalid("Account.GetFeeRate", "category is required")
	case symbol == "":
		return out, errInvalid("Account.GetFeeRate", "symbol is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	query.Set("symbol", symbol)
	var row feeRateRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/fee-rate", Query: query, Meta: queryMeta(),
	}, "Account.GetFeeRate", &row); err != nil {
		return out, err
	}
	if err = decMany("Account.GetFeeRate", []decPair{{&out.MakerFeeRate, row.MakerFeeRate}, {&out.TakerFeeRate, row.TakerFeeRate}}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetMaxTransferable — account/max-transferable.
// ---------------------------------------------------------------------

type maxTransferableRow struct {
	Coin              string `json:"coin"`
	MaxTransfer       string `json:"maxTransfer"`
	BorrowMaxTransfer string `json:"borrowMaxTransfer"`
}

// GetMaxTransferable returns the maximum transferable amount for a coin.
// coin is required.
func (a *AccountClient) GetMaxTransferable(ctx context.Context, coin string) (utatypes.MaxTransferable, error) {
	var out utatypes.MaxTransferable
	if coin == "" {
		return out, errInvalid("Account.GetMaxTransferable", "coin is required")
	}
	var query url.Values = url.Values{}
	query.Set("coin", coin)
	var row maxTransferableRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/max-transferable", Query: query, Meta: queryMeta(),
	}, "Account.GetMaxTransferable", &row); err != nil {
		return out, err
	}
	out.Coin = row.Coin
	if err = decMany("Account.GetMaxTransferable", []decPair{{&out.MaxTransfer, row.MaxTransfer}, {&out.BorrowMaxTransfer, row.BorrowMaxTransfer}}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetFinancialRecords — account/financial-records.
// ---------------------------------------------------------------------

// FinancialRecordsQuery — parameters for GetFinancialRecords. Category is
// required; the rest narrow the window / page.
type FinancialRecordsQuery struct {
	Category    utatypes.Category
	Coin        string
	Type        string
	StartTimeMs int64
	EndTimeMs   int64
	Cursor      string
	Limit       int
}

type financialRecordsRow struct {
	List []struct {
		Category string `json:"category"`
		ID       string `json:"id"`
		Symbol   string `json:"symbol"`
		Coin     string `json:"coin"`
		Type     string `json:"type"`
		Amount   string `json:"amount"`
		Fee      string `json:"fee"`
		Balance  string `json:"balance"`
		TS       string `json:"ts"`
	} `json:"list"`
	Cursor string `json:"cursor"`
}

// GetFinancialRecords returns one page of account financial records plus
// the next cursor (empty when exhausted). Category is required.
func (a *AccountClient) GetFinancialRecords(ctx context.Context, q FinancialRecordsQuery) ([]utatypes.FinancialRecord, string, error) {
	if q.Category == "" {
		return nil, "", errInvalid("Account.GetFinancialRecords", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(q.Category))
	if q.Coin != "" {
		query.Set("coin", q.Coin)
	}
	if q.Type != "" {
		query.Set("type", q.Type)
	}
	if q.StartTimeMs > 0 {
		query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
	}
	if q.EndTimeMs > 0 {
		query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
	}
	if q.Cursor != "" {
		query.Set("cursor", q.Cursor)
	}
	if q.Limit > 0 {
		query.Set("limit", strconv.Itoa(q.Limit))
	}
	var row financialRecordsRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/financial-records", Query: query, Meta: queryMeta(),
	}, "Account.GetFinancialRecords", &row); err != nil {
		return nil, "", err
	}
	var out []utatypes.FinancialRecord = make([]utatypes.FinancialRecord, 0, len(row.List))
	var i int
	for i = 0; i < len(row.List); i++ {
		var fr utatypes.FinancialRecord = utatypes.FinancialRecord{
			Category: row.List[i].Category, ID: row.List[i].ID, Symbol: row.List[i].Symbol,
			Coin: row.List[i].Coin, Type: row.List[i].Type, TimeMs: i64(row.List[i].TS),
		}
		if err = decMany("Account.GetFinancialRecords", []decPair{
			{&fr.Amount, row.List[i].Amount}, {&fr.Fee, row.List[i].Fee}, {&fr.Balance, row.List[i].Balance},
		}); err != nil {
			return nil, "", err
		}
		out = append(out, fr)
	}
	return out, row.Cursor, nil
}

// ---------------------------------------------------------------------
// GetOpenInterestLimit — account/open-interest-limit.
// ---------------------------------------------------------------------

type accountOiLimitRow struct {
	Symbol           string `json:"symbol"`
	SingleUserLimit  string `json:"singleUserLimit"`
	MasterSubLimit   string `json:"masterSubLimit"`
	MarketMakerLimit string `json:"marketMakerLimit"`
}

// GetOpenInterestLimit returns this account's per-symbol open-interest
// notional limits. category and symbol are required (futures only).
func (a *AccountClient) GetOpenInterestLimit(ctx context.Context, category utatypes.Category, symbol string) (utatypes.AccountOpenInterestLimit, error) {
	var out utatypes.AccountOpenInterestLimit
	switch {
	case category == "":
		return out, errInvalid("Account.GetOpenInterestLimit", "category is required")
	case symbol == "":
		return out, errInvalid("Account.GetOpenInterestLimit", "symbol is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	query.Set("symbol", symbol)
	var row accountOiLimitRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/open-interest-limit", Query: query, Meta: queryMeta(),
	}, "Account.GetOpenInterestLimit", &row); err != nil {
		return out, err
	}
	out.Symbol = row.Symbol
	if err = decMany("Account.GetOpenInterestLimit", []decPair{
		{&out.SingleUserLimit, row.SingleUserLimit}, {&out.MasterSubLimit, row.MasterSubLimit}, {&out.MarketMakerLimit, row.MarketMakerLimit},
	}); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// DowngradeToClassic / GetSwitchStatus — account/switch[-status].
// ---------------------------------------------------------------------

// DowngradeToClassic requests a switch back to the classic (non-unified)
// account. Parent accounts only. The switch is async — poll
// GetSwitchStatus to confirm completion.
func (a *AccountClient) DowngradeToClassic(ctx context.Context) error {
	return a.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/account/switch", Meta: queryMeta(),
	}, "Account.DowngradeToClassic", nil)
}

type switchStatusRow struct {
	Status string `json:"status"`
}

// GetSwitchStatus returns the unified→classic account-switch status.
func (a *AccountClient) GetSwitchStatus(ctx context.Context) (string, error) {
	var row switchStatusRow
	var err error
	if err = a.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/account/switch-status", Meta: queryMeta(),
	}, "Account.GetSwitchStatus", &row); err != nil {
		return "", err
	}
	return row.Status, nil
}
