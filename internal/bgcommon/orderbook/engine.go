/*
FILE: internal/bgcommon/orderbook/engine.go

DESCRIPTION:
Profile-agnostic local order-book engine for Bitget V2 "books" channel.
Maintains a per-symbol ascending-asks / descending-bids state, applies
snapshots and incremental deltas, and validates the CRC32 checksum that
Bitget ships on every frame. The engine is consumed identically by
mix/, spot/ and uta/ stream clients — the wire format and CRC formula
are uniform across profiles.

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

  - construct via NewEngine(symbol, maxDepth);
  - feed every push frame through ApplySnapshot / ApplyUpdate;
  - read state via Snapshot();
  - on Reset (called by ws.Conn before every (re)subscribe) the
    engine drops its state so the next snapshot pushed by the server
    is treated as the new authoritative truth.

CONCURRENCY:

The engine is feature-complete behind a single mutex. ApplySnapshot /
ApplyUpdate / Snapshot serialise on it. The hot path is one mutex
acquisition per push — for 200-level depth that costs <1 µs per frame
on commodity hardware, well under the per-frame budget of the consumer
side.
*/

package orderbook

import (
	"errors"
	"hash/crc32"
	"strings"
	"sync"

	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// CRCDepth — number of levels per side that participate in the CRC32
// calculation. Bitget V2 fixes this at 25 (see Bitget "books" channel
// documentation, section "checksum verification").
const CRCDepth = 25

// ErrChecksum is returned by ApplyUpdate / ApplySnapshot when the
// recomputed CRC32 disagrees with the server-shipped value. Callers
// react by triggering a resubscribe.
var ErrChecksum = errors.New("orderbook: checksum mismatch")

// ErrDirty is returned by ApplyUpdate while the engine is waiting for
// the next snapshot (e.g. after a previous CRC mismatch). Callers
// should drop the update silently and wait for the snapshot.
var ErrDirty = errors.New("orderbook: dirty, awaiting snapshot")

// Level mirrors one Bitget V2 [price, size] pair, keeping BOTH the
// parsed decimal and the verbatim wire strings. The wire strings are
// required for the bit-for-bit CRC32 reproduction; the parsed
// decimals serve every other consumer (Snapshot output, downstream
// conversion).
//
// Fields are exported so callers in profile packages (mix/, spot/,
// uta/) can construct levels directly when feeding the engine from a
// custom decoded representation. The canonical constructor for a
// wire row is ParseLevels.
type Level struct {
	Price    decimal.Decimal
	Size     decimal.Decimal
	PriceStr string
	SizeStr  string
}

// Engine — per-symbol engine state.
type Engine struct {
	mu sync.Mutex

	symbol   string
	maxDepth int

	// asks — sorted ASCENDING by price (best ask = asks[0]).
	asks []Level
	// bids — sorted DESCENDING by price (best bid = bids[0]).
	bids []Level
	// tsMs — last applied push timestamp (ms).
	tsMs int64
	// checksum — CRC32 echoed by the last successful push. Useful
	// for debugging; not used by the engine itself.
	checksum int64
	// dirty — true between a CRC mismatch and the arrival of the
	// next snapshot. While dirty, ApplyUpdate is a no-op and
	// Snapshot returns the symbol-only zero value.
	dirty bool
}

// NewEngine constructs an empty engine. maxDepth caps the stored side
// length; 0 falls back to 200 (matches the SDK Orderbook.MaxDepth
// default).
func NewEngine(symbol string, maxDepth int) *Engine {
	if maxDepth <= 0 {
		maxDepth = 200
	}
	return &Engine{
		symbol:   symbol,
		maxDepth: maxDepth,
		asks:     make([]Level, 0, maxDepth),
		bids:     make([]Level, 0, maxDepth),
		dirty:    true, // requires a snapshot before update is meaningful
	}
}

// Reset drops state. Called by the ws.Conn supervisor before every
// (re)subscribe so a stale push that arrived on the previous socket
// cannot race with the engine.
func (e *Engine) Reset() {
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
// but the new state is committed regardless — "the server says this
// is the truth" trumps the engine's local recomputation.
func (e *Engine) ApplySnapshot(asks, bids []Level, tsMs, checksum int64) error {
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
	var got int32 = ComputeCRC(e.asks, e.bids)
	if int32(checksum) != got {
		// Snapshot mismatch — keep the state (it's still the most
		// authoritative thing we have) but surface the discrepancy.
		return ErrChecksum
	}
	return nil
}

// ApplyUpdate merges an incremental delta. Returns ErrDirty when called
// before the first snapshot (or right after a CRC mismatch),
// ErrChecksum when the recomputed CRC disagrees with the server-shipped
// value.
func (e *Engine) ApplyUpdate(asks, bids []Level, tsMs, checksum int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.dirty {
		return ErrDirty
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
	var got int32 = ComputeCRC(e.asks, e.bids)
	if int32(checksum) != got {
		// Mark dirty so subsequent updates are dropped until the
		// next snapshot lands.
		e.dirty = true
		return ErrChecksum
	}
	return nil
}

// Snapshot returns the current engine state as a roottypes
// OrderBookSnapshot. The slices are copies — callers may retain them
// across calls without worrying about mutation.
func (e *Engine) Snapshot() roottypes.OrderBookSnapshot {
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
func (e *Engine) IsDirty() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dirty
}

// ---------------------------------------------------------------------
// Wire decoding.
// ---------------------------------------------------------------------

// ParseLevels converts a slice of [price, size] string tuples (the
// shape Bitget ships on the "books" channel) into engine Levels with
// both decimal-parsed values and verbatim wire strings preserved for
// CRC reproduction. Empty / 1-element rows are rejected with the same
// error so callers can wrap the message uniformly.
func ParseLevels(rows [][]string) ([]Level, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	var out []Level = make([]Level, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var row []string = rows[i]
		if len(row) < 2 {
			return nil, errors.New("orderbook: level must be [price, size]")
		}
		var price, size decimal.Decimal
		var err error
		price, err = decimal.NewFromString(row[0])
		if err != nil {
			return nil, err
		}
		size, err = decimal.NewFromString(row[1])
		if err != nil {
			return nil, err
		}
		out = append(out, Level{
			Price:    price,
			Size:     size,
			PriceStr: row[0],
			SizeStr:  row[1],
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Sorted-slice helpers.
// ---------------------------------------------------------------------

// sortAsks sorts the slice ASCENDING by price.
func sortAsks(levels []Level) {
	// Insertion sort: snapshot input is mostly already sorted, so
	// the O(N) best case beats O(N log N) of sort.Slice for the
	// hot path.
	var i int
	for i = 1; i < len(levels); i++ {
		var j int
		for j = i; j > 0 && levels[j].Price.LessThan(levels[j-1].Price); j-- {
			levels[j], levels[j-1] = levels[j-1], levels[j]
		}
	}
}

// sortBids sorts the slice DESCENDING by price.
func sortBids(levels []Level) {
	var i int
	for i = 1; i < len(levels); i++ {
		var j int
		for j = i; j > 0 && levels[j].Price.GreaterThan(levels[j-1].Price); j-- {
			levels[j], levels[j-1] = levels[j-1], levels[j]
		}
	}
}

// upsertAsk inserts or replaces an ask level (ascending). size==0 →
// remove. Linear in worst case; for 200-level depth this is well
// below the per-frame budget on x86_64.
func upsertAsk(levels []Level, lvl Level) []Level {
	var i int
	for i = 0; i < len(levels); i++ {
		var cmp int = lvl.Price.Cmp(levels[i].Price)
		if cmp == 0 {
			if lvl.Size.IsZero() {
				return append(levels[:i], levels[i+1:]...)
			}
			levels[i] = lvl
			return levels
		}
		if cmp < 0 {
			if lvl.Size.IsZero() {
				return levels // already absent
			}
			return insertAt(levels, i, lvl)
		}
	}
	if lvl.Size.IsZero() {
		return levels
	}
	return append(levels, lvl)
}

// upsertBid inserts or replaces a bid level (descending). Mirror of
// upsertAsk except for the comparison direction.
func upsertBid(levels []Level, lvl Level) []Level {
	var i int
	for i = 0; i < len(levels); i++ {
		var cmp int = lvl.Price.Cmp(levels[i].Price)
		if cmp == 0 {
			if lvl.Size.IsZero() {
				return append(levels[:i], levels[i+1:]...)
			}
			levels[i] = lvl
			return levels
		}
		if cmp > 0 {
			if lvl.Size.IsZero() {
				return levels
			}
			return insertAt(levels, i, lvl)
		}
	}
	if lvl.Size.IsZero() {
		return levels
	}
	return append(levels, lvl)
}

// insertAt inserts lvl at index i, shifting the tail right.
func insertAt(levels []Level, i int, lvl Level) []Level {
	levels = append(levels, Level{})
	copy(levels[i+1:], levels[i:])
	levels[i] = lvl
	return levels
}

// copyLevelsToWire converts engine levels to roottypes
// OrderBookLevels. Allocates a fresh slice so callers can retain the
// result across further engine mutations.
func copyLevelsToWire(src []Level) []roottypes.OrderBookLevel {
	var out []roottypes.OrderBookLevel = make([]roottypes.OrderBookLevel, len(src))
	var i int
	for i = 0; i < len(src); i++ {
		out[i] = roottypes.OrderBookLevel{
			Price: src[i].Price,
			Size:  src[i].Size,
		}
	}
	return out
}

// ---------------------------------------------------------------------
// CRC32.
// ---------------------------------------------------------------------

// ComputeCRC builds the colon-joined string from the top CRCDepth
// bid/ask pairs and runs CRC32(IEEE) on it. Returns int32 (signed) —
// Bitget echoes the value as a signed integer, and that's what the
// envelope shipped from the wire.
func ComputeCRC(asks, bids []Level) int32 {
	var nAsks int = len(asks)
	if nAsks > CRCDepth {
		nAsks = CRCDepth
	}
	var nBids int = len(bids)
	if nBids > CRCDepth {
		nBids = CRCDepth
	}
	var pairs int = nAsks
	if nBids > pairs {
		pairs = nBids
	}

	// Pre-size the builder to avoid reallocation on the typical
	// 25-pair input. Each pair contributes ~40 bytes worst case.
	var sb strings.Builder
	sb.Grow(pairs * 40)

	var i int
	for i = 0; i < pairs; i++ {
		if sb.Len() > 0 {
			sb.WriteByte(':')
		}
		if i < nBids {
			sb.WriteString(bids[i].PriceStr)
			sb.WriteByte(':')
			sb.WriteString(bids[i].SizeStr)
		}
		if i < nAsks {
			if i < nBids {
				sb.WriteByte(':')
			}
			sb.WriteString(asks[i].PriceStr)
			sb.WriteByte(':')
			sb.WriteString(asks[i].SizeStr)
		}
	}

	var crc uint32 = crc32.ChecksumIEEE([]byte(sb.String()))
	return int32(crc)
}
