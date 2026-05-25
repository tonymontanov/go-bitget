/*
FILE: internal/bgcommon/parse.go

DESCRIPTION:
Profile-agnostic conversion helpers shared between mix/, spot/ and uta/
domain packages. The helpers cover the Bitget patterns repeated in every
endpoint:

  - level pairs ([price, size] string array → types.OrderBookLevel);
  - candle arrays ([startMs, o, h, l, c, vBase, vQuote] → types.Candle);
  - bid/ask slice parsing with consistent error wrapping;
  - common envelope projections (timestamp, sequence).

Every helper is a pure function — no IO, no logging — so they can be
inlined into the WS dispatch hot path without surprises.
*/

package bgcommon

import (
	"errors"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/types"
)

// ErrLevelShape is returned when a [price, size] tuple is malformed.
var ErrLevelShape = errors.New("bgcommon: order-book level must be [price, size]")

// ErrCandleShape is returned when a candle tuple has the wrong arity.
var ErrCandleShape = errors.New("bgcommon: candle must have 7 fields [t,o,h,l,c,vBase,vQuote]")

// ParseLevel converts a positional [price, size] string array into a
// typed OrderBookLevel. Empty strings are treated as zero (Bitget never
// emits them on book channels but other channels do).
func ParseLevel(pair []string) (types.OrderBookLevel, error) {
	if len(pair) < 2 {
		return types.OrderBookLevel{}, ErrLevelShape
	}
	var price, size decimal.Decimal
	var err error
	if pair[0] != "" {
		price, err = decimal.NewFromString(pair[0])
		if err != nil {
			return types.OrderBookLevel{}, err
		}
	}
	if pair[1] != "" {
		size, err = decimal.NewFromString(pair[1])
		if err != nil {
			return types.OrderBookLevel{}, err
		}
	}
	return types.OrderBookLevel{Price: price, Size: size}, nil
}

// ParseLevels converts a slice of [price, size] tuples. Returns the same
// error as ParseLevel on the first malformed entry.
func ParseLevels(pairs [][]string) ([]types.OrderBookLevel, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	var out []types.OrderBookLevel = make([]types.OrderBookLevel, 0, len(pairs))
	var i int
	for i = 0; i < len(pairs); i++ {
		var lvl types.OrderBookLevel
		var err error
		lvl, err = ParseLevel(pairs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, lvl)
	}
	return out, nil
}

// ParseCandle converts a Bitget kline tuple into a typed Candle. The
// expected arity is 7: [openTimeMs, open, high, low, close, volumeBase,
// volumeQuote]. Bitget sometimes sends an 8th element (USDT-quote
// turnover) that the helper ignores when present.
func ParseCandle(row []string) (types.Candle, error) {
	if len(row) < 7 {
		return types.Candle{}, ErrCandleShape
	}
	var ts int64
	var err error
	ts, err = strconv.ParseInt(row[0], 10, 64)
	if err != nil {
		return types.Candle{}, err
	}
	var open, high, low, closePx, volBase, volQuote decimal.Decimal
	open, err = decimal.NewFromString(row[1])
	if err != nil {
		return types.Candle{}, err
	}
	high, err = decimal.NewFromString(row[2])
	if err != nil {
		return types.Candle{}, err
	}
	low, err = decimal.NewFromString(row[3])
	if err != nil {
		return types.Candle{}, err
	}
	closePx, err = decimal.NewFromString(row[4])
	if err != nil {
		return types.Candle{}, err
	}
	volBase, err = decimal.NewFromString(row[5])
	if err != nil {
		return types.Candle{}, err
	}
	volQuote, err = decimal.NewFromString(row[6])
	if err != nil {
		return types.Candle{}, err
	}
	return types.Candle{
		OpenTimeMs:  ts,
		Open:        open,
		High:        high,
		Low:         low,
		Close:       closePx,
		Volume:      volBase,
		VolumeQuote: volQuote,
	}, nil
}

// ParseCandles converts a slice of candle tuples.
func ParseCandles(rows [][]string) (types.Candles, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	var out types.Candles = make(types.Candles, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var cdl types.Candle
		var err error
		cdl, err = ParseCandle(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, cdl)
	}
	return out, nil
}
