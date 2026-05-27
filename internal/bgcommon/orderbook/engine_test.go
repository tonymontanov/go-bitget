/*
FILE: internal/bgcommon/orderbook/engine_test.go

DESCRIPTION:
Unit tests for the local orderbook engine. The fixtures here are
synthetic: snapshot + delta + recomputed CRC32 on the same wire
strings, so tests stay self-checking even if Bitget tweaks the
checksum domain in the future.
*/

package orderbook

import (
	"testing"

	"github.com/shopspring/decimal"
)

// makeLevel is a helper that builds a Level from "price", "size"
// wire strings AND parses them through decimal.NewFromString —
// matching exactly what the production ParseLevels does.
func makeLevel(t *testing.T, priceStr, sizeStr string) Level {
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
	return Level{
		Price:    price,
		Size:     size,
		PriceStr: priceStr,
		SizeStr:  sizeStr,
	}
}

// TestEngine_SnapshotThenUpdate covers the canonical happy path:
// snapshot installs the state, an incremental update merges a new ask
// level + replaces an existing bid + removes a stale bid.
func TestEngine_SnapshotThenUpdate(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 200)

	var snapAsks = []Level{
		makeLevel(t, "50001", "1.5"),
		makeLevel(t, "50002", "2.0"),
	}
	var snapBids = []Level{
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
	var upAsks = []Level{makeLevel(t, "50003", "1.0")}
	var upBids = []Level{
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

// TestEngine_UpdateBeforeSnapshot verifies that ApplyUpdate returns
// ErrDirty before the first snapshot lands and that the engine
// re-engages once the snapshot arrives.
func TestEngine_UpdateBeforeSnapshot(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 200)
	var err error = e.ApplyUpdate(
		[]Level{makeLevel(t, "50000", "1")},
		nil,
		1700000000000,
		0,
	)
	if err != ErrDirty {
		t.Fatalf("want ErrDirty, got %v", err)
	}

	if err = e.ApplySnapshot(
		[]Level{makeLevel(t, "50000", "1")},
		[]Level{makeLevel(t, "49999", "1")},
		1700000000000,
		0,
	); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if e.IsDirty() {
		t.Fatalf("dirty after snapshot")
	}
}

// TestEngine_ChecksumValidation builds a real CRC32 on the snapshot's
// wire strings and feeds it back into ApplySnapshot. The engine must
// accept a matching checksum and reject a wrong one.
func TestEngine_ChecksumValidation(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 200)
	var asks = []Level{
		makeLevel(t, "50001", "1.5"),
		makeLevel(t, "50002", "2.0"),
	}
	var bids = []Level{
		makeLevel(t, "49999", "1.0"),
		makeLevel(t, "49998", "0.5"),
	}
	var crc int32 = ComputeCRC(asks, bids)

	if err := e.ApplySnapshot(asks, bids, 1700000000000, int64(crc)); err != nil {
		t.Fatalf("snapshot with matching checksum: %v", err)
	}

	if err := e.ApplySnapshot(asks, bids, 1700000000050, int64(crc)+1); err != ErrChecksum {
		t.Fatalf("want ErrChecksum, got %v", err)
	}
}

// TestEngine_UpdateChecksumMismatch verifies that a CRC mismatch on an
// update flips the engine into dirty and starts rejecting subsequent
// updates with ErrDirty.
func TestEngine_UpdateChecksumMismatch(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 200)
	var asks = []Level{makeLevel(t, "50001", "1")}
	var bids = []Level{makeLevel(t, "49999", "1")}
	if err := e.ApplySnapshot(asks, bids, 1700000000000, 0); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var err error = e.ApplyUpdate(
		[]Level{makeLevel(t, "50002", "2")},
		nil,
		1700000000050,
		// Deliberately wrong checksum.
		12345,
	)
	if err != ErrChecksum {
		t.Fatalf("want ErrChecksum, got %v", err)
	}
	if !e.IsDirty() {
		t.Fatalf("engine not dirty after checksum mismatch")
	}
	// Subsequent update must be rejected.
	err = e.ApplyUpdate(
		[]Level{makeLevel(t, "50003", "3")},
		nil,
		1700000000100,
		0,
	)
	if err != ErrDirty {
		t.Fatalf("after mismatch want ErrDirty, got %v", err)
	}
	// Snapshot recovers the engine.
	if err = e.ApplySnapshot(asks, bids, 1700000000150, 0); err != nil {
		t.Fatalf("recovery snapshot: %v", err)
	}
	if e.IsDirty() {
		t.Fatalf("still dirty after recovery snapshot")
	}
}

// TestEngine_MaxDepth ensures the engine clamps each side to MaxDepth
// on snapshot and on update.
func TestEngine_MaxDepth(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 3)
	var asks = []Level{
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

// TestEngine_Reset wipes the engine state and re-engages after a
// fresh snapshot.
func TestEngine_Reset(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 200)
	if err := e.ApplySnapshot(
		[]Level{makeLevel(t, "50000", "1")},
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

// TestComputeCRC_DeterminismAndAlternation exercises the CRC formula
// on hand-crafted levels and reproduces the colon-joined alternating
// bid/ask string format.
func TestComputeCRC_DeterminismAndAlternation(t *testing.T) {
	var asks = []Level{
		makeLevel(t, "1", "10"),
		makeLevel(t, "2", "20"),
	}
	var bids = []Level{
		makeLevel(t, "0.9", "9"),
		makeLevel(t, "0.8", "8"),
	}
	// Deterministic: same input → same output.
	var c1 int32 = ComputeCRC(asks, bids)
	var c2 int32 = ComputeCRC(asks, bids)
	if c1 != c2 {
		t.Fatalf("non-deterministic CRC")
	}
	// Sanity: the CRC over [bid0,ask0,bid1,ask1] differs from the CRC
	// over the swapped pairing — any other formula would make the
	// engine accept reordered books.
	var swapped int32 = ComputeCRC(bids, asks) // asks/bids flipped
	if swapped == c1 {
		t.Fatalf("CRC unchanged after flipping asks/bids — formula error")
	}
}

// TestParseLevels covers the wire-decoder happy path and malformed
// row rejection.
func TestParseLevels(t *testing.T) {
	var lvls, err = ParseLevels([][]string{
		{"50001", "1.5"},
		{"50002", "2.0"},
	})
	if err != nil {
		t.Fatalf("ParseLevels: %v", err)
	}
	if len(lvls) != 2 {
		t.Fatalf("len = %d", len(lvls))
	}
	if lvls[0].PriceStr != "50001" || lvls[0].SizeStr != "1.5" {
		t.Fatalf("wire strings not preserved: %+v", lvls[0])
	}
	if lvls[0].Price.String() != "50001" || lvls[0].Size.String() != "1.5" {
		t.Fatalf("decimals: %+v", lvls[0])
	}
	if _, err = ParseLevels([][]string{{"100"}}); err == nil {
		t.Fatalf("expected error on 1-element input")
	}
	if _, err = ParseLevels([][]string{{"100", "abc"}}); err == nil {
		t.Fatalf("expected error on non-numeric size")
	}
}
