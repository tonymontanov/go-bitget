//go:build integration

/*
FILE: integration/mix_public_test.go

DESCRIPTION:
Live, UNSIGNED V2 MIX public market-data checks against the real host.
Exists because the contract fixtures were written from the docs and the
docs lie about /merge-depth: levels are documented as quoted strings but
the live wire ships bare JSON numbers. A string-typed payload decoded the
fixture fine and failed on every production call (desk session
19.09.2026 ran with an empty order book).
*/

package integration

import (
	"testing"

	"github.com/tonymontanov/go-bitget/v2/mix"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

func TestLive_Mix_OrderBook(t *testing.T) {
	var m *mix.Client = mix.NewClient(newClient(t))
	if m == nil {
		t.Fatal("mix.NewClient returned nil")
	}

	var snap roottypes.OrderBookSnapshot
	var err error
	snap, err = m.MarketData().GetOrderBook(testCtx(t), symbol(), 15)
	if err != nil {
		t.Fatalf("GetOrderBook: %v", err)
	}
	if len(snap.Asks) == 0 || len(snap.Bids) == 0 {
		t.Fatalf("empty book: asks=%d bids=%d", len(snap.Asks), len(snap.Bids))
	}
	// The venue silently ignores an unknown `limit` and returns 100 rows
	// (the SDK used to send "max15"): more rows than asked for means the
	// limit spelling regressed.
	if len(snap.Asks) > 15 || len(snap.Bids) > 15 {
		t.Fatalf("limit not honoured: asked 15, got asks=%d bids=%d", len(snap.Asks), len(snap.Bids))
	}
	if snap.Bids[0].Price.GreaterThanOrEqual(snap.Asks[0].Price) {
		t.Fatalf("crossed book: bid=%s ask=%s", snap.Bids[0].Price, snap.Asks[0].Price)
	}
	if snap.Asks[0].Size.IsZero() || snap.Bids[0].Size.IsZero() {
		t.Fatalf("zero size at the touch: bid=%s ask=%s", snap.Bids[0].Size, snap.Asks[0].Size)
	}
	t.Logf("book %s: bestBid=%s x %s bestAsk=%s x %s levels=%d/%d ts=%d",
		symbol(), snap.Bids[0].Price, snap.Bids[0].Size, snap.Asks[0].Price, snap.Asks[0].Size,
		len(snap.Bids), len(snap.Asks), snap.TsMs)
}
