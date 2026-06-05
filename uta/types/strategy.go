/*
FILE: uta/types/strategy.go

DESCRIPTION:
Domain types for the V3 UTA STRATEGY (plan) orders — TP/SL and trigger
orders. Field mapping verified against the Bitget V3 docs and the
tiagosiebler reference type StrategyOrderV3. decimal for prices / sizes,
int64 epoch-ms.
*/

package types

import "github.com/shopspring/decimal"

// StrategyOrder — one plan (TP/SL or trigger) order
// (trade/unfilled-strategy-orders | history-strategy-orders).
type StrategyOrder struct {
	OrderID           string
	ClientOID         string
	Category          string
	Symbol            string
	Qty               decimal.Decimal
	PosSide           string
	Status            string
	TriggerType       string
	TpTriggerBy       string
	SlTriggerBy       string
	TakeProfit        decimal.Decimal
	StopLoss          decimal.Decimal
	TpOrderType       string
	SlOrderType       string
	TpLimitPrice      decimal.Decimal
	SlLimitPrice      decimal.Decimal
	TriggerBy         string
	TriggerPrice      decimal.Decimal
	TriggerOrderType  string
	TriggerOrderPrice decimal.Decimal
	CreatedTime       int64
	UpdatedTime       int64
}
