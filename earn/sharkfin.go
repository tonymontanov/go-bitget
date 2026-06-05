/*
FILE: earn/sharkfin.go

DESCRIPTION:
Shark Fin (structured product) sub-client — /api/v2/earn/sharkfin/...
Product listing, account overview, held assets, history, the
subscribe-info / subscribe flow and the subscribe-result lookup.

Request params verified against the Bitget V2 earn/sharkfin docs and the
tty666 / tiagosiebler reference clients. assets (by status) and records
(by type) walk the resultList/endId cursor; the product list is cursor
paged too.
*/

package earn

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	earntypes "github.com/tonymontanov/go-bitget/v2/earn/types"
)

// SharkFinClient — earn shark-fin sub-client. Built once per earn.Client
// and safe for concurrent use.
type SharkFinClient struct {
	c *Client
}

func newSharkFinClient(c *Client) *SharkFinClient {
	return &SharkFinClient{c: c}
}

// ---------------------------------------------------------------------
// GetProducts — sharkfin/product (cursor paged).
// ---------------------------------------------------------------------

type sharkfinProductRow struct {
	ProductID         string `json:"productId"`
	ProductName       string `json:"productName"`
	ProductCoin       string `json:"productCoin"`
	SubscribeCoin     string `json:"subscribeCoin"`
	FarmingStartTime  string `json:"farmingStartTime"`
	FarmingEndTime    string `json:"farmingEndTime"`
	LowerRate         string `json:"lowerRate"`
	DefaultRate       string `json:"defaultRate"`
	UpperRate         string `json:"upperRate"`
	Period            string `json:"period"`
	InterestStartTime string `json:"interestStartTime"`
	Status            string `json:"status"`
	MinAmount         string `json:"minAmount"`
	LimitAmount       string `json:"limitAmount"`
	SoldAmount        string `json:"soldAmount"`
	EndTime           string `json:"endTime"`
	StartTime         string `json:"startTime"`
}

type sharkfinProductsEnvelope struct {
	ResultList []sharkfinProductRow `json:"resultList"`
	EndID      string               `json:"endId"`
}

// GetProducts lists shark-fin products. coin is an optional filter (sent
// only when non-empty). Walks the idLessThan/endId cursor.
func (s *SharkFinClient) GetProducts(ctx context.Context, coin string) ([]earntypes.SharkFinProduct, error) {
	var rows []sharkfinProductRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "earn.SharkFin.GetProducts",
		func(idLessThan string, limit int) ([]sharkfinProductRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("limit", strconv.Itoa(limit))
			if coin != "" {
				query.Set("coin", coin)
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/earn/sharkfin/product",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env sharkfinProductsEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SharkFin.GetProducts", ferr)
			}
			return env.ResultList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []earntypes.SharkFinProduct = make([]earntypes.SharkFinProduct, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var p earntypes.SharkFinProduct = earntypes.SharkFinProduct{
			ProductID:     rows[i].ProductID,
			ProductName:   rows[i].ProductName,
			ProductCoin:   rows[i].ProductCoin,
			SubscribeCoin: rows[i].SubscribeCoin,
			Status:        rows[i].Status,
			Period:        rows[i].Period,
		}
		if p.LowerRate, err = bgcommon.ParseDecimalOrZero(rows[i].LowerRate); err != nil {
			return nil, errParse("SharkFin.GetProducts", err)
		}
		if p.DefaultRate, err = bgcommon.ParseDecimalOrZero(rows[i].DefaultRate); err != nil {
			return nil, errParse("SharkFin.GetProducts", err)
		}
		if p.UpperRate, err = bgcommon.ParseDecimalOrZero(rows[i].UpperRate); err != nil {
			return nil, errParse("SharkFin.GetProducts", err)
		}
		if p.MinAmount, err = bgcommon.ParseDecimalOrZero(rows[i].MinAmount); err != nil {
			return nil, errParse("SharkFin.GetProducts", err)
		}
		if p.LimitAmount, err = bgcommon.ParseDecimalOrZero(rows[i].LimitAmount); err != nil {
			return nil, errParse("SharkFin.GetProducts", err)
		}
		if p.SoldAmount, err = bgcommon.ParseDecimalOrZero(rows[i].SoldAmount); err != nil {
			return nil, errParse("SharkFin.GetProducts", err)
		}
		p.FarmingStartTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].FarmingStartTime)
		p.FarmingEndTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].FarmingEndTime)
		p.InterestStartTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].InterestStartTime)
		p.StartTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].StartTime)
		p.EndTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].EndTime)
		out = append(out, p)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAccount — sharkfin/account.
// ---------------------------------------------------------------------

type sharkfinAccountRow struct {
	BTCSubscribeAmount   string `json:"btcSubscribeAmount"`
	USDTSubscribeAmount  string `json:"usdtSubscribeAmount"`
	BTCHistoricalAmount  string `json:"btcHistoricalAmount"`
	USDTHistoricalAmount string `json:"usdtHistoricalAmount"`
	BTCTotalEarning      string `json:"btcTotalEarning"`
	USDTTotalEarning     string `json:"usdtTotalEarning"`
}

// GetAccount returns the shark-fin BTC/USDT subscribe/historical/earning
// overview.
func (s *SharkFinClient) GetAccount(ctx context.Context) (earntypes.SharkFinAccount, error) {
	var out earntypes.SharkFinAccount
	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/sharkfin/account",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row sharkfinAccountRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SharkFin.GetAccount", err)
	}
	if out.BTCSubscribeAmount, err = bgcommon.ParseDecimalOrZero(row.BTCSubscribeAmount); err != nil {
		return out, errParse("SharkFin.GetAccount", err)
	}
	if out.USDTSubscribeAmount, err = bgcommon.ParseDecimalOrZero(row.USDTSubscribeAmount); err != nil {
		return out, errParse("SharkFin.GetAccount", err)
	}
	if out.BTCHistoricalAmount, err = bgcommon.ParseDecimalOrZero(row.BTCHistoricalAmount); err != nil {
		return out, errParse("SharkFin.GetAccount", err)
	}
	if out.USDTHistoricalAmount, err = bgcommon.ParseDecimalOrZero(row.USDTHistoricalAmount); err != nil {
		return out, errParse("SharkFin.GetAccount", err)
	}
	if out.BTCTotalEarning, err = bgcommon.ParseDecimalOrZero(row.BTCTotalEarning); err != nil {
		return out, errParse("SharkFin.GetAccount", err)
	}
	if out.USDTTotalEarning, err = bgcommon.ParseDecimalOrZero(row.USDTTotalEarning); err != nil {
		return out, errParse("SharkFin.GetAccount", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAssets — sharkfin/assets (cursor paged, by status).
// ---------------------------------------------------------------------

type sharkfinAssetRow struct {
	ProductID         string `json:"productId"`
	InterestStartTime string `json:"interestStartTime"`
	InterestEndTime   string `json:"interestEndTime"`
	ProductCoin       string `json:"productCoin"`
	SubscribeCoin     string `json:"subscribeCoin"`
	Trend             string `json:"trend"`
	SettleTime        string `json:"settleTime"`
	InterestAmount    string `json:"interestAmount"`
	ProductStatus     string `json:"productStatus"`
}

type sharkfinAssetsEnvelope struct {
	ResultList []sharkfinAssetRow `json:"resultList"`
	EndID      string             `json:"endId"`
}

// GetAssets returns the held shark-fin positions filtered by status
// (required). Walks the idLessThan/endId cursor.
func (s *SharkFinClient) GetAssets(ctx context.Context, status string) ([]earntypes.SharkFinAsset, error) {
	if status == "" {
		return nil, errInvalid("SharkFin.GetAssets", "status is required")
	}

	var rows []sharkfinAssetRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "earn.SharkFin.GetAssets",
		func(idLessThan string, limit int) ([]sharkfinAssetRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("status", status)
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/earn/sharkfin/assets",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env sharkfinAssetsEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SharkFin.GetAssets", ferr)
			}
			return env.ResultList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []earntypes.SharkFinAsset = make([]earntypes.SharkFinAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var a earntypes.SharkFinAsset = earntypes.SharkFinAsset{
			ProductID:     rows[i].ProductID,
			ProductCoin:   rows[i].ProductCoin,
			SubscribeCoin: rows[i].SubscribeCoin,
			Trend:         rows[i].Trend,
			ProductStatus: rows[i].ProductStatus,
		}
		if a.InterestAmount, err = bgcommon.ParseDecimalOrZero(rows[i].InterestAmount); err != nil {
			return nil, errParse("SharkFin.GetAssets", err)
		}
		a.InterestStartTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].InterestStartTime)
		a.InterestEndTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].InterestEndTime)
		a.SettleTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].SettleTime)
		out = append(out, a)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetRecords — sharkfin/records (cursor paged, by type).
// ---------------------------------------------------------------------

type sharkfinRecordRow struct {
	OrderID string `json:"orderId"`
	Product string `json:"product"`
	Period  string `json:"period"`
	Amount  string `json:"amount"`
	TS      string `json:"ts"`
	Type    string `json:"type"`
}

type sharkfinRecordsEnvelope struct {
	ResultList []sharkfinRecordRow `json:"resultList"`
	EndID      string              `json:"endId"`
}

// SharkFinRecordsQuery — filters for GetRecords. Type is required; the
// rest narrow the result.
type SharkFinRecordsQuery struct {
	Type        string
	Coin        string
	StartTimeMs int64
	EndTimeMs   int64
}

// GetRecords returns the shark-fin history filtered by type (required).
// Walks the idLessThan/endId cursor.
func (s *SharkFinClient) GetRecords(ctx context.Context, q SharkFinRecordsQuery) ([]earntypes.SharkFinRecord, error) {
	if q.Type == "" {
		return nil, errInvalid("SharkFin.GetRecords", "type is required")
	}

	var rows []sharkfinRecordRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "earn.SharkFin.GetRecords",
		func(idLessThan string, limit int) ([]sharkfinRecordRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("type", q.Type)
			query.Set("limit", strconv.Itoa(limit))
			if q.Coin != "" {
				query.Set("coin", q.Coin)
			}
			if q.StartTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			}
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/earn/sharkfin/records",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env sharkfinRecordsEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("SharkFin.GetRecords", ferr)
			}
			return env.ResultList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []earntypes.SharkFinRecord = make([]earntypes.SharkFinRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec earntypes.SharkFinRecord = earntypes.SharkFinRecord{
			OrderID: rows[i].OrderID,
			Product: rows[i].Product,
			Period:  rows[i].Period,
			Type:    rows[i].Type,
		}
		if rec.Amount, err = bgcommon.ParseDecimalOrZero(rows[i].Amount); err != nil {
			return nil, errParse("SharkFin.GetRecords", err)
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetSubscribeInfo — sharkfin/subscribe-info.
// ---------------------------------------------------------------------

type sharkfinSubscribeInfoRow struct {
	ProductCoin        string `json:"productCoin"`
	SubscribeCoin      string `json:"subscribeCoin"`
	InterestTime       string `json:"interestTime"`
	ExpirationTime     string `json:"expirationTime"`
	MinPrice           string `json:"minPrice"`
	CurrentPrice       string `json:"currentPrice"`
	MaxPrice           string `json:"maxPrice"`
	MinRate            string `json:"minRate"`
	DefaultRate        string `json:"defaultRate"`
	MaxRate            string `json:"maxRate"`
	Period             string `json:"period"`
	ProductMinAmount   string `json:"productMinAmount"`
	AvailableBalance   string `json:"availableBalance"`
	UserAmount         string `json:"userAmount"`
	RemainingAmount    string `json:"remainingAmount"`
	ProfitPrecision    string `json:"profitPrecision"`
	SubscribePrecision string `json:"subscribePrecision"`
}

// GetSubscribeInfo returns the shark-fin subscribe constraints for the
// product. productId is required.
func (s *SharkFinClient) GetSubscribeInfo(ctx context.Context, productID string) (earntypes.SharkFinSubscribeInfo, error) {
	var out earntypes.SharkFinSubscribeInfo
	if productID == "" {
		return out, errInvalid("SharkFin.GetSubscribeInfo", "productId is required")
	}

	var query url.Values = url.Values{}
	query.Set("productId", productID)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/sharkfin/subscribe-info",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row sharkfinSubscribeInfoRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SharkFin.GetSubscribeInfo", err)
	}
	out.ProductCoin = row.ProductCoin
	out.SubscribeCoin = row.SubscribeCoin
	out.Period = row.Period
	out.ProfitPrecision = row.ProfitPrecision
	out.SubscribePrecision = row.SubscribePrecision
	out.InterestTimeMs, _ = bgcommon.ParseInt64OrZero(row.InterestTime)
	out.ExpirationTimeMs, _ = bgcommon.ParseInt64OrZero(row.ExpirationTime)
	var fields = []struct {
		dst *decimal.Decimal
		src string
	}{
		{&out.MinPrice, row.MinPrice},
		{&out.CurrentPrice, row.CurrentPrice},
		{&out.MaxPrice, row.MaxPrice},
		{&out.MinRate, row.MinRate},
		{&out.DefaultRate, row.DefaultRate},
		{&out.MaxRate, row.MaxRate},
		{&out.ProductMinAmount, row.ProductMinAmount},
		{&out.AvailableBalance, row.AvailableBalance},
		{&out.UserAmount, row.UserAmount},
		{&out.RemainingAmount, row.RemainingAmount},
	}
	var i int
	for i = 0; i < len(fields); i++ {
		if *fields[i].dst, err = bgcommon.ParseDecimalOrZero(fields[i].src); err != nil {
			return out, errParse("SharkFin.GetSubscribeInfo", err)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Subscribe / GetSubscribeResult — sharkfin/subscribe(-result).
// ---------------------------------------------------------------------

type sharkfinSubscribeBody struct {
	ProductID string `json:"productId"`
	Amount    string `json:"amount"`
}

// Subscribe subscribes `amount` to the shark-fin product. productId and
// amount are required. Returns the new order id. NOTE: moves real funds.
func (s *SharkFinClient) Subscribe(ctx context.Context, productID, amount string) (string, error) {
	switch {
	case productID == "":
		return "", errInvalid("SharkFin.Subscribe", "productId is required")
	case amount == "":
		return "", errInvalid("SharkFin.Subscribe", "amount is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/sharkfin/subscribe",
		Body:   sharkfinSubscribeBody{ProductID: productID, Amount: amount},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	var row orderIDRow
	if err = resp.UnmarshalData(&row); err != nil {
		return "", errParse("SharkFin.Subscribe", err)
	}
	return row.OrderID, nil
}

// GetSubscribeResult reports whether a shark-fin subscription succeeded.
// orderId is required.
func (s *SharkFinClient) GetSubscribeResult(ctx context.Context, orderID string) (earntypes.OpResult, error) {
	var out earntypes.OpResult
	if orderID == "" {
		return out, errInvalid("SharkFin.GetSubscribeResult", "orderId is required")
	}

	var query url.Values = url.Values{}
	query.Set("orderId", orderID)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/sharkfin/subscribe-result",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row opResultRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("SharkFin.GetSubscribeResult", err)
	}
	out.Result = row.Result
	out.Msg = row.Msg
	return out, nil
}
