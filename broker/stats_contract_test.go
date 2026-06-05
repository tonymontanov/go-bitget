/*
FILE: broker/stats_contract_test.go

DESCRIPTION:
Contract tests for the broker Stats sub-client: pageNo/pageSize stitching
(subaccounts/commissions/trade-volume), the order-commission idLessThan/
endId cursor, the nested spot/futures breakdown of total-commission, and
the rebate-info uid guard.
*/

package broker

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestContract_Stats_GetSubaccounts_Paginates(t *testing.T) {
	t.Parallel()
	// page 1: full (100 rows) → page 2: short (1 row) → stop.
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/broker/subaccounts" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		var pageNo = r.URL.Query().Get("pageNo")
		if pageNo == "1" {
			return subInfoPage(100, "u1")
		}
		return subInfoPage(1, "u-last")
	})
	var bc = brokerClient(t, client)

	var subs, err = bc.Stats().GetSubaccounts(context.Background(), BrokerReportQuery{})
	if err != nil {
		t.Fatalf("GetSubaccounts: %v", err)
	}
	if len(subs) != 101 {
		t.Fatalf("want 101 stitched rows, got %d", len(subs))
	}
	if subs[len(subs)-1].UID != "u-last" {
		t.Fatalf("unexpected tail: %+v", subs[len(subs)-1])
	}
}

func TestContract_Stats_GetTotalCommission(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/total-commission": `{"code":"00000","msg":"success","data":[{"date":"2025-06-01","totalTradingVolume":"1000","totalActiveTraders":"5","totalCommission":"3.5","spot":{"spotTradingVolume":"600","spotTradingFee":"1.2","spotPureTradingFee":"1.0","spotCommission":"0.6"},"futures":{"futuresTradingVolume":"400","futuresTradingFee":"0.8","futuresPureTradingFee":"0.7","futuresCommission":"0.4"}}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var tc, err = bc.Stats().GetTotalCommission(context.Background(), BrokerReportQuery{})
	if err != nil {
		t.Fatalf("GetTotalCommission: %v", err)
	}
	if len(tc) != 1 {
		t.Fatalf("want 1 row, got %d", len(tc))
	}
	if tc[0].TotalActiveTraders != 5 || !tc[0].TotalCommission.Equal(dec("3.5")) {
		t.Fatalf("unexpected totals: %+v", tc[0])
	}
	if !tc[0].Spot.TradingVolume.Equal(dec("600")) || !tc[0].Spot.Commission.Equal(dec("0.6")) {
		t.Fatalf("unexpected spot segment: %+v", tc[0].Spot)
	}
	if !tc[0].Futures.PureTradingFee.Equal(dec("0.7")) || !tc[0].Futures.Commission.Equal(dec("0.4")) {
		t.Fatalf("unexpected futures segment: %+v", tc[0].Futures)
	}
}

func TestContract_Stats_GetOrderCommission_Cursor(t *testing.T) {
	t.Parallel()
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/broker/order-commission" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		var id = r.URL.Query().Get("idLessThan")
		if id == "" {
			var limit = atoiOr(r.URL.Query().Get("limit"), 100)
			return orderCommissionPage(limit, "f1", "50")
		}
		return `{"code":"00000","msg":"success","data":{"commissionlist":[{"fillId":"f-last","orderId":"o","ts":"1700000000000","bizType":"spot","subBizType":"spot_trade","symbol":"BTCUSDT","volume":"1","fee":"0.1","pureFee":"0.09","rebateAmount":"0.02"}],"endId":""}}`
	})
	var bc = brokerClient(t, client)

	var items, err = bc.Stats().GetOrderCommission(context.Background(), BrokerOrderCommissionQuery{})
	if err != nil {
		t.Fatalf("GetOrderCommission: %v", err)
	}
	if len(items) == 0 || items[len(items)-1].FillID != "f-last" {
		t.Fatalf("expected stitched pages ending f-last, got %d", len(items))
	}
	if !items[len(items)-1].RebateAmount.Equal(dec("0.02")) {
		t.Fatalf("rebate parse mismatch: %+v", items[len(items)-1])
	}
}

func TestContract_Stats_GetRebateInfo(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/rebate-info": `{"code":"00000","msg":"success","data":{"affiliationType":"affiliate","userLevel":"3","clientSpotRebateRatio":"0.3","clientFuturesRebateRatio":"0.25"}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var info, err = bc.Stats().GetRebateInfo(context.Background(), "999")
	if err != nil {
		t.Fatalf("GetRebateInfo: %v", err)
	}
	if info.AffiliationType != "affiliate" || !info.ClientSpotRebateRatio.Equal(dec("0.3")) {
		t.Fatalf("unexpected info: %+v", info)
	}

	if _, err = bc.Stats().GetRebateInfo(context.Background(), ""); err == nil {
		t.Error("GetRebateInfo(\"\"): want guard error")
	}
}

// --- fixture builders ---------------------------------------------------

func subInfoPage(n int, uidTail string) string {
	var b strings.Builder
	b.WriteString(`{"code":"00000","msg":"success","data":[`)
	var i int
	for i = 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		var uid = "u" + strconv.Itoa(i)
		if i == n-1 {
			uid = uidTail
		}
		b.WriteString(`{"uid":"`)
		b.WriteString(uid)
		b.WriteString(`","asset":"1.5","firstTimeDeposit":"1700000000000","firstTimeTrade":"1700000000001","registerTime":"1699999999999"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func orderCommissionPage(n int, fillPrefix, endID string) string {
	var b strings.Builder
	b.WriteString(`{"code":"00000","msg":"success","data":{"commissionlist":[`)
	var i int
	for i = 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"fillId":"`)
		b.WriteString(fillPrefix)
		b.WriteString(`","orderId":"o","ts":"1700000000000","bizType":"spot","subBizType":"spot_trade","symbol":"BTCUSDT","volume":"1","fee":"0.1","pureFee":"0.09","rebateAmount":"0.02"}`)
	}
	b.WriteString(`],"endId":"`)
	b.WriteString(endID)
	b.WriteString(`"}}`)
	return b.String()
}
