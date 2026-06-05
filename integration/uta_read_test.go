//go:build integration

/*
FILE: integration/uta_read_test.go

DESCRIPTION:
Live, SIGNED, READ-ONLY checks for the V3 UTA account / trade / position /
strategy surface. They require a (DEMO) API key triple and never mutate
state. Reachability errors that depend on account eligibility are logged
rather than failed where appropriate, but decode / transport failures fail
the test.
*/

package integration

import (
	"testing"

	"github.com/tonymontanov/go-bitget/v2/uta"
)

func TestLive_UTA_AccountAssetsSettings(t *testing.T) {
	requireCreds(t)
	var u = utaClient(t)
	var ctx = testCtx(t)

	var assets, err = u.Account().GetAssets(ctx)
	if err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
	t.Logf("equity: %s USDT / %s BTC, coins=%d, mmr=%s mgnRatio=%s",
		assets.USDTEquity, assets.BTCEquity, len(assets.Assets), assets.MMR, assets.MgnRatio)

	var st, serr = u.Account().GetSettings(ctx)
	if serr != nil {
		t.Fatalf("GetSettings: %v", serr)
	}
	t.Logf("settings: uid=%s mode=%s level=%s holdMode=%s stp=%s symCfgs=%d coinCfgs=%d",
		st.UID, st.AccountMode, st.AccountLevel, st.HoldMode, st.STPMode, len(st.SymbolConfigs), len(st.CoinConfigs))

	// Open item: confirm account/info field set.
	var info, ierr = u.Account().GetInfo(ctx)
	if ierr != nil {
		t.Logf("[GetInfo note] %v", ierr)
	} else {
		t.Logf("info: userId=%s parentId=%q permType=%s perms=%v regis=%d",
			info.UserID, info.ParentID, info.PermType, info.Permissions, info.RegisTimeMs)
	}
}

func TestLive_UTA_FeeAndFunding(t *testing.T) {
	requireCreds(t)
	var u = utaClient(t)
	var ctx = testCtx(t)

	var fee, err = u.Account().GetFeeRate(ctx, futures, symbol())
	if err != nil {
		t.Fatalf("GetFeeRate: %v", err)
	}
	t.Logf("fee %s: maker=%s taker=%s", symbol(), fee.MakerFeeRate, fee.TakerFeeRate)

	var fa, aerr = u.Account().GetFundingAssets(ctx, "")
	if aerr != nil {
		t.Logf("[GetFundingAssets note] %v", aerr)
	} else {
		t.Logf("funding assets: %d coins", len(fa))
	}
}

func TestLive_UTA_OrdersFillsPositions(t *testing.T) {
	requireCreds(t)
	var u = utaClient(t)
	var ctx = testCtx(t)

	var open, _, err = u.Trade().GetUnfilledOrders(ctx, uta.OrdersQuery{Category: futures})
	if err != nil {
		t.Fatalf("GetUnfilledOrders: %v", err)
	}
	t.Logf("open orders: %d", len(open))

	var fills, _, ferr = u.Trade().GetFills(ctx, uta.FillsQuery{Limit: 10})
	if ferr != nil {
		t.Logf("[GetFills note] %v", ferr)
	} else {
		t.Logf("recent fills: %d", len(fills))
	}

	var pos, perr = u.Position().GetCurrentPositions(ctx, futures, "", "")
	if perr != nil {
		t.Fatalf("GetCurrentPositions: %v", perr)
	}
	t.Logf("current positions: %d", len(pos))
	var i int
	for i = 0; i < len(pos); i++ {
		t.Logf("  pos %s %s: total=%s avg=%s uPnL=%s lev=%s",
			pos[i].Symbol, pos[i].PosSide, pos[i].Total, pos[i].AvgPrice, pos[i].UnrealisedPnl, pos[i].Leverage)
	}

	var plans, serr = u.Strategy().GetUnfilledOrders(ctx, futures, "")
	if serr != nil {
		t.Logf("[Strategy.GetUnfilledOrders note] %v", serr)
	} else {
		t.Logf("open plan orders: %d", len(plans))
	}
}
