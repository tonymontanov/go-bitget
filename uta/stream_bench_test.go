/*
FILE: uta/stream_bench_test.go

DESCRIPTION:
Benchmarks for the hot decoders of uta.StreamClient. Each one drives the
REAL frame handler (decode → convert / apply → fan-out to one no-op
handler) with the payload ws.Conn hands over — the `data` array of a live
frame captured 2026-09-21.

	BenchmarkStreamTickerFrame         ticker (futures, 19 fields on the wire)
	BenchmarkStreamBooks5Frame         books5 stateless snapshot (5 + 5 levels)
	BenchmarkStreamBooksUpdateApply    books delta (2 + 2 levels) applied to a
	                                   1000 x 1000 local book, view depth 200 / 1
	BenchmarkStreamPublicTradeFrame    publicTrade update (2 trades)

The V2 profiles ship no benchmarks to compare against, so the file also
carries the GENERIC decode those profiles use (string-typed wire row →
decimal.NewFromString, [][]string levels) as a reference for the same
payloads:

	BenchmarkReferenceGenericTickerFrame
	BenchmarkReferenceGenericBooks5Frame

What remains per frame after the codec.Wire* types is the big.Int inside
every delivered decimal (2 allocations each — shopspring/decimal cannot
avoid it) plus one backing array per delivered book.

Run: go test ./uta/ -run xxx -bench 'Stream|Reference' -benchmem -count 3
*/

package uta

import (
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// benchStream builds a StreamClient that never dials.
func benchStream(b *testing.B) *StreamClient {
	b.Helper()
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(bitget.DefaultConfig())
	if err != nil {
		b.Fatalf("bitget.NewClient: %v", err)
	}
	b.Cleanup(func() { _ = parent.Close() })
	return NewClient(parent).Stream()
}

// framePayload returns the `data` bytes of a captured frame — exactly what
// ws.Conn passes to a Subscription handler.
func framePayload(b *testing.B, frame string) []byte {
	b.Helper()
	var env ws.Envelope
	if err := codec.Unmarshal([]byte(frame), &env); err != nil {
		b.Fatalf("decode fixture envelope: %v", err)
	}
	return []byte(env.Data)
}

var benchTickerSink utatypes.TickerUpdate
var benchBookSink utatypes.OrderBookUpdate
var benchTradeSink utatypes.PublicTradeUpdate

func BenchmarkStreamTickerFrame(b *testing.B) {
	var s *StreamClient = benchStream(b)
	var arg ws.SubscriptionArg = ws.SubscriptionArg{InstType: "usdt-futures", Topic: topicTicker, Symbol: "BTCUSDT"}
	var t *tickerSub = s.newTickerSub(arg, "Stream.WatchTicker", utatypes.CategoryUSDTFutures, "BTCUSDT")
	t.add(func(u utatypes.TickerUpdate) { benchTickerSink = u }, nil)
	var payload []byte = framePayload(b, fixtureTickerFutures)

	b.ReportAllocs()
	b.ResetTimer()
	var i int
	for i = 0; i < b.N; i++ {
		s.handleTickerFrame(t, payload, 1789940561885)
	}
	if benchTickerSink.LastPrice.String() != "80810.6" {
		b.Fatalf("handler not reached: %+v", benchTickerSink)
	}
}

func BenchmarkStreamBooks5Frame(b *testing.B) {
	var s *StreamClient = benchStream(b)
	var arg ws.SubscriptionArg = ws.SubscriptionArg{InstType: "usdt-futures", Topic: topicBooks5, Symbol: "BTCUSDT"}
	var sub *bookSub = s.newBookSub(arg, "Stream.WatchOrderBook", utatypes.CategoryUSDTFutures, "BTCUSDT")
	var id uint64 = sub.add(func(u utatypes.OrderBookUpdate) { benchBookSink = u }, nil)
	sub.setDepth(id, 5)
	var payload []byte = framePayload(b, fixtureBooks5)

	b.ReportAllocs()
	b.ResetTimer()
	var i int
	for i = 0; i < b.N; i++ {
		s.handleBooksFrame(sub, actionSnapshot, payload, 1789940582102)
	}
	if len(benchBookSink.Asks) != 5 || len(benchBookSink.Bids) != 5 {
		b.Fatalf("handler not reached: %+v", benchBookSink)
	}
}

// benchBookSnapshot renders a snapshot payload with n levels a side around
// 80810, one decimal, mixed exponents ("80811" next to "80810.9").
func benchBookSnapshot(n int) []byte {
	var sb strings.Builder
	sb.WriteString(`[{"a":[`)
	var i int
	for i = 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`["` + tenths(808105+int64(i)) + `","0.5"]`)
	}
	sb.WriteString(`],"b":[`)
	for i = 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`["` + tenths(808104-int64(i)) + `","0.5"]`)
	}
	sb.WriteString(`],"seq":1,"pseq":0,"ts":"1789940621751","maxdepth":"1000"}]`)
	return []byte(sb.String())
}

// tenths renders v/10 the way the venue does: no trailing ".0".
func tenths(v int64) string {
	if v%10 == 0 {
		return strconv.FormatInt(v/10, 10)
	}
	return strconv.FormatInt(v/10, 10) + "." + strconv.FormatInt(v%10, 10)
}

func benchmarkBooksUpdateApply(b *testing.B, depth int) {
	var s *StreamClient = benchStream(b)
	var arg ws.SubscriptionArg = ws.SubscriptionArg{InstType: "usdt-futures", Topic: topicBooks, Symbol: "BTCUSDT"}
	var sub *bookSub = s.newBookSub(arg, "Stream.WatchOrderBook", utatypes.CategoryUSDTFutures, "BTCUSDT")
	var id uint64 = sub.add(func(u utatypes.OrderBookUpdate) { benchBookSink = u }, nil)
	sub.setDepth(id, depth)
	s.handleBooksFrame(sub, actionSnapshot, benchBookSnapshot(1000), 1)
	if len(benchBookSink.Asks) != depth {
		b.Fatalf("snapshot not applied: %d asks", len(benchBookSink.Asks))
	}

	// Two deltas that undo each other, chained 1→2 and 2→1, so the book
	// stays at 1000 levels a side and the seq chain stays valid forever:
	// an insert INSIDE the spread-side of the book plus a size change a
	// few levels deep, then the matching delete / restore.
	var forward []byte = []byte(`[{"a":[["80810.45","1.25"],["80811.2","0.75"]],"b":[["80810.35","2.5"],["80809.9","0.25"]],"seq":2,"pseq":1,"ts":"1789940621800","maxdepth":"1000"}]`)
	var backward []byte = []byte(`[{"a":[["80810.45","0"],["80811.2","0.5"]],"b":[["80810.35","0"],["80809.9","0.5"]],"seq":1,"pseq":2,"ts":"1789940621850","maxdepth":"1000"}]`)

	b.ReportAllocs()
	b.ResetTimer()
	var i int
	for i = 0; i < b.N; i++ {
		if i&1 == 0 {
			s.handleBooksFrame(sub, actionUpdate, forward, 1)
		} else {
			s.handleBooksFrame(sub, actionUpdate, backward, 1)
		}
	}
	b.StopTimer()
	if sub.book.resyncPending || !sub.book.ready {
		b.Fatal("the benchmark broke the seq chain — it measured the error path")
	}
	if len(benchBookSink.Asks) != depth {
		b.Fatalf("view depth = %d, want %d", len(benchBookSink.Asks), depth)
	}
}

func BenchmarkStreamBooksUpdateApply(b *testing.B) {
	b.Run("depth200", func(b *testing.B) { benchmarkBooksUpdateApply(b, 200) })
	// depth 1 isolates decode + apply from the cost of copying the view.
	b.Run("depth1", func(b *testing.B) { benchmarkBooksUpdateApply(b, 1) })
}

func BenchmarkStreamPublicTradeFrame(b *testing.B) {
	var s *StreamClient = benchStream(b)
	var arg ws.SubscriptionArg = ws.SubscriptionArg{InstType: "usdt-futures", Topic: topicPublicTrade, Symbol: "BTCUSDT"}
	var t *tradeSub = s.newTradeSub(arg, "Stream.WatchPublicTrades", utatypes.CategoryUSDTFutures, "BTCUSDT")
	t.add(func(u utatypes.PublicTradeUpdate) { benchTradeSink = u }, nil)
	var payload []byte = framePayload(b, fixtureTradesUpdate)

	b.ReportAllocs()
	b.ResetTimer()
	var i int
	for i = 0; i < b.N; i++ {
		s.handleTradesFrame(t, actionUpdate, payload)
	}
	if benchTradeSink.TradeID != "1485683898127761410" {
		b.Fatalf("handler not reached: %+v", benchTradeSink)
	}
}

// ---------------------------------------------------------------------
// Reference: the generic decode used by the V2 profiles.
// ---------------------------------------------------------------------

type referenceTickerRow struct {
	LastPrice       string `json:"lastPrice"`
	Bid1Price       string `json:"bid1Price"`
	Bid1Size        string `json:"bid1Size"`
	Ask1Price       string `json:"ask1Price"`
	Ask1Size        string `json:"ask1Size"`
	MarkPrice       string `json:"markPrice"`
	IndexPrice      string `json:"indexPrice"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
}

func BenchmarkReferenceGenericTickerFrame(b *testing.B) {
	var payload []byte = framePayload(b, fixtureTickerFutures)
	b.ReportAllocs()
	b.ResetTimer()
	var i int
	for i = 0; i < b.N; i++ {
		var rows []referenceTickerRow
		if err := codec.Unmarshal(payload, &rows); err != nil {
			b.Fatal(err)
		}
		var u utatypes.TickerUpdate = utatypes.TickerUpdate{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT"}
		u.LastPrice, _ = bgcommon.ParseDecimalOrZero(rows[0].LastPrice)
		u.Bid1Price, _ = bgcommon.ParseDecimalOrZero(rows[0].Bid1Price)
		u.Bid1Size, _ = bgcommon.ParseDecimalOrZero(rows[0].Bid1Size)
		u.Ask1Price, _ = bgcommon.ParseDecimalOrZero(rows[0].Ask1Price)
		u.Ask1Size, _ = bgcommon.ParseDecimalOrZero(rows[0].Ask1Size)
		u.MarkPrice, _ = bgcommon.ParseDecimalOrZero(rows[0].MarkPrice)
		u.IndexPrice, _ = bgcommon.ParseDecimalOrZero(rows[0].IndexPrice)
		u.FundingRate, _ = bgcommon.ParseDecimalOrZero(rows[0].FundingRate)
		u.NextFundingTimeMs, _ = bgcommon.ParseInt64OrZero(rows[0].NextFundingTime)
		benchTickerSink = u
	}
}

type referenceBookRow struct {
	Asks [][]string `json:"a"`
	Bids [][]string `json:"b"`
	Seq  int64      `json:"seq"`
	Ts   string     `json:"ts"`
}

func BenchmarkReferenceGenericBooks5Frame(b *testing.B) {
	var payload []byte = framePayload(b, fixtureBooks5)
	b.ReportAllocs()
	b.ResetTimer()
	var i int
	for i = 0; i < b.N; i++ {
		var rows []referenceBookRow
		if err := codec.Unmarshal(payload, &rows); err != nil {
			b.Fatal(err)
		}
		var u utatypes.OrderBookUpdate = utatypes.OrderBookUpdate{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", Seq: rows[0].Seq}
		u.TsMs, _ = bgcommon.ParseInt64OrZero(rows[0].Ts)
		u.Asks = referenceLevels(rows[0].Asks)
		u.Bids = referenceLevels(rows[0].Bids)
		benchBookSink = u
	}
}

func referenceLevels(raw [][]string) []utatypes.PriceLevel {
	var out []utatypes.PriceLevel = make([]utatypes.PriceLevel, 0, len(raw))
	var i int
	for i = 0; i < len(raw); i++ {
		var price decimal.Decimal
		var size decimal.Decimal
		price, _ = decimal.NewFromString(raw[i][0])
		size, _ = decimal.NewFromString(raw[i][1])
		out = append(out, utatypes.PriceLevel{Price: price, Size: size})
	}
	return out
}
