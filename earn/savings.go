/*
FILE: earn/savings.go

DESCRIPTION:
Savings sub-client — /api/v2/earn/savings/... Flexible / fixed yield:
product listing, account overview, held assets, history, the subscribe /
redeem flow and the subscribe / redeem result lookups.

Request params verified against the Bitget V2 earn/savings docs and the
tty666 / tiagosiebler reference clients. assets / records require a
periodType ("flexible" | "fixed") and walk the resultList/endId cursor.
*/

package earn

import (
	"context"
	"net/url"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	earntypes "github.com/tonymontanov/go-bitget/v2/earn/types"
)

// SavingsClient — earn savings sub-client.
type SavingsClient struct {
	c *Client
}

func newSavingsClient(c *Client) *SavingsClient {
	return &SavingsClient{c: c}
}

// ---------------------------------------------------------------------
// GetProducts — savings/product.
// ---------------------------------------------------------------------

type savingsAPYTierRow struct {
	RateLevel  string `json:"rateLevel"`
	MinStepVal string `json:"minStepVal"`
	MaxStepVal string `json:"maxStepVal"`
	CurrentApy string `json:"currentApy"`
}

type savingsProductRow struct {
	ProductID     string              `json:"productId"`
	Coin          string              `json:"coin"`
	PeriodType    string              `json:"periodType"`
	Period        string              `json:"period"`
	ApyType       string              `json:"apyType"`
	AdvanceRedeem string              `json:"advanceRedeem"`
	SettleMethod  string              `json:"settleMethod"`
	Status        string              `json:"status"`
	ProductLevel  string              `json:"productLevel"`
	ApyList       []savingsAPYTierRow `json:"apyList"`
}

func parseAPYTiers(scope string, rows []savingsAPYTierRow) ([]earntypes.SavingsAPYTier, error) {
	var out []earntypes.SavingsAPYTier = make([]earntypes.SavingsAPYTier, 0, len(rows))
	var i int
	var err error
	for i = 0; i < len(rows); i++ {
		var t earntypes.SavingsAPYTier = earntypes.SavingsAPYTier{RateLevel: rows[i].RateLevel}
		if t.MinStepVal, err = bgcommon.ParseDecimalOrZero(rows[i].MinStepVal); err != nil {
			return nil, errParse(scope, err)
		}
		if t.MaxStepVal, err = bgcommon.ParseDecimalOrZero(rows[i].MaxStepVal); err != nil {
			return nil, errParse(scope, err)
		}
		if t.CurrentApy, err = bgcommon.ParseDecimalOrZero(rows[i].CurrentApy); err != nil {
			return nil, errParse(scope, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// GetProducts lists savings products. coin and filter are optional;
// filter is one of "available" / "held" / "available_and_held" / "all".
func (s *SavingsClient) GetProducts(ctx context.Context, coin, filter string) ([]earntypes.SavingsProduct, error) {
	var query url.Values = url.Values{}
	if coin != "" {
		query.Set("coin", coin)
	}
	if filter != "" {
		query.Set("filter", filter)
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/savings/product",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []savingsProductRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Savings.GetProducts", err)
	}
	var out []earntypes.SavingsProduct = make([]earntypes.SavingsProduct, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var p earntypes.SavingsProduct = earntypes.SavingsProduct{
			ProductID:     rows[i].ProductID,
			Coin:          rows[i].Coin,
			PeriodType:    rows[i].PeriodType,
			Period:        rows[i].Period,
			ApyType:       rows[i].ApyType,
			AdvanceRedeem: rows[i].AdvanceRedeem,
			SettleMethod:  rows[i].SettleMethod,
			Status:        rows[i].Status,
			ProductLevel:  rows[i].ProductLevel,
		}
		if p.ApyList, err = parseAPYTiers("Savings.GetProducts", rows[i].ApyList); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAccount — savings/account.
// ---------------------------------------------------------------------

type savingsAccountRow struct {
	BTCAmount        string `json:"btcAmount"`
	USDTAmount       string `json:"usdtAmount"`
	BTC24hEarning    string `json:"btc24hEarning"`
	USDT24hEarning   string `json:"usdt24hEarning"`
	BTCTotalEarning  string `json:"btcTotalEarning"`
	USDTTotalEarning string `json:"usdtTotalEarning"`
}

// GetAccount returns the savings BTC/USDT holdings + earnings overview.
func (s *SavingsClient) GetAccount(ctx context.Context) (earntypes.SavingsAccount, error) {
	var out earntypes.SavingsAccount
	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/savings/account",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row savingsAccountRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Savings.GetAccount", err)
	}
	if out.BTCAmount, err = bgcommon.ParseDecimalOrZero(row.BTCAmount); err != nil {
		return out, errParse("Savings.GetAccount", err)
	}
	if out.USDTAmount, err = bgcommon.ParseDecimalOrZero(row.USDTAmount); err != nil {
		return out, errParse("Savings.GetAccount", err)
	}
	if out.BTC24hEarning, err = bgcommon.ParseDecimalOrZero(row.BTC24hEarning); err != nil {
		return out, errParse("Savings.GetAccount", err)
	}
	if out.USDT24hEarning, err = bgcommon.ParseDecimalOrZero(row.USDT24hEarning); err != nil {
		return out, errParse("Savings.GetAccount", err)
	}
	if out.BTCTotalEarning, err = bgcommon.ParseDecimalOrZero(row.BTCTotalEarning); err != nil {
		return out, errParse("Savings.GetAccount", err)
	}
	if out.USDTTotalEarning, err = bgcommon.ParseDecimalOrZero(row.USDTTotalEarning); err != nil {
		return out, errParse("Savings.GetAccount", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAssets — savings/assets (cursor paged).
// ---------------------------------------------------------------------

type savingsAssetAPYRow struct {
	RateLevel  string `json:"rateLevel"`
	MinApy     string `json:"minApy"`
	MaxApy     string `json:"maxApy"`
	CurrentApy string `json:"currentApy"`
}

type savingsAssetRow struct {
	ProductID       string               `json:"productId"`
	OrderID         string               `json:"orderId"`
	ProductCoin     string               `json:"productCoin"`
	InterestCoin    string               `json:"interestCoin"`
	PeriodType      string               `json:"periodType"`
	Period          string               `json:"period"`
	HoldAmount      string               `json:"holdAmount"`
	LastProfit      string               `json:"lastProfit"`
	TotalProfit     string               `json:"totalProfit"`
	HoldDays        string               `json:"holdDays"`
	Status          string               `json:"status"`
	AllowRedemption string               `json:"allowRedemption"`
	ProductLevel    string               `json:"productLevel"`
	Apy             []savingsAssetAPYRow `json:"apy"`
}

type savingsAssetsEnvelope struct {
	ResultList []savingsAssetRow `json:"resultList"`
	EndID      string            `json:"endId"`
}

// GetAssets returns the held savings positions for the given periodType
// ("flexible" | "fixed", required). Walks the idLessThan/endId cursor.
func (s *SavingsClient) GetAssets(ctx context.Context, periodType string) ([]earntypes.SavingsAsset, error) {
	if periodType == "" {
		return nil, errInvalid("Savings.GetAssets", "periodType is required (flexible | fixed)")
	}

	var rows []savingsAssetRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "earn.Savings.GetAssets",
		func(idLessThan string, limit int) ([]savingsAssetRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("periodType", periodType)
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = s.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/earn/savings/assets",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env savingsAssetsEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Savings.GetAssets", ferr)
			}
			return env.ResultList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []earntypes.SavingsAsset = make([]earntypes.SavingsAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var a earntypes.SavingsAsset = earntypes.SavingsAsset{
			ProductID:       rows[i].ProductID,
			OrderID:         rows[i].OrderID,
			ProductCoin:     rows[i].ProductCoin,
			InterestCoin:    rows[i].InterestCoin,
			PeriodType:      rows[i].PeriodType,
			Period:          rows[i].Period,
			Status:          rows[i].Status,
			AllowRedemption: rows[i].AllowRedemption,
			ProductLevel:    rows[i].ProductLevel,
		}
		if a.HoldAmount, err = bgcommon.ParseDecimalOrZero(rows[i].HoldAmount); err != nil {
			return nil, errParse("Savings.GetAssets", err)
		}
		if a.LastProfit, err = bgcommon.ParseDecimalOrZero(rows[i].LastProfit); err != nil {
			return nil, errParse("Savings.GetAssets", err)
		}
		if a.TotalProfit, err = bgcommon.ParseDecimalOrZero(rows[i].TotalProfit); err != nil {
			return nil, errParse("Savings.GetAssets", err)
		}
		a.HoldDays, _ = bgcommon.ParseInt64OrZero(rows[i].HoldDays)
		var j int
		a.Apy = make([]earntypes.SavingsAssetAPY, 0, len(rows[i].Apy))
		for j = 0; j < len(rows[i].Apy); j++ {
			var t earntypes.SavingsAssetAPY = earntypes.SavingsAssetAPY{RateLevel: rows[i].Apy[j].RateLevel}
			if t.MinApy, err = bgcommon.ParseDecimalOrZero(rows[i].Apy[j].MinApy); err != nil {
				return nil, errParse("Savings.GetAssets", err)
			}
			if t.MaxApy, err = bgcommon.ParseDecimalOrZero(rows[i].Apy[j].MaxApy); err != nil {
				return nil, errParse("Savings.GetAssets", err)
			}
			if t.CurrentApy, err = bgcommon.ParseDecimalOrZero(rows[i].Apy[j].CurrentApy); err != nil {
				return nil, errParse("Savings.GetAssets", err)
			}
			a.Apy = append(a.Apy, t)
		}
		out = append(out, a)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetRecords — savings/records (cursor paged).
// ---------------------------------------------------------------------

type savingsRecordRow struct {
	OrderID        string `json:"orderId"`
	CoinName       string `json:"coinName"`
	SettleCoinName string `json:"settleCoinName"`
	ProductType    string `json:"productType"`
	Period         string `json:"period"`
	ProductLevel   string `json:"productLevel"`
	Amount         string `json:"amount"`
	TS             string `json:"ts"`
	OrderType      string `json:"orderType"`
}

type savingsRecordsEnvelope struct {
	ResultList []savingsRecordRow `json:"resultList"`
	EndID      string             `json:"endId"`
}

// SavingsRecordsQuery — optional filters for GetRecords. PeriodType is
// required ("flexible" | "fixed"); the rest narrow the result.
type SavingsRecordsQuery struct {
	PeriodType  string
	Coin        string
	OrderType   string
	StartTimeMs int64
	EndTimeMs   int64
}

// GetRecords returns the savings transaction history (subscribe / redeem
// / interest). PeriodType is required; walks the idLessThan/endId cursor.
func (s *SavingsClient) GetRecords(ctx context.Context, q SavingsRecordsQuery) ([]earntypes.SavingsRecord, error) {
	if q.PeriodType == "" {
		return nil, errInvalid("Savings.GetRecords", "periodType is required (flexible | fixed)")
	}

	var rows []savingsRecordRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "earn.Savings.GetRecords",
		func(idLessThan string, limit int) ([]savingsRecordRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("periodType", q.PeriodType)
			query.Set("limit", strconv.Itoa(limit))
			if q.Coin != "" {
				query.Set("coin", q.Coin)
			}
			if q.OrderType != "" {
				query.Set("orderType", q.OrderType)
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
				Path:   "/api/v2/earn/savings/records",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env savingsRecordsEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Savings.GetRecords", ferr)
			}
			return env.ResultList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []earntypes.SavingsRecord = make([]earntypes.SavingsRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec earntypes.SavingsRecord = earntypes.SavingsRecord{
			OrderID:        rows[i].OrderID,
			CoinName:       rows[i].CoinName,
			SettleCoinName: rows[i].SettleCoinName,
			ProductType:    rows[i].ProductType,
			Period:         rows[i].Period,
			ProductLevel:   rows[i].ProductLevel,
			OrderType:      rows[i].OrderType,
		}
		if rec.Amount, err = bgcommon.ParseDecimalOrZero(rows[i].Amount); err != nil {
			return nil, errParse("Savings.GetRecords", err)
		}
		rec.TimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].TS)
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetSubscribeInfo — savings/subscribe-info.
// ---------------------------------------------------------------------

type savingsSubscribeInfoRow struct {
	SingleMinAmount    string              `json:"singleMinAmount"`
	SingleMaxAmount    string              `json:"singleMaxAmount"`
	RemainingAmount    string              `json:"remainingAmount"`
	SubscribePrecision string              `json:"subscribePrecision"`
	ProfitPrecision    string              `json:"profitPrecision"`
	SubscribeTime      string              `json:"subscribeTime"`
	InterestTime       string              `json:"interestTime"`
	SettleTime         string              `json:"settleTime"`
	ExpireTime         string              `json:"expireTime"`
	RedeemTime         string              `json:"redeemTime"`
	SettleMethod       string              `json:"settleMethod"`
	RedeemDelay        string              `json:"redeemDelay"`
	ApyList            []savingsAPYTierRow `json:"apyList"`
}

// GetSubscribeInfo returns the subscribe constraints (min/max, remaining
// quota, precisions, the various time markers and the APY ladder) for the
// product. productId and periodType are required.
func (s *SavingsClient) GetSubscribeInfo(ctx context.Context, productID, periodType string) (earntypes.SavingsSubscribeInfo, error) {
	var out earntypes.SavingsSubscribeInfo
	if productID == "" || periodType == "" {
		return out, errInvalid("Savings.GetSubscribeInfo", "productId and periodType are required")
	}

	var query url.Values = url.Values{}
	query.Set("productId", productID)
	query.Set("periodType", periodType)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/savings/subscribe-info",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row savingsSubscribeInfoRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Savings.GetSubscribeInfo", err)
	}
	out.SubscribePrecision = row.SubscribePrecision
	out.ProfitPrecision = row.ProfitPrecision
	out.SubscribeTime = row.SubscribeTime
	out.InterestTime = row.InterestTime
	out.SettleTime = row.SettleTime
	out.ExpireTime = row.ExpireTime
	out.RedeemTime = row.RedeemTime
	out.SettleMethod = row.SettleMethod
	out.RedeemDelay = row.RedeemDelay
	if out.SingleMinAmount, err = bgcommon.ParseDecimalOrZero(row.SingleMinAmount); err != nil {
		return out, errParse("Savings.GetSubscribeInfo", err)
	}
	if out.SingleMaxAmount, err = bgcommon.ParseDecimalOrZero(row.SingleMaxAmount); err != nil {
		return out, errParse("Savings.GetSubscribeInfo", err)
	}
	if out.RemainingAmount, err = bgcommon.ParseDecimalOrZero(row.RemainingAmount); err != nil {
		return out, errParse("Savings.GetSubscribeInfo", err)
	}
	if out.ApyList, err = parseAPYTiers("Savings.GetSubscribeInfo", row.ApyList); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Subscribe / GetSubscribeResult — savings/subscribe(-result).
// ---------------------------------------------------------------------

type savingsSubscribeBody struct {
	ProductID  string `json:"productId"`
	PeriodType string `json:"periodType"`
	Amount     string `json:"amount"`
}

type orderIDRow struct {
	OrderID string `json:"orderId"`
}

type opResultRow struct {
	Result string `json:"result"`
	Msg    string `json:"msg"`
}

// Subscribe subscribes `amount` of the product. productId, periodType
// ("flexible" | "fixed") and amount are required. Returns the new order
// id. NOTE: this moves real funds.
func (s *SavingsClient) Subscribe(ctx context.Context, productID, periodType, amount string) (string, error) {
	switch {
	case productID == "":
		return "", errInvalid("Savings.Subscribe", "productId is required")
	case periodType == "":
		return "", errInvalid("Savings.Subscribe", "periodType is required (flexible | fixed)")
	case amount == "":
		return "", errInvalid("Savings.Subscribe", "amount is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/savings/subscribe",
		Body:   savingsSubscribeBody{ProductID: productID, PeriodType: periodType, Amount: amount},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	var row orderIDRow
	if err = resp.UnmarshalData(&row); err != nil {
		return "", errParse("Savings.Subscribe", err)
	}
	return row.OrderID, nil
}

// GetSubscribeResult reports whether a subscription succeeded. productId
// and periodType are required.
func (s *SavingsClient) GetSubscribeResult(ctx context.Context, productID, periodType string) (earntypes.OpResult, error) {
	return s.opResult(ctx, "Savings.GetSubscribeResult", "/api/v2/earn/savings/subscribe-result",
		"productId", productID, "periodType", periodType)
}

// ---------------------------------------------------------------------
// Redeem / GetRedeemResult — savings/redeem(-result).
// ---------------------------------------------------------------------

type savingsRedeemBody struct {
	ProductID  string `json:"productId"`
	PeriodType string `json:"periodType"`
	Amount     string `json:"amount"`
	OrderID    string `json:"orderId,omitempty"`
}

type savingsRedeemRow struct {
	OrderID string `json:"orderId"`
	Status  string `json:"status"`
}

// Redeem redeems `amount` from the product. ProductID, PeriodType and
// Amount are required; OrderID is optional (the held asset's order id).
// NOTE: the venue rejects redemptions less than 1 minute apart.
func (s *SavingsClient) Redeem(ctx context.Context, req earntypes.SavingsRedeemRequest) (earntypes.RedeemResult, error) {
	var out earntypes.RedeemResult
	switch {
	case req.ProductID == "":
		return out, errInvalid("Savings.Redeem", "productId is required")
	case req.PeriodType == "":
		return out, errInvalid("Savings.Redeem", "periodType is required (flexible | fixed)")
	case req.Amount == "":
		return out, errInvalid("Savings.Redeem", "amount is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/savings/redeem",
		Body: savingsRedeemBody{
			ProductID:  req.ProductID,
			PeriodType: req.PeriodType,
			Amount:     req.Amount,
			OrderID:    req.OrderID,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row savingsRedeemRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Savings.Redeem", err)
	}
	out.OrderID = row.OrderID
	out.Status = row.Status
	return out, nil
}

// GetRedeemResult reports whether a redemption succeeded. orderId and
// periodType are required.
func (s *SavingsClient) GetRedeemResult(ctx context.Context, orderID, periodType string) (earntypes.OpResult, error) {
	return s.opResult(ctx, "Savings.GetRedeemResult", "/api/v2/earn/savings/redeem-result",
		"orderId", orderID, "periodType", periodType)
}

// opResult is the shared two-param GET → {result,msg} helper backing
// GetSubscribeResult / GetRedeemResult.
func (s *SavingsClient) opResult(ctx context.Context, scope, path, k1, v1, k2, v2 string) (earntypes.OpResult, error) {
	var out earntypes.OpResult
	if v1 == "" || v2 == "" {
		return out, errInvalid(scope, k1+" and "+k2+" are required")
	}

	var query url.Values = url.Values{}
	query.Set(k1, v1)
	query.Set(k2, v2)

	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}
	var row opResultRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse(scope, err)
	}
	out.Result = row.Result
	out.Msg = row.Msg
	return out, nil
}
