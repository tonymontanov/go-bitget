/*
FILE: uta/trade.go

DESCRIPTION:
Trade sub-client — the V3 UTA order flow: place / modify / cancel (single
+ batch), cancel-symbol, close-positions, countdown-cancel-all, and the
order / fill queries. All calls are SIGNED.

Batch endpoints take a TOP-LEVEL JSON ARRAY body (one object per order);
their responses are decoded leniently from either a bare array or a
{list|successList|failureList} envelope. Request params verified against
the Bitget V3 docs and the tiagosiebler reference client.
*/

package uta

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// TradeClient — order-flow sub-client.
type TradeClient struct {
	c *Client
}

func newTradeClient(c *Client) *TradeClient {
	return &TradeClient{c: c}
}

// ---------------------------------------------------------------------
// Request structs.
// ---------------------------------------------------------------------

// PlaceOrderRequest — parameters for PlaceOrder (and for each item of
// PlaceBatchOrders). Category, Symbol, Qty, Side and OrderType are
// required. PosSide is required in hedge mode.
type PlaceOrderRequest struct {
	Category              utatypes.Category `json:"category"`
	Symbol                string            `json:"symbol"`
	Qty                   string            `json:"qty"`
	Price                 string            `json:"price,omitempty"`
	Side                  string            `json:"side"`
	OrderType             string            `json:"orderType"`
	TimeInForce           string            `json:"timeInForce,omitempty"`
	PosSide               string            `json:"posSide,omitempty"`
	ClientOID             string            `json:"clientOid,omitempty"`
	ReduceOnly            string            `json:"reduceOnly,omitempty"`
	MarginMode            string            `json:"marginMode,omitempty"`
	StpMode               string            `json:"stpMode,omitempty"`
	TakeProfitPrice       string            `json:"takeProfitPrice,omitempty"`
	StopLossPrice         string            `json:"stopLossPrice,omitempty"`
	TakeProfitTriggerType string            `json:"takeProfitTriggerType,omitempty"`
	StopLossTriggerType   string            `json:"stopLossTriggerType,omitempty"`
}

// ModifyOrderRequest — parameters for ModifyOrder (and batch-modify). One
// of OrderID / ClientOID is required.
type ModifyOrderRequest struct {
	OrderID    string `json:"orderId,omitempty"`
	ClientOID  string `json:"clientOid,omitempty"`
	Qty        string `json:"qty,omitempty"`
	Price      string `json:"price,omitempty"`
	AutoCancel string `json:"autoCancel,omitempty"`
}

// CancelOrderRequest — one of OrderID / ClientOID is required.
type CancelOrderRequest struct {
	OrderID   string `json:"orderId,omitempty"`
	ClientOID string `json:"clientOid,omitempty"`
}

// CancelBatchOrder — one item of CancelBatchOrders. Category and Symbol
// are required; one of OrderID / ClientOID identifies the order.
type CancelBatchOrder struct {
	OrderID   string            `json:"orderId,omitempty"`
	ClientOID string            `json:"clientOid,omitempty"`
	Category  utatypes.Category `json:"category"`
	Symbol    string            `json:"symbol"`
}

// ClosePositionsRequest — parameters for CloseAllPositions. Category is
// required (futures only); Symbol / PosSide narrow the scope.
type ClosePositionsRequest struct {
	Category utatypes.Category `json:"category"`
	Symbol   string            `json:"symbol,omitempty"`
	PosSide  string            `json:"posSide,omitempty"`
}

// ---------------------------------------------------------------------
// Internal row / decode helpers.
// ---------------------------------------------------------------------

type ackRow struct {
	OrderID   string `json:"orderId"`
	ClientOID string `json:"clientOid"`
}

type batchItemRow struct {
	OrderID   string `json:"orderId"`
	ClientOID string `json:"clientOid"`
	Code      string `json:"code"`
	Msg       string `json:"msg"`
}

func batchResults(items []batchItemRow) []utatypes.BatchOrderResult {
	var out []utatypes.BatchOrderResult = make([]utatypes.BatchOrderResult, 0, len(items))
	var i int
	for i = 0; i < len(items); i++ {
		out = append(out, utatypes.BatchOrderResult{
			OrderID: items[i].OrderID, ClientOID: items[i].ClientOID, Code: items[i].Code, Msg: items[i].Msg,
		})
	}
	return out
}

// doBatch posts a top-level array body and decodes the response leniently
// (bare array or {list|successList|failureList}).
func (t *TradeClient) doBatch(ctx context.Context, path, scope string, body any, symbols []string) ([]utatypes.BatchOrderResult, error) {
	var raw json.RawMessage
	var err error
	if err = t.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: path, Body: body, Meta: flowMeta(bitget.RateLimitCategoryCancel, len(symbols), symbols...),
	}, scope, &raw); err != nil {
		return nil, err
	}
	return decodeBatch(scope, raw)
}

func decodeBatch(scope string, raw json.RawMessage) ([]utatypes.BatchOrderResult, error) {
	var trimmed = bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []batchItemRow
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, errParse(scope, err)
		}
		return batchResults(items), nil
	}
	var env struct {
		List        []batchItemRow `json:"list"`
		SuccessList []batchItemRow `json:"successList"`
		FailureList []batchItemRow `json:"failureList"`
	}
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, errParse(scope, err)
	}
	if len(env.List) > 0 {
		return batchResults(env.List), nil
	}
	var merged []batchItemRow = append(env.SuccessList, env.FailureList...)
	return batchResults(merged), nil
}

// ---------------------------------------------------------------------
// PlaceOrder / ModifyOrder / CancelOrder (single).
// ---------------------------------------------------------------------

func (req PlaceOrderRequest) validate(scope string) error {
	switch {
	case req.Category == "":
		return errInvalid(scope, "category is required")
	case req.Symbol == "":
		return errInvalid(scope, "symbol is required")
	case req.Qty == "":
		return errInvalid(scope, "qty is required")
	case req.Side == "":
		return errInvalid(scope, "side is required")
	case req.OrderType == "":
		return errInvalid(scope, "orderType is required")
	}
	return nil
}

// PlaceOrder submits a single order. Category / Symbol / Qty / Side /
// OrderType are required.
func (t *TradeClient) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (utatypes.OrderAck, error) {
	var out utatypes.OrderAck
	if err := req.validate("Trade.PlaceOrder"); err != nil {
		return out, err
	}
	var row ackRow
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/place-order", Body: req,
		Meta: flowMeta(bitget.RateLimitCategoryPlace, 1, req.Symbol),
	}, "Trade.PlaceOrder", &row); err != nil {
		return out, err
	}
	return utatypes.OrderAck{OrderID: row.OrderID, ClientOID: row.ClientOID}, nil
}

// ModifyOrder amends a resting order. One of OrderID / ClientOID is
// required.
func (t *TradeClient) ModifyOrder(ctx context.Context, req ModifyOrderRequest) (utatypes.OrderAck, error) {
	var out utatypes.OrderAck
	if req.OrderID == "" && req.ClientOID == "" {
		return out, errInvalid("Trade.ModifyOrder", "orderId or clientOid is required")
	}
	var row ackRow
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/modify-order", Body: req,
		Meta: flowMeta(bitget.RateLimitCategoryAmend, 1),
	}, "Trade.ModifyOrder", &row); err != nil {
		return out, err
	}
	return utatypes.OrderAck{OrderID: row.OrderID, ClientOID: row.ClientOID}, nil
}

// CancelOrder cancels a resting order. One of OrderID / ClientOID is
// required.
func (t *TradeClient) CancelOrder(ctx context.Context, req CancelOrderRequest) (utatypes.OrderAck, error) {
	var out utatypes.OrderAck
	if req.OrderID == "" && req.ClientOID == "" {
		return out, errInvalid("Trade.CancelOrder", "orderId or clientOid is required")
	}
	var row ackRow
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/cancel-order", Body: req,
		Meta: flowMeta(bitget.RateLimitCategoryCancel, 1),
	}, "Trade.CancelOrder", &row); err != nil {
		return out, err
	}
	return utatypes.OrderAck{OrderID: row.OrderID, ClientOID: row.ClientOID}, nil
}

// ---------------------------------------------------------------------
// Batch place / modify / cancel.
// ---------------------------------------------------------------------

// PlaceBatchOrders submits up to the venue's per-call limit of orders.
// The slice must be non-empty and every item must validate.
func (t *TradeClient) PlaceBatchOrders(ctx context.Context, orders []PlaceOrderRequest) ([]utatypes.BatchOrderResult, error) {
	if len(orders) == 0 {
		return nil, errInvalid("Trade.PlaceBatchOrders", "orders must be non-empty")
	}
	var symbols []string = make([]string, 0, len(orders))
	var i int
	for i = 0; i < len(orders); i++ {
		if err := orders[i].validate("Trade.PlaceBatchOrders"); err != nil {
			return nil, err
		}
		symbols = append(symbols, orders[i].Symbol)
	}
	var raw json.RawMessage
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/place-batch", Body: orders,
		Meta: flowMeta(bitget.RateLimitCategoryPlace, len(orders), symbols...),
	}, "Trade.PlaceBatchOrders", &raw); err != nil {
		return nil, err
	}
	return decodeBatch("Trade.PlaceBatchOrders", raw)
}

// BatchModifyOrders amends multiple resting orders. The slice must be
// non-empty and every item must carry an orderId or clientOid.
func (t *TradeClient) BatchModifyOrders(ctx context.Context, mods []ModifyOrderRequest) ([]utatypes.BatchOrderResult, error) {
	if len(mods) == 0 {
		return nil, errInvalid("Trade.BatchModifyOrders", "mods must be non-empty")
	}
	var i int
	for i = 0; i < len(mods); i++ {
		if mods[i].OrderID == "" && mods[i].ClientOID == "" {
			return nil, errInvalid("Trade.BatchModifyOrders", "each item needs orderId or clientOid")
		}
	}
	var raw json.RawMessage
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/batch-modify-order", Body: mods,
		Meta: flowMeta(bitget.RateLimitCategoryAmend, len(mods)),
	}, "Trade.BatchModifyOrders", &raw); err != nil {
		return nil, err
	}
	return decodeBatch("Trade.BatchModifyOrders", raw)
}

// CancelBatchOrders cancels multiple orders. The slice must be non-empty;
// each item needs Category + Symbol and an orderId or clientOid.
func (t *TradeClient) CancelBatchOrders(ctx context.Context, cancels []CancelBatchOrder) ([]utatypes.BatchOrderResult, error) {
	if len(cancels) == 0 {
		return nil, errInvalid("Trade.CancelBatchOrders", "cancels must be non-empty")
	}
	var symbols []string = make([]string, 0, len(cancels))
	var i int
	for i = 0; i < len(cancels); i++ {
		switch {
		case cancels[i].Category == "":
			return nil, errInvalid("Trade.CancelBatchOrders", "each item needs category")
		case cancels[i].Symbol == "":
			return nil, errInvalid("Trade.CancelBatchOrders", "each item needs symbol")
		case cancels[i].OrderID == "" && cancels[i].ClientOID == "":
			return nil, errInvalid("Trade.CancelBatchOrders", "each item needs orderId or clientOid")
		}
		symbols = append(symbols, cancels[i].Symbol)
	}
	return t.doBatch(ctx, "/api/v3/trade/cancel-batch", "Trade.CancelBatchOrders", cancels, symbols)
}

// ---------------------------------------------------------------------
// CancelSymbolOrders / CloseAllPositions / CountdownCancelAll.
// ---------------------------------------------------------------------

type cancelSymbolBody struct {
	Category utatypes.Category `json:"category"`
	Symbol   string            `json:"symbol,omitempty"`
}

// CancelSymbolOrders cancels every open order in a category (optionally
// narrowed to one symbol). Category is required.
func (t *TradeClient) CancelSymbolOrders(ctx context.Context, category utatypes.Category, symbol string) ([]utatypes.BatchOrderResult, error) {
	if category == "" {
		return nil, errInvalid("Trade.CancelSymbolOrders", "category is required")
	}
	var symbols []string
	if symbol != "" {
		symbols = []string{symbol}
	}
	return t.doBatch(ctx, "/api/v3/trade/cancel-symbol-order", "Trade.CancelSymbolOrders",
		cancelSymbolBody{Category: category, Symbol: symbol}, symbols)
}

// CloseAllPositions market-closes positions in a futures category
// (optionally narrowed to one symbol / posSide). Category is required.
func (t *TradeClient) CloseAllPositions(ctx context.Context, req ClosePositionsRequest) ([]utatypes.BatchOrderResult, error) {
	if req.Category == "" {
		return nil, errInvalid("Trade.CloseAllPositions", "category is required")
	}
	var symbols []string
	if req.Symbol != "" {
		symbols = []string{req.Symbol}
	}
	return t.doBatch(ctx, "/api/v3/trade/close-positions", "Trade.CloseAllPositions", req, symbols)
}

type countdownBody struct {
	Countdown string `json:"countdown"`
}

// CountdownCancelAll arms a dead-man's switch: all open orders are
// cancelled after `seconds` (5-60) unless refreshed; 0 disables it.
func (t *TradeClient) CountdownCancelAll(ctx context.Context, seconds int) error {
	if seconds < 0 {
		return errInvalid("Trade.CountdownCancelAll", "seconds must be >= 0")
	}
	return t.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/countdown-cancel-all",
		Body: countdownBody{Countdown: strconv.Itoa(seconds)},
		Meta: flowMeta(bitget.RateLimitCategoryCancel, 0),
	}, "Trade.CountdownCancelAll", nil)
}

// ---------------------------------------------------------------------
// Order queries: order-info / unfilled / history / fills.
// ---------------------------------------------------------------------

type orderRow struct {
	OrderID      string   `json:"orderId"`
	ClientOID    string   `json:"clientOid"`
	Category     string   `json:"category"`
	Symbol       string   `json:"symbol"`
	OrderType    string   `json:"orderType"`
	Side         string   `json:"side"`
	Price        string   `json:"price"`
	Qty          string   `json:"qty"`
	Amount       string   `json:"amount"`
	CumExecQty   string   `json:"cumExecQty"`
	CumExecValue string   `json:"cumExecValue"`
	AvgPrice     string   `json:"avgPrice"`
	TimeInForce  string   `json:"timeInForce"`
	OrderStatus  string   `json:"orderStatus"`
	PosSide      string   `json:"posSide"`
	HoldMode     string   `json:"holdMode"`
	DelegateType string   `json:"delegateType"`
	ReduceOnly   string   `json:"reduceOnly"`
	FeeDetail    []feeRow `json:"feeDetail"`
	CancelReason string   `json:"cancelReason"`
	ExecType     string   `json:"execType"`
	StpMode      string   `json:"stpMode"`
	CreatedTime  string   `json:"createdTime"`
	UpdatedTime  string   `json:"updatedTime"`
}

type feeRow struct {
	FeeCoin string `json:"feeCoin"`
	Fee     string `json:"fee"`
}

func parseFeeDetail(scope string, rows []feeRow) ([]utatypes.FeeDetail, error) {
	var out []utatypes.FeeDetail = make([]utatypes.FeeDetail, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var fd utatypes.FeeDetail = utatypes.FeeDetail{FeeCoin: rows[i].FeeCoin}
		if err := decMany(scope, []decPair{{&fd.Fee, rows[i].Fee}}); err != nil {
			return nil, err
		}
		out = append(out, fd)
	}
	return out, nil
}

func parseOrder(scope string, r orderRow) (utatypes.Order, error) {
	var o utatypes.Order = utatypes.Order{
		OrderID: r.OrderID, ClientOID: r.ClientOID, Category: r.Category, Symbol: r.Symbol,
		OrderType: r.OrderType, Side: r.Side, TimeInForce: r.TimeInForce, OrderStatus: r.OrderStatus,
		PosSide: r.PosSide, HoldMode: r.HoldMode, DelegateType: r.DelegateType, ReduceOnly: r.ReduceOnly,
		CancelReason: r.CancelReason, ExecType: r.ExecType, StpMode: r.StpMode,
		CreatedTime: i64(r.CreatedTime), UpdatedTime: i64(r.UpdatedTime),
	}
	if err := decMany(scope, []decPair{
		{&o.Price, r.Price}, {&o.Qty, r.Qty}, {&o.Amount, r.Amount},
		{&o.CumExecQty, r.CumExecQty}, {&o.CumExecValue, r.CumExecValue}, {&o.AvgPrice, r.AvgPrice},
	}); err != nil {
		return o, err
	}
	var err error
	if o.FeeDetail, err = parseFeeDetail(scope, r.FeeDetail); err != nil {
		return o, err
	}
	return o, nil
}

// GetOrderInfo returns the full detail of one order. One of orderId /
// clientOid is required.
func (t *TradeClient) GetOrderInfo(ctx context.Context, orderID, clientOID string) (utatypes.Order, error) {
	var out utatypes.Order
	if orderID == "" && clientOID == "" {
		return out, errInvalid("Trade.GetOrderInfo", "orderId or clientOid is required")
	}
	var query url.Values = url.Values{}
	if orderID != "" {
		query.Set("orderId", orderID)
	}
	if clientOID != "" {
		query.Set("clientOid", clientOID)
	}
	var row orderRow
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/trade/order-info", Query: query, Meta: queryMeta(),
	}, "Trade.GetOrderInfo", &row); err != nil {
		return out, err
	}
	return parseOrder("Trade.GetOrderInfo", row)
}

// OrdersQuery — shared filter for unfilled / history order queries.
type OrdersQuery struct {
	Category    utatypes.Category
	Symbol      string
	StartTimeMs int64
	EndTimeMs   int64
	Cursor      string
	Limit       int
}

type ordersPage struct {
	List   []orderRow `json:"list"`
	Cursor string     `json:"cursor"`
}

func (q OrdersQuery) values(requireCategory bool, scope string) (url.Values, error) {
	if requireCategory && q.Category == "" {
		return nil, errInvalid(scope, "category is required")
	}
	var query url.Values = url.Values{}
	if q.Category != "" {
		query.Set("category", string(q.Category))
	}
	if q.Symbol != "" {
		query.Set("symbol", q.Symbol)
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
	return query, nil
}

func (t *TradeClient) getOrders(ctx context.Context, path, scope string, query url.Values) ([]utatypes.Order, string, error) {
	var page ordersPage
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: path, Query: query, Meta: queryMeta(),
	}, scope, &page); err != nil {
		return nil, "", err
	}
	var out []utatypes.Order = make([]utatypes.Order, 0, len(page.List))
	var i int
	for i = 0; i < len(page.List); i++ {
		var o utatypes.Order
		var err error
		if o, err = parseOrder(scope, page.List[i]); err != nil {
			return nil, "", err
		}
		out = append(out, o)
	}
	return out, page.Cursor, nil
}

// GetUnfilledOrders returns one page of open orders plus the next cursor.
// All filters are optional.
func (t *TradeClient) GetUnfilledOrders(ctx context.Context, q OrdersQuery) ([]utatypes.Order, string, error) {
	var query url.Values
	var err error
	if query, err = q.values(false, "Trade.GetUnfilledOrders"); err != nil {
		return nil, "", err
	}
	return t.getOrders(ctx, "/api/v3/trade/unfilled-orders", "Trade.GetUnfilledOrders", query)
}

// GetHistoryOrders returns one page of historical orders plus the next
// cursor. Category is required.
func (t *TradeClient) GetHistoryOrders(ctx context.Context, q OrdersQuery) ([]utatypes.Order, string, error) {
	var query url.Values
	var err error
	if query, err = q.values(true, "Trade.GetHistoryOrders"); err != nil {
		return nil, "", err
	}
	return t.getOrders(ctx, "/api/v3/trade/history-orders", "Trade.GetHistoryOrders", query)
}

// ---------------------------------------------------------------------
// GetFills — trade/fills.
// ---------------------------------------------------------------------

// FillsQuery — filter for GetFills. All fields are optional.
type FillsQuery struct {
	OrderID     string
	StartTimeMs int64
	EndTimeMs   int64
	Cursor      string
	Limit       int
}

type fillRow struct {
	ExecID      string   `json:"execId"`
	OrderID     string   `json:"orderId"`
	Category    string   `json:"category"`
	Symbol      string   `json:"symbol"`
	OrderType   string   `json:"orderType"`
	Side        string   `json:"side"`
	ExecPrice   string   `json:"execPrice"`
	ExecQty     string   `json:"execQty"`
	ExecValue   string   `json:"execValue"`
	TradeScope  string   `json:"tradeScope"`
	FeeDetail   []feeRow `json:"feeDetail"`
	ExecPnl     string   `json:"execPnl"`
	TradeSide   string   `json:"tradeSide"`
	CreatedTime string   `json:"createdTime"`
	UpdatedTime string   `json:"updatedTime"`
}

type fillsPage struct {
	List   []fillRow `json:"list"`
	Cursor string    `json:"cursor"`
}

// GetFills returns one page of executions plus the next cursor. All
// filters are optional.
func (t *TradeClient) GetFills(ctx context.Context, q FillsQuery) ([]utatypes.Fill, string, error) {
	var query url.Values = url.Values{}
	if q.OrderID != "" {
		query.Set("orderId", q.OrderID)
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
	var page fillsPage
	if err := t.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: "/api/v3/trade/fills", Query: query, Meta: queryMeta(),
	}, "Trade.GetFills", &page); err != nil {
		return nil, "", err
	}
	var out []utatypes.Fill = make([]utatypes.Fill, 0, len(page.List))
	var i int
	for i = 0; i < len(page.List); i++ {
		var r = page.List[i]
		var f utatypes.Fill = utatypes.Fill{
			ExecID: r.ExecID, OrderID: r.OrderID, Category: r.Category, Symbol: r.Symbol,
			OrderType: r.OrderType, Side: r.Side, TradeScope: r.TradeScope, TradeSide: r.TradeSide,
			CreatedTime: i64(r.CreatedTime), UpdatedTime: i64(r.UpdatedTime),
		}
		if err := decMany("Trade.GetFills", []decPair{
			{&f.ExecPrice, r.ExecPrice}, {&f.ExecQty, r.ExecQty}, {&f.ExecValue, r.ExecValue}, {&f.ExecPnl, r.ExecPnl},
		}); err != nil {
			return nil, "", err
		}
		var err error
		if f.FeeDetail, err = parseFeeDetail("Trade.GetFills", r.FeeDetail); err != nil {
			return nil, "", err
		}
		out = append(out, f)
	}
	return out, page.Cursor, nil
}
