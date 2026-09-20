/*
FILE: internal/codec/wire.go

DESCRIPTION:
Allocation-light wire field types for WebSocket hot paths. Bitget ships
every number as a quoted decimal string ("80810.6"); the generic decode
path (string struct field → decimal.NewFromString) pays for it three
times per number: one string allocation for the field, one string
concatenation inside NewFromString (it strips the decimal point) and the
big.Int itself. On a books50 frame that is ~600 avoidable allocations.

The types below are decoded by jsoniter TYPE DECODERS registered in this
package's init(): the decoder reads the JSON string as a byte slice that
aliases the iterator buffer (no copy), scans mantissa / exponent with
integer arithmetic and builds the decimal with decimal.New — the only
allocation left is the big.Int that shopspring/decimal cannot avoid.

	WireDecimal — decimal from "1.25" | 1.25 | null | ""
	WireInt64   — int64   from "1700000000000" | 1700000000000 | null | ""
	WireLevels  — [][price, size] book side, decoded into a reusable slice
	WireToken   — enum-like string ("buy", "yes", ...), interned

TOLERANCE:
Each type accepts BOTH the quoted-string and the bare-number form (the
venue is inconsistent — see bgcommon.FlexString for the incident trail),
plus null and the empty string, which decode to zero. Malformed input
fails the enclosing Unmarshal, so the caller can surface the frame
through its errHandler instead of silently trading on zeroes.

SCOPE OF THE REGISTRATION:
jsoniter type decoders are process-global and keyed by the type name
("codec.WireDecimal", ...). They only ever apply to the types declared
in THIS file, so the host application's own jsoniter usage is unaffected.
Registration happens in init(), before any decode can run, because the
jsoniter registry is not synchronised.

UnmarshalWire — FIELD NAMES WITHOUT ALLOCATIONS:
codec.Unmarshal uses the encoding/json-compatible config, which matches
object keys case-INsensitively. jsoniter implements that by registering
every struct field twice (exact + lower-case), which pushes any struct
with more than five camelCase fields off its hash-switch decoders and onto
the general one — and that one allocates a string for EVERY key in the
JSON (known or not) plus a strings.ToLower copy for every unknown key: 28
of the 44 allocations of a futures ticker frame. UnmarshalWire decodes
with a case-SENSITIVE config (keys are read as byte slices / hashed in
place), so the tags of a row decoded through it must match the wire
spelling exactly. Use it for wire shapes verified against live frames;
keep codec.Unmarshal where case tolerance is wanted.

USAGE:

	type tickerRow struct {
	    LastPrice       codec.WireDecimal `json:"lastPrice"`
	    NextFundingTime codec.WireInt64   `json:"nextFundingTime"`
	}
	var rows []tickerRow
	err := codec.UnmarshalWire(payload, &rows)
*/

package codec

import (
	"strconv"
	"unsafe"

	jsoniter "github.com/json-iterator/go"
	"github.com/shopspring/decimal"
)

// maxFastDigits — the longest digit run that is guaranteed to fit into an
// int64 mantissa (10^18 - 1 < 2^63 - 1). Longer inputs take the slow path.
const maxFastDigits int = 18

// WireDecimal is a decimal.Decimal decoded straight from the wire without
// an intermediate string. The zero value is decimal zero (field absent).
type WireDecimal decimal.Decimal

// Decimal returns the decoded value.
func (w WireDecimal) Decimal() decimal.Decimal { return decimal.Decimal(w) }

// WireInt64 is an int64 decoded from a quoted or bare JSON integer
// (timestamps, sequence numbers). The zero value is 0 (field absent).
type WireInt64 int64

// Int64 returns the decoded value.
func (w WireInt64) Int64() int64 { return int64(w) }

// WireToken is a short enum-like string (trade side, yes / no flag). The
// vocabulary the venue actually uses is interned, so decoding it does not
// allocate; any other value is still decoded (one allocation). null → "".
type WireToken string

// String returns the decoded value.
func (w WireToken) String() string { return string(w) }

// WireLevel is one [price, size] order-book level.
//
// PriceMantissa / PriceExponent expose Price as mantissa × 10^exponent
// when PriceFast is true (the price fitted the int64 fast path — always
// the case for real venue prices). Order-book code uses the pair to
// compare prices with integer arithmetic instead of decimal.Cmp, which
// allocates whenever the two operands have different exponents
// ("80811" vs "80810.4").
type WireLevel struct {
	Price         decimal.Decimal
	Size          decimal.Decimal
	PriceMantissa int64
	PriceExponent int32
	PriceFast     bool
}

// WireLevels is one side of a book as shipped on the wire:
// [["80810.4","0.8523"],["80810.5","0.0014"], ...]. Elements beyond
// [price, size] (some venues append an order count) are ignored.
//
// The decoder REUSES the capacity of the destination slice: decode into
// a long-lived row struct and the per-frame container allocation
// disappears. Callers must copy the levels out before the next decode.
type WireLevels []WireLevel

// wireAPI — the decode config behind UnmarshalWire: case-sensitive key
// matching and keys restricted to simple (escape-free) strings, which
// lets jsoniter hash / slice object keys in place instead of allocating
// them. See the file header.
var wireAPI = jsoniter.Config{
	CaseSensitive:                 true,
	ObjectFieldMustBeSimpleString: true,
}.Froze()

// UnmarshalWire parses raw into dest matching object keys
// case-SENSITIVELY and without allocating them. The registered Wire* type
// decoders apply exactly as with Unmarshal.
func UnmarshalWire(raw []byte, dest any) error {
	return wireAPI.Unmarshal(raw, dest)
}

func init() {
	jsoniter.RegisterTypeDecoderFunc("codec.WireDecimal", decodeWireDecimal)
	jsoniter.RegisterTypeDecoderFunc("codec.WireInt64", decodeWireInt64)
	jsoniter.RegisterTypeDecoderFunc("codec.WireLevels", decodeWireLevels)
	jsoniter.RegisterTypeDecoderFunc("codec.WireToken", decodeWireToken)

	var i int
	for i = 0; i < len(zeroByScale); i++ {
		zeroByScale[i] = decimal.New(0, -int32(i))
	}
}

// zeroByScale[n] is the decimal 0 with exponent -n ("0", "0.0", "0.00",
// ...). Order-book deltas delete levels with size "0" all day long; handing
// out these shared values instead of decimal.New(0, exp) saves the big.Int
// allocation per delete. Sharing is safe: shopspring/decimal never mutates
// the big.Int behind a Decimal (decimal.Zero is shared the same way).
var zeroByScale [maxFastDigits + 1]decimal.Decimal

// newDecimal builds mantissa × 10^exponent, reusing the shared zeroes.
func newDecimal(mantissa int64, exponent int32) decimal.Decimal {
	if mantissa == 0 && exponent <= 0 && int(-exponent) < len(zeroByScale) {
		return zeroByScale[-exponent]
	}
	return decimal.New(mantissa, exponent)
}

// ---------------------------------------------------------------------
// Byte-level parsers (exported: usable outside a struct decode).
// ---------------------------------------------------------------------

// scanDecimal scans b as [sign] digits [ "." digits ] into an int64
// mantissa and a base-10 exponent. ok=false means "not representable on
// the fast path" (exponent notation, more than maxFastDigits digits, no
// digits, stray bytes) — the caller falls back to decimal.NewFromString,
// which also produces the error for genuinely malformed input.
func scanDecimal(b []byte) (mantissa int64, exponent int32, ok bool) {
	var n int = len(b)
	if n == 0 {
		return 0, 0, false
	}
	var i int = 0
	var negative bool = false
	if b[0] == '-' {
		negative = true
		i = 1
	} else if b[0] == '+' {
		i = 1
	}
	var digits int = 0
	var fraction int32 = 0
	var seenPoint bool = false
	var value int64 = 0
	for ; i < n; i++ {
		var c byte = b[i]
		if c == '.' {
			if seenPoint {
				return 0, 0, false
			}
			seenPoint = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, 0, false
		}
		digits++
		if digits > maxFastDigits {
			return 0, 0, false
		}
		value = value*10 + int64(c-'0')
		if seenPoint {
			fraction++
		}
	}
	if digits == 0 {
		return 0, 0, false
	}
	if negative {
		value = -value
	}
	return value, -fraction, true
}

// ParseDecimalBytes converts a Bitget numeric byte string into a
// decimal.Decimal without allocating an intermediate string. Empty input
// → decimal.Zero, no error (same contract as ParseDecimal). The result is
// identical — value AND exponent — to decimal.NewFromString(string(b)).
func ParseDecimalBytes(b []byte) (decimal.Decimal, error) {
	if len(b) == 0 {
		return decimal.Zero, nil
	}
	var mantissa int64
	var exponent int32
	var ok bool
	mantissa, exponent, ok = scanDecimal(b)
	if ok {
		return newDecimal(mantissa, exponent), nil
	}
	return decimal.NewFromString(string(b))
}

// ParseInt64Bytes converts a decimal integer byte string to int64 without
// allocating. Empty input → 0, no error (same contract as ParseInt64).
func ParseInt64Bytes(b []byte) (int64, error) {
	var n int = len(b)
	if n == 0 {
		return 0, nil
	}
	var i int = 0
	var negative bool = false
	if b[0] == '-' {
		negative = true
		i = 1
	}
	if i == n || n-i > maxFastDigits {
		// Lone sign or a value that may overflow the fast loop — let
		// strconv decide (and produce the error text).
		return strconv.ParseInt(string(b), 10, 64)
	}
	var value int64 = 0
	for ; i < n; i++ {
		var c byte = b[i]
		if c < '0' || c > '9' {
			return strconv.ParseInt(string(b), 10, 64)
		}
		value = value*10 + int64(c-'0')
	}
	if negative {
		value = -value
	}
	return value, nil
}

// ---------------------------------------------------------------------
// jsoniter type decoders.
// ---------------------------------------------------------------------

// readScalarBytes returns the bytes of the next scalar: the content of a
// JSON string (aliasing the iterator buffer — valid until the next read),
// the literal of a bare number, or nil for null. Anything else reports
// an error on the iterator.
func readScalarBytes(iter *jsoniter.Iterator, owner string) []byte {
	switch iter.WhatIsNext() {
	case jsoniter.StringValue:
		return iter.ReadStringAsSlice()
	case jsoniter.NumberValue:
		// Rare, tolerated path (the venue normally quotes numbers); the
		// json.Number string allocation is acceptable here.
		return []byte(string(iter.ReadNumber()))
	case jsoniter.NilValue:
		iter.ReadNil()
		return nil
	default:
		iter.ReportError(owner, "expects a string, a number or null")
		iter.Skip()
		return nil
	}
}

func decodeWireDecimal(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	var raw []byte = readScalarBytes(iter, "WireDecimal")
	if iter.Error != nil {
		return
	}
	var d decimal.Decimal
	var err error
	d, err = ParseDecimalBytes(raw)
	if err != nil {
		iter.ReportError("WireDecimal", err.Error())
		return
	}
	*(*WireDecimal)(ptr) = WireDecimal(d)
}

func decodeWireInt64(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	// Bare integers (seq / pseq arrive as numbers on every books frame)
	// are read natively — no allocation.
	if iter.WhatIsNext() == jsoniter.NumberValue {
		*(*WireInt64)(ptr) = WireInt64(iter.ReadInt64())
		return
	}
	var raw []byte = readScalarBytes(iter, "WireInt64")
	if iter.Error != nil {
		return
	}
	var v int64
	var err error
	v, err = ParseInt64Bytes(raw)
	if err != nil {
		iter.ReportError("WireInt64", err.Error())
		return
	}
	*(*WireInt64)(ptr) = WireInt64(v)
}

func decodeWireToken(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	var raw []byte = readScalarBytes(iter, "WireToken")
	if iter.Error != nil {
		return
	}
	// `switch string(raw)` does not allocate; the constants are static.
	var token string
	switch string(raw) {
	case "":
		token = ""
	case "buy":
		token = "buy"
	case "sell":
		token = "sell"
	case "yes":
		token = "yes"
	case "no":
		token = "no"
	default:
		token = string(raw)
	}
	*(*WireToken)(ptr) = WireToken(token)
}

func decodeWireLevels(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	var dst *WireLevels = (*WireLevels)(ptr)
	var out WireLevels = (*dst)[:0]
	if iter.WhatIsNext() == jsoniter.NilValue {
		iter.ReadNil()
		*dst = out
		return
	}
	for iter.ReadArray() {
		var lvl WireLevel
		var err error
		var raw []byte

		// [0] price.
		if !iter.ReadArray() {
			iter.ReportError("WireLevels", "level must be [price, size]")
			return
		}
		raw = readScalarBytes(iter, "WireLevels")
		if iter.Error != nil {
			return
		}
		lvl.PriceMantissa, lvl.PriceExponent, lvl.PriceFast = scanDecimal(raw)
		if lvl.PriceFast {
			lvl.Price = newDecimal(lvl.PriceMantissa, lvl.PriceExponent)
		} else {
			lvl.Price, err = ParseDecimalBytes(raw)
			if err != nil {
				iter.ReportError("WireLevels", "price: "+err.Error())
				return
			}
		}

		// [1] size.
		if !iter.ReadArray() {
			iter.ReportError("WireLevels", "level must be [price, size]")
			return
		}
		raw = readScalarBytes(iter, "WireLevels")
		if iter.Error != nil {
			return
		}
		lvl.Size, err = ParseDecimalBytes(raw)
		if err != nil {
			iter.ReportError("WireLevels", "size: "+err.Error())
			return
		}

		// [2...] ignored.
		for iter.ReadArray() {
			iter.Skip()
		}
		if iter.Error != nil {
			return
		}
		out = append(out, lvl)
	}
	*dst = out
}
