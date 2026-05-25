/*
FILE: mix/orderbook-engine.go

DESCRIPTION:
Local order book engine for the Bitget V2 "books" channel. Maintains a
per-symbol ascending-asks / descending-bids state, applies snapshots
and incremental deltas, and validates the CRC32 checksum that Bitget
ships on every frame.

PROTOCOL (Bitget V2 books channel):

  - SNAPSHOT  (action="snapshot"): wipes any existing state and replaces
    it with the new asks/bids list. Engine treats the snapshot as the
    new authoritative truth — checksum on a snapshot is informational
    (we still validate, since "snapshot does not match its own
    checksum" implies a server-side bug worth surfacing).
  - UPDATE    (action="update"):  incremental change list. For every
    level:
    * size == 0  → remove the price level;
    * size != 0  → upsert (insert if new, replace if existing).
    After every applied delta the engine recomputes the CRC32 and
    compares to the value Bitget shipped. Mismatch → engine flips
    `dirty` and signals the caller to resync (drop+resub).

CRC32 FORMULA (Bitget V2):

	str = bid0_price : bid0_size : ask0_price : ask0_size :
	      bid1_price : bid1_size : ask1_price : ask1_size :
	      ...
	      bid24_price : bid24_size : ask24_price : ask24_size

  - Pairs go up to 25 levels per side (top of book).
  - If one side has fewer than the other, the missing slots are
    skipped entirely (NOT padded with empty strings).
  - The numeric strings are the EXACT wire values Bitget shipped
    (engine keeps them verbatim — re-rendering decimals would lose
    bit-for-bit fidelity, since "0.5" and "0.500" produce different
    CRC32s).
  - The result is interpreted as int32 (signed). Bitget echoes
    negative checksums for ~half of all frames.

ENGINE LIFECYCLE:

  - construct via newOrderbookEngine(symbol, maxDepth, logger);
  - feed every push frame through ApplySnapshot / ApplyUpdate;
  - read state via Snapshot();
  - on Reset (called by ws.Conn before every (re)subscribe) the
    engine drops its state so the next snapshot pushed by the server
    is treated as the new authoritative truth.

CONCURRENCY:

The engine is feature-complete behind a single mutex. ApplySnapshot /
ApplyUpdate / Snapshot serialise on it. The hot path is one RWMutex
acquisition per push — for 200-level depth that costs <1 µs per frame
on commodity hardware, well under the per-frame budget of the consumer
side.
*/

package mix

import (
	"errors"
	"hash/crc32"
	"strings"
	"sync"

	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// orderbookCRCDepth — number of levels per side that participate in
// the CRC32 calculation. Bitget V2 fixes this at 25 (see Bitget
// "books" channel documentation, section "checksum verification").
const orderbookCRCDepth = 25

// errOrderbookChecksum is returned by ApplyUpdate when the recomputed
// CRC32 disagrees with the server-shipped value. Callers (StreamClient)
// react by triggering a resubscribe.
var errOrderbookChecksum = errors.New("mix.Orderbook: checksum mismatch")

// errOrderbookDirty is returned by ApplyUpdate while the engine is
// waiting for the next snapshot (e.g. after a previous CRC mismatch).
// Callers should drop the update silently and wait for the snapshot.
var errOrderbookDirty = errors.New("mix.Orderbook: dirty, awaiting snapshot")

// orderbookLevel mirrors one Bitget V2 [price, size] pair, keeping
// BOTH the parsed decimal and the verbatim wire strings. The wire
// strings are required for the bit-for-bit CRC32 reproduction; the
// parsed decimals serve every other consumer (Snapshot output,
// downstream conversion).
type orderbookLevel struct {
	price    decimal.Decimal
	size     decimal.Decimal
	priceStr string
	sizeStr  string
}

// orderbookEngine — per-symbol engine state.
type orderbookEngine struct {
	mu sync.Mutex

	symbol   string
	maxDepth int

	// asks — sorted ASCENDING by price (best ask = asks[0]).
	asks []orderbookLevel
	// bids — sorted DESCENDING by price (best bid = bids[0]).
	bids []orderbookLevel
	// tsMs — last applied push timestamp (ms).
	tsMs int64
	// checksum — CRC32 echoed by the last successful push. Useful for
	// debugging; not used by the engine itself.
	checksum int64
	// dirty — true between a CRC mismatch and the arrival of the next
	// snapshot. While dirty, ApplyUpdate is a no-op and Snapshot
	// returns the symbol-only zero value.
	dirty bool
}

// newOrderbookEngine constructs an empty engine. maxDepth caps the
// stored side length; 0 falls back to the SDK default (200).
func newOrderbookEngine(symbol string, maxDepth int) *orderbookEngine {
	if maxDepth <= 0 {
		maxDepth = 200
	}
	return &orderbookEngine{
		symbol:   symbol,
		maxDepth: maxDepth,
		asks:     make([]orderbookLevel, 0, maxDepth),
		bids:     make([]orderbookLevel, 0, maxDepth),
		dirty:    true, // requires a snapshot before update is meaningful
	}
}

// Reset drops state. Called by the ws.Conn supervisor before every
// (re)subscribe so a stale push that arrived on the previous socket
// cannot race with the engine.
func (e *orderbookEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.asks = e.asks[:0]
	e.bids = e.bids[:0]
	e.tsMs = 0
	e.checksum = 0
	e.dirty = true
}

// ApplySnapshot replaces the engine state with the snapshot's
// asks/bids. The snapshot's checksum is validated for diagnostics
// (snapshot mismatch is a server-side bug rather than a sync issue),
// but the new state is committed regardless — "the server says this is
// the truth" trumps the engine's local recomputation.
func (e *orderbookEngine) ApplySnapshot(asks, bids []orderbookLevel, tsMs, checksum int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Snapshot lists arrive sorted from Bitget (asks ascending, bids
	// descending). We re-sort defensively in case the server changes
	// behaviour later.
	sortAsks(asks)
	sortBids(bids)

	if len(asks) > e.maxDepth {
		asks = asks[:e.maxDepth]
	}
	if len(bids) > e.maxDepth {
		bids = bids[:e.maxDepth]
	}

	e.asks = asks
	e.bids = bids
	e.tsMs = tsMs
	e.checksum = checksum
	e.dirty = false

	if checksum == 0 {
		return nil
	}
	var got int32 = computeOrderbookCRC(e.asks, e.bids)
	if int32(checksum) != got {
		// Snapshot mismatch — keep the state (it's still the most
		// authoritative thing we have) but surface the discrepancy.
		return errOrderbookChecksum
	}
	return nil
}

// ApplyUpdate merges an incremental delta. Returns errOrderbookDirty
// when called before the first snapshot (or right after a CRC mismatch),
// errOrderbookChecksum when the recomputed CRC disagrees with the
// server-shipped value.
func (e *orderbookEngine) ApplyUpdate(asks, bids []orderbookLevel, tsMs, checksum int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.dirty {
		return errOrderbookDirty
	}

	var i int
	for i = 0; i < len(asks); i++ {
		e.asks = upsertAsk(e.asks, asks[i])
	}
	for i = 0; i < len(bids); i++ {
		e.bids = upsertBid(e.bids, bids[i])
	}

	if len(e.asks) > e.maxDepth {
		e.asks = e.asks[:e.maxDepth]
	}
	if len(e.bids) > e.maxDepth {
		e.bids = e.bids[:e.maxDepth]
	}

	e.tsMs = tsMs
	e.checksum = checksum

	if checksum == 0 {
		return nil
	}
	var got int32 = computeOrderbookCRC(e.asks, e.bids)
	if int32(checksum) != got {
		// Mark dirty so subsequent updates are dropped until the next
		// snapshot lands.
		e.dirty = true
		return errOrderbookChecksum
	}
	return nil
}

// Snapshot returns the current engine state as a roottypes
// OrderBookSnapshot. The slices are copies — callers may retain them
// across calls without worrying about mutation.
func (e *orderbookEngine) Snapshot() roottypes.OrderBookSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.dirty {
		return roottypes.OrderBookSnapshot{Symbol: e.symbol}
	}

	return roottypes.OrderBookSnapshot{
		Symbol:   e.symbol,
		Asks:     copyLevelsToWire(e.asks),
		Bids:     copyLevelsToWire(e.bids),
		TsMs:     e.tsMs,
		Checksum: e.checksum,
	}
}

// IsDirty reports whether the engine is currently awaiting a snapshot.
// Used by stream tests; production code surfaces dirty state via the
// errors returned from ApplyUpdate.
func (e *orderbookEngine) IsDirty() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dirty
}

// ---------------------------------------------------------------------
// Sorted-slice helpers.
// ---------------------------------------------------------------------

// sortAsks sorts the slice ASCENDING by price.
func sortAsks(levels []orderbookLevel) {
	// Insertion sort: snapshot input is mostly already sorted, so the
	// O(N) best case beats O(N log N) of sort.Slice for the hot path.
	var i int
	for i = 1; i < len(levels); i++ {
		var j int
		for j = i; j > 0 && levels[j].price.LessThan(levels[j-1].price); j-- {
			levels[j], levels[j-1] = levels[j-1], levels[j]
		}
	}
}

// sortBids sorts the slice DESCENDING by price.
func sortBids(levels []orderbookLevel) {
	var i int
	for i = 1; i < len(levels); i++ {
		var j int
		for j = i; j > 0 && levels[j].price.GreaterThan(levels[j-1].price); j-- {
			levels[j], levels[j-1] = levels[j-1], levels[j]
		}
	}
}

// upsertAsk inserts or replaces an ask level (ascending). size==0 →
// remove. Linear in worst case; for 200-level depth this is well below
// the per-frame budget on x86_64.
func upsertAsk(levels []orderbookLevel, lvl orderbookLevel) []orderbookLevel {
	var i int
	for i = 0; i < len(levels); i++ {
		var cmp int = lvl.price.Cmp(levels[i].price)
		if cmp == 0 {
			if lvl.size.IsZero() {
				return append(levels[:i], levels[i+1:]...)
			}
			levels[i] = lvl
			return levels
		}
		if cmp < 0 {
			if lvl.size.IsZero() {
				return levels // already absent
			}
			return insertAt(levels, i, lvl)
		}
	}
	if lvl.size.IsZero() {
		return levels
	}
	return append(levels, lvl)
}

// upsertBid inserts or replaces a bid level (descending). Mirror of
// upsertAsk except for the comparison direction.
func upsertBid(levels []orderbookLevel, lvl orderbookLevel) []orderbookLevel {
	var i int
	for i = 0; i < len(levels); i++ {
		var cmp int = lvl.price.Cmp(levels[i].price)
		if cmp == 0 {
			if lvl.size.IsZero() {
				return append(levels[:i], levels[i+1:]...)
			}
			levels[i] = lvl
			return levels
		}
		if cmp > 0 {
			if lvl.size.IsZero() {
				return levels
			}
			return insertAt(levels, i, lvl)
		}
	}
	if lvl.size.IsZero() {
		return levels
	}
	return append(levels, lvl)
}

// insertAt inserts lvl at index i, shifting the tail right.
func insertAt(levels []orderbookLevel, i int, lvl orderbookLevel) []orderbookLevel {
	levels = append(levels, orderbookLevel{})
	copy(levels[i+1:], levels[i:])
	levels[i] = lvl
	return levels
}

// copyLevelsToWire converts engine levels to roottypes OrderBookLevels.
// Allocates a fresh slice so callers can retain the result across
// further engine mutations.
func copyLevelsToWire(src []orderbookLevel) []roottypes.OrderBookLevel {
	var out []roottypes.OrderBookLevel = make([]roottypes.OrderBookLevel, len(src))
	var i int
	for i = 0; i < len(src); i++ {
		out[i] = roottypes.OrderBookLevel{
			Price: src[i].price,
			Size:  src[i].size,
		}
	}
	return out
}

// ---------------------------------------------------------------------
// CRC32.
// ---------------------------------------------------------------------

// computeOrderbookCRC builds the colon-joined string from the top
// orderbookCRCDepth bid/ask pairs and runs CRC32(IEEE) on it. Returns
// int32 (signed) — Bitget echoes the value as a signed integer, and
// that's what the env shipped from the wire.
func computeOrderbookCRC(asks, bids []orderbookLevel) int32 {
	var nAsks int = len(asks)
	if nAsks > orderbookCRCDepth {
		nAsks = orderbookCRCDepth
	}
	var nBids int = len(bids)
	if nBids > orderbookCRCDepth {
		nBids = orderbookCRCDepth
	}
	var pairs int = nAsks
	if nBids > pairs {
		pairs = nBids
	}

	// Pre-size the builder to avoid reallocation on the typical 25-pair
	// input. Each pair contributes ~40 bytes worst case.
	var sb strings.Builder
	sb.Grow(pairs * 40)

	var i int
	for i = 0; i < pairs; i++ {
		if sb.Len() > 0 {
			sb.WriteByte(':')
		}
		if i < nBids {
			sb.WriteString(bids[i].priceStr)
			sb.WriteByte(':')
			sb.WriteString(bids[i].sizeStr)
		}
		if i < nAsks {
			if i < nBids {
				sb.WriteByte(':')
			}
			sb.WriteString(asks[i].priceStr)
			sb.WriteByte(':')
			sb.WriteString(asks[i].sizeStr)
		}
	}

	var crc uint32 = crc32.ChecksumIEEE([]byte(sb.String()))
	return int32(crc)
}
