/*
FILE: copytrading/futures_trader_profit_contract_test.go

DESCRIPTION:
Contract tests for the futures TRADER profit / profit-share surface
(M3c): profit-history-summarys / profit-history-details / profit-details
/ profits-group-coin-date. Fixtures are hand-derived from the Bitget V2
mix-trader docs.

KEY INVARIANTS:

  - profit-history-summarys parses the headline figures + per-coin list;
  - profit-history-details walks the idLessThan/endId cursor;
  - profit-details / profits-group-coin-date walk page-number pagination
    to completion.
*/

package copytrading

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/shopspring/decimal"
)

func TestContract_FuturesTrader_GetProfitSummary(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"profitSummary":{"yesterdayProfit":"0","sumProfit":"26.4519","waitProfit":"0","yesterdayTime":"1698076800000"},
			"profitHistoryList":[{"coin":"USDT","profitCount":"24.28410397","lastProfitTime":"1698076800000"}]
		}
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/profit-history-summarys": fixture}, nil)

	var sum, err = copyClient(client).FuturesTrader().GetProfitSummary(context.Background())
	if err != nil {
		t.Fatalf("GetProfitSummary: %v", err)
	}
	if !sum.SumProfit.Equal(decimal.RequireFromString("26.4519")) {
		t.Errorf("sumProfit: got %s", sum.SumProfit)
	}
	if sum.YesterdayTimeMs != 1698076800000 {
		t.Errorf("yesterdayTime: got %d", sum.YesterdayTimeMs)
	}
	if len(sum.History) != 1 || sum.History[0].Coin != "USDT" {
		t.Fatalf("history: got %v", sum.History)
	}
	if !sum.History[0].ProfitCount.Equal(decimal.RequireFromString("24.28410397")) {
		t.Errorf("profitCount: got %s", sum.History[0].ProfitCount)
	}
}

func TestContract_FuturesTrader_GetProfitShareHistory(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"endId":"",
			"profitList":[{"profitId":"1","coin":"usdt","profit":"1","nickName":"nickname","profitTime":"1691446639000"}]
		}
	}`

	var seenCoin string
	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/profit-history-details": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenCoin = r.URL.Query().Get("coin")
	})

	var recs, err = copyClient(client).FuturesTrader().GetProfitShareHistory(context.Background(), "USDT", 0, 0)
	if err != nil {
		t.Fatalf("GetProfitShareHistory: %v", err)
	}
	if seenCoin != "USDT" {
		t.Errorf("coin: got %q", seenCoin)
	}
	if len(recs) != 1 {
		t.Fatalf("recs: want 1, got %d", len(recs))
	}
	if recs[0].ProfitID != "1" || recs[0].NickName != "nickname" {
		t.Errorf("row: got %q/%q", recs[0].ProfitID, recs[0].NickName)
	}
	if !recs[0].Profit.Equal(decimal.RequireFromString("1")) || recs[0].ProfitTimeMs != 1691446639000 {
		t.Errorf("profit/time: got %s/%d", recs[0].Profit, recs[0].ProfitTimeMs)
	}
}

func TestContract_FuturesTrader_GetPendingProfitShare(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":[{"coin":"usdt","profit":"0","nickName":"nickname"}]
	}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/copy/mix-trader/profit-details": fixture}, nil)

	var pend, err = copyClient(client).FuturesTrader().GetPendingProfitShare(context.Background(), "")
	if err != nil {
		t.Fatalf("GetPendingProfitShare: %v", err)
	}
	if len(pend) != 1 {
		t.Fatalf("pend: want 1, got %d", len(pend))
	}
	if pend[0].Coin != "usdt" || pend[0].NickName != "nickname" {
		t.Errorf("row: got %q/%q", pend[0].Coin, pend[0].NickName)
	}
}

func TestContract_FuturesTrader_GetProfitByCoinDate_Paginates(t *testing.T) {
	t.Parallel()
	// Page 1 returns a full page (50 rows), page 2 a short page.
	var fullPage string
	{
		var rows []string
		var i int
		for i = 0; i < 50; i++ {
			rows = append(rows, `{"coin":"usdt","profit":"`+strconv.Itoa(i)+`","profitTime":"1627354109502"}`)
		}
		fullPage = `{"code":"00000","msg":"success","requestTime":1,"data":[` + joinComma(rows) + `]}`
	}
	const shortPage = `{"code":"00000","msg":"success","requestTime":1,"data":[{"coin":"usdt","profit":"15","profitTime":"1627354109502"}]}`

	var mu sync.Mutex
	var seenPages []string
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		mu.Lock()
		defer mu.Unlock()
		var pageNo = r.URL.Query().Get("pageNo")
		seenPages = append(seenPages, pageNo)
		if pageNo == "1" {
			return fullPage
		}
		return shortPage
	})

	var rows, err = copyClient(client).FuturesTrader().GetProfitByCoinDate(context.Background())
	if err != nil {
		t.Fatalf("GetProfitByCoinDate: %v", err)
	}
	if len(rows) != 51 {
		t.Fatalf("rows: want 51, got %d", len(rows))
	}
	if !rows[len(rows)-1].Profit.Equal(decimal.RequireFromString("15")) {
		t.Errorf("last profit: got %s", rows[len(rows)-1].Profit)
	}
	if len(seenPages) != 2 || seenPages[0] != "1" || seenPages[1] != "2" {
		t.Errorf("pages walked: got %v", seenPages)
	}
}
