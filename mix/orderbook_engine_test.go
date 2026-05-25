/*
FILE: mix/orderbook_engine_test.go

DESCRIPTION:
Unit tests for the local orderbook engine. The fixtures here are
synthetic: snapshot + delta + recomputed CRC32 on the same wire
strings, so tests stay self-checking even if Bitget tweaks the
checksum domain in the future.
*/

package mix

import (
	"testing"

	"github.com/shopspring/decimal"
)

// makeLevel is a helper that builds an orderbookLevel from "price",
// "size" wire strings AND parses them through decimal.NewFromString —
// matching exactly what the production parseLevels does.
func makeLevel(t *testing.T, priceStr, sizeStr string) orderbookLevel {
	t.Helper()
	var price, size decimal.Decimal
	var err error
	price, err = decimal.NewFromString(priceStr)
	if err != nil {
		t.Fatalf("decimal price: %v", err)
	}
	size, err = decimal.NewFromString(sizeStr)
	if err != nil {
		t.Fatalf("decimal size: %v", err)
	}
	return orderbookLevel{
		price:    price,
		size:     size,
		priceStr: priceStr,
		sizeStr:  sizeStr,
	}
}

// TestOrderbookEngine_SnapshotThenUpdate covers the canonical happy path:
// snapshot installs the state, an incremental update merges a new ask
// level + replaces an existing bid + removes a stale bid.
func TestOrderbookEngine_SnapshotThenUpdate(t *testing.T) {
	var e *orderbookEngine = newOrderbookEngine("BTCUSDT", 200)

	var snapAsks = []orderbookLevel{
		makeLevel(t, "50001", "1.5"),
		makeLevel(t, "50002", "2.0"),
	}
	var snapBids = []orderbookLevel{
		makeLevel(t, "49999", "1.0"),
		makeLevel(t, "49998", "0.5"),
	}

	if err := e.ApplySnapshot(snapAsks, snapBids, 1700000000000, 0); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	if e.IsDirty() {
		t.Fatalf("engine still dirty after snapshot")
	}
	var snap = e.Snapshot()
	if snap.Symbol != "BTCUSDT" {
		t.Fatalf("symbol = %q", snap.Symbol)
	}
	if len(snap.Asks) != 2 || len(snap.Bids) != 2 {
		t.Fatalf("len asks=%d bids=%d", len(snap.Asks), len(snap.Bids))
	}
	if snap.Asks[0].Price.String() != "50001" {
		t.Fatalf("best ask = %s", snap.Asks[0].Price.String())
	}
	if snap.Bids[0].Price.String() != "49999" {
		t.Fatalf("best bid = %s", snap.Bids[0].Price.String())
	}

	// Update: add ask 50003@1.0, replace bid 49999 → 0.7, remove bid 49998.
	var upAsks = []orderbookLevel{makeLevel(t, "50003", "1.0")}
	var upBids = []orderbookLevel{
		makeLevel(t, "49999", "0.7"),
		makeLevel(t, "49998", "0"),
	}
	if err := e.ApplyUpdate(upAsks, upBids, 1700000000050, 0); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	snap = e.Snapshot()
	if len(snap.Asks) != 3 {
		t.Fatalf("asks after update: %d", len(snap.Asks))
	}
	if snap.Asks[2].Price.String() != "50003" {
		t.Fatalf("3rd ask = %s", snap.Asks[2].Price.String())
	}
	if len(snap.Bids) != 1 {
		t.Fatalf("bids after update: %d", len(snap.Bids))
	}
	if snap.Bids[0].Price.String() != "49999" {
		t.Fatalf("bid[0] = %s", snap.Bids[0].Price.String())
	}
	if snap.Bids[0].Size.String() != "0.7" {
		t.Fatalf("bid[0] size = %s", snap.Bids[0].Size.String())
	}
}

// TestOrderbookEngine_UpdateBeforeSnapshot verifies that ApplyUpdate
// returns errOrderbookDirty before the first snapshot lands and that
// the engine re-engages once the snapshot arrives.
func TestOrderbookEngine_UpdateBeforeSnapshot(t *testing.T) {
	var e *orderbookEngine = newOrderbookEngine("BTCUSDT", 200)
	var err error = e.ApplyUpdate(
		[]orderbookLevel{makeLevel(t, "50000", "1")},
		nil,
		1700000000000,
		0,
	)
	if err != errOrderbookDirty {
		t.Fatalf("want errOrderbookDirty, got %v", err)
	}

	if err = e.ApplySnapshot(
		[]orderbookLevel{makeLevel(t, "50000", "1")},
		[]orderbookLevel{makeLevel(t, "49999", "1")},
		1700000000000,
		0,
	); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if e.IsDirty() {
		t.Fatalf("dirty after snapshot")
	}
}

// TestOrderbookEngine_ChecksumValidation builds a real CRC32 on the
// snapshot's wire strings and feeds it back into ApplySnapshot. The
// engine must accept a matching checksum and reject a wrong one.
func TestOrderbookEngine_ChecksumValidation(t *testing.T) {
	var e *orderbookEngine = newOrderbookEngine("BTCUSDT", 200)
	var asks = []orderbookLevel{
		makeLevel(t, "50001", "1.5"),
		makeLevel(t, "50002", "2.0"),
	}
	var bids = []orderbookLevel{
		makeLevel(t, "49999", "1.0"),
		makeLevel(t, "49998", "0.5"),
	}
	var crc int32 = computeOrderbookCRC(asks, bids)

	if err := e.ApplySnapshot(asks, bids, 1700000000000, int64(crc)); err != nil {
		t.Fatalf("snapshot with matching checksum: %v", err)
	}

	if err := e.ApplySnapshot(asks, bids, 1700000000050, int64(crc)+1); err != errOrderbookChecksum {
		t.Fatalf("want errOrderbookChecksum, got %v", err)
	}
}

// TestOrderbookEngine_UpdateChecksumMismatch verifies that a CRC
// mismatch on an update flips the engine into dirty and starts
// rejecting subsequent updates with errOrderbookDirty.
func TestOrderbookEngine_UpdateChecksumMismatch(t *testing.T) {
	var e *orderbookEngine = newOrderbookEngine("BTCUSDT", 200)
	var asks = []orderbookLevel{makeLevel(t, "50001", "1")}
	var bids = []orderbookLevel{makeLevel(t, "49999", "1")}
	if err := e.ApplySnapshot(asks, bids, 1700000000000, 0); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var err error = e.ApplyUpdate(
		[]orderbookLevel{makeLevel(t, "50002", "2")},
		nil,
		1700000000050,
		// Deliberately wrong checksum.
		12345,
	)
	if err != errOrderbookChecksum {
		t.Fatalf("want errOrderbookChecksum, got %v", err)
	}
	if !e.IsDirty() {
		t.Fatalf("engine not dirty after checksum mismatch")
	}
	// Subsequent update must be rejected.
	err = e.ApplyUpdate(
		[]orderbookLevel{makeLevel(t, "50003", "3")},
		nil,
		1700000000100,
		0,
	)
	if err != errOrderbookDirty {
		t.Fatalf("after mismatch want errOrderbookDirty, got %v", err)
	}
	// Snapshot recovers the engine.
	if err = e.ApplySnapshot(asks, bids, 1700000000150, 0); err != nil {
		t.Fatalf("recovery snapshot: %v", err)
	}
	if e.IsDirty() {
		t.Fatalf("still dirty after recovery snapshot")
	}
}

// TestOrderbookEngine_MaxDepth ensures the engine clamps each side to
// MaxDepth on snapshot and on update.
func TestOrderbookEngine_MaxDepth(t *testing.T) {
	var e *orderbookEngine = newOrderbookEngine("BTCUSDT", 3)
	var asks = []orderbookLevel{
		makeLevel(t, "50001", "1"),
		makeLevel(t, "50002", "1"),
		makeLevel(t, "50003", "1"),
		makeLevel(t, "50004", "1"),
		makeLevel(t, "50005", "1"),
	}
	if err := e.ApplySnapshot(asks, nil, 1700000000000, 0); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var snap = e.Snapshot()
	if len(snap.Asks) != 3 {
		t.Fatalf("clamped asks len = %d", len(snap.Asks))
	}
	if snap.Asks[0].Price.String() != "50001" {
		t.Fatalf("best ask = %s", snap.Asks[0].Price.String())
	}
}

// TestOrderbookEngine_Reset wipes the engine state and re-engages
// after a fresh snapshot.
func TestOrderbookEngine_Reset(t *testing.T) {
	var e *orderbookEngine = newOrderbookEngine("BTCUSDT", 200)
	if err := e.ApplySnapshot(
		[]orderbookLevel{makeLevel(t, "50000", "1")},
		nil,
		1700000000000,
		0,
	); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	e.Reset()
	if !e.IsDirty() {
		t.Fatalf("not dirty after reset")
	}
	var snap = e.Snapshot()
	if len(snap.Asks) != 0 || len(snap.Bids) != 0 {
		t.Fatalf("reset did not wipe state: asks=%d bids=%d", len(snap.Asks), len(snap.Bids))
	}
}

// TestComputeOrderbookCRC_DeterminismAndAlternation exercises the CRC
// formula on hand-crafted levels and reproduces the colon-joined
// alternating bid/ask string format.
func TestComputeOrderbookCRC_DeterminismAndAlternation(t *testing.T) {
	var asks = []orderbookLevel{
		makeLevel(t, "1", "10"),
		makeLevel(t, "2", "20"),
	}
	var bids = []orderbookLevel{
		makeLevel(t, "0.9", "9"),
		makeLevel(t, "0.8", "8"),
	}
	// Deterministic: same input → same output.
	var c1 int32 = computeOrderbookCRC(asks, bids)
	var c2 int32 = computeOrderbookCRC(asks, bids)
	if c1 != c2 {
		t.Fatalf("non-deterministic CRC")
	}
	// Sanity: the CRC over [bid0,ask0,bid1,ask1] differs from the CRC
	// over the swapped pairing — any other formula would make the
	// engine accept reordered books.
	var swapped int32 = computeOrderbookCRC(bids, asks) // asks/bids flipped
	if swapped == c1 {
		t.Fatalf("CRC unchanged after flipping asks/bids — formula error")
	}
}
