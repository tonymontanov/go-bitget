/*
FILE: copytrading/futures_trader_config_contract_test.go

DESCRIPTION:
Contract tests for the futures TRADER configuration surface (M3b):
config-query-symbols / config-setting-symbols / config-settings-base /
config-query-followers / config-remove-follower. Fixtures are
hand-derived from the Bitget V2 mix-trader docs.

KEY INVARIANTS:

  - query-symbols parses the per-symbol ratios + maxLeverage;
  - setting-symbols stamps the pinned productType per row + guards;
  - settings-base requires at least one switch and omits empty ones;
  - query-followers walks page-number pagination to completion;
  - remove-follower requires followerUID and POSTs {followerUid}.
*/

package copytrading

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	copytypes "github.com/tonymontanov/go-bitget/v2/copytrading/types"
)

func TestContract_FuturesTrader_GetSymbolSettings(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{"symbol":"BTCUSDT","openTrader":"YES","minOpenCount":"0.002","maxLeverage":"50","stopSurplusRatio":"100","stopLossRatio":"30"}]
	}`

	var seenProductType string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/config-query-symbols": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenProductType = r.URL.Query().Get("productType")
	})

	var cfgs, err = copyClient(client).FuturesTrader().GetSymbolSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSymbolSettings: %v", err)
	}
	if seenProductType != "USDT-FUTURES" {
		t.Errorf("productType: got %q", seenProductType)
	}
	if len(cfgs) != 1 {
		t.Fatalf("cfgs: want 1, got %d", len(cfgs))
	}
	if cfgs[0].Symbol != "BTCUSDT" || cfgs[0].OpenTrader != "YES" {
		t.Errorf("row: got %q/%q", cfgs[0].Symbol, cfgs[0].OpenTrader)
	}
	if !cfgs[0].MaxLeverage.Equal(decimal.RequireFromString("50")) || !cfgs[0].MinOpenCount.Equal(decimal.RequireFromString("0.002")) {
		t.Errorf("nums: lev=%s minOpen=%s", cfgs[0].MaxLeverage, cfgs[0].MinOpenCount)
	}
	if !cfgs[0].StopSurplusRatio.Equal(decimal.RequireFromString("100")) {
		t.Errorf("stopSurplusRatio: got %s", cfgs[0].StopSurplusRatio)
	}
}

func TestContract_FuturesTrader_SetSymbols(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":"success"}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/config-setting-symbols": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var err = copyClient(client).FuturesTrader().SetSymbols(context.Background(), []copytypes.SymbolSettingChange{{
		Symbol:           "BTCUSDT",
		SettingType:      "ADD",
		StopSurplusRatio: "100",
		StopLossRatio:    "30",
	}})
	if err != nil {
		t.Fatalf("SetSymbols: %v", err)
	}
	var list, ok = body["settingList"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("settingList: got %v", body["settingList"])
	}
	var row = list[0].(map[string]any)
	if row["productType"] != "USDT-FUTURES" {
		t.Errorf("row productType must be stamped: got %v", row["productType"])
	}
	if row["symbol"] != "BTCUSDT" || row["settingType"] != "ADD" {
		t.Errorf("row: got %v", row)
	}
}

func TestContract_FuturesTrader_SetSymbols_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var tc = copyClient(client).FuturesTrader()

	if err := tc.SetSymbols(context.Background(), nil); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty: want InvalidRequest, got %v", err)
	}
	if err := tc.SetSymbols(context.Background(), []copytypes.SymbolSettingChange{{Symbol: "BTCUSDT"}}); !bitget.IsInvalidRequest(err) {
		t.Errorf("missing settingType: want InvalidRequest, got %v", err)
	}
}

func TestContract_FuturesTrader_SetGlobalSettings(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":"success"}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/config-settings-base": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var err = copyClient(client).FuturesTrader().SetGlobalSettings(context.Background(), copytypes.GlobalSettingsRequest{Enable: "YES"})
	if err != nil {
		t.Fatalf("SetGlobalSettings: %v", err)
	}
	if body["enable"] != "YES" {
		t.Errorf("enable: got %v", body["enable"])
	}
	if _, present := body["showTpsl"]; present {
		t.Errorf("empty showTpsl must be omitted: %v", body)
	}
}

func TestContract_FuturesTrader_SetGlobalSettings_EmptyGuard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	if err := copyClient(client).FuturesTrader().SetGlobalSettings(context.Background(), copytypes.GlobalSettingsRequest{}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty: want InvalidRequest, got %v", err)
	}
}

func TestContract_FuturesTrader_GetFollowers_Paginates(t *testing.T) {
	t.Parallel()
	// Page 1 returns a full page (followersPageSize rows), page 2 a short
	// page → loop stops after page 2.
	var fullPage string
	{
		var rows []string
		var i int
		for i = 0; i < followersPageSize; i++ {
			rows = append(rows, `{"accountEquity":"100","isRemove":"NO","followerName":"f`+strconv.Itoa(i)+`","followerUid":"u`+strconv.Itoa(i)+`","followerTime":"1693533344295"}`)
		}
		fullPage = `{"code":"00000","msg":"success","requestTime":1,"data":[` + joinComma(rows) + `]}`
	}
	const shortPage = `{"code":"00000","msg":"success","requestTime":1,"data":[{"accountEquity":"1001711.2164","isRemove":"NO","followerName":"KGU","followerUid":"last","followerTime":"1693533344295"}]}`

	var mu sync.Mutex
	var seenPages []string
	var srv, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		mu.Lock()
		defer mu.Unlock()
		var pageNo = r.URL.Query().Get("pageNo")
		seenPages = append(seenPages, pageNo)
		if pageNo == "1" {
			return fullPage
		}
		return shortPage
	})
	_ = srv

	var followers, err = copyClient(client).FuturesTrader().GetFollowers(context.Background())
	if err != nil {
		t.Fatalf("GetFollowers: %v", err)
	}
	if len(followers) != followersPageSize+1 {
		t.Fatalf("followers: want %d, got %d", followersPageSize+1, len(followers))
	}
	if followers[len(followers)-1].FollowerUID != "last" {
		t.Errorf("last follower: got %q", followers[len(followers)-1].FollowerUID)
	}
	if !followers[len(followers)-1].AccountEquity.Equal(decimal.RequireFromString("1001711.2164")) {
		t.Errorf("accountEquity: got %s", followers[len(followers)-1].AccountEquity)
	}
	if len(seenPages) != 2 || seenPages[0] != "1" || seenPages[1] != "2" {
		t.Errorf("pages walked: got %v", seenPages)
	}
}

func TestContract_FuturesTrader_RemoveFollower(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":"success"}`

	var body map[string]any
	var seenPath string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/config-remove-follower": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		seenPath = r.URL.Path
		_ = json.Unmarshal(raw, &body)
	})

	var err = copyClient(client).FuturesTrader().RemoveFollower(context.Background(), "u1")
	if err != nil {
		t.Fatalf("RemoveFollower: %v", err)
	}
	if seenPath != "/api/v2/copy/mix-trader/config-remove-follower" {
		t.Errorf("path: got %q", seenPath)
	}
	if body["followerUid"] != "u1" {
		t.Errorf("followerUid: got %v", body["followerUid"])
	}
}

func TestContract_FuturesTrader_RemoveFollower_EmptyGuard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	if err := copyClient(client).FuturesTrader().RemoveFollower(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty: want InvalidRequest, got %v", err)
	}
}
