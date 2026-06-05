/*
FILE: copytrading/spot_follower_contract_test.go

DESCRIPTION:
Contract tests for the spot FOLLOWER surface (M4b). Fixtures are
hand-derived from the Bitget V2 spot-follower docs.

KEY INVARIANTS:

  - NO call sends productType (spot is not product-type scoped);
  - query-traders walks page-number pagination;
  - query-trader-symbols / query-settings require traderId;
  - query-settings parses the dual config (active rows + venue bounds)
    and maps enable -> Following bool;
  - current/history orders walk the idLessThan/endId cursor;
  - settings stamps required per-row fields and guards them;
  - setting-tpsl needs trackingNo + >=1 price;
  - close-tracking needs symbol + non-empty trackingNos; stop-order
    needs only the list; cancel-trader needs traderId.
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

func TestContract_SpotFollower_GetMyTraders_NoProductType(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{"resultList":[{
			"certificationType":"Uncertified","traceTotalAmount":"16027.6543","traceTotalNetProfit":"43.2480",
			"traceTotalProfit":"52.8743","traderName":"KGU***9Z72","traderId":"123","maxFollowLimit":"300",
			"platskMaxFollowLimit":"","followCount":"1","platskFollowCount":"","followerTime":"1693533344295"
		}]}
	}`

	var sawProductType bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/query-traders": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.URL.Query().Get("productType") != "" {
			sawProductType = true
		}
	})

	var traders, err = copyClient(client).SpotFollower().GetMyTraders(context.Background())
	if err != nil {
		t.Fatalf("GetMyTraders: %v", err)
	}
	if sawProductType {
		t.Error("spot must not send productType")
	}
	if len(traders) != 1 {
		t.Fatalf("traders: want 1, got %d", len(traders))
	}
	if traders[0].TraderID != "123" || traders[0].MaxFollowLimit != 300 || traders[0].FollowCount != 1 {
		t.Errorf("ids/counts: got %+v", traders[0])
	}
	// empty platsk fields parse to zero, not error.
	if traders[0].PlatskMaxFollowLimit != 0 || traders[0].PlatskFollowCount != 0 {
		t.Errorf("platsk: want 0/0, got %d/%d", traders[0].PlatskMaxFollowLimit, traders[0].PlatskFollowCount)
	}
	if !traders[0].TraceTotalNetProfit.Equal(decimal.RequireFromString("43.2480")) {
		t.Errorf("netProfit: got %s", traders[0].TraceTotalNetProfit)
	}
}

func TestContract_SpotFollower_GetTraderSymbols(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"currentTradingList":["ETHUSDT","BTCUSDT"]}}`

	var gotTrader string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/query-trader-symbols": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		gotTrader = r.URL.Query().Get("traderId")
	})

	var syms, err = copyClient(client).SpotFollower().GetTraderSymbols(context.Background(), "123")
	if err != nil {
		t.Fatalf("GetTraderSymbols: %v", err)
	}
	if gotTrader != "123" {
		t.Errorf("traderId: got %q", gotTrader)
	}
	if len(syms) != 2 || syms[0] != "ETHUSDT" {
		t.Errorf("symbols: got %v", syms)
	}
	if _, err = copyClient(client).SpotFollower().GetTraderSymbols(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty traderID: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotFollower_GetSettings(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"enable":"YES","profitRate":"5","settledInDays":"53",
			"tradeSettingList":[{"maxTraceAmount":"50000","stopLossRation":"10","stopSurplusRation":"10","symbol":"ETHUSDT","traceType":"FIXED_AMOUNT"}],
			"tradeSymbolSettingList":[{"maxStopLossRation":"90","maxStopSurplusRation":"500","maxTraceAmount":"1200","maxTraceAmountSystem":"50000","maxTraceSize":"500","maxTraceRation":"20","minStopLossRation":"0","minStopSurplusRation":"0","minTraceAmount":"10","minTraceSize":"0.001","minTraceRation":"0.1","sliderMaxStopLossRatio":"90","sliderMaxStopSurplusRatio":"500","symbol":"BTCUSDT"}],
			"traderHeadPic":"","traderName":"139****0981"
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/query-settings": fixture}, nil)

	var s, err = copyClient(client).SpotFollower().GetSettings(context.Background(), "bf1")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !s.Following {
		t.Error("Following: want true for enable=YES")
	}
	if s.SettledInDays != 53 || !s.ProfitRate.Equal(decimal.RequireFromString("5")) {
		t.Errorf("scalars: got days=%d rate=%s", s.SettledInDays, s.ProfitRate)
	}
	if len(s.TradeSettings) != 1 || s.TradeSettings[0].Symbol != "ETHUSDT" || !s.TradeSettings[0].MaxTraceAmount.Equal(decimal.RequireFromString("50000")) {
		t.Errorf("tradeSettings: got %v", s.TradeSettings)
	}
	if len(s.TradeSymbolSettings) != 1 {
		t.Fatalf("tradeSymbolSettings: want 1, got %d", len(s.TradeSymbolSettings))
	}
	var b = s.TradeSymbolSettings[0]
	if b.Symbol != "BTCUSDT" || !b.MaxTraceSize.Equal(decimal.RequireFromString("500")) || !b.MinTraceSize.Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("bounds: got %+v", b)
	}
	if !b.SliderMaxStopSurplusRatio.Equal(decimal.RequireFromString("500")) {
		t.Errorf("slider: got %s", b.SliderMaxStopSurplusRatio)
	}
}

func TestContract_SpotFollower_GetSettings_Guard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	if _, err := copyClient(client).SpotFollower().GetSettings(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty traderID: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotFollower_UpdateSettings(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/settings": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var err = copyClient(client).SpotFollower().UpdateSettings(context.Background(), copytypes.SpotFollowSettingsRequest{
		TraderID: "abc123",
		Mode:     "advanced",
		Settings: []copytypes.SpotFollowSetting{{Symbol: "BTCUSDT", TraceType: "amount", MaxHoldSize: "1000", TraceValue: "100", StopSurplusRatio: "10"}},
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if body["traderId"] != "abc123" || body["mode"] != "advanced" {
		t.Errorf("top fields: got %v", body)
	}
	if _, present := body["productType"]; present {
		t.Errorf("spot must not send productType: %v", body)
	}
	var list, ok = body["settings"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("settings: got %v", body["settings"])
	}
	var row = list[0].(map[string]any)
	if row["symbol"] != "BTCUSDT" || row["traceValue"] != "100" || row["maxHoldSize"] != "1000" {
		t.Errorf("row: got %v", row)
	}
	// empty stopLossRatio must be omitted.
	if _, present := row["stopLossRatio"]; present {
		t.Errorf("empty stopLossRatio must be omitted: %v", row)
	}
}

func TestContract_SpotFollower_UpdateSettings_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var fc = copyClient(client).SpotFollower()
	var base = copytypes.SpotFollowSetting{Symbol: "BTCUSDT", TraceType: "amount", MaxHoldSize: "1000", TraceValue: "100"}

	if err := fc.UpdateSettings(context.Background(), copytypes.SpotFollowSettingsRequest{Settings: []copytypes.SpotFollowSetting{base}}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty traderID: want InvalidRequest, got %v", err)
	}
	if err := fc.UpdateSettings(context.Background(), copytypes.SpotFollowSettingsRequest{TraderID: "x"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty settings: want InvalidRequest, got %v", err)
	}
	var noValue = base
	noValue.TraceValue = ""
	if err := fc.UpdateSettings(context.Background(), copytypes.SpotFollowSettingsRequest{TraderID: "x", Settings: []copytypes.SpotFollowSetting{noValue}}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty traceValue: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotFollower_SetTPSL_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var fc = copyClient(client).SpotFollower()
	if err := fc.SetTPSL(context.Background(), copytypes.SpotFollowTPSLRequest{StopLossPrice: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty trackingNo: want InvalidRequest, got %v", err)
	}
	if err := fc.SetTPSL(context.Background(), copytypes.SpotFollowTPSLRequest{TrackingNo: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("no prices: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotFollower_SetTPSL(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/setting-tpsl": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	var err = copyClient(client).SpotFollower().SetTPSL(context.Background(), copytypes.SpotFollowTPSLRequest{TrackingNo: "123", StopSurplusPrice: "2000", StopLossPrice: "1500"})
	if err != nil {
		t.Fatalf("SetTPSL: %v", err)
	}
	if body["trackingNo"] != "123" || body["stopSurplusPrice"] != "2000" || body["stopLossPrice"] != "1500" {
		t.Errorf("body: got %v", body)
	}
}

func TestContract_SpotFollower_GetCurrentOrders(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","trackingList":[{
			"trackingNo":"1","traderId":"123","buyFillSize":"0.0317","buyDelegateSize":"0.0317190800000000",
			"buyPrice":"34870.7","unrealizedPL":"-0.77945295","buyTime":"1695729895491","buyFee":"-0.00001908",
			"unrealizedPLR":"-0.07","symbol":"BTCUSDT","stopSurplusPrice":null,"stopLossPrice":null
		}]}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/query-current-orders": fixture}, nil)

	var orders, err = copyClient(client).SpotFollower().GetCurrentOrders(context.Background(), "", "")
	if err != nil {
		t.Fatalf("GetCurrentOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if orders[0].TraderID != "123" || !orders[0].BuyPrice.Equal(decimal.RequireFromString("34870.7")) {
		t.Errorf("row: got %+v", orders[0])
	}
	if !orders[0].StopLossPrice.IsZero() || !orders[0].StopSurplusPrice.IsZero() {
		t.Errorf("null TP/SL must be zero")
	}
}

func TestContract_SpotFollower_GetHistoryOrders(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","trackingList":[{
			"trackingNo":"1","traderId":"123","fillSize":"0.0316","buyPrice":"34870.7","sellPrice":"34865.5",
			"buyFee":"-0.00001902","sellFee":"-0.66104988","achievedPL":"-0.82756","achievedPLR":"-0.07",
			"symbol":"BTCUSDT","buyTime":"1695729617968","sellTime":"1695729886269"
		}]}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/query-history-orders": fixture}, nil)

	var orders, err = copyClient(client).SpotFollower().GetHistoryOrders(context.Background(), "BTCUSDT", "123", 0, 0)
	if err != nil {
		t.Fatalf("GetHistoryOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: want 1, got %d", len(orders))
	}
	if !orders[0].AchievedPL.Equal(decimal.RequireFromString("-0.82756")) || orders[0].SellTimeMs != 1695729886269 {
		t.Errorf("row: got %+v", orders[0])
	}
}

func TestContract_SpotFollower_ClosePositions(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/order-close-tracking": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	var err = copyClient(client).SpotFollower().ClosePositions(context.Background(), "ETHUSDT", []string{"12213123"})
	if err != nil {
		t.Fatalf("ClosePositions: %v", err)
	}
	if body["symbol"] != "ETHUSDT" {
		t.Errorf("symbol: got %v", body["symbol"])
	}
	var list, ok = body["trackingNoList"].([]any)
	if !ok || len(list) != 1 || list[0] != "12213123" {
		t.Errorf("trackingNoList: got %v", body["trackingNoList"])
	}
}

func TestContract_SpotFollower_ClosePositions_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var fc = copyClient(client).SpotFollower()
	if err := fc.ClosePositions(context.Background(), "", []string{"1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty symbol: want InvalidRequest, got %v", err)
	}
	if err := fc.ClosePositions(context.Background(), "ETHUSDT", nil); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty list: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotFollower_StopOrders(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/stop-order": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	if err := copyClient(client).SpotFollower().StopOrders(context.Background(), []string{"123"}); err != nil {
		t.Fatalf("StopOrders: %v", err)
	}
	var list, ok = body["trackingNoList"].([]any)
	if !ok || len(list) != 1 || list[0] != "123" {
		t.Errorf("trackingNoList: got %v", body["trackingNoList"])
	}
	// stop-order has no symbol.
	if _, present := body["symbol"]; present {
		t.Errorf("stop-order must not send symbol: %v", body)
	}
	if err := copyClient(client).SpotFollower().StopOrders(context.Background(), nil); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty list: want InvalidRequest, got %v", err)
	}
}

func TestContract_SpotFollower_Unfollow(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":""}`
	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/spot-follower/cancel-trader": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})
	if err := copyClient(client).SpotFollower().Unfollow(context.Background(), "123"); err != nil {
		t.Fatalf("Unfollow: %v", err)
	}
	if body["traderId"] != "123" {
		t.Errorf("traderId: got %v", body["traderId"])
	}
	if err := copyClient(client).SpotFollower().Unfollow(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty: want InvalidRequest, got %v", err)
	}
}
