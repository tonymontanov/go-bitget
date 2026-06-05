/*
FILE: copytrading/futures_trader_contract_test.go

DESCRIPTION:
Contract tests for the futures TRADER order surface (M3a):
order-current-track / order-history-track / order-total-detail /
order-modify-tpsl / order-close-positions. Fixtures are hand-derived
from the Bitget V2 mix-trader docs.

KEY INVARIANTS:

  - every call carries the pinned productType (USDT-FUTURES here);
  - current/history-track walk the idLessThan/endId cursor and parse the
    preset TP/SL and realised-PnL fields;
  - history-track tolerates both openPriceAvg/openAvgPrice spellings;
  - order-total-detail keeps the currency-prefixed totalpl as a raw
    string and parses the weekly/monthly series;
  - modify-tpsl requires trackingNo + at least one price (client guard);
  - close-positions returns the targeted {trackingNo,symbol,productType}.
*/

package copytrading

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	copytypes "github.com/tonymontanov/go-bitget/v2/copytrading/types"
)

func TestContract_FuturesTrader_GetCurrentOrders(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"endId":"",
			"trackingList":[{
				"trackingNo":"1231231231","openOrderId":"123123123123","symbol":"BTCUSDT","posSide":"long",
				"openLeverage":"20","openPriceAvg":"26248.9","openTime":"1695801595658","openSize":"0.1",
				"presetStopSurplusPrice":"27561.34","presetStopLossPrice":"25855.16","openFee":"-2.62489","followCount":"1"
			}]
		}
	}`

	var seenPath, seenProductType, seenSymbol string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/order-current-track": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenPath = r.URL.Path
		seenProductType = r.URL.Query().Get("productType")
		seenSymbol = r.URL.Query().Get("symbol")
	})

	var orders, err = copyClient(client).FuturesTrader().GetCurrentOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetCurrentOrders: %v", err)
	}
	if seenPath != "/api/v2/copy/mix-trader/order-current-track" {
		t.Errorf("path: got %q", seenPath)
	}
	if seenProductType != "USDT-FUTURES" || seenSymbol != "BTCUSDT" {
		t.Errorf("query: got productType=%q symbol=%q", seenProductType, seenSymbol)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if orders[0].TrackingNo != "1231231231" || orders[0].PosSide != "long" {
		t.Errorf("row: got %q/%q", orders[0].TrackingNo, orders[0].PosSide)
	}
	if !orders[0].PresetStopSurplusPrice.Equal(decimal.RequireFromString("27561.34")) {
		t.Errorf("presetTP: got %s", orders[0].PresetStopSurplusPrice)
	}
	if orders[0].FollowCount != 1 {
		t.Errorf("followCount: got %d", orders[0].FollowCount)
	}
	if orders[0].OpenTimeMs != 1695801595658 {
		t.Errorf("openTime: got %d", orders[0].OpenTimeMs)
	}
}

func TestContract_FuturesTrader_GetHistoryOrders_AvgPriceFallback(t *testing.T) {
	t.Parallel()
	// Uses the legacy openAvgPrice/closeAvgPrice spellings.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"endId":"",
			"trackingList":[{
				"trackingNo":"123123","symbol":"BTCUSDT","openOrderId":"o1","closeOrderId":"c1",
				"productType":"USDT-FUTURES","posSide":"long","openLeverage":"20",
				"openAvgPrice":"32000.5","openTime":"1695035398292","openSize":"0.1","closeSize":"0.1",
				"closeTime":"1695176679764","closeAvgPrice":"32000.0","stopType":"loss",
				"achievedPL":"-0.05","openFee":"-3.2","closeFee":"-3.2","cTime":"1695035398292"
			}]
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/order-history-track": fixture}, nil)

	var orders, err = copyClient(client).FuturesTrader().GetHistoryOrders(context.Background(), "BTCUSDT", 0, 0)
	if err != nil {
		t.Fatalf("GetHistoryOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if !orders[0].OpenPriceAvg.Equal(decimal.RequireFromString("32000.5")) {
		t.Errorf("openPriceAvg fallback: got %s", orders[0].OpenPriceAvg)
	}
	if !orders[0].ClosePriceAvg.Equal(decimal.RequireFromString("32000.0")) {
		t.Errorf("closePriceAvg fallback: got %s", orders[0].ClosePriceAvg)
	}
	if orders[0].StopType != "loss" {
		t.Errorf("stopType: got %q", orders[0].StopType)
	}
	if !orders[0].AchievedPL.Equal(decimal.RequireFromString("-0.05")) {
		t.Errorf("achievedPL: got %s", orders[0].AchievedPL)
	}
}

func TestContract_FuturesTrader_GetOrderSummary(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"roi":"19.38","tradingOrderNum":"105","totalFollowerNum":"134","currentFollowerNum":"4",
			"totalpl":"$46.95","gainNum":"59","lossNum":"46","winRate":"56.123",
			"tradingPairsAvailableList":["BTCUSDT"],
			"lastWeekRoiList":[{"rate":"-14.130944","ctime":"1695139200000"}],
			"lastWeekProfitList":[],
			"lastMonthRoiList":[{"rate":"-14.130944","ctime":"1693152000000"}],
			"lastMonthProfitList":[{"amount":"12.5","ctime":"1693152000000"}],
			"totalEquity":"1776.03"
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/order-total-detail": fixture}, nil)

	var sum, err = copyClient(client).FuturesTrader().GetOrderSummary(context.Background())
	if err != nil {
		t.Fatalf("GetOrderSummary: %v", err)
	}
	if !sum.ROI.Equal(decimal.RequireFromString("19.38")) || !sum.WinRate.Equal(decimal.RequireFromString("56.123")) {
		t.Errorf("roi/winRate: got %s/%s", sum.ROI, sum.WinRate)
	}
	if sum.TotalPL != "$46.95" {
		t.Errorf("totalPL raw: got %q", sum.TotalPL)
	}
	if sum.TradingOrderNum != 105 || sum.CurrentFollowerNum != 4 {
		t.Errorf("counts: got %d/%d", sum.TradingOrderNum, sum.CurrentFollowerNum)
	}
	if !sum.TotalEquity.Equal(decimal.RequireFromString("1776.03")) {
		t.Errorf("totalEquity: got %s", sum.TotalEquity)
	}
	if len(sum.TradingPairsAvailable) != 1 || sum.TradingPairsAvailable[0] != "BTCUSDT" {
		t.Errorf("pairs: got %v", sum.TradingPairsAvailable)
	}
	if len(sum.LastWeekROI) != 1 || !sum.LastWeekROI[0].Rate.Equal(decimal.RequireFromString("-14.130944")) {
		t.Errorf("lastWeekROI: got %v", sum.LastWeekROI)
	}
	if sum.LastWeekROI[0].TimeMs != 1695139200000 {
		t.Errorf("lastWeekROI time: got %d", sum.LastWeekROI[0].TimeMs)
	}
	if len(sum.LastWeekProfit) != 0 {
		t.Errorf("lastWeekProfit: want empty, got %v", sum.LastWeekProfit)
	}
	if len(sum.LastMonthProfit) != 1 || !sum.LastMonthProfit[0].Amount.Equal(decimal.RequireFromString("12.5")) {
		t.Errorf("lastMonthProfit: got %v", sum.LastMonthProfit)
	}
}

func TestContract_FuturesTrader_ModifyTPSL(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":"success"}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/order-modify-tpsl": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var err = copyClient(client).FuturesTrader().ModifyTPSL(context.Background(), copytypes.TraderModifyTPSLRequest{
		TrackingNo:       "1",
		Symbol:           "BTCUSDT",
		StopSurplusPrice: "36333",
	})
	if err != nil {
		t.Fatalf("ModifyTPSL: %v", err)
	}
	if body["productType"] != "USDT-FUTURES" || body["trackingNo"] != "1" {
		t.Errorf("required body: got %v", body)
	}
	if body["stopSurplusPrice"] != "36333" {
		t.Errorf("stopSurplusPrice: got %v", body["stopSurplusPrice"])
	}
	if _, present := body["stopLossPrice"]; present {
		t.Errorf("empty stopLossPrice must be omitted: %v", body)
	}
}

func TestContract_FuturesTrader_ModifyTPSL_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var tc = copyClient(client).FuturesTrader()

	if err := tc.ModifyTPSL(context.Background(), copytypes.TraderModifyTPSLRequest{StopSurplusPrice: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty trackingNo: want InvalidRequest, got %v", err)
	}
	if err := tc.ModifyTPSL(context.Background(), copytypes.TraderModifyTPSLRequest{TrackingNo: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("no prices: want InvalidRequest, got %v", err)
	}
}

func TestContract_FuturesTrader_ClosePositions(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[
			{"trackingNo":"123","symbol":"ETHUSDT","productType":"USDT-FUTURES"},
			{"trackingNo":"32123","symbol":"BTCUSDT","productType":"USDT-FUTURES"}
		]
	}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/order-close-positions": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var closed, err = copyClient(client).FuturesTrader().ClosePositions(context.Background(), copytypes.TraderCloseRequest{
		TrackingNo: "123",
		Symbol:     "ETHUSDT",
	})
	if err != nil {
		t.Fatalf("ClosePositions: %v", err)
	}
	if body["productType"] != "USDT-FUTURES" || body["trackingNo"] != "123" {
		t.Errorf("body: got %v", body)
	}
	if len(closed) != 2 {
		t.Fatalf("closed: want 2, got %d", len(closed))
	}
	if closed[0].TrackingNo != "123" || closed[0].Symbol != "ETHUSDT" || closed[0].ProductType != "USDT-FUTURES" {
		t.Errorf("row0: got %+v", closed[0])
	}
}

func TestContract_FuturesTrader_ClosePositions_AllOmitsSelector(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":[]}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/order-close-positions": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var _, err = copyClient(client).FuturesTrader().ClosePositions(context.Background(), copytypes.TraderCloseRequest{})
	if err != nil {
		t.Fatalf("ClosePositions(all): %v", err)
	}
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType: got %v", body["productType"])
	}
	if _, ok := body["trackingNo"]; ok {
		t.Errorf("trackingNo must be omitted when empty: %v", body)
	}
	if _, ok := body["symbol"]; ok {
		t.Errorf("symbol must be omitted when empty: %v", body)
	}
}
