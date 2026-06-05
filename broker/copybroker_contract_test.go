/*
FILE: broker/copybroker_contract_test.go

DESCRIPTION:
Contract tests for the broker CopyBroker sub-client: query-traders
parsing + page stitching, the history-trace close/profit fields, and the
current-traces endpoint NOT sending the time window.
*/

package broker

import (
	"context"
	"net/http"
	"testing"
)

func TestContract_CopyBroker_GetTraders(t *testing.T) {
	t.Parallel()
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/copy/mix-broker/query-traders" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		// Single short page → stops.
		return `{"code":"00000","msg":"success","data":[{"traderId":"t1","traderName":"alpha","certificationType":"Certified","maxFollowLimit":"100","bgbMaxFollowLimit":"200","followCount":"40","bgbFollowCount":"5","traceTotalMarginAmount":"1000","traceTotalNetProfit":"123.4","traceTotalProfit":"150","currentTradingPairs":["BTCUSDT","ETHUSDT"],"followerTime":"1693381832000"}]}`
	})
	var bc = brokerClient(t, client)

	var traders, err = bc.CopyBroker().GetTraders(context.Background(), BrokerReportQuery{})
	if err != nil {
		t.Fatalf("GetTraders: %v", err)
	}
	if len(traders) != 1 {
		t.Fatalf("want 1 trader, got %d", len(traders))
	}
	var tr = traders[0]
	if tr.TraderID != "t1" || tr.MaxFollowLimit != 100 || tr.BGBMaxFollowLimit != 200 {
		t.Fatalf("unexpected trader: %+v", tr)
	}
	if !tr.TraceTotalNetProfit.Equal(dec("123.4")) || len(tr.CurrentTradingPairs) != 2 {
		t.Fatalf("unexpected stats: %+v", tr)
	}
}

func TestContract_CopyBroker_HistoricalOrders(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/copy/mix-broker/query-history-traces": `{"code":"00000","msg":"success","data":[{"trackingNo":"tn1","traderId":"t1","traderName":"alpha","symbol":"BTCUSDT","holdSide":"long","openOrderId":"oo","closeOrderId":"co","openLeverage":"10","openPriceAvg":"50000","openSize":"0.1","openFee":"0.01","openTime":"1700000000000","closePriceAvg":"51000","closeSize":"0.1","closeFee":"0.01","closeTime":"1700000600000","netProfit":"99.5","profitRate":"0.02","achievedProfits":"100"}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var traces, err = bc.CopyBroker().GetHistoricalOrders(context.Background(), BrokerReportQuery{})
	if err != nil {
		t.Fatalf("GetHistoricalOrders: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(traces))
	}
	var tc = traces[0]
	if tc.PosSide != "long" || tc.TrackingNo != "tn1" {
		t.Fatalf("unexpected trace: %+v", tc)
	}
	if !tc.NetProfit.Equal(dec("99.5")) || !tc.ClosePriceAvg.Equal(dec("51000")) || tc.CloseTimeMs != 1700000600000 {
		t.Fatalf("close/profit mapping mismatch: %+v", tc)
	}
}

func TestContract_CopyBroker_PendingOrders_NoTimeWindow(t *testing.T) {
	t.Parallel()
	var sawStart, sawEnd bool
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/copy/mix-broker/query-current-traces" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		if r.URL.Query().Get("startTime") != "" {
			sawStart = true
		}
		if r.URL.Query().Get("endTime") != "" {
			sawEnd = true
		}
		return `{"code":"00000","msg":"success","data":[{"trackingNo":"tn2","traderId":"t1","symbol":"ETHUSDT","posSide":"short","openPriceAvg":"3000","openSize":"1","openTime":"1700000000000"}]}`
	})
	var bc = brokerClient(t, client)

	// Pass a window; current-traces must NOT forward it.
	var traces, err = bc.CopyBroker().GetPendingOrders(context.Background(), BrokerReportQuery{StartTimeMs: 1, EndTimeMs: 2})
	if err != nil {
		t.Fatalf("GetPendingOrders: %v", err)
	}
	if sawStart || sawEnd {
		t.Error("current-traces must not send startTime/endTime")
	}
	if len(traces) != 1 || traces[0].PosSide != "short" || !traces[0].OpenSize.Equal(dec("1")) {
		t.Fatalf("unexpected traces: %+v", traces)
	}
}
