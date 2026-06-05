/*
FILE: earn/elite.go

DESCRIPTION:
On-Chain Elite sub-client — /api/v2/earn/elite/... Product listing, held
assets, history, the subscribe-info / subscribe flow, the subscribe
result lookup and the redeem-info / redeem flow.

Request params verified against the Bitget V2 earn/elite reference request
types. records walks a `cursor`/`endId` cursor over `recordList`; assets
is a single `resultList` (no cursor). The redeemType / paymentAccount
fields arrive as a string or a string array and are normalised via
flexStringList.
*/

package earn

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	earntypes "github.com/tonymontanov/go-bitget/v2/earn/types"
)

// EliteClient — earn on-chain-elite sub-client. Built once per
// earn.Client and safe for concurrent use.
type EliteClient struct {
	c *Client
}

func newEliteClient(c *Client) *EliteClient {
	return &EliteClient{c: c}
}

// flexStringList decodes a JSON value that may be either a string or an
// array of strings into a []string (the elite redeemType / paymentAccount
// fields use both forms).
type flexStringList []string

func (f *flexStringList) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*f = nil
		return nil
	}
	if b[0] == '[' {
		var arr []string
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*f = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*f = nil
	} else {
		*f = []string{s}
	}
	return nil
}

// eliteSubCoinRow is the wire shape of one subscription-coin entry.
type eliteSubCoinRow struct {
	SubscriptionCoin string `json:"subscriptionCoin"`
	Precision        string `json:"precision"`
	FeeRate          string `json:"feeRate"`
	ExchangeRate     string `json:"exchangeRate"`
	RemainQuota      string `json:"remainQuota"`
	MinAmount        string `json:"minAmount"`
}

func parseEliteSubCoins(scope string, rows []eliteSubCoinRow) ([]earntypes.EliteSubscriptionCoin, error) {
	var out []earntypes.EliteSubscriptionCoin = make([]earntypes.EliteSubscriptionCoin, 0, len(rows))
	var i int
	var err error
	for i = 0; i < len(rows); i++ {
		var sc earntypes.EliteSubscriptionCoin = earntypes.EliteSubscriptionCoin{
			SubscriptionCoin: rows[i].SubscriptionCoin,
			Precision:        rows[i].Precision,
		}
		if sc.FeeRate, err = bgcommon.ParseDecimalOrZero(rows[i].FeeRate); err != nil {
			return nil, errParse(scope, err)
		}
		if sc.ExchangeRate, err = bgcommon.ParseDecimalOrZero(rows[i].ExchangeRate); err != nil {
			return nil, errParse(scope, err)
		}
		if sc.RemainQuota, err = bgcommon.ParseDecimalOrZero(rows[i].RemainQuota); err != nil {
			return nil, errParse(scope, err)
		}
		if sc.MinAmount, err = bgcommon.ParseDecimalOrZero(rows[i].MinAmount); err != nil {
			return nil, errParse(scope, err)
		}
		out = append(out, sc)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetProducts — elite/product.
// ---------------------------------------------------------------------

type eliteProductRow struct {
	ProductID            string            `json:"productId"`
	Coin                 string            `json:"coin"`
	MinApr               string            `json:"minApr"`
	MaxApr               string            `json:"maxApr"`
	SellOut              string            `json:"sellOut"`
	SubscriptionCoinList []eliteSubCoinRow `json:"subscriptionCoinList"`
}

// GetProducts lists on-chain elite products.
func (e *EliteClient) GetProducts(ctx context.Context) ([]earntypes.EliteProduct, error) {
	var resp rest.Response
	var err error
	resp, _, err = e.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/elite/product",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var rows []eliteProductRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, errParse("Elite.GetProducts", err)
	}
	var out []earntypes.EliteProduct = make([]earntypes.EliteProduct, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var p earntypes.EliteProduct = earntypes.EliteProduct{
			ProductID: rows[i].ProductID,
			Coin:      rows[i].Coin,
			SellOut:   rows[i].SellOut,
		}
		if p.MinApr, err = bgcommon.ParseDecimalOrZero(rows[i].MinApr); err != nil {
			return nil, errParse("Elite.GetProducts", err)
		}
		if p.MaxApr, err = bgcommon.ParseDecimalOrZero(rows[i].MaxApr); err != nil {
			return nil, errParse("Elite.GetProducts", err)
		}
		if p.SubscriptionCoinList, err = parseEliteSubCoins("Elite.GetProducts", rows[i].SubscriptionCoinList); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAssets — elite/assets (single resultList, no cursor).
// ---------------------------------------------------------------------

type eliteAssetProjectRow struct {
	ProjectName string `json:"projectName"`
}

type eliteAssetRow struct {
	ProductID         string                 `json:"productId"`
	ProductCoin       string                 `json:"productCoin"`
	HoldingAmount     string                 `json:"holdingAmount"`
	USDTHoldingAmount string                 `json:"usdtHoldingAmount"`
	ExchangeRate      string                 `json:"exchangeRate"`
	Apr               string                 `json:"apr"`
	MinApy            string                 `json:"minApy"`
	MaxApy            string                 `json:"maxApy"`
	SubscriptionCoin  string                 `json:"subscriptionCoin"`
	ExchangeAmount    string                 `json:"exchangeAmount"`
	ProjectList       []eliteAssetProjectRow `json:"projectList"`
	UnsettledBGPoints string                 `json:"unsettledBGPoints"`
	InterestCoin      string                 `json:"interestCoin"`
	TotalProfit       string                 `json:"totalProfit"`
}

type eliteAssetsEnvelope struct {
	ResultList []eliteAssetRow `json:"resultList"`
}

// GetAssets returns the held on-chain elite positions.
func (e *EliteClient) GetAssets(ctx context.Context) ([]earntypes.EliteAsset, error) {
	var resp rest.Response
	var err error
	resp, _, err = e.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/elite/assets",
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return nil, err
	}

	var env eliteAssetsEnvelope
	if err = resp.UnmarshalData(&env); err != nil {
		return nil, errParse("Elite.GetAssets", err)
	}
	var out []earntypes.EliteAsset = make([]earntypes.EliteAsset, 0, len(env.ResultList))
	var i int
	for i = 0; i < len(env.ResultList); i++ {
		var a earntypes.EliteAsset = earntypes.EliteAsset{
			ProductID:        env.ResultList[i].ProductID,
			ProductCoin:      env.ResultList[i].ProductCoin,
			SubscriptionCoin: env.ResultList[i].SubscriptionCoin,
			InterestCoin:     env.ResultList[i].InterestCoin,
		}
		var fields = []struct {
			dst *decimal.Decimal
			src string
		}{
			{&a.HoldingAmount, env.ResultList[i].HoldingAmount},
			{&a.USDTHoldingAmount, env.ResultList[i].USDTHoldingAmount},
			{&a.ExchangeRate, env.ResultList[i].ExchangeRate},
			{&a.Apr, env.ResultList[i].Apr},
			{&a.MinApy, env.ResultList[i].MinApy},
			{&a.MaxApy, env.ResultList[i].MaxApy},
			{&a.ExchangeAmount, env.ResultList[i].ExchangeAmount},
			{&a.UnsettledBGPoints, env.ResultList[i].UnsettledBGPoints},
			{&a.TotalProfit, env.ResultList[i].TotalProfit},
		}
		var j int
		for j = 0; j < len(fields); j++ {
			if *fields[j].dst, err = bgcommon.ParseDecimalOrZero(fields[j].src); err != nil {
				return nil, errParse("Elite.GetAssets", err)
			}
		}
		a.ProjectList = make([]string, 0, len(env.ResultList[i].ProjectList))
		for j = 0; j < len(env.ResultList[i].ProjectList); j++ {
			a.ProjectList = append(a.ProjectList, env.ResultList[i].ProjectList[j].ProjectName)
		}
		out = append(out, a)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetRecords — elite/records (cursor paged via cursor/endId, recordList).
// ---------------------------------------------------------------------

type eliteRecordRow struct {
	RecordID               string         `json:"recordId"`
	ProductID              string         `json:"productId"`
	Coin                   string         `json:"coin"`
	Status                 string         `json:"status"`
	ExchangeRate           string         `json:"exchangeRate"`
	ReceivedCoin           string         `json:"receivedCoin"`
	ReceivedAmount         string         `json:"receivedAmount"`
	InvestAmount           string         `json:"investAmount"`
	FeeRate                string         `json:"feeRate"`
	RedeemType             flexStringList `json:"redeemType"`
	ReceivingAccount       string         `json:"receivingAccount"`
	ActualReceivingAccount string         `json:"actualReceivingAccount"`
	PaymentAccount         flexStringList `json:"paymentAccount"`
	SettlePoints           string         `json:"settlePoints"`
	Fee                    string         `json:"fee"`
}

type eliteRecordsEnvelope struct {
	RecordList []eliteRecordRow `json:"recordList"`
	EndID      string           `json:"endId"`
}

// EliteRecordType narrows GetRecords; one of "subscribe" / "redeem" /
// "interest" (required by the venue).
type EliteRecordsQuery struct {
	Type        string
	StartTimeMs int64
	EndTimeMs   int64
}

// GetRecords returns the elite history filtered by type (required: one of
// "subscribe" / "redeem" / "interest"). Walks the cursor/endId cursor
// over recordList.
func (e *EliteClient) GetRecords(ctx context.Context, q EliteRecordsQuery) ([]earntypes.EliteRecord, error) {
	if q.Type == "" {
		return nil, errInvalid("Elite.GetRecords", "type is required (subscribe | redeem | interest)")
	}

	var rows []eliteRecordRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "earn.Elite.GetRecords",
		func(cursor string, limit int) ([]eliteRecordRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("type", q.Type)
			query.Set("limit", strconv.Itoa(limit))
			if q.StartTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(q.StartTimeMs, 10))
			}
			if q.EndTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(q.EndTimeMs, 10))
			}
			if cursor != "" {
				query.Set("cursor", cursor)
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = e.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/earn/elite/records",
				Query:  query,
				Signed: true,
				Meta:   queryMeta(),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var env eliteRecordsEnvelope
			if ferr = resp.UnmarshalData(&env); ferr != nil {
				return nil, "", errParse("Elite.GetRecords", ferr)
			}
			return env.RecordList, env.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []earntypes.EliteRecord = make([]earntypes.EliteRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec earntypes.EliteRecord = earntypes.EliteRecord{
			RecordID:               rows[i].RecordID,
			ProductID:              rows[i].ProductID,
			Coin:                   rows[i].Coin,
			Status:                 rows[i].Status,
			ReceivedCoin:           rows[i].ReceivedCoin,
			ReceivingAccount:       rows[i].ReceivingAccount,
			ActualReceivingAccount: rows[i].ActualReceivingAccount,
			RedeemType:             []string(rows[i].RedeemType),
			PaymentAccount:         []string(rows[i].PaymentAccount),
		}
		var fields = []struct {
			dst *decimal.Decimal
			src string
		}{
			{&rec.ExchangeRate, rows[i].ExchangeRate},
			{&rec.ReceivedAmount, rows[i].ReceivedAmount},
			{&rec.InvestAmount, rows[i].InvestAmount},
			{&rec.FeeRate, rows[i].FeeRate},
			{&rec.SettlePoints, rows[i].SettlePoints},
			{&rec.Fee, rows[i].Fee},
		}
		var j int
		for j = 0; j < len(fields); j++ {
			if *fields[j].dst, err = bgcommon.ParseDecimalOrZero(fields[j].src); err != nil {
				return nil, errParse("Elite.GetRecords", err)
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetSubscribeInfo — elite/subscribe-info.
// ---------------------------------------------------------------------

type eliteSubscribeInfoRow struct {
	ProductSubID         string            `json:"productSubId"`
	MinAmount            string            `json:"minAmount"`
	RemainQuota          string            `json:"remainQuota"`
	ExchangeRate         string            `json:"exchangeRate"`
	ProductCoin          string            `json:"productCoin"`
	InterestTime         string            `json:"interestTime"`
	SettleTime           string            `json:"settleTime"`
	Precision            string            `json:"precision"`
	FeeRate              string            `json:"feeRate"`
	SubscriptionCoinList []eliteSubCoinRow `json:"subscriptionCoinList"`
}

// GetSubscribeInfo returns the elite subscribe constraints for the
// product. productId is required.
func (e *EliteClient) GetSubscribeInfo(ctx context.Context, productID string) (earntypes.EliteSubscribeInfo, error) {
	var out earntypes.EliteSubscribeInfo
	if productID == "" {
		return out, errInvalid("Elite.GetSubscribeInfo", "productId is required")
	}

	var query url.Values = url.Values{}
	query.Set("productId", productID)

	var resp rest.Response
	var err error
	resp, _, err = e.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/elite/subscribe-info",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row eliteSubscribeInfoRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Elite.GetSubscribeInfo", err)
	}
	out.ProductSubID = row.ProductSubID
	out.ProductCoin = row.ProductCoin
	out.Precision = row.Precision
	out.InterestTime = row.InterestTime
	out.SettleTime = row.SettleTime
	if out.MinAmount, err = bgcommon.ParseDecimalOrZero(row.MinAmount); err != nil {
		return out, errParse("Elite.GetSubscribeInfo", err)
	}
	if out.RemainQuota, err = bgcommon.ParseDecimalOrZero(row.RemainQuota); err != nil {
		return out, errParse("Elite.GetSubscribeInfo", err)
	}
	if out.ExchangeRate, err = bgcommon.ParseDecimalOrZero(row.ExchangeRate); err != nil {
		return out, errParse("Elite.GetSubscribeInfo", err)
	}
	if out.FeeRate, err = bgcommon.ParseDecimalOrZero(row.FeeRate); err != nil {
		return out, errParse("Elite.GetSubscribeInfo", err)
	}
	if out.SubscriptionCoinList, err = parseEliteSubCoins("Elite.GetSubscribeInfo", row.SubscriptionCoinList); err != nil {
		return out, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Subscribe / GetSubscribeResult — elite/subscribe(-result).
// ---------------------------------------------------------------------

type eliteSubscribeBody struct {
	ProductSubID   string `json:"productSubId"`
	Amount         string `json:"amount"`
	Coin           string `json:"coin,omitempty"`
	PaymentAccount string `json:"paymentAccount,omitempty"`
}

// Subscribe subscribes to an elite product sub-id. ProductSubID and
// Amount are required; Coin and PaymentAccount ("spot" | "unified") are
// optional. Returns the new order id. NOTE: moves real funds.
func (e *EliteClient) Subscribe(ctx context.Context, req earntypes.EliteSubscribeRequest) (string, error) {
	switch {
	case req.ProductSubID == "":
		return "", errInvalid("Elite.Subscribe", "productSubId is required")
	case req.Amount == "":
		return "", errInvalid("Elite.Subscribe", "amount is required")
	}

	var resp rest.Response
	var err error
	resp, _, err = e.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/elite/subscribe",
		Body: eliteSubscribeBody{
			ProductSubID:   req.ProductSubID,
			Amount:         req.Amount,
			Coin:           req.Coin,
			PaymentAccount: req.PaymentAccount,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	var row orderIDRow
	if err = resp.UnmarshalData(&row); err != nil {
		return "", errParse("Elite.Subscribe", err)
	}
	return row.OrderID, nil
}

type eliteStatusRow struct {
	Result string `json:"result"`
}

// GetSubscribeResult reports the terminal status of an elite
// subscription ("settled" / "pending" / "rejected"). orderId is required.
func (e *EliteClient) GetSubscribeResult(ctx context.Context, orderID string) (string, error) {
	if orderID == "" {
		return "", errInvalid("Elite.GetSubscribeResult", "orderId is required")
	}

	var query url.Values = url.Values{}
	query.Set("orderId", orderID)

	var resp rest.Response
	var err error
	resp, _, err = e.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/elite/subscribe-result",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	var row eliteStatusRow
	if err = resp.UnmarshalData(&row); err != nil {
		return "", errParse("Elite.GetSubscribeResult", err)
	}
	return row.Result, nil
}

// ---------------------------------------------------------------------
// GetRedeemInfo / Redeem — elite/redeem-info, elite/redeem.
// ---------------------------------------------------------------------

type eliteBgusdReceiveCoinRow struct {
	BgusdReceiveCoin  string `json:"bgusdReceiveCoin"`
	BgusdExchangeRate string `json:"bgusdExchangeRate"`
}

type eliteRedeemModeRow struct {
	RedeemFeeRate   string `json:"redeemFeeRate"`
	RemainQuota     string `json:"remainQuota"`
	RedeemType      string `json:"redeemType"`
	RedeemScale     string `json:"redeemScale"`
	RedeemDelayDate string `json:"redeemDelayDate"`
	MinRedeemAmount string `json:"minRedeemAmount"`
	RedeemTime      string `json:"redeemTime"`
}

type eliteRedeemInfoRow struct {
	ProductID                string                     `json:"productId"`
	ProductSubID             string                     `json:"productSubId"`
	ProductCoin              string                     `json:"productCoin"`
	SubscriptionCoin         string                     `json:"subscriptionCoin"`
	ProfitCoin               string                     `json:"profitCoin"`
	ExchangeRate             string                     `json:"exchangeRate"`
	TotalUnPayInterestAmount string                     `json:"totalUnPayInterestAmount"`
	PreSettleApr             string                     `json:"preSettleApr"`
	ReceivedCoin             string                     `json:"receivedCoin"`
	UnsettledPoints          string                     `json:"unsettledPoints"`
	BgusdReceiveCoinList     []eliteBgusdReceiveCoinRow `json:"bgusdReceiveCoinList"`
	RedeemModeList           []eliteRedeemModeRow       `json:"redeemModeList"`
}

// GetRedeemInfo returns the elite redemption preview (exchange rate,
// unpaid interest, the available redeem modes). productId is required.
func (e *EliteClient) GetRedeemInfo(ctx context.Context, productID string) (earntypes.EliteRedeemInfo, error) {
	var out earntypes.EliteRedeemInfo
	if productID == "" {
		return out, errInvalid("Elite.GetRedeemInfo", "productId is required")
	}

	var query url.Values = url.Values{}
	query.Set("productId", productID)

	var resp rest.Response
	var err error
	resp, _, err = e.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/earn/elite/redeem-info",
		Query:  query,
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return out, err
	}

	var row eliteRedeemInfoRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, errParse("Elite.GetRedeemInfo", err)
	}
	out.ProductID = row.ProductID
	out.ProductSubID = row.ProductSubID
	out.ProductCoin = row.ProductCoin
	out.SubscriptionCoin = row.SubscriptionCoin
	out.ProfitCoin = row.ProfitCoin
	out.ReceivedCoin = row.ReceivedCoin
	if out.ExchangeRate, err = bgcommon.ParseDecimalOrZero(row.ExchangeRate); err != nil {
		return out, errParse("Elite.GetRedeemInfo", err)
	}
	if out.TotalUnPayInterestAmount, err = bgcommon.ParseDecimalOrZero(row.TotalUnPayInterestAmount); err != nil {
		return out, errParse("Elite.GetRedeemInfo", err)
	}
	if out.PreSettleApr, err = bgcommon.ParseDecimalOrZero(row.PreSettleApr); err != nil {
		return out, errParse("Elite.GetRedeemInfo", err)
	}
	if out.UnsettledPoints, err = bgcommon.ParseDecimalOrZero(row.UnsettledPoints); err != nil {
		return out, errParse("Elite.GetRedeemInfo", err)
	}

	var i int
	out.BgusdReceiveCoinList = make([]earntypes.EliteBgusdReceiveCoin, 0, len(row.BgusdReceiveCoinList))
	for i = 0; i < len(row.BgusdReceiveCoinList); i++ {
		var bc earntypes.EliteBgusdReceiveCoin = earntypes.EliteBgusdReceiveCoin{BgusdReceiveCoin: row.BgusdReceiveCoinList[i].BgusdReceiveCoin}
		if bc.BgusdExchangeRate, err = bgcommon.ParseDecimalOrZero(row.BgusdReceiveCoinList[i].BgusdExchangeRate); err != nil {
			return out, errParse("Elite.GetRedeemInfo", err)
		}
		out.BgusdReceiveCoinList = append(out.BgusdReceiveCoinList, bc)
	}

	out.RedeemModeList = make([]earntypes.EliteRedeemMode, 0, len(row.RedeemModeList))
	for i = 0; i < len(row.RedeemModeList); i++ {
		var rm earntypes.EliteRedeemMode = earntypes.EliteRedeemMode{
			RedeemType:      row.RedeemModeList[i].RedeemType,
			RedeemDelayDate: row.RedeemModeList[i].RedeemDelayDate,
			RedeemTime:      row.RedeemModeList[i].RedeemTime,
		}
		if rm.RedeemFeeRate, err = bgcommon.ParseDecimalOrZero(row.RedeemModeList[i].RedeemFeeRate); err != nil {
			return out, errParse("Elite.GetRedeemInfo", err)
		}
		if rm.RemainQuota, err = bgcommon.ParseDecimalOrZero(row.RedeemModeList[i].RemainQuota); err != nil {
			return out, errParse("Elite.GetRedeemInfo", err)
		}
		if rm.RedeemScale, err = bgcommon.ParseDecimalOrZero(row.RedeemModeList[i].RedeemScale); err != nil {
			return out, errParse("Elite.GetRedeemInfo", err)
		}
		if rm.MinRedeemAmount, err = bgcommon.ParseDecimalOrZero(row.RedeemModeList[i].MinRedeemAmount); err != nil {
			return out, errParse("Elite.GetRedeemInfo", err)
		}
		out.RedeemModeList = append(out.RedeemModeList, rm)
	}
	return out, nil
}

type eliteRedeemBody struct {
	ProductID      string `json:"productId"`
	ProductSubID   string `json:"productSubId"`
	RedeemType     string `json:"redeemType"`
	Amount         string `json:"amount"`
	ReceiveAccount string `json:"receiveAccount"`
	AdvancedSettle string `json:"advancedSettle,omitempty"`
	Coin           string `json:"coin,omitempty"`
}

// Redeem redeems from an elite position. ProductID, ProductSubID,
// RedeemType ("fast" | "standard"), Amount and ReceiveAccount ("spot" |
// "unified") are required; AdvancedSettle ("yes" | "no") and Coin are
// optional. Returns the new order id. NOTE: moves real funds.
func (e *EliteClient) Redeem(ctx context.Context, req earntypes.EliteRedeemRequest) (string, error) {
	switch {
	case req.ProductID == "":
		return "", errInvalid("Elite.Redeem", "productId is required")
	case req.ProductSubID == "":
		return "", errInvalid("Elite.Redeem", "productSubId is required")
	case req.RedeemType == "":
		return "", errInvalid("Elite.Redeem", "redeemType is required (fast | standard)")
	case req.Amount == "":
		return "", errInvalid("Elite.Redeem", "amount is required")
	case req.ReceiveAccount == "":
		return "", errInvalid("Elite.Redeem", "receiveAccount is required (spot | unified)")
	}

	var resp rest.Response
	var err error
	resp, _, err = e.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/earn/elite/redeem",
		Body: eliteRedeemBody{
			ProductID:      req.ProductID,
			ProductSubID:   req.ProductSubID,
			RedeemType:     req.RedeemType,
			Amount:         req.Amount,
			ReceiveAccount: req.ReceiveAccount,
			AdvancedSettle: req.AdvancedSettle,
			Coin:           req.Coin,
		},
		Signed: true,
		Meta:   queryMeta(),
	})
	if err != nil {
		return "", err
	}
	var row orderIDRow
	if err = resp.UnmarshalData(&row); err != nil {
		return "", errParse("Elite.Redeem", err)
	}
	return row.OrderID, nil
}
