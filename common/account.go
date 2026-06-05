/*
FILE: common/account.go

DESCRIPTION:
Account sub-client — account-wide assets + trade-rate
(/api/v2/account/{funding-assets,bot-assets,all-account-balance} +
/api/v2/common/trade-rate). All signed reads.

Request params verified against the Bitget V2 docs and the tiagosiebler
reference client.
*/

package common

import (
	"context"
	"net/url"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	commontypes "github.com/tonymontanov/go-bitget/v2/common/types"
)

// AccountClient — account-wide assets / trade-rate sub-client.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// decErr parses raw into *dst, wrapping a failure as a scoped parse error.
func decErr(scope string, dst *decimal.Decimal, raw string) error {
	var err error
	if *dst, err = bgcommon.ParseDecimalOrZero(raw); err != nil {
		return errParse(scope, err)
	}
	return nil
}

// ---------------------------------------------------------------------
// GetFundingAssets — account/funding-assets.
// ---------------------------------------------------------------------

type fundingAssetRow struct {
	Coin      string `json:"coin"`
	Available string `json:"available"`
	Frozen    string `json:"frozen"`
	USDTValue string `json:"usdtValue"`
}

// GetFundingAssets returns the funding (P2P) wallet balances. coin is
// optional (filters to one asset).
func (a *AccountClient) GetFundingAssets(ctx context.Context, coin string) ([]commontypes.FundingAsset, error) {
	var query url.Values = url.Values{}
	if coin != "" {
		query.Set("coin", coin)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/account/funding-assets",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []fundingAssetRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Account.GetFundingAssets", err)
	}
	var out []commontypes.FundingAsset = make([]commontypes.FundingAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var f commontypes.FundingAsset = commontypes.FundingAsset{Coin: rows[i].Coin}
		if err = decErr("Account.GetFundingAssets", &f.Available, rows[i].Available); err != nil {
			return nil, err
		}
		if err = decErr("Account.GetFundingAssets", &f.Frozen, rows[i].Frozen); err != nil {
			return nil, err
		}
		if err = decErr("Account.GetFundingAssets", &f.USDTValue, rows[i].USDTValue); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetBotAssets — account/bot-assets.
// ---------------------------------------------------------------------

type botAssetRow struct {
	Coin      string `json:"coin"`
	Available string `json:"available"`
	Equity    string `json:"equity"`
	Bonus     string `json:"bonus"`
	Frozen    string `json:"frozen"`
	USDTValue string `json:"usdtValue"`
}

// GetBotAssets returns the strategy-bot wallet balances. accountType is
// optional (venue-defined bot account type).
func (a *AccountClient) GetBotAssets(ctx context.Context, accountType string) ([]commontypes.BotAsset, error) {
	var query url.Values = url.Values{}
	if accountType != "" {
		query.Set("accountType", accountType)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/account/bot-assets",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []botAssetRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Account.GetBotAssets", err)
	}
	var out []commontypes.BotAsset = make([]commontypes.BotAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var r = rows[i]
		var b commontypes.BotAsset = commontypes.BotAsset{Coin: r.Coin}
		var scope = "Account.GetBotAssets"
		if err = decErr(scope, &b.Available, r.Available); err != nil {
			return nil, err
		}
		if err = decErr(scope, &b.Equity, r.Equity); err != nil {
			return nil, err
		}
		if err = decErr(scope, &b.Bonus, r.Bonus); err != nil {
			return nil, err
		}
		if err = decErr(scope, &b.Frozen, r.Frozen); err != nil {
			return nil, err
		}
		if err = decErr(scope, &b.USDTValue, r.USDTValue); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAllAccountBalance — account/all-account-balance.
// ---------------------------------------------------------------------

type accountBalanceRow struct {
	AccountType string `json:"accountType"`
	USDTBalance string `json:"usdtBalance"`
}

// GetAllAccountBalance returns the USDT value held in each account type.
func (a *AccountClient) GetAllAccountBalance(ctx context.Context) ([]commontypes.AccountBalance, error) {
	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/account/all-account-balance",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}
	var rows []accountBalanceRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Account.GetAllAccountBalance", err)
	}
	var out []commontypes.AccountBalance = make([]commontypes.AccountBalance, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var b commontypes.AccountBalance = commontypes.AccountBalance{AccountType: rows[i].AccountType}
		if err = decErr("Account.GetAllAccountBalance", &b.USDTBalance, rows[i].USDTBalance); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetTradeRate — common/trade-rate.
// ---------------------------------------------------------------------

type tradeRateRow struct {
	MakerFeeRate string `json:"makerFeeRate"`
	TakerFeeRate string `json:"takerFeeRate"`
}

// GetTradeRate returns the caller's maker / taker fee rate for a symbol.
// symbol and businessType (e.g. "spot" | "usdt_futures" | "margin") are
// required.
func (a *AccountClient) GetTradeRate(ctx context.Context, symbol, businessType string) (commontypes.TradeRate, error) {
	var out commontypes.TradeRate
	switch {
	case symbol == "":
		return out, errInvalid("Account.GetTradeRate", "symbol is required")
	case businessType == "":
		return out, errInvalid("Account.GetTradeRate", "businessType is required")
	}

	var query url.Values = url.Values{}
	query.Set("symbol", symbol)
	query.Set("businessType", businessType)

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/common/trade-rate",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row tradeRateRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Account.GetTradeRate", err)
	}
	if err = decErr("Account.GetTradeRate", &out.MakerFeeRate, row.MakerFeeRate); err != nil {
		return out, err
	}
	if err = decErr("Account.GetTradeRate", &out.TakerFeeRate, row.TakerFeeRate); err != nil {
		return out, err
	}
	return out, nil
}
