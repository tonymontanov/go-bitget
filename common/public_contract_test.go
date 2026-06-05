/*
FILE: common/public_contract_test.go

DESCRIPTION:
Contract tests for the common Public + Account sub-clients: the unsigned
public reads (server time, announcements) NOT sending an ACCESS-SIGN
header, plus the account-wide asset parsing and the trade-rate guard.
*/

package common

import (
	"context"
	"net/http"
	"testing"
)

func TestContract_Public_ServerTime_Unsigned(t *testing.T) {
	t.Parallel()
	var sawSign bool
	var routes = map[string]string{
		"/api/v2/public/time": `{"code":"00000","msg":"success","data":{"serverTime":"1700000000123"}}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v2/public/time" && r.Header.Get("ACCESS-SIGN") != "" {
			sawSign = true
		}
	})
	var cc = commonClient(t, client)

	var ms, err = cc.Public().GetServerTime(context.Background())
	if err != nil {
		t.Fatalf("GetServerTime: %v", err)
	}
	if ms != 1700000000123 {
		t.Fatalf("want 1700000000123, got %d", ms)
	}
	if sawSign {
		t.Error("public/time must be unsigned (no ACCESS-SIGN header)")
	}
}

func TestContract_Public_Announcements(t *testing.T) {
	t.Parallel()
	var sawSign bool
	var sawLang string
	var routes = map[string]string{
		"/api/v2/public/annoucements": `{"code":"00000","msg":"success","data":[{"annId":"1","annTitle":"t","annDesc":"d","cTime":"1700000000000","language":"en_US","annUrl":"https://x"}]}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v2/public/annoucements" {
			if r.Header.Get("ACCESS-SIGN") != "" {
				sawSign = true
			}
			sawLang = r.URL.Query().Get("language")
		}
	})
	var cc = commonClient(t, client)

	var anns, err = cc.Public().GetAnnouncements(context.Background(), AnnouncementsQuery{Language: "en_US"})
	if err != nil {
		t.Fatalf("GetAnnouncements: %v", err)
	}
	if sawSign {
		t.Error("public/annoucements must be unsigned")
	}
	if sawLang != "en_US" {
		t.Errorf("language query mismatch: %q", sawLang)
	}
	if len(anns) != 1 || anns[0].AnnID != "1" || anns[0].CTimeMs != 1700000000000 {
		t.Fatalf("unexpected anns: %+v", anns)
	}

	// Guard: language required.
	if _, err = cc.Public().GetAnnouncements(context.Background(), AnnouncementsQuery{}); err == nil {
		t.Error("GetAnnouncements(no language): want guard error")
	}
}

func TestContract_Account_Assets(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/account/funding-assets":      `{"code":"00000","msg":"success","data":[{"coin":"USDT","available":"100.5","frozen":"1.0","usdtValue":"100.5"}]}`,
		"/api/v2/account/bot-assets":          `{"code":"00000","msg":"success","data":[{"coin":"USDT","available":"50","equity":"55","bonus":"5","frozen":"0","usdtValue":"55"}]}`,
		"/api/v2/account/all-account-balance": `{"code":"00000","msg":"success","data":[{"accountType":"spot","usdtBalance":"200.25"},{"accountType":"usdt_futures","usdtBalance":"1000"}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var cc = commonClient(t, client)

	var f, ferr = cc.Account().GetFundingAssets(context.Background(), "USDT")
	if ferr != nil {
		t.Fatalf("GetFundingAssets: %v", ferr)
	}
	if len(f) != 1 || !f[0].Available.Equal(dec("100.5")) {
		t.Fatalf("unexpected funding: %+v", f)
	}

	var b, berr = cc.Account().GetBotAssets(context.Background(), "")
	if berr != nil {
		t.Fatalf("GetBotAssets: %v", berr)
	}
	if len(b) != 1 || !b[0].Equity.Equal(dec("55")) || !b[0].Bonus.Equal(dec("5")) {
		t.Fatalf("unexpected bot: %+v", b)
	}

	var bal, balerr = cc.Account().GetAllAccountBalance(context.Background())
	if balerr != nil {
		t.Fatalf("GetAllAccountBalance: %v", balerr)
	}
	if len(bal) != 2 || bal[1].AccountType != "usdt_futures" || !bal[1].USDTBalance.Equal(dec("1000")) {
		t.Fatalf("unexpected balances: %+v", bal)
	}
}

func TestContract_Account_TradeRate(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/common/trade-rate": `{"code":"00000","msg":"success","data":{"makerFeeRate":"0.001","takerFeeRate":"0.0015"}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var cc = commonClient(t, client)

	var tr, err = cc.Account().GetTradeRate(context.Background(), "BTCUSDT", "spot")
	if err != nil {
		t.Fatalf("GetTradeRate: %v", err)
	}
	if !tr.MakerFeeRate.Equal(dec("0.001")) || !tr.TakerFeeRate.Equal(dec("0.0015")) {
		t.Fatalf("unexpected rate: %+v", tr)
	}

	// Guards.
	if _, err = cc.Account().GetTradeRate(context.Background(), "", "spot"); err == nil {
		t.Error("GetTradeRate(no symbol): want guard error")
	}
	if _, err = cc.Account().GetTradeRate(context.Background(), "BTCUSDT", ""); err == nil {
		t.Error("GetTradeRate(no businessType): want guard error")
	}
}
