/*
FILE: uta/account_contract_test.go

DESCRIPTION:
Contract tests for the V3 UTA Account sub-client: the signed invariant,
POST body shaping for set-leverage / set-hold-mode, assets / settings /
fee-rate / max-transferable / financial-records decoding, the cursor
pass-through and the required-field guards.
*/

package uta

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func TestContract_Account_AssetsAndSettings(t *testing.T) {
	t.Parallel()
	var sawSign bool
	var routes = map[string]string{
		"/api/v3/account/assets":   `{"code":"00000","msg":"success","data":{"accountEquity":"10000","usdtEquity":"10000","btcEquity":"0.16","unrealisedPnl":"5","usdtUnrealisedPnl":"5","btcUnrealizedPnl":"0","effEquity":"9000","mmr":"0.01","imr":"0.05","mgnRatio":"0.2","positionMgnRatio":"0.1","assets":[{"coin":"USDT","equity":"10000","usdValue":"10000","balance":"9995","available":"9000","debt":"0","locked":"5"}]}}`,
		"/api/v3/account/settings": `{"code":"00000","msg":"success","data":{"uid":"123","accountMode":"unified","assetMode":"single","accountLevel":"advanced","holdMode":"hedge_mode","stpMode":"none","symbolConfigList":[{"category":"USDT-FUTURES","symbol":"BTCUSDT","marginMode":"crossed","leverage":"20"}],"coinConfigList":[{"coin":"USDT","leverage":"10"}]}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.Header.Get("ACCESS-SIGN") != "" {
			sawSign = true
		}
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var as, err = uc.Account().GetAssets(ctx)
	if err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
	if !sawSign {
		t.Error("account calls must be signed")
	}
	if !as.AccountEquity.Equal(decv("10000")) || len(as.Assets) != 1 || !as.Assets[0].Locked.Equal(decv("5")) {
		t.Fatalf("unexpected assets: %+v", as)
	}

	var st, serr = uc.Account().GetSettings(ctx)
	if serr != nil {
		t.Fatalf("GetSettings: %v", serr)
	}
	if st.HoldMode != "hedge_mode" || len(st.SymbolConfigs) != 1 || !st.SymbolConfigs[0].Leverage.Equal(decv("20")) || !st.CoinConfigs[0].Leverage.Equal(decv("10")) {
		t.Fatalf("unexpected settings: %+v", st)
	}
}

// TestContract_Account_DemoHeaderOnSignedV3 locks in the demo-routing rule:
// in demo mode a SIGNED v3 account call must carry `paptrading: 1`.
func TestContract_Account_DemoHeaderOnSignedV3(t *testing.T) {
	t.Parallel()
	var sawPap, sawSign string
	var _, client = mockBitgetDynamic(t, true, func(w http.ResponseWriter, r *http.Request, body []byte) {
		sawPap = r.Header.Get("paptrading")
		sawSign = r.Header.Get("ACCESS-SIGN")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"accountEquity":"1","usdtEquity":"1","btcEquity":"0","unrealisedPnl":"0","usdtUnrealisedPnl":"0","btcUnrealizedPnl":"0","effEquity":"1","mmr":"0","imr":"0","mgnRatio":"0","positionMgnRatio":"0","assets":[]}}`))
	})
	var uc = utaClient(t, client)
	if _, err := uc.Account().GetAssets(context.Background()); err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
	if sawSign == "" {
		t.Error("account call must be signed")
	}
	if sawPap != "1" {
		t.Errorf("demo mode must send paptrading:1 on signed v3, got %q", sawPap)
	}
}

func TestContract_Account_SetLeverageAndHoldMode(t *testing.T) {
	t.Parallel()
	var leverageBody, holdBody []byte
	var _, client = mockBitgetDynamic(t, false, func(w http.ResponseWriter, r *http.Request, body []byte) {
		switch r.URL.Path {
		case "/api/v3/account/set-leverage":
			leverageBody = body
		case "/api/v3/account/set-hold-mode":
			holdBody = body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":null}`))
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var err = uc.Account().SetLeverage(ctx, SetLeverageRequest{
		Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", Leverage: "20", PosSide: "long",
	})
	if err != nil {
		t.Fatalf("SetLeverage: %v", err)
	}
	var lev map[string]any
	if jerr := json.Unmarshal(leverageBody, &lev); jerr != nil {
		t.Fatalf("leverage body not json: %v (%s)", jerr, leverageBody)
	}
	if lev["category"] != "USDT-FUTURES" || lev["leverage"] != "20" || lev["posSide"] != "long" {
		t.Fatalf("unexpected leverage body: %v", lev)
	}

	if err = uc.Account().SetHoldMode(ctx, utatypes.HoldModeHedge); err != nil {
		t.Fatalf("SetHoldMode: %v", err)
	}
	var hm map[string]any
	if jerr := json.Unmarshal(holdBody, &hm); jerr != nil {
		t.Fatalf("hold body not json: %v", jerr)
	}
	if hm["holdMode"] != "hedge_mode" {
		t.Fatalf("unexpected hold body: %v", hm)
	}

	// Guards.
	if err = uc.Account().SetLeverage(ctx, SetLeverageRequest{Category: utatypes.CategoryUSDTFutures}); err == nil {
		t.Error("SetLeverage(no leverage): want guard")
	}
	if err = uc.Account().SetHoldMode(ctx, ""); err == nil {
		t.Error("SetHoldMode(empty): want guard")
	}
}

func TestContract_Account_RatesRecordsSwitch(t *testing.T) {
	t.Parallel()
	var sawCursor string
	var routes = map[string]string{
		"/api/v3/account/fee-rate":            `{"code":"00000","msg":"success","data":{"makerFeeRate":"0.0002","takerFeeRate":"0.0006"}}`,
		"/api/v3/account/max-transferable":    `{"code":"00000","msg":"success","data":{"coin":"USDT","maxTransfer":"5000","borrowMaxTransfer":"8000"}}`,
		"/api/v3/account/financial-records":   `{"code":"00000","msg":"success","data":{"list":[{"category":"USDT-FUTURES","id":"r1","symbol":"BTCUSDT","coin":"USDT","type":"trans_fee","amount":"-0.5","fee":"0","balance":"9999.5","ts":"1700000000000"}],"cursor":"next123"}}`,
		"/api/v3/account/open-interest-limit": `{"code":"00000","msg":"success","data":{"symbol":"BTCUSDT","singleUserLimit":"1000000","masterSubLimit":"5000000","marketMakerLimit":"20000000"}}`,
		"/api/v3/account/switch-status":       `{"code":"00000","msg":"success","data":{"status":"successSuccess"}}`,
		"/api/v3/account/funding-assets":      `{"code":"00000","msg":"success","data":[{"coin":"USDT","available":"100","frozen":"0","balance":"100"}]}`,
		"/api/v3/account/info":                `{"code":"00000","msg":"success","data":{"userId":"123","inviterId":"0","parentId":"","channelCode":"","channel":"","ips":"","permType":"read-and-write","permissions":["uta_trade"],"regisTime":"1600000000000"}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v3/account/financial-records" {
			sawCursor = r.URL.Query().Get("cursor")
		}
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var fr, ferr = uc.Account().GetFeeRate(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT")
	if ferr != nil || !fr.TakerFeeRate.Equal(decv("0.0006")) {
		t.Fatalf("GetFeeRate: %v %+v", ferr, fr)
	}
	var mt, merr = uc.Account().GetMaxTransferable(ctx, "USDT")
	if merr != nil || !mt.BorrowMaxTransfer.Equal(decv("8000")) {
		t.Fatalf("GetMaxTransferable: %v %+v", merr, mt)
	}
	var recs, cursor, rerr = uc.Account().GetFinancialRecords(ctx, FinancialRecordsQuery{Category: utatypes.CategoryUSDTFutures, Cursor: "c0", Limit: 50})
	if rerr != nil || len(recs) != 1 || cursor != "next123" || !recs[0].Amount.Equal(decv("-0.5")) {
		t.Fatalf("GetFinancialRecords: %v %+v cursor=%q", rerr, recs, cursor)
	}
	if sawCursor != "c0" {
		t.Errorf("cursor not forwarded: %q", sawCursor)
	}
	var oi, oerr = uc.Account().GetOpenInterestLimit(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT")
	if oerr != nil || !oi.MarketMakerLimit.Equal(decv("20000000")) {
		t.Fatalf("GetOpenInterestLimit: %v %+v", oerr, oi)
	}
	var status, serr = uc.Account().GetSwitchStatus(ctx)
	if serr != nil || status != "successSuccess" {
		t.Fatalf("GetSwitchStatus: %v %q", serr, status)
	}
	var fa, aerr = uc.Account().GetFundingAssets(ctx, "USDT")
	if aerr != nil || len(fa) != 1 || !fa[0].Balance.Equal(decv("100")) {
		t.Fatalf("GetFundingAssets: %v %+v", aerr, fa)
	}
	var info, ierr = uc.Account().GetInfo(ctx)
	if ierr != nil || info.UserID != "123" || info.RegisTimeMs != 1600000000000 || len(info.Permissions) != 1 {
		t.Fatalf("GetInfo: %v %+v", ierr, info)
	}

	// Guards.
	if _, err := uc.Account().GetFeeRate(ctx, utatypes.CategoryUSDTFutures, ""); err == nil {
		t.Error("GetFeeRate(no symbol): want guard")
	}
	if _, _, err := uc.Account().GetFinancialRecords(ctx, FinancialRecordsQuery{}); err == nil {
		t.Error("GetFinancialRecords(no category): want guard")
	}
}
