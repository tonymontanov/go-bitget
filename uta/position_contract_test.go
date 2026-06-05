/*
FILE: uta/position_contract_test.go

DESCRIPTION:
Contract tests for the V3 UTA Position sub-client: current positions
(hedge posSide), position history (page + cursor), the max-open-available
POST probe and the ADL rank — parsing + required-field guards.
*/

package uta

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func TestContract_Position_CurrentAndHistory(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v3/position/current-position": `{"code":"00000","msg":"success","data":{"list":[{"category":"USDT-FUTURES","symbol":"BTCUSDT","marginCoin":"USDT","holdMode":"hedge_mode","posSide":"long","marginMode":"crossed","positionBalance":"300","available":"0.005","frozen":"0","total":"0.005","leverage":"20","curRealisedPnl":"1.2","avgPrice":"60000","positionStatus":"normal","unrealisedPnl":"5","liquidationPrice":"40000","mmr":"0.004","profitRate":"0.02","markPrice":"61000","breakEvenPrice":"60050","totalFunding":"-0.5","openFeeTotal":"-0.18","closeFeeTotal":"0","createdTime":"1700000000000","updatedTime":"1700000001000"}]}}`,
		"/api/v3/position/history-position": `{"code":"00000","msg":"success","data":{"list":[{"positionId":"p1","category":"USDT-FUTURES","symbol":"BTCUSDT","marginCoin":"USDT","holdMode":"hedge_mode","posSide":"long","marginMode":"crossed","openPriceAvg":"59000","closePriceAvg":"61000","openTotalPos":"0.01","closeTotalPos":"0.01","cumRealisedPnl":"20","netProfit":"19.5","totalFunding":"-0.3","openFeeTotal":"-0.2","closeFeeTotal":"-0.2","createdTime":"1699990000000","updatedTime":"1700000000000"}],"cursor":"ph9"}}`,
	}
	var sawPosSide string
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v3/position/current-position" {
			sawPosSide = r.URL.Query().Get("posSide")
		}
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var pos, err = uc.Position().GetCurrentPositions(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT", "long")
	if err != nil || len(pos) != 1 {
		t.Fatalf("GetCurrentPositions: %v %+v", err, pos)
	}
	if sawPosSide != "long" {
		t.Errorf("posSide not forwarded: %q", sawPosSide)
	}
	if pos[0].PosSide != "long" || !pos[0].UnrealisedPnl.Equal(decv("5")) || !pos[0].Leverage.Equal(decv("20")) || pos[0].CreatedTime != 1700000000000 {
		t.Fatalf("unexpected position: %+v", pos[0])
	}

	var hist, cur, herr = uc.Position().GetPositionHistory(ctx, OrdersQuery{Category: utatypes.CategoryUSDTFutures})
	if herr != nil || len(hist) != 1 || cur != "ph9" || !hist[0].NetProfit.Equal(decv("19.5")) || hist[0].PositionID != "p1" {
		t.Fatalf("GetPositionHistory: %v %+v cur=%q", herr, hist, cur)
	}

	// Guards.
	if _, err = uc.Position().GetCurrentPositions(ctx, "", "", ""); err == nil {
		t.Error("GetCurrentPositions(no category): want guard")
	}
	if _, _, err := uc.Position().GetPositionHistory(ctx, OrdersQuery{}); err == nil {
		t.Error("GetPositionHistory(no category): want guard")
	}
}

func TestContract_Position_MaxOpenAndAdl(t *testing.T) {
	t.Parallel()
	var maxBody []byte
	var _, client = mockBitgetDynamic(t, false, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/account/max-open-available":
			maxBody = body
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"available":"5000","maxOpen":"0.08","buyOpenCost":"600","sellOpenCost":"600","maxBuyOpen":"0.08","maxSellOpen":"0.08"}}`))
		case "/api/v3/position/adlRank":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":[{"symbol":"BTCUSDT","marginCoin":"USDT","adlRank":"3","holdSide":"long"}]}`))
		}
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var mo, err = uc.Position().GetMaxOpenAvailable(ctx, MaxOpenAvailableRequest{
		Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", OrderType: "limit", Side: "buy", Price: "60000",
	})
	if err != nil || !mo.MaxBuyOpen.Equal(decv("0.08")) || !mo.Available.Equal(decv("5000")) {
		t.Fatalf("GetMaxOpenAvailable: %v %+v", err, mo)
	}
	var mb map[string]any
	if jerr := json.Unmarshal(maxBody, &mb); jerr != nil || mb["orderType"] != "limit" || mb["side"] != "buy" {
		t.Fatalf("unexpected max-open body: %v (%v)", mb, jerr)
	}

	var adl, aerr = uc.Position().GetAdlRank(ctx)
	if aerr != nil || len(adl) != 1 || adl[0].AdlRank != "3" || adl[0].HoldSide != "long" {
		t.Fatalf("GetAdlRank: %v %+v", aerr, adl)
	}

	// Guards.
	if _, err = uc.Position().GetMaxOpenAvailable(ctx, MaxOpenAvailableRequest{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT"}); err == nil {
		t.Error("GetMaxOpenAvailable(no orderType/side): want guard")
	}
}
