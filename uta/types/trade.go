/*
FILE: uta/types/trade.go

DESCRIPTION:
Domain types for the V3 UTA order-flow surface: order acks, batch
per-item results, the unified Order view (order-info / unfilled / history)
and execution Fills. Field mapping verified against the Bitget V3 docs
and the tiagosiebler reference types (OrderInfoV3 / UnfilledOrderV3 /
HistoryOrderV3 / FillV3). decimal for prices / sizes, int64 epoch-ms.
*/

package types

import "github.com/shopspring/decimal"

// OrderAck — the minimal ack returned by place / modify / cancel single.
type OrderAck struct {
	OrderID   string
	ClientOID string
}

// BatchOrderResult — one per-item result of a batch place / modify /
// cancel (and of cancel-symbol-order / close-positions). Code / Msg are
// empty on success.
type BatchOrderResult struct {
	OrderID   string
	ClientOID string
	Code      string
	Msg       string
}

// FeeDetail — one fee line on an order or fill.
type FeeDetail struct {
	FeeCoin string
	Fee     decimal.Decimal
}

// Order — unified order view (order-info / unfilled-orders / history).
// Fields absent for a given endpoint stay zero.
type Order struct {
	OrderID      string
	ClientOID    string
	Category     string
	Symbol       string
	OrderType    string
	Side         string
	Price        decimal.Decimal
	Qty          decimal.Decimal
	Amount       decimal.Decimal
	CumExecQty   decimal.Decimal
	CumExecValue decimal.Decimal
	AvgPrice     decimal.Decimal
	TimeInForce  string
	OrderStatus  string
	PosSide      string
	HoldMode     string
	DelegateType string
	ReduceOnly   string
	FeeDetail    []FeeDetail
	CancelReason string
	ExecType     string
	StpMode      string
	CreatedTime  int64
	UpdatedTime  int64
}

// Fill — one execution row (trade/fills).
type Fill struct {
	ExecID      string
	OrderID     string
	Category    string
	Symbol      string
	OrderType   string
	Side        string
	ExecPrice   decimal.Decimal
	ExecQty     decimal.Decimal
	ExecValue   decimal.Decimal
	TradeScope  string
	FeeDetail   []FeeDetail
	ExecPnl     decimal.Decimal
	TradeSide   string
	CreatedTime int64
	UpdatedTime int64
}
