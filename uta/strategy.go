/*
FILE: uta/strategy.go

DESCRIPTION:
Strategy sub-client — the V3 UTA plan (TP/SL + trigger) orders:
place / modify / cancel and the open / history queries. All calls are
SIGNED and futures-only. Request params verified against the Bitget V3
docs and the tiagosiebler reference client.
*/

package uta

import (
	"context"
	"net/url"
	"strconv"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// StrategyClient — plan-order sub-client.
type StrategyClient struct {
	c *Client
}

func newStrategyClient(c *Client) *StrategyClient {
	return &StrategyClient{c: c}
}

// ---------------------------------------------------------------------
// Request structs.
// ---------------------------------------------------------------------

// PlaceStrategyOrderRequest — parameters for PlaceOrder. Category and
// Symbol are required; the TP/SL vs trigger fields depend on Type.
type PlaceStrategyOrderRequest struct {
	Category          utatypes.Category `json:"category"`
	Symbol            string            `json:"symbol"`
	ClientOID         string            `json:"clientOid,omitempty"`
	Type              string            `json:"type,omitempty"`
	TpslMode          string            `json:"tpslMode,omitempty"`
	Qty               string            `json:"qty,omitempty"`
	Side              string            `json:"side,omitempty"`
	PosSide           string            `json:"posSide,omitempty"`
	ReduceOnly        string            `json:"reduceOnly,omitempty"`
	TpTriggerBy       string            `json:"tpTriggerBy,omitempty"`
	SlTriggerBy       string            `json:"slTriggerBy,omitempty"`
	TakeProfit        string            `json:"takeProfit,omitempty"`
	StopLoss          string            `json:"stopLoss,omitempty"`
	TpOrderType       string            `json:"tpOrderType,omitempty"`
	SlOrderType       string            `json:"slOrderType,omitempty"`
	TpLimitPrice      string            `json:"tpLimitPrice,omitempty"`
	SlLimitPrice      string            `json:"slLimitPrice,omitempty"`
	TriggerBy         string            `json:"triggerBy,omitempty"`
	TriggerPrice      string            `json:"triggerPrice,omitempty"`
	TriggerOrderType  string            `json:"triggerOrderType,omitempty"`
	TriggerOrderPrice string            `json:"triggerOrderPrice,omitempty"`
}

// ModifyStrategyOrderRequest — parameters for ModifyOrder. One of OrderID
// / ClientOID and Qty are required.
type ModifyStrategyOrderRequest struct {
	OrderID           string `json:"orderId,omitempty"`
	ClientOID         string `json:"clientOid,omitempty"`
	Qty               string `json:"qty"`
	TpTriggerBy       string `json:"tpTriggerBy,omitempty"`
	SlTriggerBy       string `json:"slTriggerBy,omitempty"`
	TakeProfit        string `json:"takeProfit,omitempty"`
	StopLoss          string `json:"stopLoss,omitempty"`
	TpOrderType       string `json:"tpOrderType,omitempty"`
	SlOrderType       string `json:"slOrderType,omitempty"`
	TpLimitPrice      string `json:"tpLimitPrice,omitempty"`
	SlLimitPrice      string `json:"slLimitPrice,omitempty"`
	TriggerBy         string `json:"triggerBy,omitempty"`
	TriggerPrice      string `json:"triggerPrice,omitempty"`
	TriggerOrderType  string `json:"triggerOrderType,omitempty"`
	TriggerOrderPrice string `json:"triggerOrderPrice,omitempty"`
}

// CancelStrategyOrderRequest — one of OrderID / ClientOID is required.
type CancelStrategyOrderRequest struct {
	OrderID   string `json:"orderId,omitempty"`
	ClientOID string `json:"clientOid,omitempty"`
}

// ---------------------------------------------------------------------
// Place / Modify / Cancel.
// ---------------------------------------------------------------------

// PlaceOrder submits a plan (TP/SL or trigger) order. Category and Symbol
// are required.
func (s *StrategyClient) PlaceOrder(ctx context.Context, req PlaceStrategyOrderRequest) (utatypes.OrderAck, error) {
	var out utatypes.OrderAck
	switch {
	case req.Category == "":
		return out, errInvalid("Strategy.PlaceOrder", "category is required")
	case req.Symbol == "":
		return out, errInvalid("Strategy.PlaceOrder", "symbol is required")
	}
	var row ackRow
	if err := s.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/place-strategy-order", Body: req,
		Meta: flowMeta(bitget.RateLimitCategoryPlace, 1, req.Symbol),
	}, "Strategy.PlaceOrder", &row); err != nil {
		return out, err
	}
	return utatypes.OrderAck{OrderID: row.OrderID, ClientOID: row.ClientOID}, nil
}

// ModifyOrder amends a resting plan order. One of OrderID / ClientOID and
// Qty are required.
func (s *StrategyClient) ModifyOrder(ctx context.Context, req ModifyStrategyOrderRequest) (utatypes.OrderAck, error) {
	var out utatypes.OrderAck
	switch {
	case req.OrderID == "" && req.ClientOID == "":
		return out, errInvalid("Strategy.ModifyOrder", "orderId or clientOid is required")
	case req.Qty == "":
		return out, errInvalid("Strategy.ModifyOrder", "qty is required")
	}
	var row ackRow
	if err := s.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/modify-strategy-order", Body: req,
		Meta: flowMeta(bitget.RateLimitCategoryAmend, 1),
	}, "Strategy.ModifyOrder", &row); err != nil {
		return out, err
	}
	return utatypes.OrderAck{OrderID: row.OrderID, ClientOID: row.ClientOID}, nil
}

// CancelOrder cancels a resting plan order. One of OrderID / ClientOID is
// required.
func (s *StrategyClient) CancelOrder(ctx context.Context, req CancelStrategyOrderRequest) (utatypes.OrderAck, error) {
	var out utatypes.OrderAck
	if req.OrderID == "" && req.ClientOID == "" {
		return out, errInvalid("Strategy.CancelOrder", "orderId or clientOid is required")
	}
	var row ackRow
	if err := s.c.callSigned(ctx, rest.Options{
		Method: "POST", Path: "/api/v3/trade/cancel-strategy-order", Body: req,
		Meta: flowMeta(bitget.RateLimitCategoryCancel, 1),
	}, "Strategy.CancelOrder", &row); err != nil {
		return out, err
	}
	return utatypes.OrderAck{OrderID: row.OrderID, ClientOID: row.ClientOID}, nil
}

// ---------------------------------------------------------------------
// Queries.
// ---------------------------------------------------------------------

type strategyOrderRow struct {
	OrderID           string `json:"orderId"`
	ClientOID         string `json:"clientOid"`
	Category          string `json:"category"`
	Symbol            string `json:"symbol"`
	Qty               string `json:"qty"`
	PosSide           string `json:"posSide"`
	Status            string `json:"status"`
	TriggerType       string `json:"triggerType"`
	TpTriggerBy       string `json:"tpTriggerBy"`
	SlTriggerBy       string `json:"slTriggerBy"`
	TakeProfit        string `json:"takeProfit"`
	StopLoss          string `json:"stopLoss"`
	TpOrderType       string `json:"tpOrderType"`
	SlOrderType       string `json:"slOrderType"`
	TpLimitPrice      string `json:"tpLimitPrice"`
	SlLimitPrice      string `json:"slLimitPrice"`
	TriggerBy         string `json:"triggerBy"`
	TriggerPrice      string `json:"triggerPrice"`
	TriggerOrderType  string `json:"triggerOrderType"`
	TriggerOrderPrice string `json:"triggerOrderPrice"`
	CreatedTime       string `json:"createdTime"`
	UpdatedTime       string `json:"updatedTime"`
}

type strategyPage struct {
	List   []strategyOrderRow `json:"list"`
	Cursor string             `json:"cursor"`
}

func parseStrategyOrder(scope string, r strategyOrderRow) (utatypes.StrategyOrder, error) {
	var o utatypes.StrategyOrder = utatypes.StrategyOrder{
		OrderID: r.OrderID, ClientOID: r.ClientOID, Category: r.Category, Symbol: r.Symbol,
		PosSide: r.PosSide, Status: r.Status, TriggerType: r.TriggerType,
		TpTriggerBy: r.TpTriggerBy, SlTriggerBy: r.SlTriggerBy,
		TpOrderType: r.TpOrderType, SlOrderType: r.SlOrderType,
		TriggerBy: r.TriggerBy, TriggerOrderType: r.TriggerOrderType,
		CreatedTime: i64(r.CreatedTime), UpdatedTime: i64(r.UpdatedTime),
	}
	if err := decMany(scope, []decPair{
		{&o.Qty, r.Qty}, {&o.TakeProfit, r.TakeProfit}, {&o.StopLoss, r.StopLoss},
		{&o.TpLimitPrice, r.TpLimitPrice}, {&o.SlLimitPrice, r.SlLimitPrice},
		{&o.TriggerPrice, r.TriggerPrice}, {&o.TriggerOrderPrice, r.TriggerOrderPrice},
	}); err != nil {
		return o, err
	}
	return o, nil
}

func (s *StrategyClient) getStrategyOrders(ctx context.Context, path, scope string, query url.Values) ([]utatypes.StrategyOrder, string, error) {
	var page strategyPage
	if err := s.c.callSigned(ctx, rest.Options{
		Method: "GET", Path: path, Query: query, Meta: queryMeta(),
	}, scope, &page); err != nil {
		return nil, "", err
	}
	var out []utatypes.StrategyOrder = make([]utatypes.StrategyOrder, 0, len(page.List))
	var i int
	for i = 0; i < len(page.List); i++ {
		var o utatypes.StrategyOrder
		var err error
		if o, err = parseStrategyOrder(scope, page.List[i]); err != nil {
			return nil, "", err
		}
		out = append(out, o)
	}
	return out, page.Cursor, nil
}

// GetUnfilledOrders returns the open plan orders for a futures category.
// category is required; orderType ("tpsl" | "trigger") is optional.
func (s *StrategyClient) GetUnfilledOrders(ctx context.Context, category utatypes.Category, orderType string) ([]utatypes.StrategyOrder, error) {
	if category == "" {
		return nil, errInvalid("Strategy.GetUnfilledOrders", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(category))
	if orderType != "" {
		query.Set("type", orderType)
	}
	var out []utatypes.StrategyOrder
	var err error
	out, _, err = s.getStrategyOrders(ctx, "/api/v3/trade/unfilled-strategy-orders", "Strategy.GetUnfilledOrders", query)
	return out, err
}

// StrategyHistoryQuery — filter for GetHistoryOrders. Category is required.
type StrategyHistoryQuery struct {
	Category    utatypes.Category
	Type        string
	StartTimeMs int64
	EndTimeMs   int64
	Cursor      string
	Limit       int
}

// GetHistoryOrders returns one page of historical plan orders plus the
// next cursor. Category is required.
func (s *StrategyClient) GetHistoryOrders(ctx context.Context, q StrategyHistoryQuery) ([]utatypes.StrategyOrder, string, error) {
	if q.Category == "" {
		return nil, "", errInvalid("Strategy.GetHistoryOrders", "category is required")
	}
	var query url.Values = url.Values{}
	query.Set("category", string(q.Category))
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
	return s.getStrategyOrders(ctx, "/api/v3/trade/history-strategy-orders", "Strategy.GetHistoryOrders", query)
}
