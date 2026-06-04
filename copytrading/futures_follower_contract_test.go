/*
FILE: copytrading/futures_follower_contract_test.go

DESCRIPTION:
Contract tests for the futures FOLLOWER sub-client. Fixtures are
hand-derived from the Bitget V2 mix-follower docs; no network calls.

KEY INVARIANTS:

  - every call carries the pinned productType (USDT-FUTURES here);
  - query-traders parses counts as integers and amounts as decimals;
  - current-orders tolerates both openPriceAvg / openAvgPrice spellings;
  - history-orders walks the idLessThan / endId cursor and parses the
    realised P/L fields;
  - close-positions sends productType + the close selector and returns
    the generated order-id list;
  - Unfollow requires a non-empty traderID (client-side guard) and POSTs
    {traderId}.
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

func TestContract_FuturesFollower_GetMyTraders(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{
			"certificationType":"Certified","traderId":"T1","traderName":"alpha",
			"maxFollowLimit":"1000","followCount":"42",
			"bgbMaxFollowLimit":"2000","bgbFollowCount":"10",
			"traceTotalMarginAmount":"1234.5","traceTotalNetProfit":"678.9","traceTotalProfit":"700",
			"currentTradingPairs":["BTCUSDT","ETHUSDT"],"followerTime":"1700000000000"
		}]
	}`

	var seenPath, seenProductType string
	var srv, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/query-traders": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenPath = r.URL.Path
		seenProductType = r.URL.Query().Get("productType")
	})
	_ = srv

	var traders, err = copyClient(client).FuturesFollower().GetMyTraders(context.Background())
	if err != nil {
		t.Fatalf("GetMyTraders: %v", err)
	}
	if seenPath != "/api/v2/copy/mix-follower/query-traders" {
		t.Errorf("path: got %q", seenPath)
	}
	if seenProductType != "USDT-FUTURES" {
		t.Errorf("productType: got %q", seenProductType)
	}
	if len(traders) != 1 {
		t.Fatalf("traders: want 1, got %d", len(traders))
	}
	var tr copytypes.Trader = traders[0]
	if tr.TraderID != "T1" || tr.TraderName != "alpha" {
		t.Errorf("id/name: got %q/%q", tr.TraderID, tr.TraderName)
	}
	if tr.MaxFollowLimit != 1000 || tr.FollowCount != 42 {
		t.Errorf("limits: got %d/%d", tr.MaxFollowLimit, tr.FollowCount)
	}
	if tr.BGBMaxFollowLimit != 2000 || tr.BGBFollowCount != 10 {
		t.Errorf("bgb limits: got %d/%d", tr.BGBMaxFollowLimit, tr.BGBFollowCount)
	}
	if !tr.TraceTotalNetProfit.Equal(decimal.RequireFromString("678.9")) {
		t.Errorf("net profit: got %s", tr.TraceTotalNetProfit)
	}
	if len(tr.CurrentTradingPairs) != 2 || tr.CurrentTradingPairs[0] != "BTCUSDT" {
		t.Errorf("pairs: got %v", tr.CurrentTradingPairs)
	}
	if tr.FollowerTimeMs != 1700000000000 {
		t.Errorf("followerTime: got %d", tr.FollowerTimeMs)
	}
}

func TestContract_FuturesFollower_GetCurrentOrders_AvgPriceFallback(t *testing.T) {
	t.Parallel()
	// Row uses the legacy openAvgPrice spelling (openPriceAvg empty) to
	// exercise the fallback.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{
			"trackingNo":"TR1","traderId":"T1","traderName":"alpha",
			"openOrderId":"O1","closeOrderId":"","symbol":"BTCUSDT","posSide":"long",
			"openLeverage":"10","openAvgPrice":"50000","openSize":"0.01","openMarginSz":"50",
			"openFee":"-0.1","openTime":"1700000000000","closeAvgPrice":"0","closeSize":"0","closeTime":"0"
		}]
	}`

	var seenSymbol, seenTrader string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/query-current-orders": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenSymbol = r.URL.Query().Get("symbol")
		seenTrader = r.URL.Query().Get("traderId")
	})

	var orders, err = copyClient(client).FuturesFollower().GetCurrentOrders(context.Background(), "BTCUSDT", "T1")
	if err != nil {
		t.Fatalf("GetCurrentOrders: %v", err)
	}
	if seenSymbol != "BTCUSDT" || seenTrader != "T1" {
		t.Errorf("filters: got symbol=%q trader=%q", seenSymbol, seenTrader)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if !orders[0].OpenPriceAvg.Equal(decimal.RequireFromString("50000")) {
		t.Errorf("openPriceAvg fallback: got %s", orders[0].OpenPriceAvg)
	}
	if !orders[0].OpenMarginSize.Equal(decimal.RequireFromString("50")) {
		t.Errorf("openMarginSz: got %s", orders[0].OpenMarginSize)
	}
	if orders[0].PosSide != "long" {
		t.Errorf("posSide: got %q", orders[0].PosSide)
	}
}

func TestContract_FuturesFollower_GetHistoryOrders_SinglePage(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"endId":"",
			"trackingList":[{
				"trackingNo":"TR9","traderId":"T1","openOrderId":"O9","closeOrderId":"C9",
				"productType":"USDT-FUTURES","symbol":"ETHUSDT","posSide":"short",
				"openLeverage":"5","openPriceAvg":"3000","openSize":"1","openFee":"-0.2","openTime":"1700000000000",
				"closePriceAvg":"2900","closeSize":"1","closeFee":"-0.2","closeTime":"1700000100000",
				"profitRate":"0.033","netProfit":"99.6","achievedPL":"100"
			}]
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/query-history-orders": fixture}, nil)

	var orders, err = copyClient(client).FuturesFollower().GetHistoryOrders(context.Background(), "ETHUSDT", 0, 0)
	if err != nil {
		t.Fatalf("GetHistoryOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if orders[0].TrackingNo != "TR9" || orders[0].Symbol != "ETHUSDT" {
		t.Errorf("row: got %q/%q", orders[0].TrackingNo, orders[0].Symbol)
	}
	if !orders[0].NetProfit.Equal(decimal.RequireFromString("99.6")) {
		t.Errorf("netProfit: got %s", orders[0].NetProfit)
	}
	if orders[0].CloseTimeMs != 1700000100000 {
		t.Errorf("closeTime: got %d", orders[0].CloseTimeMs)
	}
}

func TestContract_FuturesFollower_ClosePositions(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderIdList":["1001","1002"]}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/close-positions": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var res, err = copyClient(client).FuturesFollower().ClosePositions(context.Background(), copytypes.CloseFollowerPositionsRequest{
		TrackingNo: "TR1",
		Symbol:     "BTCUSDT",
		MarginCoin: "USDT",
		MarginMode: "crossed",
		HoldSide:   "long",
	})
	if err != nil {
		t.Fatalf("ClosePositions: %v", err)
	}
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType in body: got %v", body["productType"])
	}
	if body["trackingNo"] != "TR1" || body["holdSide"] != "long" {
		t.Errorf("selector: got %v / %v", body["trackingNo"], body["holdSide"])
	}
	if len(res.OrderIDList) != 2 || res.OrderIDList[0] != "1001" {
		t.Errorf("orderIdList: got %v", res.OrderIDList)
	}
}

func TestContract_FuturesFollower_ClosePositions_AllOmitsSelector(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderIdList":[]}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/close-positions": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var _, err = copyClient(client).FuturesFollower().ClosePositions(context.Background(), copytypes.CloseFollowerPositionsRequest{})
	if err != nil {
		t.Fatalf("ClosePositions(all): %v", err)
	}
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType: got %v", body["productType"])
	}
	if _, ok := body["trackingNo"]; ok {
		t.Errorf("trackingNo must be omitted when empty, body=%v", body)
	}
	if _, ok := body["symbol"]; ok {
		t.Errorf("symbol must be omitted when empty, body=%v", body)
	}
}

func TestContract_FuturesFollower_Unfollow(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":null}`

	var body map[string]any
	var seenPath string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/cancel-trader": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		seenPath = r.URL.Path
		_ = json.Unmarshal(raw, &body)
	})

	var err = copyClient(client).FuturesFollower().Unfollow(context.Background(), "T1")
	if err != nil {
		t.Fatalf("Unfollow: %v", err)
	}
	if seenPath != "/api/v2/copy/mix-follower/cancel-trader" {
		t.Errorf("path: got %q", seenPath)
	}
	if body["traderId"] != "T1" {
		t.Errorf("traderId: got %v", body["traderId"])
	}
}

func TestContract_FuturesFollower_Unfollow_EmptyGuard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)

	var err = copyClient(client).FuturesFollower().Unfollow(context.Background(), "")
	if err == nil {
		t.Fatal("Unfollow(\"\") must fail client-side")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("want InvalidRequest, got %v", err)
	}
}
