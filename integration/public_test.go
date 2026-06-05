//go:build integration

/*
FILE: integration/public_test.go

DESCRIPTION:
Live, UNSIGNED V3 UTA public market-data checks against the real host.
These run without credentials and validate that the documented wire shapes
decode cleanly (and confirm a couple of the Phase-7 open items: candle
turnover column, instrument field set).
*/

package integration

import (
	"testing"

	"github.com/tonymontanov/go-bitget/v2/uta"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func TestLive_UTA_ServerTime(t *testing.T) {
	var u = utaClient(t)
	var ms, err = u.Public().GetServerTime(testCtx(t))
	if err != nil {
		t.Fatalf("GetServerTime: %v", err)
	}
	if ms < 1_600_000_000_000 {
		t.Fatalf("implausible server time: %d", ms)
	}
	t.Logf("server time: %d", ms)
}

func TestLive_UTA_Instruments(t *testing.T) {
	var u = utaClient(t)
	var insts, err = u.Public().GetInstruments(testCtx(t), futures, "")
	if err != nil {
		t.Fatalf("GetInstruments: %v", err)
	}
	if len(insts) == 0 {
		t.Fatal("no instruments returned")
	}
	var found bool
	var i int
	for i = 0; i < len(insts); i++ {
		if insts[i].Symbol == symbol() {
			found = true
			t.Logf("%s: pricePrec=%d qtyPrec=%d minQty=%s maxLev=%s status=%s",
				insts[i].Symbol, insts[i].PricePrecision, insts[i].QuantityPrecision,
				insts[i].MinOrderQty, insts[i].MaxLeverage, insts[i].Status)
		}
	}
	if !found {
		t.Logf("note: %s not in instrument list (%d total)", symbol(), len(insts))
	}
}

func TestLive_UTA_TickerBookCandles(t *testing.T) {
	var u = utaClient(t)
	var ctx = testCtx(t)

	var tks, terr = u.Public().GetTickers(ctx, futures, symbol())
	if terr != nil {
		t.Fatalf("GetTickers: %v", terr)
	}
	if len(tks) == 0 || tks[0].LastPrice.IsZero() {
		t.Fatalf("bad ticker: %+v", tks)
	}
	t.Logf("ticker %s: last=%s mark=%s funding=%s", tks[0].Symbol, tks[0].LastPrice, tks[0].MarkPrice, tks[0].FundingRate)

	var ob, oerr = u.Public().GetOrderBook(ctx, futures, symbol(), 5)
	if oerr != nil {
		t.Fatalf("GetOrderBook: %v", oerr)
	}
	if len(ob.Asks) == 0 || len(ob.Bids) == 0 {
		t.Fatalf("empty book: %+v", ob)
	}
	if ob.Bids[0].Price.GreaterThanOrEqual(ob.Asks[0].Price) {
		t.Fatalf("crossed book: bid=%s ask=%s", ob.Bids[0].Price, ob.Asks[0].Price)
	}
	t.Logf("book %s: bestBid=%s bestAsk=%s", symbol(), ob.Bids[0].Price, ob.Asks[0].Price)

	var ks, kerr = u.Public().GetCandles(ctx, uta.CandlesQuery{
		Category: futures, Symbol: symbol(), Interval: utatypes.Interval1H, Limit: 5,
	})
	if kerr != nil {
		t.Fatalf("GetCandles: %v", kerr)
	}
	if len(ks) == 0 {
		t.Fatal("no candles")
	}
	// Open item: confirm the turnover column is populated.
	if ks[0].Turnover.IsZero() {
		t.Logf("note: candle turnover is zero/absent (open item) — first row: %+v", ks[0])
	} else {
		t.Logf("candle[0]: t=%d o=%s h=%s l=%s c=%s vol=%s turnover=%s",
			ks[0].TimeMs, ks[0].Open, ks[0].High, ks[0].Low, ks[0].Close, ks[0].Volume, ks[0].Turnover)
	}
}

func TestLive_UTA_FundingAndTiers(t *testing.T) {
	var u = utaClient(t)
	var ctx = testCtx(t)

	var fr, ferr = u.Public().GetCurrentFundingRate(ctx, symbol())
	if ferr != nil {
		t.Fatalf("GetCurrentFundingRate: %v", ferr)
	}
	t.Logf("funding %s: rate=%s interval=%s", fr.Symbol, fr.FundingRate, fr.FundingRateInterval)

	var tiers, perr = u.Public().GetPositionTier(ctx, futures, symbol(), "")
	if perr != nil {
		t.Fatalf("GetPositionTier: %v", perr)
	}
	t.Logf("position tiers for %s: %d", symbol(), len(tiers))

	var oi, oerr = u.Public().GetOpenInterest(ctx, futures, symbol())
	if oerr != nil {
		t.Fatalf("GetOpenInterest: %v", oerr)
	}
	t.Logf("open interest rows: %d (ts=%d)", len(oi.List), oi.TimeMs)
}
