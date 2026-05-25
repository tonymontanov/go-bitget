/*
FILE: types/kline-update.go

DESCRIPTION:
KlineUpdate is one element of the Bitget "candle{interval}" WebSocket
channel — protocol-common across every profile. Bitget pushes a fresh
kline on every interval boundary and updates the in-progress kline on
each trade; the SDK does NOT receive an explicit "closed" flag from the
exchange, so Confirmed is computed at the boundary by the stream
dispatcher (StartMs of the next push exceeds the previous candle's
EndMs).

FIELDS:
  - Symbol   : Bitget symbol.
  - Interval : Timeframe enum.
  - StartMs  : kline start timestamp (ms).
  - EndMs    : kline close timestamp (ms) — StartMs + interval length.
  - Open / High / Low / Close : OHLC.
  - Volume   : volume in base asset.
  - Turnover : volume in quote asset.
  - Confirmed: true on the candle-close push; false otherwise.
*/

package types

import "github.com/shopspring/decimal"

// KlineUpdate — one event from the candle{interval} channel.
type KlineUpdate struct {
	Symbol    string
	Interval  Timeframe
	StartMs   int64
	EndMs     int64
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
	Turnover  decimal.Decimal
	Confirmed bool
}
