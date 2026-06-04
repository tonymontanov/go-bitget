/*
FILE: margin/trading.go

DESCRIPTION:
Trading sub-client for the Bitget V2 MARGIN profile. Wires the single +
batch place / cancel endpoints (mode segment = crossed / isolated):

  - POST /api/v2/margin/<mode>/place-order        — CreateOrder
  - POST /api/v2/margin/<mode>/batch-place-order  — CreateBatchOrders
  - POST /api/v2/margin/<mode>/cancel-order       — CancelOrder
  - POST /api/v2/margin/<mode>/batch-cancel-order — CancelBatchOrders

NO AMEND ENDPOINT:

Bitget V2 ships NO margin cancel-replace / amend endpoint (unlike spot's
native batch-cancel-replace-order). A re-price on margin is a cancel
followed by a fresh place, and that orchestration belongs to the caller
— the SDK does not synthesise it.

DIFFERENCES FROM spot.TradingClient:

  - Every order carries a loanType (auto-borrow / auto-repay control);
    empty defaults to "normal".
  - Size is split into baseSize / quoteSize on the wire (spot uses a
    single side-dependent `size`). The SDK ships the field that matches
    the order shape and validates the right one is set.
  - Optional stpMode (self-trade prevention).
  - batch-place is per-symbol (symbol at the top level + orderList of
    same-symbol rows) — same homogeneity rule as spot / mix.

NUMERIC HANDLING:

baseSize / quoteSize / price are shipped verbatim from the typed request
(decimal.Decimal.String()); the SDK performs no base/quote conversion.
*/

package margin

import (
	"context"
	"strconv"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// TradingClient — REST trading sub-client. Built once per margin.Client
// (see client.go) and safe for concurrent use.
type TradingClient struct {
	c *Client
}

func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{c: c}
}

// pathFor builds a margin REST path for the pinned mode:
// "/api/v2/margin/<mode>/" + suffix.
func (t *TradingClient) pathFor(suffix string) string {
	return "/api/v2/margin/" + t.c.modeSegment() + "/" + suffix
}

// ---------------------------------------------------------------------
// Single-order: CreateOrder.
// ---------------------------------------------------------------------

// placeOrderBody mirrors the JSON body Bitget V2 expects on
// POST /api/v2/margin/<mode>/place-order. Optional fields use omitempty;
// loanType is always populated (defaulted to "normal" upstream).
type placeOrderBody struct {
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	OrderType string `json:"orderType"`
	LoanType  string `json:"loanType"`
	Force     string `json:"force,omitempty"`
	Price     string `json:"price,omitempty"`
	BaseSize  string `json:"baseSize,omitempty"`
	QuoteSize string `json:"quoteSize,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
	STPMode   string `json:"stpMode,omitempty"`
}

// placeOrderResp is the JSON `data` returned by place-order on success
// (verified against the live docs: {orderId, clientOid}).
type placeOrderResp struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// CreateOrder submits one margin order. The returned OrderInfo is built
// from the response payload (orderId + clientOid) plus the request
// itself — Bitget does not echo full lifecycle here; the M3 query
// endpoints are the source of truth for fill / status.
func (t *TradingClient) CreateOrder(ctx context.Context, req margintypes.CreateOrderRequest) (margintypes.OrderInfo, error) {
	var out margintypes.OrderInfo
	var err error
	if err = validateCreateOrderRequest(req); err != nil {
		return out, err
	}

	var body placeOrderBody = buildPlaceBody(req)

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   t.pathFor("place-order"),
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{req.Symbol},
			OrderCount: 1,
			Category:   string(bitget.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return out, err
	}

	var data placeOrderResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "margin.Trading.CreateOrder: parse", err)
	}
	return margintypes.OrderInfo{
		OrderID:       data.OrderID,
		ClientOrderID: bgcommon.ChooseClientOid(data.ClientOid, req.ClientOrderID),
		Symbol:        req.Symbol,
		Side:          req.Side,
		OrderType:     req.OrderType,
		TimeInForce:   req.TimeInForce,
		LoanType:      resolveLoanType(req.LoanType),
		Status:        roottypes.OrderStatusLive,
		Quantity:      orderQuantity(req),
		Price:         req.Price,
	}, nil
}

// ---------------------------------------------------------------------
// Single-order: CancelOrder.
// ---------------------------------------------------------------------

// cancelOrderBody mirrors the JSON body of POST
// /api/v2/margin/<mode>/cancel-order. Either orderId or clientOid is
// required; if both are present Bitget gives orderId priority.
type cancelOrderBody struct {
	Symbol    string `json:"symbol"`
	OrderID   string `json:"orderId,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

// CancelOrder cancels one margin order. Returns nil on success, error on
// any rejection.
func (t *TradingClient) CancelOrder(ctx context.Context, req roottypes.CancelOrderRequest) error {
	var err error
	if err = validateCancelOrderRequest(req); err != nil {
		return err
	}

	var body cancelOrderBody = cancelOrderBody{
		Symbol:    req.Symbol,
		OrderID:   req.OrderID,
		ClientOid: req.ClientOrderID,
	}
	_, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   t.pathFor("cancel-order"),
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{req.Symbol},
			OrderCount: 1,
			Category:   string(bitget.RateLimitCategoryCancel),
		},
	})
	return err
}

// ---------------------------------------------------------------------
// Batch: CreateBatchOrders.
// ---------------------------------------------------------------------

// batchPlaceOrderBody is the wire payload for batch-place-order. Bitget
// requires `symbol` at the top level (every orderList row inherits it)
// — the SDK validates that all reqs share the same symbol.
type batchPlaceOrderBody struct {
	Symbol    string                 `json:"symbol"`
	OrderList []batchPlaceOrderEntry `json:"orderList"`
}

// batchPlaceOrderEntry is one row of orderList (symbol lives at the top
// level).
type batchPlaceOrderEntry struct {
	Side      string `json:"side"`
	OrderType string `json:"orderType"`
	LoanType  string `json:"loanType"`
	Force     string `json:"force,omitempty"`
	Price     string `json:"price,omitempty"`
	BaseSize  string `json:"baseSize,omitempty"`
	QuoteSize string `json:"quoteSize,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
	STPMode   string `json:"stpMode,omitempty"`
}

/*
CreateBatchOrders submits a batch of margin orders.

CONSTRAINTS:
  - 1 <= len(reqs) <= bgcommon.MaxBatchSize (50).
  - All rows MUST share the same Symbol — Bitget V2 batch-place-order is
    per-symbol (the symbol is at the top level, not per-row). The SDK
    validates this client-side.

The returned slice has the same length as reqs and is ordered to match:
every input row maps 1-to-1 to an output entry. Successful rows have
Order set (OrderID + ClientOrderID populated); failed rows have Err set
with a typed *bitget.Error. ClientOrderID is populated on every row.
*/
func (t *TradingClient) CreateBatchOrders(ctx context.Context, reqs []margintypes.CreateOrderRequest) ([]margintypes.BatchOrderResult, error) {
	var err error
	if err = bgcommon.ValidateBatchSize("margin.Trading.CreateBatchOrders", len(reqs)); err != nil {
		return nil, err
	}

	var symbol string = reqs[0].Symbol
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateCreateOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading.CreateBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		if reqs[i].Symbol != symbol {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading.CreateBatchOrders: all rows must share the same symbol (row 0="+symbol+", row "+strconv.Itoa(i)+"="+reqs[i].Symbol+")", nil)
		}
	}

	var body batchPlaceOrderBody = batchPlaceOrderBody{
		Symbol:    symbol,
		OrderList: make([]batchPlaceOrderEntry, len(reqs)),
	}
	for i = 0; i < len(reqs); i++ {
		body.OrderList[i] = buildBatchPlaceEntry(reqs[i])
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   t.pathFor("batch-place-order"),
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{symbol},
			OrderCount: len(reqs),
			Category:   string(bitget.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return nil, err
	}

	var data bgcommon.BatchEnvelope
	if err = resp.UnmarshalData(&data); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "margin.Trading.CreateBatchOrders: parse", err)
	}

	var clientOids []string = make([]string, len(reqs))
	for i = 0; i < len(reqs); i++ {
		clientOids[i] = reqs[i].ClientOrderID
	}
	return collateCreateResults(reqs, clientOids, data, symbol), nil
}

// ---------------------------------------------------------------------
// Batch: CancelBatchOrders.
// ---------------------------------------------------------------------

// batchCancelOrderBody is the wire payload for batch-cancel-order. Like
// batch-place, it pins `symbol` at the top level and the SDK enforces
// homogeneity.
type batchCancelOrderBody struct {
	Symbol      string                  `json:"symbol"`
	OrderIDList []batchCancelOrderEntry `json:"orderList"`
}

type batchCancelOrderEntry struct {
	OrderID   string `json:"orderId,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

/*
CancelBatchOrders cancels a batch of margin orders. Same shape contract
as CreateBatchOrders (per-symbol, 1..50 rows). Each row needs exactly
one of (OrderID, ClientOrderID).
*/
func (t *TradingClient) CancelBatchOrders(ctx context.Context, reqs []roottypes.CancelOrderRequest) ([]margintypes.BatchOrderResult, error) {
	var err error
	if err = bgcommon.ValidateBatchSize("margin.Trading.CancelBatchOrders", len(reqs)); err != nil {
		return nil, err
	}

	var symbol string = reqs[0].Symbol
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateCancelOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading.CancelBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		if reqs[i].Symbol != symbol {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading.CancelBatchOrders: all rows must share the same symbol (row 0="+symbol+", row "+strconv.Itoa(i)+"="+reqs[i].Symbol+")", nil)
		}
	}

	var body batchCancelOrderBody = batchCancelOrderBody{
		Symbol:      symbol,
		OrderIDList: make([]batchCancelOrderEntry, len(reqs)),
	}
	for i = 0; i < len(reqs); i++ {
		body.OrderIDList[i] = batchCancelOrderEntry{
			OrderID:   reqs[i].OrderID,
			ClientOid: reqs[i].ClientOrderID,
		}
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   t.pathFor("batch-cancel-order"),
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{symbol},
			OrderCount: len(reqs),
			Category:   string(bitget.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return nil, err
	}

	var data bgcommon.BatchEnvelope
	if err = resp.UnmarshalData(&data); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "margin.Trading.CancelBatchOrders: parse", err)
	}

	var clientOids []string = make([]string, len(reqs))
	for i = 0; i < len(reqs); i++ {
		clientOids[i] = reqs[i].ClientOrderID
	}
	return collateCancelResults(clientOids, data, symbol), nil
}

// ---------------------------------------------------------------------
// Helpers — request building.
// ---------------------------------------------------------------------

// resolveLoanType returns the wire loanType, defaulting an empty value
// to "normal" (Bitget marks the field required).
func resolveLoanType(lt margintypes.LoanType) margintypes.LoanType {
	if lt == "" {
		return margintypes.LoanTypeNormal
	}
	return lt
}

// orderQuantity returns the size that was populated for the request
// (BaseSize for limit / market-sell, QuoteSize for market-buy). Used to
// keep the returned OrderInfo self-describing without an extra GET.
func orderQuantity(req margintypes.CreateOrderRequest) decimal.Decimal {
	if req.BaseSize.Sign() > 0 {
		return req.BaseSize
	}
	return req.QuoteSize
}

// isMarketBuy reports whether the request is a market buy — the one
// shape that uses QuoteSize instead of BaseSize.
func isMarketBuy(req margintypes.CreateOrderRequest) bool {
	return req.OrderType == roottypes.OrderTypeMarket && req.Side == roottypes.SideTypeBuy
}

// buildPlaceBody assembles the wire body for a single place-order call.
func buildPlaceBody(req margintypes.CreateOrderRequest) placeOrderBody {
	var body placeOrderBody = placeOrderBody{
		Symbol:    req.Symbol,
		Side:      string(req.Side),
		OrderType: string(req.OrderType),
		LoanType:  string(resolveLoanType(req.LoanType)),
		ClientOid: req.ClientOrderID,
		STPMode:   string(req.STPMode),
	}
	// Force is only meaningful on limit orders (Bitget ignores it for
	// market). Mirror spot: emit it only for limit.
	if req.OrderType == roottypes.OrderTypeLimit {
		body.Force = string(req.TimeInForce)
		body.Price = req.Price.String()
	}
	if isMarketBuy(req) {
		body.QuoteSize = req.QuoteSize.String()
	} else {
		body.BaseSize = req.BaseSize.String()
	}
	return body
}

// buildBatchPlaceEntry mirrors buildPlaceBody but produces a per-row
// entry for batch-place-order (no symbol — that lives at the top level).
func buildBatchPlaceEntry(req margintypes.CreateOrderRequest) batchPlaceOrderEntry {
	var entry batchPlaceOrderEntry = batchPlaceOrderEntry{
		Side:      string(req.Side),
		OrderType: string(req.OrderType),
		LoanType:  string(resolveLoanType(req.LoanType)),
		ClientOid: req.ClientOrderID,
		STPMode:   string(req.STPMode),
	}
	if req.OrderType == roottypes.OrderTypeLimit {
		entry.Force = string(req.TimeInForce)
		entry.Price = req.Price.String()
	}
	if isMarketBuy(req) {
		entry.QuoteSize = req.QuoteSize.String()
	} else {
		entry.BaseSize = req.BaseSize.String()
	}
	return entry
}

// ---------------------------------------------------------------------
// Helpers — validation.
// ---------------------------------------------------------------------

func validateCreateOrderRequest(req margintypes.CreateOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: symbol is empty", nil)
	}
	if req.Side == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: side is empty", nil)
	}
	if req.OrderType == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: orderType is empty", nil)
	}
	if req.OrderType == roottypes.OrderTypeLimit && !decimalIsPositive(req.Price) {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: price must be > 0 for limit orders", nil)
	}
	// Size denomination is side-dependent: market buys spend QuoteSize,
	// everything else sells / posts BaseSize.
	if isMarketBuy(req) {
		if !decimalIsPositive(req.QuoteSize) {
			return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: quoteSize must be > 0 for market buy orders", nil)
		}
	} else {
		if !decimalIsPositive(req.BaseSize) {
			return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: baseSize must be > 0 for limit / market-sell orders", nil)
		}
	}
	return nil
}

func validateCancelOrderRequest(req roottypes.CancelOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: symbol is empty", nil)
	}
	if req.OrderID == "" && req.ClientOrderID == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Trading: either orderId or clientOrderId is required", nil)
	}
	return nil
}

// decimalIsPositive returns true iff d > 0.
func decimalIsPositive(d decimal.Decimal) bool {
	return d.Sign() > 0
}

// ---------------------------------------------------------------------
// Helpers — response collation.
// ---------------------------------------------------------------------

/*
collateCreateResults maps Bitget's parallel successList / failureList
into a per-row BatchOrderResult slice ordered to match the original
request slice.

PAIRING STRATEGY (same as the spot / mix counterparts): Bitget echoes
clientOid on every row of both lists. We index the original request
slice by clientOid and use that as the join key; rows without a
clientOid fall back to a positional join. Extra rows (should never
happen) are appended at the end rather than dropped silently.
*/
func collateCreateResults(
	reqs []margintypes.CreateOrderRequest,
	clientOids []string,
	data bgcommon.BatchEnvelope,
	symbol string,
) []margintypes.BatchOrderResult {
	var results []margintypes.BatchOrderResult = make([]margintypes.BatchOrderResult, len(reqs))
	var byClientOid map[string]*margintypes.BatchOrderResult = map[string]*margintypes.BatchOrderResult{}
	var positional []*margintypes.BatchOrderResult
	var i int
	for i = 0; i < len(reqs); i++ {
		results[i] = margintypes.BatchOrderResult{ClientOrderID: clientOids[i]}
		if clientOids[i] != "" {
			byClientOid[clientOids[i]] = &results[i]
		} else {
			positional = append(positional, &results[i])
		}
	}

	var ok bgcommon.BatchSuccessRow
	var idx int = 0
	for _, ok = range data.SuccessList {
		var target *margintypes.BatchOrderResult
		if ok.ClientOid != "" {
			target = byClientOid[ok.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		if target == nil {
			results = append(results, margintypes.BatchOrderResult{
				ClientOrderID: ok.ClientOid,
				Order: &margintypes.OrderInfo{
					OrderID:       ok.OrderID,
					ClientOrderID: ok.ClientOid,
					Symbol:        symbol,
					Status:        roottypes.OrderStatusLive,
				},
			})
			continue
		}
		var info *margintypes.OrderInfo = &margintypes.OrderInfo{
			OrderID:       ok.OrderID,
			ClientOrderID: bgcommon.ChooseClientOid(ok.ClientOid, target.ClientOrderID),
			Symbol:        symbol,
			Status:        roottypes.OrderStatusLive,
		}
		var reqIdx int
		reqIdx, _ = findResultIndex(results, target)
		if reqIdx >= 0 {
			info.Side = reqs[reqIdx].Side
			info.OrderType = reqs[reqIdx].OrderType
			info.TimeInForce = reqs[reqIdx].TimeInForce
			info.LoanType = resolveLoanType(reqs[reqIdx].LoanType)
			info.Quantity = orderQuantity(reqs[reqIdx])
			info.Price = reqs[reqIdx].Price
		}
		target.Order = info
	}

	var fail bgcommon.BatchFailureRow
	idx = 0
	for _, fail = range data.FailureList {
		var target *margintypes.BatchOrderResult
		if fail.ClientOid != "" {
			target = byClientOid[fail.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		var perRowErr error = bitget.NewError(
			bitget.MapBitgetCode(fail.ErrorCode, fail.ErrorMsg),
			fail.ErrorCode,
			fail.ErrorMsg,
			nil,
		)
		if target == nil {
			results = append(results, margintypes.BatchOrderResult{
				ClientOrderID: fail.ClientOid,
				Err:           perRowErr,
			})
			continue
		}
		target.Err = perRowErr
	}

	return results
}

// collateCancelResults pairs cancel-batch outcomes with the originating
// CancelOrderRequest rows. Cancellation responses do not carry quantity
// / price; the success Order contains only OrderID + ClientOrderID + a
// "cancelled" status placeholder.
func collateCancelResults(
	clientOids []string,
	data bgcommon.BatchEnvelope,
	symbol string,
) []margintypes.BatchOrderResult {
	var results []margintypes.BatchOrderResult = make([]margintypes.BatchOrderResult, len(clientOids))
	var byClientOid map[string]*margintypes.BatchOrderResult = map[string]*margintypes.BatchOrderResult{}
	var positional []*margintypes.BatchOrderResult
	var i int
	for i = 0; i < len(clientOids); i++ {
		results[i] = margintypes.BatchOrderResult{ClientOrderID: clientOids[i]}
		if clientOids[i] != "" {
			byClientOid[clientOids[i]] = &results[i]
		} else {
			positional = append(positional, &results[i])
		}
	}

	var ok bgcommon.BatchSuccessRow
	var idx int = 0
	for _, ok = range data.SuccessList {
		var target *margintypes.BatchOrderResult
		if ok.ClientOid != "" {
			target = byClientOid[ok.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		var info *margintypes.OrderInfo = &margintypes.OrderInfo{
			OrderID:       ok.OrderID,
			ClientOrderID: bgcommon.ChooseClientOid(ok.ClientOid, ""),
			Symbol:        symbol,
			Status:        roottypes.OrderStatusCancelled,
		}
		if target == nil {
			results = append(results, margintypes.BatchOrderResult{
				ClientOrderID: ok.ClientOid,
				Order:         info,
			})
			continue
		}
		info.ClientOrderID = bgcommon.ChooseClientOid(ok.ClientOid, target.ClientOrderID)
		target.Order = info
	}

	var fail bgcommon.BatchFailureRow
	idx = 0
	for _, fail = range data.FailureList {
		var target *margintypes.BatchOrderResult
		if fail.ClientOid != "" {
			target = byClientOid[fail.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		var perRowErr error = bitget.NewError(
			bitget.MapBitgetCode(fail.ErrorCode, fail.ErrorMsg),
			fail.ErrorCode,
			fail.ErrorMsg,
			nil,
		)
		if target == nil {
			results = append(results, margintypes.BatchOrderResult{
				ClientOrderID: fail.ClientOid,
				Err:           perRowErr,
			})
			continue
		}
		target.Err = perRowErr
	}

	return results
}

// findResultIndex maps a *BatchOrderResult to its index inside the
// results slice. Returns (index, true) on success, (-1, false) if the
// pointer is foreign to the slice (defensive).
func findResultIndex(results []margintypes.BatchOrderResult, target *margintypes.BatchOrderResult) (int, bool) {
	var i int
	for i = 0; i < len(results); i++ {
		if &results[i] == target {
			return i, true
		}
	}
	return -1, false
}
