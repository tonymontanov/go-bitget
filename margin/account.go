/*
FILE: margin/account.go

DESCRIPTION:
Account / assets / borrow-repay / records sub-client for the Bitget V2
MARGIN profile. Every path is built for the pinned mode
(/api/v2/margin/<mode>/...). Wired in M3:

	GET  account/assets                  — GetAccountAssets
	POST account/borrow                  — Borrow
	POST account/repay                   — Repay
	GET  account/max-borrowable-amount   — GetMaxBorrowable
	GET  account/max-transfer-out-amount — GetMaxTransferOut
	GET  open-orders                     — GetOpenOrders   (paged)
	GET  history-orders                  — GetOrderHistory (paged)
	GET  fills                           — GetFills        (paged)
	GET  borrow-history                  — GetBorrowHistory      (paged)
	GET  repay-history                   — GetRepayHistory       (paged)
	GET  interest-history                — GetInterestHistory    (paged)
	GET  liquidation-history             — GetLiquidationHistory (paged)
	GET  financial-records               — GetFinancialRecords   (paged)

DEFERRED to M3b (complex / advanced — documented, not yet wired):
interest-rate-and-limit, tier-data, account/risk-rate, account/flash-repay
+ query-flash-repay-status, liquidation-order. These carry nested vip /
tier lists or async flash-repay state that the desk does not consume on
the hot path.

ISOLATED vs CROSSED PARAMS:

Isolated endpoints are per-symbol: borrow / repay REQUIRE `symbol`, and
the assets / max-* queries accept it. Crossed endpoints are per-coin and
MUST NOT carry a symbol. The SDK validates the symbol requirement for
isolated borrow / repay and otherwise forwards what the caller supplied.

PAGINATION:

The paged queries use the shared bgcommon.PaginateByCursor over Bitget's
idLessThan cursor; the cursor seed is the last row's id (orderId /
tradeId / loanId / repayId / interestId / liqId / marginId, per endpoint).
*/

package margin

import (
	"context"
	"net/url"
	"strconv"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// AccountClient — account / balance / borrow-repay / history sub-client.
// Built once per margin.Client (see client.go) and safe for concurrent
// use.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// pathFor builds a margin REST path for the pinned mode.
func (a *AccountClient) pathFor(suffix string) string {
	return "/api/v2/margin/" + a.c.modeSegment() + "/" + suffix
}

// isolated reports whether the client is pinned to isolated margin.
func (a *AccountClient) isolated() bool {
	return a.c.mode == roottypes.MarginModeIsolated
}

func (a *AccountClient) errInvalid(method, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Account."+method+": "+msg, nil)
}

func (a *AccountClient) errParse(method string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "margin.Account."+method+": parse", cause)
}

// ---------------------------------------------------------------------
// GetAccountAssets — account/assets.
// ---------------------------------------------------------------------

type assetsRow struct {
	Symbol      string `json:"symbol"`
	Coin        string `json:"coin"`
	TotalAmount string `json:"totalAmount"`
	Available   string `json:"available"`
	Frozen      string `json:"frozen"`
	Borrow      string `json:"borrow"`
	Interest    string `json:"interest"`
	Net         string `json:"net"`
	Coupon      string `json:"coupon"`
	UTime       string `json:"uTime"`
}

// GetAccountAssets returns the per-coin (crossed) or per-symbol-coin
// (isolated) margin balance snapshot. `coin` and `symbol` are optional
// filters; pass "" to omit. On crossed margin `symbol` is ignored by the
// venue.
func (a *AccountClient) GetAccountAssets(ctx context.Context, coin, symbol string) ([]margintypes.MarginAsset, error) {
	var query url.Values = url.Values{}
	if coin != "" {
		query.Set("coin", coin)
	}
	if symbol != "" {
		query.Set("symbol", symbol)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   a.pathFor("account/assets"),
		Query:  query,
		Signed: true,
		Meta:   a.queryMeta(symbol),
	})
	if err != nil {
		return nil, err
	}

	var rows []assetsRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, a.errParse("GetAccountAssets", err)
	}

	var out []margintypes.MarginAsset = make([]margintypes.MarginAsset, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var asset margintypes.MarginAsset
		asset, err = convertAssetRow(rows[i])
		if err != nil {
			return nil, a.errParse("GetAccountAssets", err)
		}
		out = append(out, asset)
	}
	return out, nil
}

func convertAssetRow(row assetsRow) (margintypes.MarginAsset, error) {
	var out margintypes.MarginAsset = margintypes.MarginAsset{
		Coin:   row.Coin,
		Symbol: row.Symbol,
	}
	var err error
	if out.TotalAmount, err = bgcommon.ParseDecimalOrZero(row.TotalAmount); err != nil {
		return margintypes.MarginAsset{}, err
	}
	if out.Available, err = bgcommon.ParseDecimalOrZero(row.Available); err != nil {
		return margintypes.MarginAsset{}, err
	}
	if out.Frozen, err = bgcommon.ParseDecimalOrZero(row.Frozen); err != nil {
		return margintypes.MarginAsset{}, err
	}
	if out.Borrow, err = bgcommon.ParseDecimalOrZero(row.Borrow); err != nil {
		return margintypes.MarginAsset{}, err
	}
	if out.Interest, err = bgcommon.ParseDecimalOrZero(row.Interest); err != nil {
		return margintypes.MarginAsset{}, err
	}
	if out.Net, err = bgcommon.ParseDecimalOrZero(row.Net); err != nil {
		return margintypes.MarginAsset{}, err
	}
	if out.Coupon, err = bgcommon.ParseDecimalOrZero(row.Coupon); err != nil {
		return margintypes.MarginAsset{}, err
	}
	if out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(row.UTime); err != nil {
		return margintypes.MarginAsset{}, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Borrow — account/borrow.
// ---------------------------------------------------------------------

type borrowBody struct {
	Coin         string `json:"coin"`
	BorrowAmount string `json:"borrowAmount"`
	Symbol       string `json:"symbol,omitempty"`
}

type borrowResp struct {
	LoanID       string `json:"loanId"`
	Coin         string `json:"coin"`
	Symbol       string `json:"symbol"`
	BorrowAmount string `json:"borrowAmount"`
}

// Borrow takes out a margin loan in `coin`. On isolated margin `symbol`
// is REQUIRED (the loan is scoped to the pair); on crossed margin it is
// omitted.
func (a *AccountClient) Borrow(ctx context.Context, coin, amount, symbol string) (margintypes.BorrowResult, error) {
	var out margintypes.BorrowResult
	if coin == "" {
		return out, a.errInvalid("Borrow", "coin is empty")
	}
	if amount == "" {
		return out, a.errInvalid("Borrow", "amount is empty")
	}
	if a.isolated() && symbol == "" {
		return out, a.errInvalid("Borrow", "symbol is required for isolated margin")
	}

	var body borrowBody = borrowBody{Coin: coin, BorrowAmount: amount}
	if a.isolated() {
		body.Symbol = symbol
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   a.pathFor("account/borrow"),
		Body:   body,
		Signed: true,
		Meta:   a.queryMeta(symbol),
	})
	if err != nil {
		return out, err
	}

	var data borrowResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, a.errParse("Borrow", err)
	}
	out.LoanID = data.LoanID
	out.Coin = data.Coin
	out.Symbol = data.Symbol
	if out.BorrowAmount, err = bgcommon.ParseDecimalOrZero(data.BorrowAmount); err != nil {
		return margintypes.BorrowResult{}, a.errParse("Borrow", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Repay — account/repay.
// ---------------------------------------------------------------------

type repayBody struct {
	Coin        string `json:"coin"`
	RepayAmount string `json:"repayAmount"`
	Symbol      string `json:"symbol,omitempty"`
	ClientOid   string `json:"clientOid,omitempty"`
}

type repayResp struct {
	RepayID          string `json:"repayId"`
	Coin             string `json:"coin"`
	Symbol           string `json:"symbol"`
	RepayAmount      string `json:"repayAmount"`
	RemainDebtAmount string `json:"remainDebtAmount"`
}

// Repay repays a margin loan in `coin`. On isolated margin `symbol` is
// REQUIRED; on crossed margin it is omitted. `clientOid` is optional.
func (a *AccountClient) Repay(ctx context.Context, coin, amount, symbol, clientOid string) (margintypes.RepayResult, error) {
	var out margintypes.RepayResult
	if coin == "" {
		return out, a.errInvalid("Repay", "coin is empty")
	}
	if amount == "" {
		return out, a.errInvalid("Repay", "amount is empty")
	}
	if a.isolated() && symbol == "" {
		return out, a.errInvalid("Repay", "symbol is required for isolated margin")
	}

	var body repayBody = repayBody{Coin: coin, RepayAmount: amount, ClientOid: clientOid}
	if a.isolated() {
		body.Symbol = symbol
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   a.pathFor("account/repay"),
		Body:   body,
		Signed: true,
		Meta:   a.queryMeta(symbol),
	})
	if err != nil {
		return out, err
	}

	var data repayResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, a.errParse("Repay", err)
	}
	out.RepayID = data.RepayID
	out.Coin = data.Coin
	out.Symbol = data.Symbol
	if out.RepayAmount, err = bgcommon.ParseDecimalOrZero(data.RepayAmount); err != nil {
		return margintypes.RepayResult{}, a.errParse("Repay", err)
	}
	if out.RemainDebtAmount, err = bgcommon.ParseDecimalOrZero(data.RemainDebtAmount); err != nil {
		return margintypes.RepayResult{}, a.errParse("Repay", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetMaxBorrowable — account/max-borrowable-amount.
// ---------------------------------------------------------------------

type maxBorrowableResp struct {
	Coin                     string `json:"coin"`
	MaxBorrowableAmount      string `json:"maxBorrowableAmount"`
	Symbol                   string `json:"symbol"`
	BaseCoin                 string `json:"baseCoin"`
	BaseCoinMaxBorrowAmount  string `json:"baseCoinMaxBorrowAmount"`
	QuoteCoin                string `json:"quoteCoin"`
	QuoteCoinMaxBorrowAmount string `json:"quoteCoinMaxBorrowAmount"`
}

// GetMaxBorrowable returns the maximum borrowable amount. On crossed
// margin pass `coin` (symbol ignored); on isolated margin pass `symbol`
// (the response carries base/quote variants).
func (a *AccountClient) GetMaxBorrowable(ctx context.Context, coin, symbol string) (margintypes.MaxBorrowable, error) {
	var out margintypes.MaxBorrowable
	var query url.Values = url.Values{}
	if coin != "" {
		query.Set("coin", coin)
	}
	if symbol != "" {
		query.Set("symbol", symbol)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   a.pathFor("account/max-borrowable-amount"),
		Query:  query,
		Signed: true,
		Meta:   a.queryMeta(symbol),
	})
	if err != nil {
		return out, err
	}

	var data maxBorrowableResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, a.errParse("GetMaxBorrowable", err)
	}
	out.Coin = data.Coin
	out.Symbol = data.Symbol
	out.BaseCoin = data.BaseCoin
	out.QuoteCoin = data.QuoteCoin
	if out.MaxBorrowableAmount, err = bgcommon.ParseDecimalOrZero(data.MaxBorrowableAmount); err != nil {
		return margintypes.MaxBorrowable{}, a.errParse("GetMaxBorrowable", err)
	}
	if out.BaseCoinMaxBorrowAmount, err = bgcommon.ParseDecimalOrZero(data.BaseCoinMaxBorrowAmount); err != nil {
		return margintypes.MaxBorrowable{}, a.errParse("GetMaxBorrowable", err)
	}
	if out.QuoteCoinMaxBorrowAmount, err = bgcommon.ParseDecimalOrZero(data.QuoteCoinMaxBorrowAmount); err != nil {
		return margintypes.MaxBorrowable{}, a.errParse("GetMaxBorrowable", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetMaxTransferOut — account/max-transfer-out-amount.
// ---------------------------------------------------------------------

type maxTransferOutResp struct {
	Coin                          string `json:"coin"`
	MaxTransferOutAmount          string `json:"maxTransferOutAmount"`
	Symbol                        string `json:"symbol"`
	BaseCoin                      string `json:"baseCoin"`
	BaseCoinMaxTransferOutAmount  string `json:"baseCoinMaxTransferOutAmount"`
	QuoteCoin                     string `json:"quoteCoin"`
	QuoteCoinMaxTransferOutAmount string `json:"quoteCoinMaxTransferOutAmount"`
}

// GetMaxTransferOut returns the maximum transferable-out amount. Crossed
// uses `coin`; isolated uses `symbol` (base/quote variants in the
// response).
func (a *AccountClient) GetMaxTransferOut(ctx context.Context, coin, symbol string) (margintypes.MaxTransferOut, error) {
	var out margintypes.MaxTransferOut
	var query url.Values = url.Values{}
	if coin != "" {
		query.Set("coin", coin)
	}
	if symbol != "" {
		query.Set("symbol", symbol)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   a.pathFor("account/max-transfer-out-amount"),
		Query:  query,
		Signed: true,
		Meta:   a.queryMeta(symbol),
	})
	if err != nil {
		return out, err
	}

	var data maxTransferOutResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, a.errParse("GetMaxTransferOut", err)
	}
	out.Coin = data.Coin
	out.Symbol = data.Symbol
	out.BaseCoin = data.BaseCoin
	out.QuoteCoin = data.QuoteCoin
	if out.MaxTransferOutAmount, err = bgcommon.ParseDecimalOrZero(data.MaxTransferOutAmount); err != nil {
		return margintypes.MaxTransferOut{}, a.errParse("GetMaxTransferOut", err)
	}
	if out.BaseCoinMaxTransferOutAmount, err = bgcommon.ParseDecimalOrZero(data.BaseCoinMaxTransferOutAmount); err != nil {
		return margintypes.MaxTransferOut{}, a.errParse("GetMaxTransferOut", err)
	}
	if out.QuoteCoinMaxTransferOutAmount, err = bgcommon.ParseDecimalOrZero(data.QuoteCoinMaxTransferOutAmount); err != nil {
		return margintypes.MaxTransferOut{}, a.errParse("GetMaxTransferOut", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Order / fill rows.
// ---------------------------------------------------------------------

type marginOrderRow struct {
	OrderID   string `json:"orderId"`
	Symbol    string `json:"symbol"`
	OrderType string `json:"orderType"`
	ClientOid string `json:"clientOid"`
	LoanType  string `json:"loanType"`
	Price     string `json:"price"`
	Side      string `json:"side"`
	Status    string `json:"status"`
	BaseSize  string `json:"baseSize"`
	QuoteSize string `json:"quoteSize"`
	PriceAvg  string `json:"priceAvg"`
	Size      string `json:"size"`
	Amount    string `json:"amount"`
	Force     string `json:"force"`
	CTime     string `json:"cTime"`
	UTime     string `json:"uTime"`
}

// convertOrderRow maps one margin order row into the typed OrderInfo.
//
// Quantity is the base-denominated order size: baseSize when populated,
// otherwise `size`. FilledQuantity has no dedicated field on the margin
// order rows (Bitget exposes only baseSize / size / amount / priceAvg)
// — it is left zero; callers needing fills go through GetFills.
func convertOrderRow(row marginOrderRow) (margintypes.OrderInfo, error) {
	var out margintypes.OrderInfo = margintypes.OrderInfo{
		OrderID:       row.OrderID,
		ClientOrderID: row.ClientOid,
		Symbol:        row.Symbol,
		Side:          roottypes.SideType(row.Side),
		OrderType:     roottypes.OrderType(row.OrderType),
		TimeInForce:   roottypes.TimeInForceType(row.Force),
		Status:        roottypes.OrderStatus(row.Status),
		LoanType:      margintypes.LoanType(row.LoanType),
	}
	var err error
	var qtyRaw string = row.BaseSize
	if qtyRaw == "" {
		qtyRaw = row.Size
	}
	if out.Quantity, err = bgcommon.ParseDecimalOrZero(qtyRaw); err != nil {
		return margintypes.OrderInfo{}, err
	}
	if out.Price, err = bgcommon.ParseDecimalOrZero(row.Price); err != nil {
		return margintypes.OrderInfo{}, err
	}
	if out.AvgFilledPrice, err = bgcommon.ParseDecimalOrZero(row.PriceAvg); err != nil {
		return margintypes.OrderInfo{}, err
	}
	if out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(row.CTime); err != nil {
		return margintypes.OrderInfo{}, err
	}
	if out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(row.UTime); err != nil {
		return margintypes.OrderInfo{}, err
	}
	return out, nil
}

type marginFillRow struct {
	OrderID    string          `json:"orderId"`
	TradeID    string          `json:"tradeId"`
	OrderType  string          `json:"orderType"`
	Side       string          `json:"side"`
	PriceAvg   string          `json:"priceAvg"`
	Size       string          `json:"size"`
	Amount     string          `json:"amount"`
	TradeScope string          `json:"tradeScope"`
	FeeDetail  marginFeeDetail `json:"feeDetail"`
	CTime      string          `json:"cTime"`
	UTime      string          `json:"uTime"`
}

type marginFeeDetail struct {
	Deduction         string `json:"deduction"`
	FeeCoin           string `json:"feeCoin"`
	TotalDeductionFee string `json:"totalDeductionFee"`
	TotalFee          string `json:"totalFee"`
}

func convertFillRow(row marginFillRow) (margintypes.Fill, error) {
	var out margintypes.Fill = margintypes.Fill{
		OrderID:    row.OrderID,
		TradeID:    row.TradeID,
		Side:       row.Side,
		OrderType:  row.OrderType,
		FeeCoin:    row.FeeDetail.FeeCoin,
		TradeScope: row.TradeScope,
	}
	var err error
	if out.FillPrice, err = bgcommon.ParseDecimalOrZero(row.PriceAvg); err != nil {
		return margintypes.Fill{}, err
	}
	if out.Size, err = bgcommon.ParseDecimalOrZero(row.Size); err != nil {
		return margintypes.Fill{}, err
	}
	if out.Amount, err = bgcommon.ParseDecimalOrZero(row.Amount); err != nil {
		return margintypes.Fill{}, err
	}
	if out.TotalFee, err = bgcommon.ParseDecimalOrZero(row.FeeDetail.TotalFee); err != nil {
		return margintypes.Fill{}, err
	}
	if out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(row.CTime); err != nil {
		return margintypes.Fill{}, err
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetOpenOrders / GetOrderHistory — open-orders / history-orders.
// ---------------------------------------------------------------------

// GetOpenOrders returns every live margin order for `symbol`. Bitget V2
// margin requires the symbol (unlike spot, where it is optional).
func (a *AccountClient) GetOpenOrders(ctx context.Context, symbol string) ([]margintypes.OrderInfo, error) {
	if symbol == "" {
		return nil, a.errInvalid("GetOpenOrders", "symbol is required")
	}
	return a.fetchOrders(ctx, "GetOpenOrders", a.pathFor("open-orders"), symbol, 0, 0)
}

// GetOrderHistory returns closed margin orders for `symbol` in the
// optional [startTimeMs, endTimeMs] window (0 leaves a bound off).
func (a *AccountClient) GetOrderHistory(ctx context.Context, symbol string, startTimeMs, endTimeMs int64) ([]margintypes.OrderInfo, error) {
	if symbol == "" {
		return nil, a.errInvalid("GetOrderHistory", "symbol is required")
	}
	return a.fetchOrders(ctx, "GetOrderHistory", a.pathFor("history-orders"), symbol, startTimeMs, endTimeMs)
}

func (a *AccountClient) fetchOrders(ctx context.Context, method, path, symbol string, startTimeMs, endTimeMs int64) ([]margintypes.OrderInfo, error) {
	var rows []marginOrderRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "margin.Account."+method,
		func(idLessThan string, limit int) ([]marginOrderRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("symbol", symbol)
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			if startTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
			}
			if endTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   path,
				Query:  query,
				Signed: true,
				Meta:   a.queryMeta(symbol),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var pageRows []marginOrderRow
			if ferr = resp.UnmarshalData(&pageRows); ferr != nil {
				return nil, "", a.errParse(method, ferr)
			}
			var nextEndID string
			if len(pageRows) > 0 {
				nextEndID = pageRows[len(pageRows)-1].OrderID
			}
			return pageRows, nextEndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []margintypes.OrderInfo = make([]margintypes.OrderInfo, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var info margintypes.OrderInfo
		info, err = convertOrderRow(rows[i])
		if err != nil {
			return nil, a.errParse(method, err)
		}
		out = append(out, info)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetFills — fills.
// ---------------------------------------------------------------------

// GetFills returns trade executions for `symbol`, optionally restricted
// to a single `orderID` and a [startTimeMs, endTimeMs] window. Symbol is
// required.
func (a *AccountClient) GetFills(ctx context.Context, symbol, orderID string, startTimeMs, endTimeMs int64) ([]margintypes.Fill, error) {
	if symbol == "" {
		return nil, a.errInvalid("GetFills", "symbol is required")
	}

	var rows []marginFillRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "margin.Account.GetFills",
		func(idLessThan string, limit int) ([]marginFillRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("symbol", symbol)
			if orderID != "" {
				query.Set("orderId", orderID)
			}
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			if startTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
			}
			if endTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   a.pathFor("fills"),
				Query:  query,
				Signed: true,
				Meta:   a.queryMeta(symbol),
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var pageRows []marginFillRow
			if ferr = resp.UnmarshalData(&pageRows); ferr != nil {
				return nil, "", a.errParse("GetFills", ferr)
			}
			var nextEndID string
			if len(pageRows) > 0 {
				nextEndID = pageRows[len(pageRows)-1].TradeID
			}
			return pageRows, nextEndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []margintypes.Fill = make([]margintypes.Fill, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var f margintypes.Fill
		f, err = convertFillRow(rows[i])
		if err != nil {
			return nil, a.errParse("GetFills", err)
		}
		out = append(out, f)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// History records — borrow / repay / interest / liquidation / financial.
// ---------------------------------------------------------------------

type borrowRecordRow struct {
	LoanID       string `json:"loanId"`
	Coin         string `json:"coin"`
	BorrowAmount string `json:"borrowAmount"`
	BorrowType   string `json:"borrowType"`
	CTime        string `json:"cTime"`
	UTime        string `json:"uTime"`
}

// GetBorrowHistory returns borrow records for the optional `coin` filter
// and [startTimeMs, endTimeMs] window.
func (a *AccountClient) GetBorrowHistory(ctx context.Context, coin string, startTimeMs, endTimeMs int64) ([]margintypes.BorrowRecord, error) {
	var rows []borrowRecordRow
	var err error
	rows, err = fetchRecords(ctx, a, "GetBorrowHistory", a.pathFor("borrow-history"), coin, startTimeMs, endTimeMs,
		func(r borrowRecordRow) string { return r.LoanID })
	if err != nil {
		return nil, err
	}
	var out []margintypes.BorrowRecord = make([]margintypes.BorrowRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec margintypes.BorrowRecord = margintypes.BorrowRecord{
			LoanID:     rows[i].LoanID,
			Coin:       rows[i].Coin,
			BorrowType: rows[i].BorrowType,
		}
		if rec.BorrowAmount, err = bgcommon.ParseDecimalOrZero(rows[i].BorrowAmount); err != nil {
			return nil, a.errParse("GetBorrowHistory", err)
		}
		rec.CreatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		rec.UpdatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, rec)
	}
	return out, nil
}

type repayRecordRow struct {
	RepayID        string `json:"repayId"`
	Coin           string `json:"coin"`
	Symbol         string `json:"symbol"`
	RepayAmount    string `json:"repayAmount"`
	RepayInterest  string `json:"repayInterest"`
	RepayPrincipal string `json:"repayPrincipal"`
	RepayType      string `json:"repayType"`
	CTime          string `json:"cTime"`
	UTime          string `json:"uTime"`
}

// GetRepayHistory returns repay records for the optional `coin` filter
// and time window.
func (a *AccountClient) GetRepayHistory(ctx context.Context, coin string, startTimeMs, endTimeMs int64) ([]margintypes.RepayRecord, error) {
	var rows []repayRecordRow
	var err error
	rows, err = fetchRecords(ctx, a, "GetRepayHistory", a.pathFor("repay-history"), coin, startTimeMs, endTimeMs,
		func(r repayRecordRow) string { return r.RepayID })
	if err != nil {
		return nil, err
	}
	var out []margintypes.RepayRecord = make([]margintypes.RepayRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec margintypes.RepayRecord = margintypes.RepayRecord{
			RepayID:   rows[i].RepayID,
			Coin:      rows[i].Coin,
			Symbol:    rows[i].Symbol,
			RepayType: rows[i].RepayType,
		}
		if rec.RepayAmount, err = bgcommon.ParseDecimalOrZero(rows[i].RepayAmount); err != nil {
			return nil, a.errParse("GetRepayHistory", err)
		}
		if rec.RepayInterest, err = bgcommon.ParseDecimalOrZero(rows[i].RepayInterest); err != nil {
			return nil, a.errParse("GetRepayHistory", err)
		}
		if rec.RepayPrincipal, err = bgcommon.ParseDecimalOrZero(rows[i].RepayPrincipal); err != nil {
			return nil, a.errParse("GetRepayHistory", err)
		}
		rec.CreatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		rec.UpdatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, rec)
	}
	return out, nil
}

type interestRecordRow struct {
	InterestID        string `json:"interestId"`
	InterestCoin      string `json:"interestCoin"`
	LoanCoin          string `json:"loanCoin"`
	Symbol            string `json:"symbol"`
	DailyInterestRate string `json:"dailyInterestRate"`
	InterestAmount    string `json:"interestAmount"`
	InterestType      string `json:"interstType"`
	CTime             string `json:"cTime"`
	UTime             string `json:"uTime"`
}

// GetInterestHistory returns interest-accrual records for the optional
// `coin` filter and time window.
func (a *AccountClient) GetInterestHistory(ctx context.Context, coin string, startTimeMs, endTimeMs int64) ([]margintypes.InterestRecord, error) {
	var rows []interestRecordRow
	var err error
	rows, err = fetchRecords(ctx, a, "GetInterestHistory", a.pathFor("interest-history"), coin, startTimeMs, endTimeMs,
		func(r interestRecordRow) string { return r.InterestID })
	if err != nil {
		return nil, err
	}
	var out []margintypes.InterestRecord = make([]margintypes.InterestRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec margintypes.InterestRecord = margintypes.InterestRecord{
			InterestID:   rows[i].InterestID,
			InterestCoin: rows[i].InterestCoin,
			LoanCoin:     rows[i].LoanCoin,
			Symbol:       rows[i].Symbol,
			InterestType: rows[i].InterestType,
		}
		if rec.DailyInterestRate, err = bgcommon.ParseDecimalOrZero(rows[i].DailyInterestRate); err != nil {
			return nil, a.errParse("GetInterestHistory", err)
		}
		if rec.InterestAmount, err = bgcommon.ParseDecimalOrZero(rows[i].InterestAmount); err != nil {
			return nil, a.errParse("GetInterestHistory", err)
		}
		rec.CreatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		rec.UpdatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, rec)
	}
	return out, nil
}

type liquidationRecordRow struct {
	LiqID        string `json:"liqId"`
	Symbol       string `json:"symbol"`
	LiqStartTime string `json:"liqStartTime"`
	LiqEndTime   string `json:"liqEndTime"`
	LiqRiskRatio string `json:"liqRiskRatio"`
	TotalAssets  string `json:"totalAssets"`
	TotalDebt    string `json:"totalDebt"`
	LiqFee       string `json:"liqFee"`
	CTime        string `json:"cTime"`
	UTime        string `json:"uTime"`
}

// GetLiquidationHistory returns liquidation records for the optional
// time window. There is no coin filter on this endpoint.
func (a *AccountClient) GetLiquidationHistory(ctx context.Context, startTimeMs, endTimeMs int64) ([]margintypes.LiquidationRecord, error) {
	var rows []liquidationRecordRow
	var err error
	rows, err = fetchRecords(ctx, a, "GetLiquidationHistory", a.pathFor("liquidation-history"), "", startTimeMs, endTimeMs,
		func(r liquidationRecordRow) string { return r.LiqID })
	if err != nil {
		return nil, err
	}
	var out []margintypes.LiquidationRecord = make([]margintypes.LiquidationRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec margintypes.LiquidationRecord = margintypes.LiquidationRecord{
			LiqID:  rows[i].LiqID,
			Symbol: rows[i].Symbol,
		}
		if rec.LiqRiskRatio, err = bgcommon.ParseDecimalOrZero(rows[i].LiqRiskRatio); err != nil {
			return nil, a.errParse("GetLiquidationHistory", err)
		}
		if rec.TotalAssets, err = bgcommon.ParseDecimalOrZero(rows[i].TotalAssets); err != nil {
			return nil, a.errParse("GetLiquidationHistory", err)
		}
		if rec.TotalDebt, err = bgcommon.ParseDecimalOrZero(rows[i].TotalDebt); err != nil {
			return nil, a.errParse("GetLiquidationHistory", err)
		}
		if rec.LiqFee, err = bgcommon.ParseDecimalOrZero(rows[i].LiqFee); err != nil {
			return nil, a.errParse("GetLiquidationHistory", err)
		}
		rec.LiqStartTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].LiqStartTime)
		rec.LiqEndTimeMs, _ = bgcommon.ParseInt64OrZero(rows[i].LiqEndTime)
		rec.CreatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		rec.UpdatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, rec)
	}
	return out, nil
}

type financialRecordRow struct {
	Coin       string `json:"coin"`
	Symbol     string `json:"symbol"`
	MarginID   string `json:"marginId"`
	Amount     string `json:"amount"`
	Balance    string `json:"balance"`
	Fee        string `json:"fee"`
	MarginType string `json:"marginType"`
	CTime      string `json:"cTime"`
	UTime      string `json:"uTime"`
}

// GetFinancialRecords returns financial-flow records for the optional
// `coin` filter and time window.
func (a *AccountClient) GetFinancialRecords(ctx context.Context, coin string, startTimeMs, endTimeMs int64) ([]margintypes.FinancialRecord, error) {
	var rows []financialRecordRow
	var err error
	rows, err = fetchRecords(ctx, a, "GetFinancialRecords", a.pathFor("financial-records"), coin, startTimeMs, endTimeMs,
		func(r financialRecordRow) string { return r.MarginID })
	if err != nil {
		return nil, err
	}
	var out []margintypes.FinancialRecord = make([]margintypes.FinancialRecord, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var rec margintypes.FinancialRecord = margintypes.FinancialRecord{
			Coin:       rows[i].Coin,
			Symbol:     rows[i].Symbol,
			MarginID:   rows[i].MarginID,
			MarginType: rows[i].MarginType,
		}
		if rec.Amount, err = bgcommon.ParseDecimalOrZero(rows[i].Amount); err != nil {
			return nil, a.errParse("GetFinancialRecords", err)
		}
		if rec.Balance, err = bgcommon.ParseDecimalOrZero(rows[i].Balance); err != nil {
			return nil, a.errParse("GetFinancialRecords", err)
		}
		if rec.Fee, err = bgcommon.ParseDecimalOrZero(rows[i].Fee); err != nil {
			return nil, a.errParse("GetFinancialRecords", err)
		}
		rec.CreatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].CTime)
		rec.UpdatedAtMs, _ = bgcommon.ParseInt64OrZero(rows[i].UTime)
		out = append(out, rec)
	}
	return out, nil
}

// fetchRecords runs the shared cursor-paginated record query. T is the
// raw row type (inferred from cursorOf); cursorOf extracts the per-row
// id used as the next idLessThan.
func fetchRecords[T any](
	ctx context.Context,
	a *AccountClient,
	method, path, coin string,
	startTimeMs, endTimeMs int64,
	cursorOf func(T) string,
) ([]T, error) {
	return bgcommon.PaginateByCursor(ctx, "margin.Account."+method,
		func(idLessThan string, limit int) ([]T, string, error) {
			var query url.Values = url.Values{}
			if coin != "" {
				query.Set("coin", coin)
			}
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			if startTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
			}
			if endTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   path,
				Query:  query,
				Signed: true,
				Meta:   rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)},
			})
			if ferr != nil {
				return nil, "", ferr
			}
			var pageRows []T
			if ferr = resp.UnmarshalData(&pageRows); ferr != nil {
				return nil, "", a.errParse(method, ferr)
			}
			var nextEndID string
			if len(pageRows) > 0 {
				nextEndID = cursorOf(pageRows[len(pageRows)-1])
			}
			return pageRows, nextEndID, nil
		})
}

// queryMeta builds the rate-limit meta for a query, tagging the symbol
// when known.
func (a *AccountClient) queryMeta(symbol string) rest.RequestMeta {
	var meta rest.RequestMeta = rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)}
	if symbol != "" {
		meta.Symbols = []string{symbol}
	}
	return meta
}
