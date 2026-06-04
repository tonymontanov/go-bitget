/*
FILE: margin/public.go

DESCRIPTION:
Public reference sub-client for the Bitget V2 MARGIN profile.

Margin trades SPOT instruments, so there is NO margin-specific price /
orderbook / candle endpoint — callers use spot.MarketData() for those.
The one public margin endpoint is the supported-currencies reference:

	GET /api/v2/margin/currencies — Currencies (margin-coin list + limits)

This endpoint is MODE-AGNOSTIC (no crossed/isolated segment): the
supported-margin-coin catalogue is shared across both modes, and the
per-row flags (IsCrossBorrowable / IsIsolatedBaseBorrowable / ...) tell
the caller what each mode permits.
*/

package margin

import (
	"context"
	"net/url"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
)

// PublicClient — public reference sub-client (margin currencies). Built
// once per margin.Client (see client.go) and safe for concurrent use.
type PublicClient struct {
	c *Client
}

func newPublicClient(c *Client) *PublicClient {
	return &PublicClient{c: c}
}

// currencyRow mirrors one row of GET /api/v2/margin/currencies.
type currencyRow struct {
	Symbol                    string `json:"symbol"`
	BaseCoin                  string `json:"baseCoin"`
	QuoteCoin                 string `json:"quoteCoin"`
	MaxCrossedLeverage        string `json:"maxCrossedLeverage"`
	MaxIsolatedLeverage       string `json:"maxIsolatedLeverage"`
	MinTradeAmount            string `json:"minTradeAmount"`
	MaxTradeAmount            string `json:"maxTradeAmount"`
	MinTradeUSDT              string `json:"minTradeUSDT"`
	TakerFeeRate              string `json:"takerFeeRate"`
	MakerFeeRate              string `json:"makerFeeRate"`
	PricePrecision            string `json:"pricePrecision"`
	QuantityPrecision         string `json:"quantityPrecision"`
	UserMinBorrow             string `json:"userMinBorrow"`
	Status                    string `json:"status"`
	IsBorrowable              bool   `json:"isBorrowable"`
	IsCrossBorrowable         bool   `json:"isCrossBorrowable"`
	IsIsolatedBaseBorrowable  bool   `json:"isIsolatedBaseBorrowable"`
	IsIsolatedQuoteBorrowable bool   `json:"isIsolatedQuoteBorrowable"`
}

// Currencies returns the margin-tradable currencies / pairs and their
// limits. `symbol` and `coin` are optional filters; pass "" to omit.
func (p *PublicClient) Currencies(ctx context.Context, symbol, coin string) ([]margintypes.Currency, error) {
	var query url.Values = url.Values{}
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	if coin != "" {
		query.Set("coin", coin)
	}

	var resp rest.Response
	var err error
	resp, _, err = p.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/margin/currencies",
		Query:  query,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)},
	})
	if err != nil {
		return nil, err
	}

	var rows []currencyRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "margin.Public.Currencies: parse", err)
	}

	var out []margintypes.Currency = make([]margintypes.Currency, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var cur margintypes.Currency
		cur, err = convertCurrencyRow(rows[i])
		if err != nil {
			return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "margin.Public.Currencies: parse", err)
		}
		out = append(out, cur)
	}
	return out, nil
}

func convertCurrencyRow(row currencyRow) (margintypes.Currency, error) {
	var out margintypes.Currency = margintypes.Currency{
		Symbol:                    row.Symbol,
		BaseCoin:                  row.BaseCoin,
		QuoteCoin:                 row.QuoteCoin,
		MaxCrossedLeverage:        row.MaxCrossedLeverage,
		MaxIsolatedLeverage:       row.MaxIsolatedLeverage,
		Status:                    row.Status,
		IsBorrowable:              row.IsBorrowable,
		IsCrossBorrowable:         row.IsCrossBorrowable,
		IsIsolatedBaseBorrowable:  row.IsIsolatedBaseBorrowable,
		IsIsolatedQuoteBorrowable: row.IsIsolatedQuoteBorrowable,
	}
	var err error
	if out.MinTradeAmount, err = bgcommon.ParseDecimalOrZero(row.MinTradeAmount); err != nil {
		return margintypes.Currency{}, err
	}
	if out.MaxTradeAmount, err = bgcommon.ParseDecimalOrZero(row.MaxTradeAmount); err != nil {
		return margintypes.Currency{}, err
	}
	if out.MinTradeUSDT, err = bgcommon.ParseDecimalOrZero(row.MinTradeUSDT); err != nil {
		return margintypes.Currency{}, err
	}
	if out.TakerFeeRate, err = bgcommon.ParseDecimalOrZero(row.TakerFeeRate); err != nil {
		return margintypes.Currency{}, err
	}
	if out.MakerFeeRate, err = bgcommon.ParseDecimalOrZero(row.MakerFeeRate); err != nil {
		return margintypes.Currency{}, err
	}
	out.PricePrecision, _ = parseIntOrZero(row.PricePrecision)
	out.QuantityPrecision, _ = parseIntOrZero(row.QuantityPrecision)
	if out.UserMinBorrow, err = bgcommon.ParseDecimalOrZero(row.UserMinBorrow); err != nil {
		return margintypes.Currency{}, err
	}
	return out, nil
}

// parseIntOrZero parses a base-10 int, returning 0 for an empty string.
func parseIntOrZero(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	var v int64
	var err error
	v, err = bgcommon.ParseInt64OrZero(s)
	return int(v), err
}
