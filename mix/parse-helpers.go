/*
FILE: mix/parse-helpers.go

DESCRIPTION:
Bitget MIX-specific REST response decoding helpers. Cross-profile
helpers (level pairs, candle arrays) live in internal/bgcommon and are
imported here directly. The helpers in this file know about the
specific JSON shapes Bitget V2 ships on the MIX endpoints — they are
NOT shared with spot / uta because the wire formats differ.

USAGE:
The helpers are pure functions: no IO, no logging, allocate only what
they return. They are fast enough for the hot path on
GetOrderBook / GetHistoricalCandles.

Why decimal-from-string vs float64:
Bitget returns ALL numeric fields as JSON strings — the API contract
explicitly forbids parsing them as floats (precision loss on prices
like 0.1234567890). The helpers therefore use decimal.NewFromString
exclusively and surface decimal.Decimal to the caller.
*/

package mix

import (
	"strconv"

	"github.com/shopspring/decimal"
)

// parseDecimalOrZero is a forgiving counterpart to decimal.NewFromString
// — empty strings are treated as zero (Bitget occasionally emits "" for
// fields that do not apply to a particular contract instead of "0").
func parseDecimalOrZero(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(s)
}

// parseInt64OrZero is a forgiving counterpart to strconv.ParseInt for
// integer fields that Bitget ships as strings ("1700000000000") and
// occasionally as empty strings.
func parseInt64OrZero(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// parseIntOrZero is the int counterpart of parseInt64OrZero. Used for
// fields whose maximum is small (precision digits, leverage, ...).
func parseIntOrZero(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	var v int64
	var err error
	v, err = strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return int(v), nil
}
