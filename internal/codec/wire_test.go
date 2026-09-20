/*
FILE: internal/codec/wire_test.go

DESCRIPTION:
Unit tests and micro-benchmarks for the allocation-light wire types
(WireDecimal / WireInt64 / WireLevels) and their byte-level parsers.

The load-bearing assertion is EQUIVALENCE: ParseDecimalBytes must return
exactly what decimal.NewFromString returns — same value and same exponent
— for every input, fast path or fallback. The registered jsoniter type
decoders are pinned through a regular codec.Unmarshal so a broken
registration (renamed type, moved package) fails loudly here.
*/

package codec

import (
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// TestWireTypeNamesMatchRegistration guards the string keys used in
// init(): jsoniter resolves type decoders by reflect type name.
func TestWireTypeNamesMatchRegistration(t *testing.T) {
	t.Parallel()
	if got := reflect.TypeOf(WireDecimal{}).String(); got != "codec.WireDecimal" {
		t.Fatalf("WireDecimal type name = %q", got)
	}
	if got := reflect.TypeOf(WireInt64(0)).String(); got != "codec.WireInt64" {
		t.Fatalf("WireInt64 type name = %q", got)
	}
	if got := reflect.TypeOf(WireLevels{}).String(); got != "codec.WireLevels" {
		t.Fatalf("WireLevels type name = %q", got)
	}
	if got := reflect.TypeOf(WireToken("")).String(); got != "codec.WireToken" {
		t.Fatalf("WireToken type name = %q", got)
	}
}

func TestParseDecimalBytesMatchesNewFromString(t *testing.T) {
	t.Parallel()
	var inputs []string = []string{
		"0", "0.0", "0.000051", "-0.00238", "80810.6", "80810.60", "81470",
		"1682569476.10781", "20843.3982", "0.3743", "1", "-1", "+5", "5.", ".5",
		"000123.4500", "999999999999999999", "0.999999999999999999",
		// Slow path: more than 18 digits, exponent notation.
		"32117.3964999999767", "1234567890123456789012", "1e-7", "1.5E+3", "-2.5e2",
	}
	var i int
	for i = 0; i < len(inputs); i++ {
		var in string = inputs[i]
		var want decimal.Decimal
		var wantErr error
		want, wantErr = decimal.NewFromString(in)
		var got decimal.Decimal
		var gotErr error
		got, gotErr = ParseDecimalBytes([]byte(in))
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("%q: error mismatch: want %v, got %v", in, wantErr, gotErr)
		}
		if wantErr != nil {
			continue
		}
		if !got.Equal(want) || got.Exponent() != want.Exponent() || got.String() != want.String() {
			t.Fatalf("%q: got %s (exp %d), want %s (exp %d)", in, got, got.Exponent(), want, want.Exponent())
		}
	}
}

func TestParseDecimalBytesRandomised(t *testing.T) {
	t.Parallel()
	var rng *rand.Rand = rand.New(rand.NewSource(20260921))
	var i int
	for i = 0; i < 20000; i++ {
		var intDigits int = rng.Intn(12)
		var fracDigits int = rng.Intn(12)
		var sb strings.Builder
		if rng.Intn(4) == 0 {
			sb.WriteByte('-')
		}
		var j int
		for j = 0; j < intDigits; j++ {
			sb.WriteByte(byte('0' + rng.Intn(10)))
		}
		if fracDigits > 0 || intDigits == 0 {
			sb.WriteByte('.')
			if fracDigits == 0 {
				fracDigits = 1
			}
			for j = 0; j < fracDigits; j++ {
				sb.WriteByte(byte('0' + rng.Intn(10)))
			}
		}
		var in string = sb.String()
		var want decimal.Decimal
		var err error
		want, err = decimal.NewFromString(in)
		if err != nil {
			t.Fatalf("generator produced invalid input %q: %v", in, err)
		}
		var got decimal.Decimal
		got, err = ParseDecimalBytes([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !got.Equal(want) || got.Exponent() != want.Exponent() {
			t.Fatalf("%q: got %s (exp %d), want %s (exp %d)", in, got, got.Exponent(), want, want.Exponent())
		}
	}
}

func TestParseDecimalBytesEmptyAndMalformed(t *testing.T) {
	t.Parallel()
	var d decimal.Decimal
	var err error
	d, err = ParseDecimalBytes(nil)
	if err != nil || !d.IsZero() {
		t.Fatalf("empty: %s, %v", d, err)
	}
	var bad []string = []string{"abc", "1.2.3", "-", ".", "1,5", "0x10"}
	var i int
	for i = 0; i < len(bad); i++ {
		if _, err = ParseDecimalBytes([]byte(bad[i])); err == nil {
			t.Fatalf("%q: want error", bad[i])
		}
	}
}

func TestParseInt64Bytes(t *testing.T) {
	t.Parallel()
	type tc struct {
		in      string
		want    int64
		wantErr bool
	}
	var cases []tc = []tc{
		{"", 0, false},
		{"0", 0, false},
		{"1789940582101", 1789940582101, false},
		{"-42", -42, false},
		{"1304314508780744705", 1304314508780744705, false}, // 19 digits → strconv path
		{"9223372036854775808", 0, true},                    // overflow
		{"-", 0, true},
		{"12a", 0, true},
		{"1.5", 0, true},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var got int64
		var err error
		got, err = ParseInt64Bytes([]byte(cases[i].in))
		if (err != nil) != cases[i].wantErr {
			t.Fatalf("%q: err = %v, wantErr = %v", cases[i].in, err, cases[i].wantErr)
		}
		if err == nil && got != cases[i].want {
			t.Fatalf("%q: got %d, want %d", cases[i].in, got, cases[i].want)
		}
		if err == nil {
			var ref int64
			ref, _ = strconv.ParseInt(cases[i].in, 10, 64)
			if cases[i].in != "" && ref != got {
				t.Fatalf("%q: diverges from strconv: %d vs %d", cases[i].in, got, ref)
			}
		}
	}
}

type wireRow struct {
	Price   WireDecimal `json:"price"`
	Size    WireDecimal `json:"size"`
	Missing WireDecimal `json:"missing"`
	Ts      WireInt64   `json:"ts"`
	Seq     WireInt64   `json:"seq"`
	Asks    WireLevels  `json:"a"`
	Bids    WireLevels  `json:"b"`
	Name    string      `json:"name"`
}

// TestWireTypesDecodeAllForms: quoted string, bare number, null and ""
// all decode; unknown fields are skipped; absent fields stay zero.
func TestWireTypesDecodeAllForms(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{"name":"x","price":"80810.6","size":0.25,"ts":"1789940582101","seq":993094633676,
		"ignored":{"deep":[1,2,3]},
		"a":[["80810.4","0.8523"],["80811","0.0001","3"],[80811.1,0.0124]],"b":null}`)
	var row wireRow
	if err := Unmarshal(raw, &row); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if row.Name != "x" {
		t.Fatalf("name = %q", row.Name)
	}
	if row.Price.Decimal().String() != "80810.6" || row.Size.Decimal().String() != "0.25" {
		t.Fatalf("price/size = %s/%s", row.Price.Decimal(), row.Size.Decimal())
	}
	if !row.Missing.Decimal().IsZero() {
		t.Fatalf("absent field = %s", row.Missing.Decimal())
	}
	if row.Ts.Int64() != 1789940582101 || row.Seq.Int64() != 993094633676 {
		t.Fatalf("ts/seq = %d/%d", row.Ts, row.Seq)
	}
	if len(row.Asks) != 3 || len(row.Bids) != 0 {
		t.Fatalf("levels: asks=%d bids=%d", len(row.Asks), len(row.Bids))
	}
	if row.Asks[0].Price.String() != "80810.4" || row.Asks[0].Size.String() != "0.8523" {
		t.Fatalf("ask[0] = %+v", row.Asks[0])
	}
	if !row.Asks[0].PriceFast || row.Asks[0].PriceMantissa != 808104 || row.Asks[0].PriceExponent != -1 {
		t.Fatalf("ask[0] key = %d e%d fast=%v", row.Asks[0].PriceMantissa, row.Asks[0].PriceExponent, row.Asks[0].PriceFast)
	}
	// Third element (order count) ignored; bare-number level tolerated.
	if row.Asks[1].Price.String() != "80811" || row.Asks[1].PriceExponent != 0 {
		t.Fatalf("ask[1] = %+v", row.Asks[1])
	}
	if row.Asks[2].Price.String() != "80811.1" || row.Asks[2].Size.String() != "0.0124" {
		t.Fatalf("ask[2] = %+v", row.Asks[2])
	}

	var nulls wireRow
	if err := Unmarshal([]byte(`{"price":null,"size":"","ts":null,"seq":"","a":[],"b":[]}`), &nulls); err != nil {
		t.Fatalf("Unmarshal nulls: %v", err)
	}
	if !nulls.Price.Decimal().IsZero() || !nulls.Size.Decimal().IsZero() || nulls.Ts != 0 || nulls.Seq != 0 {
		t.Fatalf("nulls: %+v", nulls)
	}
}

// TestWireLevelsReuseCapacity: decoding into a long-lived row reuses the
// level storage and truncates stale tails.
func TestWireLevelsReuseCapacity(t *testing.T) {
	t.Parallel()
	var row wireRow
	if err := Unmarshal([]byte(`{"a":[["1","1"],["2","2"],["3","3"]]}`), &row); err != nil {
		t.Fatalf("first: %v", err)
	}
	var firstCap int = cap(row.Asks)
	var firstPtr *WireLevel = &row.Asks[0]
	if err := Unmarshal([]byte(`{"a":[["9","9"]]}`), &row); err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(row.Asks) != 1 || row.Asks[0].Price.String() != "9" {
		t.Fatalf("second decode = %+v", row.Asks)
	}
	if cap(row.Asks) != firstCap || &row.Asks[0] != firstPtr {
		t.Fatalf("storage not reused: cap %d → %d", firstCap, cap(row.Asks))
	}
}

func TestWireTypesRejectMalformed(t *testing.T) {
	t.Parallel()
	var bad []string = []string{
		`{"price":"abc"}`,
		`{"price":true}`,
		`{"price":{"x":1}}`,
		`{"ts":"12x"}`,
		`{"ts":[1]}`,
		`{"a":[["1"]]}`,
		`{"a":[[]]}`,
		`{"a":[["1","zz"]]}`,
		`{"a":"nope"}`,
	}
	var i int
	for i = 0; i < len(bad); i++ {
		var row wireRow
		if err := Unmarshal([]byte(bad[i]), &row); err == nil {
			t.Fatalf("%s: want error, got %+v", bad[i], row)
		}
	}
}

// TestZeroDecimalsKeepTheirExponent: the shared zero values handed out
// for "0" / "0.00" must be indistinguishable from NewFromString's.
func TestZeroDecimalsKeepTheirExponent(t *testing.T) {
	t.Parallel()
	var inputs []string = []string{"0", "0.0", "0.00", "-0", "0.000000000000000000", "00"}
	var i int
	for i = 0; i < len(inputs); i++ {
		var want decimal.Decimal = decimal.RequireFromString(inputs[i])
		var got decimal.Decimal
		var err error
		got, err = ParseDecimalBytes([]byte(inputs[i]))
		if err != nil {
			t.Fatalf("%q: %v", inputs[i], err)
		}
		if !got.IsZero() || got.Exponent() != want.Exponent() || got.String() != want.String() {
			t.Fatalf("%q: got %s (exp %d), want %s (exp %d)", inputs[i], got, got.Exponent(), want, want.Exponent())
		}
		// Arithmetic on a shared zero must not disturb the next user.
		var sum decimal.Decimal = got.Add(decimal.RequireFromString("1.5"))
		if sum.String() != "1.5" {
			t.Fatalf("%q: 0 + 1.5 = %s", inputs[i], sum)
		}
	}
	var again decimal.Decimal
	again, _ = ParseDecimalBytes([]byte("0.00"))
	if !again.IsZero() || again.Exponent() != -2 {
		t.Fatalf("shared zero was mutated: %s (exp %d)", again, again.Exponent())
	}
}

type tokenRow struct {
	Side  WireToken `json:"S"`
	IsRPI WireToken `json:"isRPI"`
	Other WireToken `json:"other"`
	Null  WireToken `json:"null"`
}

func TestWireTokenDecodes(t *testing.T) {
	// Not parallel: testing.AllocsPerRun refuses to run in a parallel test.
	var row tokenRow
	if err := UnmarshalWire([]byte(`{"S":"sell","isRPI":"no","other":"liquidation","null":null}`), &row); err != nil {
		t.Fatalf("UnmarshalWire: %v", err)
	}
	if row.Side != "sell" || row.IsRPI != "no" || row.Other.String() != "liquidation" || row.Null != "" {
		t.Fatalf("row = %+v", row)
	}
	var payload []byte = []byte(`{"S":"buy","isRPI":"yes"}`)
	var allocs float64 = testing.AllocsPerRun(200, func() {
		var r tokenRow
		_ = UnmarshalWire(payload, &r)
	})
	// One allocation is the escaping row itself; the tokens add none.
	if allocs > 1 {
		t.Fatalf("interned tokens allocated: %.0f allocs/op", allocs)
	}
}

// TestUnmarshalWireIsCaseSensitive documents the trade-off: keys must
// match the tags exactly (codec.Unmarshal stays case-insensitive), and
// unknown keys — of any spelling — are skipped.
func TestUnmarshalWireIsCaseSensitive(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{"PRICE":"1","price":"2","Size":"3","extra":{"x":[1,2]},"ts":"5"}`)
	var strict wireRow
	if err := UnmarshalWire(raw, &strict); err != nil {
		t.Fatalf("UnmarshalWire: %v", err)
	}
	if strict.Price.Decimal().String() != "2" || !strict.Size.Decimal().IsZero() || strict.Ts != 5 {
		t.Fatalf("strict = price %s size %s ts %d", strict.Price.Decimal(), strict.Size.Decimal(), strict.Ts)
	}
	var lenient wireRow
	if err := Unmarshal(raw, &lenient); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if lenient.Size.Decimal().String() != "3" {
		t.Fatalf("lenient size = %s (codec.Unmarshal must stay case-insensitive)", lenient.Size.Decimal())
	}
}

// ---------------------------------------------------------------------
// Benchmarks: fast byte parser vs the generic string path.
// ---------------------------------------------------------------------

var benchDecimalSink decimal.Decimal

func BenchmarkParseDecimalBytes(b *testing.B) {
	var in []byte = []byte("80810.6")
	b.ReportAllocs()
	var i int
	for i = 0; i < b.N; i++ {
		benchDecimalSink, _ = ParseDecimalBytes(in)
	}
}

func BenchmarkParseDecimalString(b *testing.B) {
	var in string = "80810.6"
	b.ReportAllocs()
	var i int
	for i = 0; i < b.N; i++ {
		benchDecimalSink, _ = ParseDecimal(in)
	}
}
