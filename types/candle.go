/*
FILE: types/candle.go

DESCRIPTION:
Historical kline (candlestick) — protocol-common across every Bitget
profile. Mapped from the array returned by GET /api/v2/{mix,spot}/market/candles:

	[ startMs, open, high, low, close, volumeBase, volumeQuote ]

All numeric fields are strings on the wire and are normalised into
decimal.Decimal here. Bitget returns klines ordered ASCENDING by start
time (oldest first); the SDK preserves that order to match the public
docs.

PROFILE NOTES (informational, no schema impact):
  - For mix endpoints VolumeBase is denominated in BASE asset and
    VolumeQuote in QUOTE asset; there is no contract multiplier.
  - For spot endpoints VolumeBase / VolumeQuote are likewise in
    base / quote — also no multiplier.

Bitget does NOT include a "closed" flag in REST responses; only the
newest candle pushed by the WS "candle{tf}" channel is treated as still
forming. For REST historical fetches all candles are considered closed.
*/

package types

import "github.com/shopspring/decimal"

// Candle — one historical kline.
type Candle struct {
	OpenTimeMs  int64
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Close       decimal.Decimal
	Volume      decimal.Decimal
	VolumeQuote decimal.Decimal
}

// Candles — slice of candles. Order matches what Bitget returns
// (ascending by OpenTimeMs).
type Candles []Candle
