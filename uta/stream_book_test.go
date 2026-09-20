/*
FILE: uta/stream_book_test.go

DESCRIPTION:
Unit tests for the local incremental order book behind the V3 `books`
topic (stream-book.go): price comparison across decimal exponents, sorted
insert / replace / delete against a reference model, snapshot loading
(including a mis-ordered venue snapshot) and the seq / pseq chain rules.
*/

package uta

import (
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// mustBookRow decodes one books data element ({"a":...,"b":...,"seq":...}).
func mustBookRow(t testing.TB, raw string) *wsBookRow {
	t.Helper()
	var row wsBookRow
	if err := codec.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatalf("decode book row %s: %v", raw, err)
	}
	return &row
}

func mustLevel(t testing.TB, price, size string) *codec.WireLevel {
	t.Helper()
	var row *wsBookRow = mustBookRow(t, `{"a":[["`+price+`","`+size+`"]]}`)
	return &row.Asks[0]
}

func TestComparePrice(t *testing.T) {
	t.Parallel()
	type tc struct {
		a, b string
		want int
	}
	var cases []tc = []tc{
		{"80811", "80810.4", 1},  // different exponents
		{"80810.4", "80811", -1}, // mirrored
		{"1.50", "1.5", 0},       // equal value, different exponents
		{"0.00000123", "0.0000012", 1},
		{"100", "100", 0},
		{"99.999", "100", -1},
		{"1234567890123456789012", "1", 1},                        // slow path (no int64 mantissa)
		{"1234567890123456789012", "1234567890123456789012.0", 0}, // slow vs slow
		{"900000000000000000", "0.000000000000000001", 1},         // rescale would overflow → decimal.Cmp
		{"0.000000000000000001", "900000000000000000", -1},        // mirrored overflow
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var a *codec.WireLevel = mustLevel(t, cases[i].a, "1")
		var b *codec.WireLevel = mustLevel(t, cases[i].b, "1")
		var got int = comparePrice(a, b)
		if got != cases[i].want {
			t.Fatalf("comparePrice(%s, %s) = %d, want %d", cases[i].a, cases[i].b, got, cases[i].want)
		}
		// Must always agree with decimal.Cmp.
		if got != a.Price.Cmp(b.Price) {
			t.Fatalf("comparePrice(%s, %s) = %d disagrees with decimal.Cmp", cases[i].a, cases[i].b, got)
		}
	}
}

// TestLocalBookAgainstReferenceModel drives the book with random deltas
// (mixed price exponents, inserts, replacements, deletes of present and
// absent levels) and compares every view with a map-based reference.
func TestLocalBookAgainstReferenceModel(t *testing.T) {
	t.Parallel()
	var rng *rand.Rand = rand.New(rand.NewSource(7))
	var lb *localBook = newLocalBook()
	var refAsks map[string]string = map[string]string{}
	var refBids map[string]string = map[string]string{}

	var snapshot *wsBookRow = mustBookRow(t, `{"a":[["100.5","1"],["101","2"],["101.25","3"]],"b":[["100","1"],["99.5","2"],["99","3"]],"seq":1,"pseq":0}`)
	var outcome bookOutcome
	outcome, _, _, _ = lb.apply(actionSnapshot, snapshot, 0)
	if outcome != bookApplied {
		t.Fatalf("snapshot outcome = %d", outcome)
	}
	refAsks["100.5"], refAsks["101"], refAsks["101.25"] = "1", "2", "3"
	refBids["100"], refBids["99.5"], refBids["99"] = "1", "2", "3"

	var seq int64 = 1
	var step int
	for step = 0; step < 600; step++ {
		var askJSON string = randomDelta(rng, refAsks, 100.01, 140)
		var bidJSON string = randomDelta(rng, refBids, 60, 100)
		var row *wsBookRow = mustBookRow(t, `{"a":`+askJSON+`,"b":`+bidJSON+`,"seq":`+strconv.FormatInt(seq+1, 10)+`,"pseq":`+strconv.FormatInt(seq, 10)+`}`)
		seq++
		var asks []utatypes.PriceLevel
		var bids []utatypes.PriceLevel
		outcome, _, asks, bids = lb.apply(actionUpdate, row, 0)
		if outcome != bookApplied {
			t.Fatalf("step %d: outcome = %d", step, outcome)
		}
		assertSideMatches(t, step, "asks", asks, refAsks, true)
		assertSideMatches(t, step, "bids", bids, refBids, false)
	}
}

// randomDelta mutates ref and returns the matching wire delta. Prices mix
// 0 / 1 / 2 decimals so neighbours routinely differ in exponent.
func randomDelta(rng *rand.Rand, ref map[string]string, lo, hi float64) string {
	var sb strings.Builder
	sb.WriteByte('[')
	var n int = rng.Intn(4)
	var i int
	for i = 0; i < n; i++ {
		var decimals int = rng.Intn(3)
		var price string = strconv.FormatFloat(lo+rng.Float64()*(hi-lo), 'f', decimals, 64)
		// Canonical key: the decimal's own rendering ("100.50" == "100.5").
		var key string = decimal.RequireFromString(price).String()
		var size string = "0"
		if rng.Intn(3) != 0 {
			size = strconv.FormatFloat(0.001+rng.Float64()*5, 'f', 3, 64)
		} else if len(ref) > 0 && rng.Intn(2) == 0 {
			// Delete an EXISTING level half of the time.
			var k string
			for k = range ref {
				break
			}
			price = k
			key = k
		}
		if size == "0" {
			delete(ref, key)
		} else {
			ref[key] = decimal.RequireFromString(size).String()
		}
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`["` + price + `","` + size + `"]`)
	}
	sb.WriteByte(']')
	return sb.String()
}

func assertSideMatches(t *testing.T, step int, name string, got []utatypes.PriceLevel, ref map[string]string, ascending bool) {
	t.Helper()
	var prices []decimal.Decimal = make([]decimal.Decimal, 0, len(ref))
	var k string
	for k = range ref {
		prices = append(prices, decimal.RequireFromString(k))
	}
	sort.Slice(prices, func(i, j int) bool {
		if ascending {
			return prices[i].LessThan(prices[j])
		}
		return prices[i].GreaterThan(prices[j])
	})
	if len(got) != len(prices) {
		t.Fatalf("step %d: %s has %d levels, reference %d", step, name, len(got), len(prices))
	}
	var i int
	for i = 0; i < len(got); i++ {
		if !got[i].Price.Equal(prices[i]) {
			t.Fatalf("step %d: %s[%d] price = %s, reference %s", step, name, i, got[i].Price, prices[i])
		}
		if got[i].Size.String() != ref[prices[i].String()] {
			t.Fatalf("step %d: %s[%d] size = %s, reference %s", step, name, i, got[i].Size, ref[prices[i].String()])
		}
	}
}

func TestLocalBookSnapshotRepairsOrderAndDropsZeroSizes(t *testing.T) {
	t.Parallel()
	var lb *localBook = newLocalBook()
	// Asks out of order, a duplicate price (the entry listed first wins),
	// a zero-size level; bids out of order.
	var row *wsBookRow = mustBookRow(t, `{"a":[["101","2"],["100.5","1"],["102","0"],["101","9"]],"b":[["99","3"],["100","1"],["99.5","2"]],"seq":5,"pseq":0}`)
	var outcome bookOutcome
	var asks []utatypes.PriceLevel
	var bids []utatypes.PriceLevel
	outcome, _, asks, bids = lb.apply(actionSnapshot, row, 0)
	if outcome != bookApplied {
		t.Fatalf("outcome = %d", outcome)
	}
	if levelsString(asks) != "100.5x1 101x2" {
		t.Fatalf("asks = %s (duplicate / zero-size levels must not survive)", levelsString(asks))
	}
	if levelsString(bids) != "100x1 99.5x2 99x3" {
		t.Fatalf("bids = %s", levelsString(bids))
	}
}

func TestLocalBookViewDepth(t *testing.T) {
	t.Parallel()
	var lb *localBook = newLocalBook()
	var row *wsBookRow = mustBookRow(t, `{"a":[["101","1"],["102","1"],["103","1"]],"b":[["100","1"],["99","1"],["98","1"]],"seq":1,"pseq":0}`)
	var asks []utatypes.PriceLevel
	var bids []utatypes.PriceLevel
	_, _, asks, bids = lb.apply(actionSnapshot, row, 2)
	if levelsString(asks) != "101x1 102x1" || levelsString(bids) != "100x1 99x1" {
		t.Fatalf("depth 2 view = %s | %s", levelsString(asks), levelsString(bids))
	}
	// The STORED book keeps every level: deleting the touch reveals the
	// third level instead of a hole.
	_, _, asks, bids = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["101","0"]],"b":[["100","0"]],"seq":2,"pseq":1}`), 2)
	if levelsString(asks) != "102x1 103x1" || levelsString(bids) != "99x1 98x1" {
		t.Fatalf("view after touch delete = %s | %s", levelsString(asks), levelsString(bids))
	}
	// Separate backing arrays per delivery; capacity capped per side.
	if cap(asks) != len(asks) {
		t.Fatalf("asks cap = %d, len = %d", cap(asks), len(asks))
	}
}

func TestLocalBookChainRules(t *testing.T) {
	t.Parallel()
	var lb *localBook = newLocalBook()
	var outcome bookOutcome
	var reason string

	// Update before any snapshot.
	outcome, reason, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["1","1"]],"b":[],"seq":11,"pseq":10}`), 0)
	if outcome != bookBroken || reason == "" {
		t.Fatalf("update before snapshot: outcome=%d reason=%q", outcome, reason)
	}
	// ... and everything after it is dropped SILENTLY until a snapshot.
	outcome, _, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["1","1"]],"b":[],"seq":12,"pseq":11}`), 0)
	if outcome != bookSkipped {
		t.Fatalf("update while resync pending: outcome=%d", outcome)
	}

	outcome, _, _, _ = lb.apply(actionSnapshot, mustBookRow(t, `{"a":[["101","1"]],"b":[["100","1"]],"seq":100,"pseq":0}`), 0)
	if outcome != bookApplied {
		t.Fatalf("snapshot: outcome=%d", outcome)
	}

	// First update after the snapshot — venue docs: stale ones
	// (seq ≤ snapshot.seq) are skipped ...
	outcome, _, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["555","1"]],"b":[],"seq":100,"pseq":95}`), 0)
	if outcome != bookSkipped {
		t.Fatalf("stale first update: outcome=%d", outcome)
	}
	// ... and one that STRADDLES the snapshot (pseq ≤ snapshot.seq < seq)
	// is applied.
	var asks []utatypes.PriceLevel
	outcome, _, asks, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["102","2"]],"b":[],"seq":105,"pseq":98}`), 0)
	if outcome != bookApplied || levelsString(asks) != "101x1 102x2" {
		t.Fatalf("straddling first update: outcome=%d asks=%s", outcome, levelsString(asks))
	}
	// From now on the chain is STRICT: pseq must equal the last seq.
	outcome, _, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["103","3"]],"b":[],"seq":110,"pseq":105}`), 0)
	if outcome != bookApplied {
		t.Fatalf("chained update: outcome=%d", outcome)
	}
	outcome, reason, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["104","4"]],"b":[],"seq":120,"pseq":109}`), 0)
	if outcome != bookBroken || !strings.Contains(reason, "pseq=109") || !strings.Contains(reason, "want=110") {
		t.Fatalf("gap: outcome=%d reason=%q", outcome, reason)
	}

	// A gap right after a snapshot (pseq beyond the snapshot seq).
	_, _, _, _ = lb.apply(actionSnapshot, mustBookRow(t, `{"a":[["101","1"]],"b":[["100","1"]],"seq":200,"pseq":0}`), 0)
	outcome, _, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["102","1"]],"b":[],"seq":210,"pseq":205}`), 0)
	if outcome != bookBroken {
		t.Fatalf("gap after snapshot: outcome=%d", outcome)
	}

	// pseq == 0 on an update = venue sequence reset.
	_, _, _, _ = lb.apply(actionSnapshot, mustBookRow(t, `{"a":[["101","1"]],"b":[["100","1"]],"seq":300,"pseq":0}`), 0)
	outcome, reason, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["102","1"]],"b":[],"seq":3,"pseq":0}`), 0)
	if outcome != bookBroken || !strings.Contains(reason, "pseq=0") {
		t.Fatalf("pseq=0: outcome=%d reason=%q", outcome, reason)
	}

	// reset() (reconnect) clears the silent-drop window: the next update
	// before a snapshot is reported again.
	lb.reset()
	outcome, _, _, _ = lb.apply(actionUpdate, mustBookRow(t, `{"a":[["1","1"]],"b":[],"seq":2,"pseq":1}`), 0)
	if outcome != bookBroken {
		t.Fatalf("update after reset: outcome=%d", outcome)
	}

	// Unknown actions are ignored.
	outcome, _, _, _ = lb.apply("delete", mustBookRow(t, `{"a":[],"b":[],"seq":1,"pseq":0}`), 0)
	if outcome != bookSkipped {
		t.Fatalf("unknown action: outcome=%d", outcome)
	}
}
