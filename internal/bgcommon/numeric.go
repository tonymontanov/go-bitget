/*
FILE: internal/bgcommon/numeric.go

DESCRIPTION:
Profile-agnostic numeric parsing helpers for the Bitget V2 wire format.
Bitget ships every numeric scalar as a JSON string ("size":"0.5",
"leverage":"5", "cTime":"1700000000000") and occasionally emits an
empty string for fields that do not apply to a particular instrument
("liquidationPrice":""). The helpers below codify this as
"empty → zero, otherwise strict parse". They are pure functions, hot-
path safe, and used identically across mix/, spot/, and uta/ profile
converters.

USAGE:
The helpers are pure functions: no IO, no logging, allocate only what
they return. They are fast enough for the hot path on
GetOrderBook / GetHistoricalCandles and per-frame WS dispatch.

DECIMAL VS FLOAT:
Bitget returns ALL numeric fields as JSON strings — the API contract
explicitly forbids parsing them as floats (precision loss on prices
like 0.1234567890). The decimal helper therefore uses
decimal.NewFromString exclusively and surfaces decimal.Decimal to the
caller.
*/

package bgcommon

import (
	"strconv"

	"github.com/shopspring/decimal"
)

// ParseDecimalOrZero is a forgiving counterpart to decimal.NewFromString
// — empty strings are treated as zero (Bitget occasionally emits "" for
// fields that do not apply to a particular instrument instead of "0").
func ParseDecimalOrZero(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(s)
}

// ParseInt64OrZero is a forgiving counterpart to strconv.ParseInt for
// integer fields that Bitget ships as strings ("1700000000000") and
// occasionally as empty strings.
func ParseInt64OrZero(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// ParseIntOrZero is the int counterpart of ParseInt64OrZero. Used for
// fields whose maximum is small (precision digits, leverage, ...).
func ParseIntOrZero(s string) (int, error) {
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
