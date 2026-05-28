/*
FILE: internal/bgcommon/wsframes.go

DESCRIPTION:
Profile-agnostic WebSocket wire shapes and per-row converters shared
by mix/, spot/ and (future) uta/ public streams.

Bitget V2 ships the books / trade / candle channels with byte-for-
byte identical payloads across every product type — only the
subscription arg's `instType` field changes ("USDT-FUTURES" vs
"SPOT" vs "USDC-FUTURES"). This package consolidates the wire
structs and the value-level decoders so each profile's stream
client never re-implements them.

WHAT IS NOT HERE:

  - The ticker channel is INTENTIONALLY profile-specific. Mix
    ships markPrice / indexPrice / fundingRate / nextFundingTime
    next to the basic last-price snapshot; spot ships 24h roll-up
    metrics (open24h / high24h / low24h / change24h / ...) instead.
    A shared ticker shape would either omit half of mix's data or
    bloat spot's path with always-zero fields. Each profile owns its
    own `tickerFrame` + converter.

  - Engine / resync orchestration lives in each profile's stream
    client — the *ws.Conn lifecycle is intertwined with the
    profile's private-channel state (login, instType-specific
    subscribe arg, ...) which would force ugly knobs on a shared
    "stream base" type. The reuse here is at the value-conversion
    level, not at the orchestration level.
*/

package bgcommon

import (
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ---------------------------------------------------------------------
// Books channel.
// ---------------------------------------------------------------------

// OrderbookFrame mirrors one element of the "books" channel data
// array on Bitget V2 (mix and spot both). The level rows are
// `[price, size]` pairs as JSON strings — the orderbook engine in
// internal/bgcommon/orderbook parses them via ParseLevels.
//
// `Checksum` is the Bitget CRC32 over the top-25 sorted bid/ask
// levels (signed int32 on the wire, decoded as int64 for safety).
// `Ts` is the per-row publish time as a millisecond string —
// callers that prefer the envelope ts use that instead.
type OrderbookFrame struct {
	Asks     [][]string `json:"asks"`
	Bids     [][]string `json:"bids"`
	Checksum int64      `json:"checksum"`
	Ts       string     `json:"ts"`
}

// ---------------------------------------------------------------------
// Trade channel.
// ---------------------------------------------------------------------

// TradeFrame mirrors one element of the "trade" channel data array.
// All five fields are JSON strings on the wire (Bitget normalises
// numeric values to strings to avoid floating-point drift across
// language SDKs).
type TradeFrame struct {
	Ts      string `json:"ts"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Side    string `json:"side"`
	TradeID string `json:"tradeId"`
}

// ParseTradeFrame normalises one TradeFrame into roottypes.TradeUpdate.
// The returned error is the FIRST malformed numeric — callers fan-out
// over a slice and skip rows on error.
//
// Side normalisation: Bitget always ships "buy" / "sell" lower-cased
// on V2; the helper still echoes any future extension verbatim
// rather than forcing the value into a closed enum, keeping the SDK
// forward-compatible.
func ParseTradeFrame(symbol string, t TradeFrame) (roottypes.TradeUpdate, error) {
	var price decimal.Decimal
	var err error
	price, err = ParseDecimalOrZero(t.Price)
	if err != nil {
		return roottypes.TradeUpdate{}, err
	}
	var size decimal.Decimal
	size, err = ParseDecimalOrZero(t.Size)
	if err != nil {
		return roottypes.TradeUpdate{}, err
	}
	var side roottypes.SideType
	switch t.Side {
	case "buy":
		side = roottypes.SideTypeBuy
	case "sell":
		side = roottypes.SideTypeSell
	default:
		side = roottypes.SideType(t.Side)
	}
	var tsMs int64
	tsMs, _ = ParseInt64OrZero(t.Ts)
	return roottypes.TradeUpdate{
		Symbol:  symbol,
		Price:   price,
		Size:    size,
		Side:    side,
		TradeID: t.TradeID,
		TsMs:    tsMs,
	}, nil
}

// ---------------------------------------------------------------------
// Candle channel.
// ---------------------------------------------------------------------

// ParseCandleRow turns one 7-element candle row from the wire into a
// roottypes.KlineUpdate. The Bitget V2 wire format is identical
// across mix and spot:
//
//	[openTime, open, high, low, close, baseVolume, quoteVolume]
//
// Each element is a JSON string. Rows shorter than 7 elements
// surface as ErrorKindUnknown — keeps the SDK from emitting bogus
// zero-volume bars when Bitget changes the protocol.
//
// CONFIRMATION FLAG:
//
// The WS push does NOT explicitly flag closed bars; Bitget ships
// the in-progress bar repeatedly with the same openTime, then a
// frame with the next openTime starts the new bar. The SDK reports
// Confirmed=false uniformly — closure detection is the consumer's
// responsibility (it already needs the closed-bar logic for backfill
// mismatches anyway).
func ParseCandleRow(symbol string, tf roottypes.Timeframe, row []string) (roottypes.KlineUpdate, error) {
	if len(row) < 7 {
		return roottypes.KlineUpdate{}, bgerr.New(bgerr.ErrorKindUnknown, "",
			"bgcommon.ParseCandleRow: row arity "+strconv.Itoa(len(row))+" < 7", nil)
	}
	var openMs int64
	var err error
	openMs, err = ParseInt64OrZero(row[0])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	var open, high, low, close, volume, turnover decimal.Decimal
	open, err = ParseDecimalOrZero(row[1])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	high, err = ParseDecimalOrZero(row[2])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	low, err = ParseDecimalOrZero(row[3])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	close, err = ParseDecimalOrZero(row[4])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	volume, err = ParseDecimalOrZero(row[5])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	turnover, err = ParseDecimalOrZero(row[6])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	return roottypes.KlineUpdate{
		Symbol:    symbol,
		Interval:  tf,
		StartMs:   openMs,
		EndMs:     0,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		Turnover:  turnover,
		Confirmed: false,
	}, nil
}
