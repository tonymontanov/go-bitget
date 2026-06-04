/*
FILE: copytrading/futures_follower_config_contract_test.go

DESCRIPTION:
Contract tests for the futures FOLLOWER configuration surface (M2b):
settings / query-settings / setting-tpsl / query-quantity-limit.
Fixtures are hand-derived from the Bitget V2 mix-follower docs.

KEY INVARIANTS:

  - UpdateSettings stamps the pinned productType onto every row and
    enforces the required-field / 10-row guards client-side;
  - query-settings parses followerEnable + the per-symbol detail list;
  - setting-tpsl sends productType + trackingNo and omits empty prices
    (preserving the empty / "0" / >0 semantics);
  - query-quantity-limit parses the min/max copy size rows.
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

func TestContract_FuturesFollower_UpdateSettings(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":"success"}`

	var body map[string]any
	var seenPath string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/settings": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		seenPath = r.URL.Path
		_ = json.Unmarshal(raw, &body)
	})

	var err = copyClient(client).FuturesFollower().UpdateSettings(context.Background(), copytypes.FollowSettingsRequest{
		TraderID: "T1",
		Mode:     "advanced",
		Settings: []copytypes.SymbolSetting{{
			Symbol:     "BTCUSDT",
			MarginType: "trader",
			MarginCoin: "USDT",
			LeverType:  "trader",
			TraceType:  "amount",
			TraceValue: "330",
		}},
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if seenPath != "/api/v2/copy/mix-follower/settings" {
		t.Errorf("path: got %q", seenPath)
	}
	if body["traderId"] != "T1" || body["mode"] != "advanced" {
		t.Errorf("top-level: got %v", body)
	}
	var settings, ok = body["settings"].([]any)
	if !ok || len(settings) != 1 {
		t.Fatalf("settings: got %v", body["settings"])
	}
	var row = settings[0].(map[string]any)
	if row["productType"] != "USDT-FUTURES" {
		t.Errorf("row productType must be stamped from client: got %v", row["productType"])
	}
	if row["symbol"] != "BTCUSDT" || row["traceValue"] != "330" {
		t.Errorf("row: got %v", row)
	}
	// Empty optionals must be omitted.
	if _, present := row["stopLossRatio"]; present {
		t.Errorf("empty stopLossRatio must be omitted: %v", row)
	}
}

func TestContract_FuturesFollower_UpdateSettings_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var fc = copyClient(client).FuturesFollower()

	if err := fc.UpdateSettings(context.Background(), copytypes.FollowSettingsRequest{Settings: []copytypes.SymbolSetting{{Symbol: "BTCUSDT"}}}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty traderID: want InvalidRequest, got %v", err)
	}
	if err := fc.UpdateSettings(context.Background(), copytypes.FollowSettingsRequest{TraderID: "T1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty settings: want InvalidRequest, got %v", err)
	}
	if err := fc.UpdateSettings(context.Background(), copytypes.FollowSettingsRequest{
		TraderID: "T1",
		Settings: []copytypes.SymbolSetting{{Symbol: "BTCUSDT"}}, // missing marginType/leverType/traceType/traceValue
	}); !bitget.IsInvalidRequest(err) {
		t.Errorf("missing required row fields: want InvalidRequest, got %v", err)
	}
}

func TestContract_FuturesFollower_GetSettings(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"followerEnable":"YES",
			"detailList":[{
				"symbol":"ETHUSDT","productType":"USDT-FUTURES","marginType":"specify","marginCoin":"USDT",
				"leverType":"trader","longLeverage":"20","shortLeverage":"20","traceType":"percent",
				"traceValue":"1","maxHoldSize":"50000","stopSurplusRatio":"200","stopLossRatio":""
			}]
		}
	}`

	var seenTrader string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/query-settings": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenTrader = r.URL.Query().Get("traderId")
	})

	var cfg, err = copyClient(client).FuturesFollower().GetSettings(context.Background(), "T1")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if seenTrader != "T1" {
		t.Errorf("traderId: got %q", seenTrader)
	}
	if cfg.FollowerEnable != "YES" || !cfg.Following {
		t.Errorf("followerEnable: got %q / %v", cfg.FollowerEnable, cfg.Following)
	}
	if len(cfg.DetailList) != 1 {
		t.Fatalf("detailList: want 1, got %d", len(cfg.DetailList))
	}
	var d = cfg.DetailList[0]
	if d.Symbol != "ETHUSDT" || d.MarginType != "specify" || d.LeverType != "trader" {
		t.Errorf("classifiers: got %q/%q/%q", d.Symbol, d.MarginType, d.LeverType)
	}
	if !d.LongLeverage.Equal(decimal.RequireFromString("20")) || !d.TraceValue.Equal(decimal.RequireFromString("1")) {
		t.Errorf("numerics: lev=%s trace=%s", d.LongLeverage, d.TraceValue)
	}
	if !d.StopSurplusRatio.Equal(decimal.RequireFromString("200")) {
		t.Errorf("stopSurplusRatio: got %s", d.StopSurplusRatio)
	}
	if !d.StopLossRatio.IsZero() {
		t.Errorf("empty stopLossRatio must parse to zero: got %s", d.StopLossRatio)
	}
}

func TestContract_FuturesFollower_GetSettings_EmptyGuard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	if _, err := copyClient(client).FuturesFollower().GetSettings(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty traderID: want InvalidRequest, got %v", err)
	}
}

func TestContract_FuturesFollower_SetTPSL(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":"success"}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/setting-tpsl": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	// Only take-profit set; stop-loss left empty (must be omitted).
	var err = copyClient(client).FuturesFollower().SetTPSL(context.Background(), copytypes.FollowTPSLRequest{
		TrackingNo:       "1",
		Symbol:           "BTCUSDT",
		StopSurplusPrice: "37878",
	})
	if err != nil {
		t.Fatalf("SetTPSL: %v", err)
	}
	if body["productType"] != "USDT-FUTURES" || body["trackingNo"] != "1" {
		t.Errorf("required body: got %v", body)
	}
	if body["stopSurplusPrice"] != "37878" {
		t.Errorf("stopSurplusPrice: got %v", body["stopSurplusPrice"])
	}
	if _, present := body["stopLossPrice"]; present {
		t.Errorf("empty stopLossPrice must be omitted: %v", body)
	}
}

func TestContract_FuturesFollower_SetTPSL_EmptyGuard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	if err := copyClient(client).FuturesFollower().SetTPSL(context.Background(), copytypes.FollowTPSLRequest{}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty trackingNo: want InvalidRequest, got %v", err)
	}
}

func TestContract_FuturesFollower_GetCopyLimit(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{"maxFollowSize":"20000","minFollowSize":"0.005","symbol":"BTCUSDT"}]
	}`

	var seenProductType, seenSymbol string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-follower/query-quantity-limit": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenProductType = r.URL.Query().Get("productType")
		seenSymbol = r.URL.Query().Get("symbol")
	})

	var limits, err = copyClient(client).FuturesFollower().GetCopyLimit(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetCopyLimit: %v", err)
	}
	if seenProductType != "USDT-FUTURES" || seenSymbol != "BTCUSDT" {
		t.Errorf("query: got productType=%q symbol=%q", seenProductType, seenSymbol)
	}
	if len(limits) != 1 {
		t.Fatalf("limits: want 1, got %d", len(limits))
	}
	if limits[0].Symbol != "BTCUSDT" {
		t.Errorf("symbol: got %q", limits[0].Symbol)
	}
	if !limits[0].MaxFollowSize.Equal(decimal.RequireFromString("20000")) {
		t.Errorf("maxFollowSize: got %s", limits[0].MaxFollowSize)
	}
	if !limits[0].MinFollowSize.Equal(decimal.RequireFromString("0.005")) {
		t.Errorf("minFollowSize: got %s", limits[0].MinFollowSize)
	}
}
