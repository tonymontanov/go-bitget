/*
FILE: convert/convert.go

DESCRIPTION:
The CONVERT (flash-swap) + BGB-convert REST surface
(/api/v2/convert/...). All calls are signed and account-level (no
productType, no WebSocket).
*/

package convert

import (
	"context"
	"net/url"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	convtypes "github.com/tonymontanov/go-bitget/v2/convert/types"
)

// ---------------------------------------------------------------------
// GetCurrencies — convert/currencies.
// ---------------------------------------------------------------------

type currencyRow struct {
	Coin      string `json:"coin"`
	Available string `json:"available"`
	MaxAmount string `json:"maxAmount"`
	MinAmount string `json:"minAmount"`
}

// GetCurrencies lists the coins available for flash conversion, with the
// per-call min/max amounts and the caller's available balance.
func (c *Client) GetCurrencies(ctx context.Context) ([]convtypes.ConvertCurrency, error) {
	var resp rest.Response
	var err error
	resp, _, err = c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/convert/currencies",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []currencyRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("GetCurrencies", err)
	}
	var out []convtypes.ConvertCurrency = make([]convtypes.ConvertCurrency, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var cur convtypes.ConvertCurrency = convtypes.ConvertCurrency{Coin: rows[i].Coin}
		if cur.Available, err = bgcommon.ParseDecimalOrZero(rows[i].Available); err != nil {
			return nil, errParse("GetCurrencies", err)
		}
		if cur.MaxAmount, err = bgcommon.ParseDecimalOrZero(rows[i].MaxAmount); err != nil {
			return nil, errParse("GetCurrencies", err)
		}
		if cur.MinAmount, err = bgcommon.ParseDecimalOrZero(rows[i].MinAmount); err != nil {
			return nil, errParse("GetCurrencies", err)
		}
		out = append(out, cur)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetQuotedPrice — convert/quoted-price.
// ---------------------------------------------------------------------

type quotedPriceRow struct {
	Fee          string `json:"fee"`
	FromCoinSize string `json:"fromCoinSize"`
	FromCoin     string `json:"fromCoin"`
	CnvtPrice    string `json:"cnvtPrice"`
	ToCoinSize   string `json:"toCoinSize"`
	ToCoin       string `json:"toCoin"`
	TraceID      string `json:"traceId"`
}

// GetQuotedPrice requests an RFQ to swap fromCoin -> toCoin. Exactly one
// of fromCoinSize / toCoinSize must be non-empty (the other is solved
// for); fromCoin and toCoin are required. The returned QuotedPrice
// carries a traceId + cnvtPrice valid for a short TTL (~8s) — feed them
// straight into Trade to execute.
//
// NOTE: the GET request uses the venue's `fromCoinSz` / `toCoinSz` query
// keys (per the doc's curl + parameter description), while Trade's POST
// body uses `fromCoinSize` / `toCoinSize`; the response echoes the
// `...Size` spelling. To be confirmed by the live smoke run.
func (c *Client) GetQuotedPrice(ctx context.Context, fromCoin, toCoin, fromCoinSize, toCoinSize string) (convtypes.QuotedPrice, error) {
	var out convtypes.QuotedPrice
	if fromCoin == "" || toCoin == "" {
		return out, errInvalid("GetQuotedPrice", "fromCoin and toCoin are required")
	}
	if (fromCoinSize == "") == (toCoinSize == "") {
		return out, errInvalid("GetQuotedPrice", "exactly one of fromCoinSize / toCoinSize is required")
	}

	var query url.Values = url.Values{}
	query.Set("fromCoin", fromCoin)
	query.Set("toCoin", toCoin)
	if fromCoinSize != "" {
		query.Set("fromCoinSz", fromCoinSize)
	}
	if toCoinSize != "" {
		query.Set("toCoinSz", toCoinSize)
	}

	var resp rest.Response
	var err error
	resp, _, err = c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/convert/quoted-price",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row quotedPriceRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("GetQuotedPrice", err)
	}
	out.TraceID = row.TraceID
	out.FromCoin = row.FromCoin
	out.ToCoin = row.ToCoin
	if out.FromCoinSize, err = bgcommon.ParseDecimalOrZero(row.FromCoinSize); err != nil {
		return out, errParse("GetQuotedPrice", err)
	}
	if out.ToCoinSize, err = bgcommon.ParseDecimalOrZero(row.ToCoinSize); err != nil {
		return out, errParse("GetQuotedPrice", err)
	}
	if out.CnvtPrice, err = bgcommon.ParseDecimalOrZero(row.CnvtPrice); err != nil {
		return out, errParse("GetQuotedPrice", err)
	}
	if out.Fee, err = bgcommon.ParseDecimalOrZero(row.Fee); err != nil {
		return out, errParse("GetQuotedPrice", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Trade — convert/trade.
// ---------------------------------------------------------------------

type tradeBody struct {
	FromCoin     string `json:"fromCoin"`
	FromCoinSize string `json:"fromCoinSize"`
	ToCoin       string `json:"toCoin"`
	ToCoinSize   string `json:"toCoinSize"`
	CnvtPrice    string `json:"cnvtPrice"`
	TraceID      string `json:"traceId"`
}

type tradeResultRow struct {
	TS         string `json:"ts"`
	CnvtPrice  string `json:"cnvtPrice"`
	ToCoinSize string `json:"toCoinSize"`
	ToCoin     string `json:"toCoin"`
}

// Trade executes a flash swap against a fresh RFQ. Every field is
// required and must come from the matching GetQuotedPrice response,
// submitted within the quote TTL.
func (c *Client) Trade(ctx context.Context, req convtypes.TradeRequest) (convtypes.TradeResult, error) {
	var out convtypes.TradeResult
	switch {
	case req.FromCoin == "" || req.ToCoin == "":
		return out, errInvalid("Trade", "fromCoin and toCoin are required")
	case req.FromCoinSize == "" || req.ToCoinSize == "":
		return out, errInvalid("Trade", "fromCoinSize and toCoinSize are required")
	case req.CnvtPrice == "":
		return out, errInvalid("Trade", "cnvtPrice is required")
	case req.TraceID == "":
		return out, errInvalid("Trade", "traceId is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/convert/trade",
		Body: tradeBody{
			FromCoin:     req.FromCoin,
			FromCoinSize: req.FromCoinSize,
			ToCoin:       req.ToCoin,
			ToCoinSize:   req.ToCoinSize,
			CnvtPrice:    req.CnvtPrice,
			TraceID:      req.TraceID,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row tradeResultRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Trade", err)
	}
	out.ToCoin = row.ToCoin
	out.TimeMs, _ = bgcommon.ParseInt64OrZero(row.TS)
	if out.ToCoinSize, err = bgcommon.ParseDecimalOrZero(row.ToCoinSize); err != nil {
		return out, errParse("Trade", err)
	}
	if out.CnvtPrice, err = bgcommon.ParseDecimalOrZero(row.CnvtPrice); err != nil {
		return out, errParse("Trade", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetHistory — convert/convert-record.
// ---------------------------------------------------------------------

type convertRecordRow struct {
	ID           string `json:"id"`
	TS           string `json:"ts"`
	CnvtPrice    string `json:"cnvtPrice"`
	Fee          string `json:"fee"`
	FromCoinSize string `json:"fromCoinSize"`
	FromCoin     string `json:"fromCoin"`
	ToCoinSize   string `json:"toCoinSize"`
	ToCoin       string `json:"toCoin"`
}

type convertRecordEnvelope struct {
	DataList []convertRecordRow `json:"dataList"`
	EndID    string             `json:"endId"`
}

// GetHistory returns the flash-swap history within the [startTimeMs,
// endTimeMs] window (both required by the venue; max span 90 days).
// Walks the idLessThan / endId cursor.
func (c *Client) GetHistory(ctx context.Context, startTimeMs, endTimeMs int64) ([]convtypes.ConvertRecord, error) {
	if startTimeMs <= 0 || endTimeMs <= 0 {
		return nil, errInvalid("GetHistory", "startTimeMs and endTimeMs are required")
	}

	var rows []convertRecordRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "convert.GetHistory",
		func(idLessThan string, limit int) ([]convertRecordRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
			query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/convert/convert-record",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env convertRecordEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("GetHistory", ferr)
			}
			return env.DataList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []convtypes.ConvertRecord = make([]convtypes.ConvertRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec convtypes.ConvertRecord = convtypes.ConvertRecord{
			ID:       rows[i].ID,
			FromCoin: rows[i].FromCoin,
			ToCoin:   rows[i].ToCoin,
		}
		if rec.FromCoinSize, err = bgcommon.ParseDecimalOrZero(rows[i].FromCoinSize); err != nil {
			return nil, errParse("GetHistory", err)
		}
		if rec.ToCoinSize, err = bgcommon.ParseDecimalOrZero(rows[i].ToCoinSize); err != nil {
			return nil, errParse("GetHistory", err)
		}
		if rec.CnvtPrice, err = bgcommon.ParseDecimalOrZero(rows[i].CnvtPrice); err != nil {
			return nil, errParse("GetHistory", err)
		}
		if rec.Fee, err = bgcommon.ParseDecimalOrZero(rows[i].Fee); err != nil {
			return nil, errParse("GetHistory", err)
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetBGBCoins — convert/bgb-convert-coin-list.
// ---------------------------------------------------------------------

type bgbFeeTierRow struct {
	FeeRate string `json:"feeRate"`
	Fee     string `json:"fee"`
}

type bgbCoinRow struct {
	Coin         string          `json:"coin"`
	Available    string          `json:"available"`
	BGBEstAmount string          `json:"bgbEstAmount"`
	Precision    string          `json:"precision"`
	FeeDetail    []bgbFeeTierRow `json:"feeDetail"`
	CTime        string          `json:"cTime"`
}

type bgbCoinEnvelope struct {
	CoinList []bgbCoinRow `json:"coinList"`
}

// GetBGBCoins lists the small balances eligible for conversion to BGB,
// with the estimated BGB amount and fee tiers per coin.
func (c *Client) GetBGBCoins(ctx context.Context) ([]convtypes.BGBConvertCoin, error) {
	var resp rest.Response
	var err error
	resp, _, err = c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/convert/bgb-convert-coin-list",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var env bgbCoinEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return nil, errParse("GetBGBCoins", err)
	}
	var out []convtypes.BGBConvertCoin = make([]convtypes.BGBConvertCoin, 0, len(env.CoinList))
	var i int
	for i = 0; i < len(env.CoinList); i++ {
		var coin convtypes.BGBConvertCoin = convtypes.BGBConvertCoin{
			Coin:      env.CoinList[i].Coin,
			Precision: env.CoinList[i].Precision,
		}
		if coin.Available, err = bgcommon.ParseDecimalOrZero(env.CoinList[i].Available); err != nil {
			return nil, errParse("GetBGBCoins", err)
		}
		if coin.BGBEstAmount, err = bgcommon.ParseDecimalOrZero(env.CoinList[i].BGBEstAmount); err != nil {
			return nil, errParse("GetBGBCoins", err)
		}
		coin.TimeMs, _ = bgcommon.ParseInt64OrZero(env.CoinList[i].CTime)
		var j int
		coin.FeeDetail = make([]convtypes.BGBFeeTier, 0, len(env.CoinList[i].FeeDetail))
		for j = 0; j < len(env.CoinList[i].FeeDetail); j++ {
			var ft convtypes.BGBFeeTier
			if ft.FeeRate, err = bgcommon.ParseDecimalOrZero(env.CoinList[i].FeeDetail[j].FeeRate); err != nil {
				return nil, errParse("GetBGBCoins", err)
			}
			if ft.Fee, err = bgcommon.ParseDecimalOrZero(env.CoinList[i].FeeDetail[j].Fee); err != nil {
				return nil, errParse("GetBGBCoins", err)
			}
			coin.FeeDetail = append(coin.FeeDetail, ft)
		}
		out = append(out, coin)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// ConvertBGB — convert/bgb-convert.
// ---------------------------------------------------------------------

type bgbConvertBody struct {
	CoinList []string `json:"coinList"`
}

type bgbConvertOrderRow struct {
	Coin    string `json:"coin"`
	OrderID string `json:"orderId"`
}

type bgbConvertEnvelope struct {
	OrderList []bgbConvertOrderRow `json:"orderList"`
}

// ConvertBGB converts the given small-balance coins to BGB. coins must be
// non-empty; the venue processes each coin as its own order.
func (c *Client) ConvertBGB(ctx context.Context, coins []string) ([]convtypes.BGBConvertOrder, error) {
	if len(coins) == 0 {
		return nil, errInvalid("ConvertBGB", "coins is empty")
	}

	var resp rest.Response
	var err error
	resp, _, err = c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/convert/bgb-convert",
		Body:   bgbConvertBody{CoinList: coins},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var env bgbConvertEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return nil, errParse("ConvertBGB", err)
	}
	var out []convtypes.BGBConvertOrder = make([]convtypes.BGBConvertOrder, 0, len(env.OrderList))
	var i int
	for i = 0; i < len(env.OrderList); i++ {
		out = append(out, convtypes.BGBConvertOrder{Coin: env.OrderList[i].Coin, OrderID: env.OrderList[i].OrderID})
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetBGBHistory — convert/bgb-convert-records.
// ---------------------------------------------------------------------

type bgbFeeDetailRow struct {
	FeeCoin string `json:"feeCoin"`
	Fee     string `json:"fee"`
}

type bgbHistoryRow struct {
	OrderID       string            `json:"orderId"`
	FromCoin      string            `json:"fromCoin"`
	FromAmount    string            `json:"fromAmount"`
	FromCoinPrice string            `json:"fromCoinPrice"`
	ToCoin        string            `json:"toCoin"`
	ToAmount      string            `json:"toAmount"`
	ToCoinPrice   string            `json:"toCoinPrice"`
	FeeDetail     []bgbFeeDetailRow `json:"feeDetail"`
	Status        string            `json:"status"`
	CTime         string            `json:"ctime"`
}

// GetBGBHistory returns the BGB-conversion history within the optional
// [startTimeMs, endTimeMs] window. The venue returns a flat list (no
// cursor envelope); this is a single page (limit 100). BGB conversions
// are infrequent, so a single page is sufficient in practice.
func (c *Client) GetBGBHistory(ctx context.Context, startTimeMs, endTimeMs int64) ([]convtypes.BGBConvertHistory, error) {
	var query url.Values = url.Values{}
	query.Set("limit", "100")
	if startTimeMs > 0 {
		query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
	}
	if endTimeMs > 0 {
		query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
	}

	var resp rest.Response
	var err error
	resp, _, err = c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/convert/bgb-convert-records",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []bgbHistoryRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("GetBGBHistory", err)
	}
	var out []convtypes.BGBConvertHistory = make([]convtypes.BGBConvertHistory, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec convtypes.BGBConvertHistory = convtypes.BGBConvertHistory{
			OrderID:  rows[i].OrderID,
			FromCoin: rows[i].FromCoin,
			ToCoin:   rows[i].ToCoin,
			Status:   rows[i].Status,
		}
		if rec.FromAmount, err = bgcommon.ParseDecimalOrZero(rows[i].FromAmount); err != nil {
			return nil, errParse("GetBGBHistory", err)
		}
		if rec.ToAmount, err = bgcommon.ParseDecimalOrZero(rows[i].ToAmount); err != nil {
			return nil, errParse("GetBGBHistory", err)
		}
		if rec.FromCoinPrice, err = bgcommon.ParseDecimalOrZero(rows[i].FromCoinPrice); err != nil {
			return nil, errParse("GetBGBHistory", err)
		}
		if rec.ToCoinPrice, err = bgcommon.ParseDecimalOrZero(rows[i].ToCoinPrice); err != nil {
			return nil, errParse("GetBGBHistory", err)
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		var j int
		rec.FeeDetail = make([]convtypes.BGBFeeDetail, 0, len(rows[i].FeeDetail))
		for j = 0; j < len(rows[i].FeeDetail); j++ {
			var fd convtypes.BGBFeeDetail = convtypes.BGBFeeDetail{FeeCoin: rows[i].FeeDetail[j].FeeCoin}
			if fd.Fee, err = bgcommon.ParseDecimalOrZero(rows[i].FeeDetail[j].Fee); err != nil {
				return nil, errParse("GetBGBHistory", err)
			}
			rec.FeeDetail = append(rec.FeeDetail, fd)
		}
		out = append(out, rec)
	}
	return out, nil
}
