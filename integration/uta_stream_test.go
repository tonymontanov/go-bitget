//go:build integration

/*
FILE: integration/uta_stream_test.go

DESCRIPTION:
Live, UNSIGNED checks of the V3 (UTA) PUBLIC WebSocket through
uta.StreamClient — no API key needed:

  - TestLive_UTA_Stream_Public: production host
    (wss://ws.bitget.com/v3/ws/public). Within 20 s it must deliver at least
    one ticker, one books5 view and the `books` snapshot plus three APPLIED
    incremental updates for the futures symbol, with a sane book (best bid
    < best ask, asks ascending, bids descending) and WITHOUT a single
    decode error or order-book resync — i.e. the live seq / pseq chain
    validates frame after frame.
  - TestLive_UTA_Stream_PublicDemoHost: the same smoke on the demo host
    (wss://wspap.bitget.com/v3/ws/public), reached through Config.Demo;
    the demo book is quiet, so a single applied update suffices there.

The private topics need a UTA / Demo key and are not covered here.

Run: go test -tags integration ./integration/ -run TestLive_UTA_Stream -v
*/

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/uta"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// streamWindow — how long the venue gets to deliver everything.
const streamWindow time.Duration = 20 * time.Second

// warnLogger forwards SDK warnings / errors to the test log so a venue
// error event (e.g. 30001 "doesn't exist") is visible in the output.
type warnLogger struct {
	t *testing.T
}

func (l warnLogger) Debug(string, ...bitget.Field) {}
func (l warnLogger) Info(string, ...bitget.Field)  {}
func (l warnLogger) Warn(msg string, fields ...bitget.Field) {
	l.t.Logf("SDK WARN: %s %v", msg, fields)
}
func (l warnLogger) Error(msg string, fields ...bitget.Field) {
	l.t.Logf("SDK ERROR: %s %v", msg, fields)
}

// newStreamClient builds an unsigned client for the chosen environment.
// It does NOT go through newClient(): that helper forces Demo on, which
// would silently move the "production" test to the demo host.
func newStreamClient(t *testing.T, demo bool) *uta.StreamClient {
	t.Helper()
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.Demo = demo
	cfg.Logger = warnLogger{t: t}
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	var wantURL string = bitget.DefaultWsUTAPublicURL
	if demo {
		wantURL = bitget.DefaultWsPublicURLDemo
	}
	if parent.Config().WS.UTAPublicURL != wantURL {
		t.Fatalf("UTA public URL = %q, want %q", parent.Config().WS.UTAPublicURL, wantURL)
	}
	var s *uta.StreamClient = uta.NewClient(parent).Stream()
	t.Cleanup(func() {
		_ = s.Close()
		_ = parent.Close()
	})
	return s
}

// streamStats collects what the handlers saw.
type streamStats struct {
	mu          sync.Mutex
	tickers     int
	lastTicker  utatypes.TickerUpdate
	books5      int
	lastBooks5  utatypes.OrderBookUpdate
	books       int
	firstBookTs int64
	lastBook    utatypes.OrderBookUpdate
	trades      int
	lastTradeTs int64
	tradeOrder  bool
	errs        []error
}

func (st *streamStats) fail(err error) {
	st.mu.Lock()
	st.errs = append(st.errs, err)
	st.mu.Unlock()
}

func assertSaneBook(t *testing.T, name string, ob utatypes.OrderBookUpdate, maxDepth int) {
	t.Helper()
	if len(ob.Asks) == 0 || len(ob.Bids) == 0 {
		t.Fatalf("%s: empty side: asks=%d bids=%d", name, len(ob.Asks), len(ob.Bids))
	}
	if len(ob.Asks) > maxDepth || len(ob.Bids) > maxDepth {
		t.Fatalf("%s: depth not honoured: asks=%d bids=%d max=%d", name, len(ob.Asks), len(ob.Bids), maxDepth)
	}
	if !ob.Bids[0].Price.LessThan(ob.Asks[0].Price) {
		t.Fatalf("%s: crossed book: bid=%s ask=%s", name, ob.Bids[0].Price, ob.Asks[0].Price)
	}
	var i int
	for i = 1; i < len(ob.Asks); i++ {
		if !ob.Asks[i-1].Price.LessThan(ob.Asks[i].Price) {
			t.Fatalf("%s: asks not strictly ascending at %d: %s then %s", name, i, ob.Asks[i-1].Price, ob.Asks[i].Price)
		}
	}
	for i = 1; i < len(ob.Bids); i++ {
		if !ob.Bids[i-1].Price.GreaterThan(ob.Bids[i].Price) {
			t.Fatalf("%s: bids not strictly descending at %d: %s then %s", name, i, ob.Bids[i-1].Price, ob.Bids[i].Price)
		}
	}
	for i = 0; i < len(ob.Asks); i++ {
		if !ob.Asks[i].Size.IsPositive() {
			t.Fatalf("%s: non-positive ask size at %d: %s", name, i, ob.Asks[i].Size)
		}
	}
	for i = 0; i < len(ob.Bids); i++ {
		if !ob.Bids[i].Size.IsPositive() {
			t.Fatalf("%s: non-positive bid size at %d: %s", name, i, ob.Bids[i].Size)
		}
	}
	if ob.Seq <= 0 || ob.TsMs <= 0 {
		t.Fatalf("%s: seq/ts = %d/%d", name, ob.Seq, ob.TsMs)
	}
}

func runPublicStreamSmoke(t *testing.T, demo bool, wantBookUpdates int) {
	var s *uta.StreamClient = newStreamClient(t, demo)
	var st *streamStats = &streamStats{tradeOrder: true}
	const bookDepth int = 200

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), streamWindow)
	defer cancel()

	var reconnects int
	defer s.OnPublicReconnect(func() {
		st.mu.Lock()
		reconnects++
		st.mu.Unlock()
	})()

	var err error = s.WatchTicker(ctx, futures, symbol(), func(u utatypes.TickerUpdate) {
		st.mu.Lock()
		st.tickers++
		st.lastTicker = u
		st.mu.Unlock()
	}, st.fail)
	if err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	err = s.WatchOrderBook(ctx, futures, symbol(), 5, func(u utatypes.OrderBookUpdate) {
		st.mu.Lock()
		st.books5++
		st.lastBooks5 = u
		st.mu.Unlock()
	}, st.fail)
	if err != nil {
		t.Fatalf("WatchOrderBook(5): %v", err)
	}
	err = s.WatchOrderBook(ctx, futures, symbol(), bookDepth, func(u utatypes.OrderBookUpdate) {
		st.mu.Lock()
		st.books++
		if st.books == 1 {
			st.firstBookTs = u.TsMs
		}
		st.lastBook = u
		st.mu.Unlock()
	}, st.fail)
	if err != nil {
		t.Fatalf("WatchOrderBook(%d): %v", bookDepth, err)
	}
	err = s.WatchPublicTrades(ctx, futures, symbol(), func(u utatypes.PublicTradeUpdate) {
		st.mu.Lock()
		st.trades++
		if u.TsMs < st.lastTradeTs {
			st.tradeOrder = false
		}
		st.lastTradeTs = u.TsMs
		st.mu.Unlock()
	}, st.fail)
	if err != nil {
		t.Fatalf("WatchPublicTrades: %v", err)
	}

	// 1 snapshot delivery + wantBookUpdates applied incremental updates.
	var wantBooks int = 1 + wantBookUpdates
	var started time.Time = time.Now()
	var done bool
	for !done && ctx.Err() == nil {
		time.Sleep(50 * time.Millisecond)
		st.mu.Lock()
		done = st.tickers >= 1 && st.books5 >= 1 && st.books >= wantBooks
		st.mu.Unlock()
	}
	var elapsed time.Duration = time.Since(started)

	st.mu.Lock()
	defer st.mu.Unlock()
	t.Logf("after %s: tickers=%d books5=%d books=%d (1 snapshot + %d updates) trades=%d reconnects=%d errors=%d",
		elapsed.Round(time.Millisecond), st.tickers, st.books5, st.books, st.books-1, st.trades, reconnects, len(st.errs))
	if len(st.errs) > 0 {
		t.Fatalf("stream errors (decode / order-book resync): %v", st.errs)
	}
	if st.tickers < 1 {
		t.Fatalf("no ticker within %s", streamWindow)
	}
	if st.books5 < 1 {
		t.Fatalf("no books5 view within %s", streamWindow)
	}
	if st.books < wantBooks {
		t.Fatalf("books: %d deliveries within %s, want ≥ %d (snapshot + %d applied updates)", st.books, streamWindow, wantBooks, wantBookUpdates)
	}
	if !st.tradeOrder {
		t.Fatalf("public trades were delivered out of chronological order")
	}

	var tk utatypes.TickerUpdate = st.lastTicker
	if tk.Category != futures || tk.Symbol != symbol() {
		t.Fatalf("ticker category/symbol = %q/%q", tk.Category, tk.Symbol)
	}
	if !tk.LastPrice.IsPositive() || !tk.Bid1Price.IsPositive() || !tk.Ask1Price.IsPositive() {
		t.Fatalf("ticker prices: last=%s bid=%s ask=%s", tk.LastPrice, tk.Bid1Price, tk.Ask1Price)
	}
	if !tk.Bid1Price.LessThan(tk.Ask1Price) {
		t.Fatalf("ticker crossed: bid=%s ask=%s", tk.Bid1Price, tk.Ask1Price)
	}
	if !tk.MarkPrice.IsPositive() || !tk.IndexPrice.IsPositive() || tk.NextFundingTimeMs <= 0 || tk.TsMs <= 0 {
		t.Fatalf("ticker futures fields: mark=%s index=%s nextFunding=%d ts=%d", tk.MarkPrice, tk.IndexPrice, tk.NextFundingTimeMs, tk.TsMs)
	}

	assertSaneBook(t, "books5", st.lastBooks5, 5)
	assertSaneBook(t, "books", st.lastBook, bookDepth)
	if len(st.lastBook.Asks) < 50 || len(st.lastBook.Bids) < 50 {
		t.Fatalf("books: suspiciously shallow local book: asks=%d bids=%d", len(st.lastBook.Asks), len(st.lastBook.Bids))
	}

	t.Logf("ticker %s: last=%s bid=%s x %s ask=%s x %s mark=%s index=%s funding=%s",
		symbol(), tk.LastPrice, tk.Bid1Price, tk.Bid1Size, tk.Ask1Price, tk.Ask1Size, tk.MarkPrice, tk.IndexPrice, tk.FundingRate)
	t.Logf("books5: bid=%s x %s ask=%s x %s seq=%d",
		st.lastBooks5.Bids[0].Price, st.lastBooks5.Bids[0].Size, st.lastBooks5.Asks[0].Price, st.lastBooks5.Asks[0].Size, st.lastBooks5.Seq)
	t.Logf("books(local, depth %d): bid=%s x %s ask=%s x %s levels=%d/%d seq=%d span=%dms",
		bookDepth, st.lastBook.Bids[0].Price, st.lastBook.Bids[0].Size, st.lastBook.Asks[0].Price, st.lastBook.Asks[0].Size,
		len(st.lastBook.Bids), len(st.lastBook.Asks), st.lastBook.Seq, st.lastBook.TsMs-st.firstBookTs)
}

func TestLive_UTA_Stream_Public(t *testing.T) {
	runPublicStreamSmoke(t, false, 3)
}

// The demo venue's book is quiet (a handful of `books` deltas per 10 s,
// measured 2026-09-21), so one applied update is all it is asked for.
func TestLive_UTA_Stream_PublicDemoHost(t *testing.T) {
	runPublicStreamSmoke(t, true, 1)
}
