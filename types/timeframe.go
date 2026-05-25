/*
FILE: types/timeframe.go

DESCRIPTION:
Bitget kline `granularity` enum — protocol-common across every profile.
Bitget uses a small set of named values: "1m", "5m", "15m", "30m", "1H",
"4H", "6H", "12H", "1D", "1W", "1M".

Wire() returns the exact value the GET /api/v2/{mix,spot}/market/candles
endpoint expects.

Note: Bitget historical / index kline endpoints accept the same set, but
they expose them under a slightly different URL path. The granularity
encoding is identical, so this enum is reusable.
*/

package types

// Timeframe is a closed enum of kline intervals supported by Bitget.
type Timeframe string

const (
	// Timeframe1m — 1 minute.
	Timeframe1m Timeframe = "1m"
	// Timeframe5m — 5 minutes.
	Timeframe5m Timeframe = "5m"
	// Timeframe15m — 15 minutes.
	Timeframe15m Timeframe = "15m"
	// Timeframe30m — 30 minutes.
	Timeframe30m Timeframe = "30m"
	// Timeframe1h — 1 hour.
	Timeframe1h Timeframe = "1H"
	// Timeframe4h — 4 hours.
	Timeframe4h Timeframe = "4H"
	// Timeframe6h — 6 hours.
	Timeframe6h Timeframe = "6H"
	// Timeframe12h — 12 hours.
	Timeframe12h Timeframe = "12H"
	// Timeframe1d — 1 day.
	Timeframe1d Timeframe = "1D"
	// Timeframe1w — 1 week.
	Timeframe1w Timeframe = "1W"
	// Timeframe1M — 1 month.
	Timeframe1M Timeframe = "1M"
)

// Wire returns the Bitget string representation of the timeframe.
func (t Timeframe) Wire() string {
	return string(t)
}
