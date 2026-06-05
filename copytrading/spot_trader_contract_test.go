/*
FILE: copytrading/spot_trader_contract_test.go

DESCRIPTION:
Contract tests for the spot TRADER surface (M4a). Fixtures are
hand-derived from the Bitget V2 spot-trader docs.

KEY INVARIANTS:

  - NO call sends productType (spot is not product-type scoped);
  - current/history-track walk the idLessThan/endId cursor and parse the
    buy/sell spot order shapes;
  - order-total-detail parses headline stats + weekly/monthly series;
  - modify-tpsl requires trackingNo + >=1 price; close-tracking requires
    symbol + non-empty trackingNos (<=50);
  - config-query-settings parses the quote caps + trace symbols;
  - profit-summarys parses the by-date nested rollup;
  - profit-history-details (cursor) + profit-details (page-no) paginate.
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

func TestContract_SpotTrader_GetCurrentOrders_NoProductType(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","trackingList":[{
			"trackingNo":"1","orderId":"1","buyFillSize":"0.0317","buyDelegateSize":"0.0317190800000000",
			"buyPrice":"34870.7","unrealizedPL":"-0.79530295","buyTime":"1695729894873","buyFee":"-0.00001908",
			"unrealizedPLR":"-0.07","symbol":"BTCUSDT","stopLossPrice":null,"stopSurplusPrice":null,"followCount":"1"
		}]}
	}`

	var sawProductType bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/order-current-track": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.URL.Query().Get("productType") != "" {
			sawProductType = true
		}
	})

	var orders, err = copyClient(client).SpotTrader().GetCurrentOrders(context.Background(), "")
	if err != nil {
		t.Fatalf("GetCurrentOrders: %v", err)
	}
	if sawProductType {
		t.Error("spot must not send productType")
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if !orders[0].BuyPrice.Equal(decimal.RequireFromString("34870.7")) {
		t.Errorf("buyPrice: got %s", orders[0].BuyPrice)
	}
	if !orders[0].UnrealizedPL.Equal(decimal.RequireFromString("-0.79530295")) {
		t.Errorf("unrealizedPL: got %s", orders[0].UnrealizedPL)
	}
	// null TP/SL must parse to zero, not error.
	if !orders[0].StopLossPrice.IsZero() || !orders[0].StopSurplusPrice.IsZero() {
		t.Errorf("null TP/SL must be zero: %s/%s", orders[0].StopLossPrice, orders[0].StopSurplusPrice)
	}
	if orders[0].FollowCount != 1 {
		t.Errorf("followCount: got %d", orders[0].FollowCount)
	}
}

func TestContract_SpotTrader_GetHistoryOrders(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","trackingList":[{
			"trackingNo":"1","fillSize":"0.0317","buyPrice":"34869.2","sellPrice":"34865.5","achievedPL":"-0.78259",
			"buyTime":"1695728919902","sellTime":"1695729885589","buyFee":"-0.00001908","sellFee":"-0.66314181",
			"achievedPLR":"-0.07","symbol":"BTCUSDT","netProfit":"-1.44573","followCount":"1"
		}]}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/order-history-track": fixture}, nil)

	var orders, err = copyClient(client).SpotTrader().GetHistoryOrders(context.Background(), "BTCUSDT", 0, 0)
	if err != nil {
		t.Fatalf("GetHistoryOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if !orders[0].SellPrice.Equal(decimal.RequireFromString("34865.5")) {
		t.Errorf("sellPrice: got %s", orders[0].SellPrice)
	}
	if !orders[0].NetProfit.Equal(decimal.RequireFromString("-1.44573")) {
		t.Errorf("netProfit: got %s", orders[0].NetProfit)
	}
	if orders[0].SellTimeMs != 1695729885589 {
		t.Errorf("sellTime: got %d", orders[0].SellTimeMs)
	}
}

func TestContract_SpotTrader_GetOrderSummary(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"totalFollowerNum":"1","currentFollowerNum":"1","maxFollowerNum":"300","tradingOrderNum":"20",
			"totalpl":"180.81650415","gainNum":"9","lossNum":"11","totalEquity":"1002283.20","winRate":"10.00",
			"lastWeekRoiList":[{"rate":"14.08","ctime":"1695139200000"}],
			"lastMonthRoiList":[{"rate":"0","ctime":"1693152000000"}],
			"lastWeekProfitList":[{"amount":"156.403443","ctime":"1695139200000"}],
			"lastMonthProfitList":[{"amount":"-0.703906","ctime":"1695139200000"}]
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/order-total-detail": fixture}, nil)

	var sum, err = copyClient(client).SpotTrader().GetOrderSummary(context.Background())
	if err != nil {
		t.Fatalf("GetOrderSummary: %v", err)
	}
	if sum.MaxFollowerNum != 300 || sum.TradingOrderNum != 20 {
		t.Errorf("counts: got %d/%d", sum.MaxFollowerNum, sum.TradingOrderNum)
	}
	if sum.TotalPL != "180.81650415" {
		t.Errorf("totalPL raw: got %q", sum.TotalPL)
	}
	if !sum.WinRate.Equal(decimal.RequireFromString("10.00")) {
		t.Errorf("winRate: got %s", sum.WinRate)
	}
	if len(sum.LastWeekProfit) != 1 || !sum.LastWeekProfit[0].Amount.Equal(decimal.RequireFromString("156.403443")) {
		t.Errorf("lastWeekProfit: got %v", sum.LastWeekProfit)
	}
}

func TestContract_SpotTrader_ModifyTPSL_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var sc = copyClient(client).SpotTrader()
	if err := sc.ModifyTPSL(context.Background(), copytypes.SpotTraderModifyTPSLRequest{StopLossPrice: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty trackingNo: want InvalidRequest, got %v", err)
	}
	if err := sc.ModifyTPSL(context.Background(), copytypes.SpotTraderModifyTPSLRequest{TrackingNo: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("no prices: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotTrader_ModifyTPSL(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/order-modify-tpsl": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	var err = copyClient(client).SpotTrader().ModifyTPSL(context.Background(), copytypes.SpotTraderModifyTPSLRequest{TrackingNo: "123", StopSurplusPrice: "2000"})
	if err != nil {
		t.Fatalf("ModifyTPSL: %v", err)
	}
	if body["trackingNo"] != "123" || body["stopSurplusPrice"] != "2000" {
		t.Errorf("body: got %v", body)
	}
	if _, present := body["productType"]; present {
		t.Errorf("spot must not send productType: %v", body)
	}
	if _, present := body["stopLossPrice"]; present {
		t.Errorf("empty stopLossPrice must be omitted: %v", body)
	}
}

func TestContract_SpotTrader_ClosePositions(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/order-close-tracking": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	var err = copyClient(client).SpotTrader().ClosePositions(context.Background(), "BTCUSDT", []string{"1", "2"})
	if err != nil {
		t.Fatalf("ClosePositions: %v", err)
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("symbol: got %v", body["symbol"])
	}
	var list, ok = body["trackingNoList"].([]any)
	if !ok || len(list) != 2 || list[0] != "1" {
		t.Errorf("trackingNoList: got %v", body["trackingNoList"])
	}
}

func TestContract_SpotTrader_ClosePositions_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var sc = copyClient(client).SpotTrader()
	if err := sc.ClosePositions(context.Background(), "", []string{"1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty symbol: want InvalidRequest, got %v", err)
	}
	if err := sc.ClosePositions(context.Background(), "BTCUSDT", nil); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty list: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotTrader_GetConfig(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"removeLimitUsdt":"100",
			"spotInfoList":[{"maxQuoteSize":"5000000","surplusQuoteSize":"4998894.59","symbol":"BTCUSDT"}],
			"labelList":[],
			"enable":"YES","showAssetsMap":"NO","showEquity":"NO",
			"traceSymbolList":[{"enable":"YES","symbol":"BTCUSDT","minOpenCount":"0.0005"}]
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/config-query-settings": fixture}, nil)

	var cfg, err = copyClient(client).SpotTrader().GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.Enable != "YES" || cfg.ShowEquity != "NO" {
		t.Errorf("switches: got %q/%q", cfg.Enable, cfg.ShowEquity)
	}
	if !cfg.RemoveLimitUsdt.Equal(decimal.RequireFromString("100")) {
		t.Errorf("removeLimitUsdt: got %s", cfg.RemoveLimitUsdt)
	}
	if len(cfg.SpotInfoList) != 1 || !cfg.SpotInfoList[0].MaxQuoteSize.Equal(decimal.RequireFromString("5000000")) {
		t.Errorf("spotInfoList: got %v", cfg.SpotInfoList)
	}
	if len(cfg.TraceSymbols) != 1 || cfg.TraceSymbols[0].Symbol != "BTCUSDT" || !cfg.TraceSymbols[0].MinOpenCount.Equal(decimal.RequireFromString("0.0005")) {
		t.Errorf("traceSymbols: got %v", cfg.TraceSymbols)
	}
}

func TestContract_SpotTrader_SetSymbols(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/config-setting-symbols": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	var err = copyClient(client).SpotTrader().SetSymbols(context.Background(), []string{"BTCUSDT", "ETHUSDT"}, "add")
	if err != nil {
		t.Fatalf("SetSymbols: %v", err)
	}
	if body["settingType"] != "add" {
		t.Errorf("settingType: got %v", body["settingType"])
	}
	var list, ok = body["symbolList"].([]any)
	if !ok || len(list) != 2 {
		t.Errorf("symbolList: got %v", body["symbolList"])
	}
}

func TestContract_SpotTrader_SetSymbols_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var sc = copyClient(client).SpotTrader()
	if err := sc.SetSymbols(context.Background(), nil, "add"); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty symbols: want InvalidRequest, got %v", err)
	}
	if err := sc.SetSymbols(context.Background(), []string{"BTCUSDT"}, ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty settingType: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotTrader_RemoveFollower(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/config-remove-follower": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	if err := copyClient(client).SpotTrader().RemoveFollower(context.Background(), "u1"); err != nil {
		t.Fatalf("RemoveFollower: %v", err)
	}
	if body["followerUid"] != "u1" {
		t.Errorf("followerUid: got %v", body["followerUid"])
	}
	if err := copyClient(client).SpotTrader().RemoveFollower(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotTrader_GetProfitSummary_ByDate(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"profitSummarys":{"yesterdayProfit":"0","yesterdayTime":"1695720000000","sumProfit":"4.5837","waitProfit":"0"},
			"profitHistoryList":[{"coin":"USDT","profitCount":"4.58376728","lastProfitTime":"1695371160000",
				"historysByDateList":[{"profit":"2.40377986","profitTime":"1695371100000"},{"profit":"0.01625000","profitTime":"1693556100000"}]}]
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/profit-summarys": fixture}, nil)

	var sum, err = copyClient(client).SpotTrader().GetProfitSummary(context.Background())
	if err != nil {
		t.Fatalf("GetProfitSummary: %v", err)
	}
	if !sum.SumProfit.Equal(decimal.RequireFromString("4.5837")) {
		t.Errorf("sumProfit: got %s", sum.SumProfit)
	}
	if len(sum.History) != 1 || sum.History[0].Coin != "USDT" {
		t.Fatalf("history: got %v", sum.History)
	}
	if len(sum.History[0].ByDate) != 2 || !sum.History[0].ByDate[0].Amount.Equal(decimal.RequireFromString("2.40377986")) {
		t.Errorf("byDate: got %v", sum.History[0].ByDate)
	}
	if sum.History[0].ByDate[0].TimeMs != 1695371100000 {
		t.Errorf("byDate time: got %d", sum.History[0].ByDate[0].TimeMs)
	}
}

func TestContract_SpotTrader_GetProfitShareHistory(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","profitList":[{"profitId":"1","coin":"USDT","distributeRatio":"8","profit":"2.40377986","followerName":"xx1","profitTime":"1695371100000"}]}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/profit-history-details": fixture}, nil)

	var recs, err = copyClient(client).SpotTrader().GetProfitShareHistory(context.Background(), "USDT", 0, 0)
	if err != nil {
		t.Fatalf("GetProfitShareHistory: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("recs: want 1, got %d", len(recs))
	}
	if recs[0].FollowerName != "xx1" || !recs[0].DistributeRatio.Equal(decimal.RequireFromString("8")) {
		t.Errorf("row: got name=%q ratio=%s", recs[0].FollowerName, recs[0].DistributeRatio)
	}
}

func TestContract_SpotTrader_GetPendingProfitShare(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{"distributeRatio":"11.11","coin":"usdt","profit":"0","followerName":"cointr"}]
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-trader/profit-details": fixture}, nil)

	var pend, err = copyClient(client).SpotTrader().GetPendingProfitShare(context.Background(), "")
	if err != nil {
		t.Fatalf("GetPendingProfitShare: %v", err)
	}
	if len(pend) != 1 {
		t.Fatalf("pend: want 1, got %d", len(pend))
	}
	if pend[0].FollowerName != "cointr" || !pend[0].DistributeRatio.Equal(decimal.RequireFromString("11.11")) {
		t.Errorf("row: got %q/%s", pend[0].FollowerName, pend[0].DistributeRatio)
	}
}
