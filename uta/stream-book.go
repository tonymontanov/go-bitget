/*
FILE: uta/stream-book.go

DESCRIPTION:
Order-book streaming for the V3 UTA profile: WatchOrderBook, the frame
handler shared by the four `books*` topics, the local incremental book
behind the full-depth `books` topic and its resync.

TOPIC SELECTION (by requested depth):

	depth ≤ 1  → books1   every push is the full 1-level book   (~1 ms)
	depth ≤ 5  → books5   every push is the full 5-level book   (~10 ms)
	depth ≤ 50 → books50  every push is the full 50-level book  (~20 ms)
	depth > 50 → books    snapshot (≤ 1000 levels a side) + 50 ms deltas
	depth ≤ 0  → 50

books1 / books5 / books50 are STATELESS: every push is action=snapshot
with pseq=0 (verified live 2026-09-21) — decode and deliver. There is no
books15 / books200 on V3 (the venue answers 30001).

`books` — LOCAL INCREMENTAL BOOK. V3 ships NO checksum (unlike the V2
CRC32); integrity is the seq / pseq chain:

  - snapshot          → replace the local state, remember seq;
  - update            → valid only when update.pseq == seq of the
                        previously applied frame; a level with size 0 is
                        deleted, any other size is an upsert;
  - first update after a snapshot → the venue docs allow
                        pseq ≤ snapshot.seq < seq (the snapshot may fall
                        INSIDE the first delta; levels are absolute sizes,
                        so re-applying is idempotent) and an update with
                        seq ≤ snapshot.seq is stale and skipped. Live, the
                        first update's pseq equals the snapshot seq;
  - pseq == 0 on an update (the venue reset its numbering), any other gap,
    or an update before the snapshot → the local state is dropped, ONE
    error (wrapping ErrOrderBookResync) goes to the errHandlers and the
    SDK resubscribes (unsubscribe → 50 ms → subscribe, de-duplicated like
    mix.scheduleResync). Deliveries resume with the venue's new snapshot.

The shared V2 engine (internal/bgcommon/orderbook) is NOT reused: it is
built around the CRC step (verbatim wire strings per level), truncates
its STORED state to maxDepth (without a checksum to catch it, levels that
fell off the tail would silently go missing when the book shrinks back)
and has no sequence validation. The book below keeps EVERY level the
venue sent and truncates only the delivered view.

LAYOUT / COST:
Each side is a sorted slice with the BEST price LAST: almost all churn is
at the touch, so inserts and deletes move a handful of elements instead
of the whole side. Lookup is a binary search; prices are compared through
the integer mantissa / exponent pair captured by the wire decoder, which
— unlike decimal.Cmp on operands with different exponents ("80811" vs
"80810.4") — never allocates. A delivery allocates one backing array for
both sides.
*/

package uta

import (
	"context"
	"errors"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// ErrOrderBookResync is the cause wrapped by the error WatchOrderBook
// surfaces when the local `books` order book lost the venue's seq / pseq
// chain and is being rebuilt. Detect it with errors.Is; it is
// informational — the SDK resubscribes on its own and deliveries resume
// with the next snapshot.
var ErrOrderBookResync error = errors.New("uta: order book out of sync, resubscribing")

// defaultBookDepth is used when the caller passes depth ≤ 0.
const defaultBookDepth int = 50

// bookResyncPause separates the unsubscribe from the re-subscribe of a
// resync: the venue rejects a back-to-back unsub / sub of the same arg.
const bookResyncPause time.Duration = 50 * time.Millisecond

// initialBookCapacity — levels pre-allocated per side of a local book
// (the venue's snapshot holds up to 1000).
const initialBookCapacity int = 1024

// ---------------------------------------------------------------------
// WatchOrderBook.
// ---------------------------------------------------------------------

// bookTopicFor maps a requested depth to the wire topic and the depth
// actually delivered.
func bookTopicFor(depth int) (topic string, effectiveDepth int) {
	if depth <= 0 {
		depth = defaultBookDepth
	}
	switch {
	case depth <= 1:
		return topicBooks1, depth
	case depth <= 5:
		return topicBooks5, depth
	case depth <= 50:
		return topicBooks50, depth
	default:
		return topicBooks, depth
	}
}

// bookSub — wire subscription of one (category, symbol, books topic).
type bookSub struct {
	wireSub[utatypes.OrderBookUpdate]
	category utatypes.Category
	symbol   string

	// rows — decode scratch; touched only by the read goroutine.
	rows []wsBookRow

	// book — the local incremental book; nil on the stateless topics.
	book *localBook

	// depths — requested depth per attached handler; guarded by
	// StreamClient.public.mu.
	depths map[uint64]int
	// viewDepth — max(depths): how many levels a delivery copies out.
	viewDepth atomic.Int64
	// resyncing — a resync round-trip is in flight (de-dup flag).
	resyncing atomic.Bool
}

// setDepth / dropDepth maintain viewDepth; the caller holds public.mu.
func (b *bookSub) setDepth(id uint64, depth int) {
	b.depths[id] = depth
	b.recomputeViewDepth()
}

func (b *bookSub) dropDepth(id uint64) {
	delete(b.depths, id)
	b.recomputeViewDepth()
}

func (b *bookSub) recomputeViewDepth() {
	var max int = 0
	var d int
	for _, d = range b.depths {
		if d > max {
			max = d
		}
	}
	b.viewDepth.Store(int64(max))
}

// WatchOrderBook subscribes to the order book of (category, symbol) and
// invokes handler with the FULL top-of-book view after every applied
// push: asks ascending, bids descending, best first, at most `depth`
// levels per side.
//
// depth selects the topic: ≤1 → books1, ≤5 → books5, ≤50 → books50
// (stateless venue snapshots), >50 → the full-depth `books` topic kept as
// a local incremental book validated by the seq / pseq chain; depth ≤ 0
// means 50. Callers asking for the same topic share one wire subscription
// (each gets its own depth); different topics of one symbol are
// independent subscriptions.
//
// errHandler receives decode errors and, on the `books` topic, one error
// wrapping ErrOrderBookResync whenever the chain broke and the SDK is
// resubscribing. The delivered slices may be retained (read-only — they
// are shared between the handlers of one subscription).
func (s *StreamClient) WatchOrderBook(
	ctx context.Context,
	category utatypes.Category,
	symbol string,
	depth int,
	handler func(utatypes.OrderBookUpdate),
	errHandler func(error),
) error {
	const scope string = "Stream.WatchOrderBook"
	var canonical utatypes.Category
	var instType string
	var err error
	canonical, instType, err = resolveCategory(scope, category)
	if err != nil {
		return err
	}
	if symbol == "" {
		return errInvalid(scope, "symbol is empty")
	}
	if handler == nil {
		return errInvalid(scope, "handler is nil")
	}

	var conn *ws.Conn
	conn, err = s.ensureConn(&s.public, false, scope)
	if err != nil {
		return err
	}

	var topic string
	var effectiveDepth int
	topic, effectiveDepth = bookTopicFor(depth)
	var arg ws.SubscriptionArg = ws.SubscriptionArg{InstType: instType, Topic: topic, Symbol: symbol}

	// Every handler sees at most ITS depth: callers sharing one topic may
	// have asked for different depths. The full slice expression caps the
	// capacity so an append by one handler cannot scribble over the
	// backing array shared with the others.
	var truncating func(utatypes.OrderBookUpdate) = func(u utatypes.OrderBookUpdate) {
		if len(u.Asks) > effectiveDepth {
			u.Asks = u.Asks[:effectiveDepth:effectiveDepth]
		}
		if len(u.Bids) > effectiveDepth {
			u.Bids = u.Bids[:effectiveDepth:effectiveDepth]
		}
		handler(u)
	}

	return attachHandler(s, &s.public, conn, s.books, arg, scope,
		func() *bookSub { return s.newBookSub(arg, scope, canonical, symbol) },
		ctx, truncating, errHandler,
		func(b *bookSub, id uint64) { b.setDepth(id, effectiveDepth) },
		func(b *bookSub, id uint64) { b.dropDepth(id) },
	)
}

func (s *StreamClient) newBookSub(arg ws.SubscriptionArg, scope string, category utatypes.Category, symbol string) *bookSub {
	var b *bookSub = &bookSub{
		category: category,
		symbol:   symbol,
		rows:     make([]wsBookRow, 0, 1),
		depths:   make(map[uint64]int, 2),
	}
	b.arg = arg
	b.scope = scope
	b.sub = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, action string, payload []byte, tsMs int64, _ int64) {
			s.handleBooksFrame(b, action, payload, tsMs)
		},
	}
	if arg.Topic == topicBooks {
		b.book = newLocalBook()
		// ws.Conn calls Reset before every (re)subscribe on a fresh socket:
		// the venue's next frame is a new authoritative snapshot.
		b.sub.Reset = b.book.reset
	}
	return b
}

// wsBookRow mirrors one `books*` data element. `maxDepth` (docs) /
// `maxdepth` (live) is informational and not consumed. seq / pseq arrive
// as bare numbers, ts as a quoted string; WireInt64 takes either.
type wsBookRow struct {
	Asks codec.WireLevels `json:"a"`
	Bids codec.WireLevels `json:"b"`
	Seq  codec.WireInt64  `json:"seq"`
	Pseq codec.WireInt64  `json:"pseq"`
	Ts   codec.WireInt64  `json:"ts"`
}

// decodeBookRows decodes payload into the scratch rows, REUSING both the
// row storage and the level storage of every row (codec.WireLevels
// truncates and refills its destination). Rows are reset field by field
// rather than zeroed so that the level capacity survives; a field absent
// from this frame must not inherit the previous frame's value. The scratch
// is passed by pointer to the long-lived subscription field — a local
// slice header would escape to the heap on every frame.
func decodeBookRows(scratch *[]wsBookRow, payload []byte) error {
	var rows []wsBookRow = (*scratch)[:cap(*scratch)]
	var i int
	for i = 0; i < len(rows); i++ {
		rows[i].Asks = rows[i].Asks[:0]
		rows[i].Bids = rows[i].Bids[:0]
		rows[i].Seq = 0
		rows[i].Pseq = 0
		rows[i].Ts = 0
	}
	*scratch = rows[:0]
	return codec.UnmarshalWire(payload, scratch)
}

// handleBooksFrame parses one `books*` frame, applies it (stateless copy
// or local-book apply) and delivers the resulting top-of-book view.
func (s *StreamClient) handleBooksFrame(b *bookSub, action string, payload []byte, tsMs int64) {
	if len(payload) == 0 {
		return
	}
	var err error = decodeBookRows(&b.rows, payload)
	if err != nil {
		surfaceError(s, &b.wireSub, "decode books frame", errParse(b.scope, err))
		return
	}
	var viewDepth int = int(b.viewDepth.Load())
	var i int
	for i = 0; i < len(b.rows); i++ {
		var row *wsBookRow = &b.rows[i]
		var update utatypes.OrderBookUpdate = utatypes.OrderBookUpdate{
			Category: b.category,
			Symbol:   b.symbol,
			Seq:      row.Seq.Int64(),
			TsMs:     row.Ts.Int64(),
		}
		if update.TsMs <= 0 {
			update.TsMs = tsMs
		}

		if b.book == nil {
			// Stateless topic: the frame IS the book — as long as it is a
			// snapshot. A delta here would mean the venue changed the
			// protocol; delivering it as a full book would be wrong data,
			// so it is surfaced instead.
			if action != actionSnapshot {
				surfaceError(s, &b.wireSub, "unexpected action on a stateless books topic",
					bitget.NewError(bitget.ErrorKindUnknown, "",
						"uta."+b.scope+": "+b.arg.Topic+" pushed action="+action+" (only snapshot is defined)", nil))
				continue
			}
			update.Asks, update.Bids = copyWireLevels(row.Asks, row.Bids, viewDepth)
			b.deliver(update)
			continue
		}

		var outcome bookOutcome
		var reason string
		outcome, reason, update.Asks, update.Bids = b.book.apply(action, row, viewDepth)
		switch outcome {
		case bookApplied:
			b.deliver(update)
		case bookBroken:
			surfaceError(s, &b.wireSub, "order book chain broken",
				bitget.NewError(bitget.ErrorKindUnknown, "",
					"uta."+b.scope+": "+b.symbol+": "+reason, ErrOrderBookResync))
			s.scheduleBookResync(b)
		case bookSkipped:
			// Stale frame or a resync in flight — nothing to deliver.
		}
	}
}

// copyWireLevels copies up to depth best-first wire levels of each side
// into ONE freshly allocated backing array (the delivered slices may be
// retained by the consumer; the wire levels are decode scratch).
func copyWireLevels(asks, bids codec.WireLevels, depth int) ([]utatypes.PriceLevel, []utatypes.PriceLevel) {
	var na int = len(asks)
	var nb int = len(bids)
	if depth > 0 {
		if na > depth {
			na = depth
		}
		if nb > depth {
			nb = depth
		}
	}
	var backing []utatypes.PriceLevel = make([]utatypes.PriceLevel, na+nb)
	var i int
	for i = 0; i < na; i++ {
		backing[i] = utatypes.PriceLevel{Price: asks[i].Price, Size: asks[i].Size}
	}
	for i = 0; i < nb; i++ {
		backing[na+i] = utatypes.PriceLevel{Price: bids[i].Price, Size: bids[i].Size}
	}
	return backing[:na:na], backing[na : na+nb : na+nb]
}

// ---------------------------------------------------------------------
// Resync.
// ---------------------------------------------------------------------

// scheduleBookResync runs an unsubscribe → pause → subscribe round-trip
// in the background so the venue sends a fresh snapshot. De-duplicated
// per subscription; a no-op once the last handler detached or the client
// was closed.
//
// It is called from the connection's READ goroutine, so it must not
// block: the de-dup flag is atomic and both wire ops run in the spawned
// goroutine, under public.mu, which serialises them with attach / detach
// of the same arg. While the round-trip is in flight the local book drops
// updates silently (localBook.resyncPending).
func (s *StreamClient) scheduleBookResync(b *bookSub) {
	if !b.resyncing.CompareAndSwap(false, true) {
		return
	}
	var side *connSide = &s.public
	var key string = b.arg.Key()

	go func() {
		side.mu.Lock()
		if side.closed || side.conn == nil || s.books[key] != b {
			b.resyncing.Store(false)
			side.mu.Unlock()
			return
		}
		var conn *ws.Conn = side.conn
		_ = conn.Unsubscribe(b.arg)
		side.mu.Unlock()

		select {
		case <-time.After(bookResyncPause):
		case <-s.closed:
		}

		side.mu.Lock()
		defer side.mu.Unlock()
		// Cleared BEFORE the subscribe: a chain break on the fresh
		// subscription must be able to schedule the next resync.
		b.resyncing.Store(false)
		if side.closed || s.books[key] != b {
			return
		}
		// Re-Subscribe the SAME Subscription object: its Handler closes
		// over this bookSub, so frames keep flowing to the handlers
		// attached to it.
		if err := conn.Subscribe(b.sub); err != nil {
			surfaceError(s, &b.wireSub, "resubscribe after resync", err)
		}
	}()
}

// ---------------------------------------------------------------------
// Local incremental book.
// ---------------------------------------------------------------------

// bookOutcome — result of applying one frame to the local book.
type bookOutcome int

const (
	// bookApplied — the state changed; deliver the returned view.
	bookApplied bookOutcome = iota
	// bookSkipped — stale frame, or a resync is pending; drop silently.
	bookSkipped
	// bookBroken — the seq chain broke; surface one error and resync.
	bookBroken
)

// localBook — seq-validated incremental book of one symbol.
type localBook struct {
	mu sync.Mutex

	// asks — DESCENDING by price, best (lowest) ask LAST.
	asks []codec.WireLevel
	// bids — ASCENDING by price, best (highest) bid LAST.
	bids []codec.WireLevel

	// ready — a snapshot was applied and the chain is intact.
	ready bool
	// lastSeq — seq of the last applied frame.
	lastSeq int64
	// afterSnapshot — the next update is the first one after a snapshot
	// (relaxed chain rule, see the file header).
	afterSnapshot bool
	// resyncPending — the chain broke and a resync was requested: updates
	// are dropped SILENTLY (one error per break) until the next snapshot.
	resyncPending bool
}

func newLocalBook() *localBook {
	return &localBook{
		asks: make([]codec.WireLevel, 0, initialBookCapacity),
		bids: make([]codec.WireLevel, 0, initialBookCapacity),
	}
}

// reset drops the state. Called by ws.Conn before every (re)subscribe on
// a fresh socket; the next frame the venue sends is a snapshot.
func (lb *localBook) reset() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.drop()
	lb.resyncPending = false
}

// drop clears levels and chain state; the caller holds lb.mu.
func (lb *localBook) drop() {
	lb.asks = lb.asks[:0]
	lb.bids = lb.bids[:0]
	lb.ready = false
	lb.lastSeq = 0
	lb.afterSnapshot = false
}

// breakChain drops the state and arms the silent-drop window.
func (lb *localBook) breakChain() {
	lb.drop()
	lb.resyncPending = true
}

// apply feeds one decoded frame into the book and, when the state
// changed, returns the top `depth` levels of each side (best first).
func (lb *localBook) apply(action string, row *wsBookRow, depth int) (bookOutcome, string, []utatypes.PriceLevel, []utatypes.PriceLevel) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	var seq int64 = row.Seq.Int64()
	var pseq int64 = row.Pseq.Int64()

	switch action {
	case actionSnapshot:
		lb.loadSnapshot(row.Asks, row.Bids)
		lb.ready = true
		lb.lastSeq = seq
		lb.afterSnapshot = true
		lb.resyncPending = false

	case actionUpdate:
		if lb.resyncPending {
			return bookSkipped, "", nil, nil
		}
		if !lb.ready {
			lb.breakChain()
			return bookBroken, "update before snapshot", nil, nil
		}
		if pseq == 0 {
			lb.breakChain()
			return bookBroken, "update with pseq=0 (venue sequence reset), seq=" + strconv.FormatInt(seq, 10), nil, nil
		}
		if lb.afterSnapshot {
			if seq <= lb.lastSeq {
				// Already contained in the snapshot.
				return bookSkipped, "", nil, nil
			}
			if pseq > lb.lastSeq {
				var reason string = "sequence gap after snapshot: pseq=" + strconv.FormatInt(pseq, 10) +
					" snapshot seq=" + strconv.FormatInt(lb.lastSeq, 10)
				lb.breakChain()
				return bookBroken, reason, nil, nil
			}
		} else if pseq != lb.lastSeq {
			var reason string = "sequence gap: pseq=" + strconv.FormatInt(pseq, 10) +
				" want=" + strconv.FormatInt(lb.lastSeq, 10)
			lb.breakChain()
			return bookBroken, reason, nil, nil
		}
		var i int
		for i = 0; i < len(row.Asks); i++ {
			lb.asks = upsertLevel(lb.asks, &row.Asks[i], true)
		}
		for i = 0; i < len(row.Bids); i++ {
			lb.bids = upsertLevel(lb.bids, &row.Bids[i], false)
		}
		lb.lastSeq = seq
		lb.afterSnapshot = false

	default:
		// Unknown action — ignore; a protocol extension must not break
		// the stream.
		return bookSkipped, "", nil, nil
	}

	var asks []utatypes.PriceLevel
	var bids []utatypes.PriceLevel
	asks, bids = lb.view(depth)
	return bookApplied, "", asks, bids
}

// loadSnapshot replaces both sides. The venue lists each side best first;
// the book stores best LAST, so the copy reverses. The order is verified
// on the way and repaired by a sort if the venue ever breaks it. Levels
// with size 0 are dropped.
func (lb *localBook) loadSnapshot(asks, bids codec.WireLevels) {
	lb.asks = loadSide(lb.asks[:0], asks, true)
	lb.bids = loadSide(lb.bids[:0], bids, false)
}

func loadSide(dst []codec.WireLevel, bestFirst codec.WireLevels, isAsk bool) []codec.WireLevel {
	var sorted bool = true
	var i int
	for i = len(bestFirst) - 1; i >= 0; i-- {
		if bestFirst[i].Size.IsZero() {
			continue
		}
		if len(dst) > 0 && sideOrder(&dst[len(dst)-1], &bestFirst[i], isAsk) >= 0 {
			sorted = false
		}
		dst = append(dst, bestFirst[i])
	}
	if !sorted {
		sort.SliceStable(dst, func(x, y int) bool { return sideOrder(&dst[x], &dst[y], isAsk) < 0 })
		dst = dedupeSide(dst, isAsk)
	}
	return dst
}

// dedupeSide removes duplicate prices from a sorted side (slow path of
// loadSide only). Of a run of equal prices the entry the venue listed
// FIRST survives: loadSide reverses the wire order and the sort is stable,
// so that entry is the last one of the run.
func dedupeSide(levels []codec.WireLevel, isAsk bool) []codec.WireLevel {
	if len(levels) < 2 {
		return levels
	}
	var out []codec.WireLevel = levels[:1]
	var i int
	for i = 1; i < len(levels); i++ {
		if sideOrder(&out[len(out)-1], &levels[i], isAsk) == 0 {
			out[len(out)-1] = levels[i]
			continue
		}
		out = append(out, levels[i])
	}
	return out
}

// view copies the top `depth` levels of each side (all levels when
// depth ≤ 0) into one freshly allocated backing array, best first.
func (lb *localBook) view(depth int) ([]utatypes.PriceLevel, []utatypes.PriceLevel) {
	var na int = len(lb.asks)
	var nb int = len(lb.bids)
	if depth > 0 {
		if na > depth {
			na = depth
		}
		if nb > depth {
			nb = depth
		}
	}
	var backing []utatypes.PriceLevel = make([]utatypes.PriceLevel, na+nb)
	var lastAsk int = len(lb.asks) - 1
	var lastBid int = len(lb.bids) - 1
	var i int
	for i = 0; i < na; i++ {
		backing[i] = utatypes.PriceLevel{Price: lb.asks[lastAsk-i].Price, Size: lb.asks[lastAsk-i].Size}
	}
	for i = 0; i < nb; i++ {
		backing[na+i] = utatypes.PriceLevel{Price: lb.bids[lastBid-i].Price, Size: lb.bids[lastBid-i].Size}
	}
	return backing[:na:na], backing[na : na+nb : na+nb]
}

// sideOrder orders two levels by their POSITION in a side's slice:
// negative when a sits before b. Asks are stored descending, bids
// ascending (best last on both sides).
func sideOrder(a, b *codec.WireLevel, isAsk bool) int {
	var c int = comparePrice(a, b)
	if isAsk {
		return -c
	}
	return c
}

// upsertLevel applies one delta level: size 0 deletes the price, any
// other size inserts or replaces it. Binary search + one memmove.
func upsertLevel(levels []codec.WireLevel, lvl *codec.WireLevel, isAsk bool) []codec.WireLevel {
	// First index whose level does not sit before lvl.
	var lo int = 0
	var hi int = len(levels)
	for lo < hi {
		var mid int = int(uint(lo+hi) >> 1)
		if sideOrder(&levels[mid], lvl, isAsk) < 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	var found bool = lo < len(levels) && comparePrice(&levels[lo], lvl) == 0
	if lvl.Size.IsZero() {
		if found {
			copy(levels[lo:], levels[lo+1:])
			levels[len(levels)-1] = codec.WireLevel{}
			levels = levels[:len(levels)-1]
		}
		return levels
	}
	if found {
		levels[lo] = *lvl
		return levels
	}
	levels = append(levels, codec.WireLevel{})
	copy(levels[lo+1:], levels[lo:])
	levels[lo] = *lvl
	return levels
}

// pow10 — powers of ten that fit in int64, for mantissa rescaling.
var pow10 [19]int64 = [19]int64{
	1, 10, 100, 1000, 10000, 100000, 1000000, 10000000, 100000000, 1000000000,
	10000000000, 100000000000, 1000000000000, 10000000000000, 100000000000000,
	1000000000000000, 10000000000000000, 100000000000000000, 1000000000000000000,
}

// comparePrice compares two level prices: -1 / 0 / +1. Fast path: integer
// compare of the mantissas, rescaling the coarser one when the exponents
// differ; falls back to decimal.Cmp when a price missed the int64 fast
// path or the rescale would overflow.
func comparePrice(a, b *codec.WireLevel) int {
	if a.PriceFast && b.PriceFast {
		var am int64 = a.PriceMantissa
		var bm int64 = b.PriceMantissa
		var ok bool = true
		if a.PriceExponent > b.PriceExponent {
			am, ok = scaleMantissa(am, a.PriceExponent-b.PriceExponent)
		} else if b.PriceExponent > a.PriceExponent {
			bm, ok = scaleMantissa(bm, b.PriceExponent-a.PriceExponent)
		}
		if ok {
			switch {
			case am < bm:
				return -1
			case am > bm:
				return 1
			default:
				return 0
			}
		}
	}
	return a.Price.Cmp(b.Price)
}

// scaleMantissa multiplies m by 10^shift; ok=false on int64 overflow.
func scaleMantissa(m int64, shift int32) (int64, bool) {
	if shift < 0 || int(shift) >= len(pow10) {
		return 0, false
	}
	var factor int64 = pow10[shift]
	var limit int64 = math.MaxInt64 / factor
	if m > limit || m < -limit {
		return 0, false
	}
	return m * factor, true
}
