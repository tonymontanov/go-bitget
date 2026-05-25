/*
FILE: types/trade-update.go

DESCRIPTION:
TradeUpdate is one element of the Bitget "trade.{symbol}" WebSocket
channel — protocol-common across every profile. Bitget ships trade
frames in batches; the dispatcher fans them out so handlers receive one
TradeUpdate per call.

FIELDS:
  - Symbol  : Bitget symbol (e.g. "BTCUSDT").
  - Price   : trade price.
  - Size    : trade size in base asset.
  - Side    : taker side (Buy = aggressor bought, Sell = aggressor sold).
  - TradeID : Bitget trade id (the "tradeId" field on the wire).
  - TsMs    : trade match timestamp (ms).
*/

package types

import "github.com/shopspring/decimal"

// TradeUpdate — one trade event from the public trade channel.
type TradeUpdate struct {
	Symbol  string
	Price   decimal.Decimal
	Size    decimal.Decimal
	Side    SideType
	TradeID string
	TsMs    int64
}
