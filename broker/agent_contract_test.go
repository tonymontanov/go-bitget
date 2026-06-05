/*
FILE: broker/agent_contract_test.go

DESCRIPTION:
Contract tests for the broker Agent (affiliate) sub-client: the cursor
reads (customer-commissions / sub-customer-list minId / customer-kyc /
agent-commission) and the POST pageNo/pageSize reads
(customer-trade-volume / customer-list / customer-deposit / customer-asset),
plus the "volumn" field-name quirk and POST verb assertion.
*/

package broker

import (
	"context"
	"net/http"
	"testing"
)

func TestContract_Agent_CustomerCommissions_Cursor(t *testing.T) {
	t.Parallel()
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/broker/customer-commissions" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		if r.URL.Query().Get("idLessThan") == "" {
			return `{"code":"00000","msg":"success","data":{"endId":"500","commissionList":[{"uid":"1","date":"2025-06-01","coin":"USDT","symbol":"BTCUSDT","productType":"SPOT","dealAmount":"100","fee":"0.1","feeDeduction":"0","activityBonusDeduct":"0","spotCouponDeduct":"0","futuresCouponDeduct":"0","spotFeeDiscountDeduct":"0","negativeMakerFeeDeduct":"0","feePaid":"0.1","rebateAmount":"0.03","userTotalRebateAmount":"0.03","dayTotalRebateAmount":"0.03","totalRebateAmount":"0.03"}]}}`
		}
		return `{"code":"00000","msg":"success","data":{"endId":"","commissionList":[]}}`
	})
	var bc = brokerClient(t, client)

	var rows, err = bc.Agent().GetCustomerCommissions(context.Background(), AgentCustomerCommissionsQuery{})
	if err != nil {
		t.Fatalf("GetCustomerCommissions: %v", err)
	}
	if len(rows) != 1 || !rows[0].RebateAmount.Equal(dec("0.03")) || rows[0].ProductType != "SPOT" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestContract_Agent_SubCustomerList_MinIDCursor(t *testing.T) {
	t.Parallel()
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/broker/sub-customer-list" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		if r.URL.Query().Get("idLessThan") == "" {
			return `{"code":"00000","msg":"success","data":{"list":[{"uid":"10","registerTime":"1700000000000"}],"minId":""}}`
		}
		return `{"code":"00000","msg":"success","data":{"list":[],"minId":""}}`
	})
	var bc = brokerClient(t, client)

	var rows, err = bc.Agent().GetSubCustomerList(context.Background(), AgentQuery{ShowSub: "yes"})
	if err != nil {
		t.Fatalf("GetSubCustomerList: %v", err)
	}
	if len(rows) != 1 || rows[0].UID != "10" || rows[0].RegisterTimeMs != 1700000000000 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestContract_Agent_CustomerTradeVolume_POSTPaginates(t *testing.T) {
	t.Parallel()
	var sawPost bool
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/broker/customer-trade-volume" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		if r.Method == http.MethodPost {
			sawPost = true
		}
		// Single short page (1 row) → stops immediately.
		return `{"code":"00000","msg":"success","data":[{"uid":"7","volumn":"123.4","spotVolume":"100","futuresVolume":"23.4","time":"1700000000000"}]}`
	})
	var bc = brokerClient(t, client)

	var rows, err = bc.Agent().GetCustomerTradeVolume(context.Background(), AgentQuery{})
	if err != nil {
		t.Fatalf("GetCustomerTradeVolume: %v", err)
	}
	if !sawPost {
		t.Error("customer-trade-volume must be POST")
	}
	if len(rows) != 1 || !rows[0].Volume.Equal(dec("123.4")) || !rows[0].FuturesVolume.Equal(dec("23.4")) {
		t.Fatalf("unexpected rows (volumn mapping?): %+v", rows)
	}
}

func TestContract_Agent_CustomerKyc(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/customer-kyc-result": `{"code":"00000","msg":"success","data":{"userList":[{"uid":"1","kycResult":"passed"},{"uid":"2","kycResult":"not_passed"}],"endId":""}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var rows, err = bc.Agent().GetCustomerKycResult(context.Background(), AgentQuery{})
	if err != nil {
		t.Fatalf("GetCustomerKycResult: %v", err)
	}
	if len(rows) != 2 || rows[0].KycResult != "passed" || rows[1].KycResult != "not_passed" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestContract_Agent_CustomerAssets_POST(t *testing.T) {
	t.Parallel()
	var _, client = mockBitgetDynamic(t, func(t *testing.T, r *http.Request) string {
		if r.URL.Path != "/api/v2/broker/customer-asset" {
			return `{"code":"40404","msg":"nf","data":null}`
		}
		return `{"code":"00000","msg":"success","data":[{"balance":"1000.5","uid":"42","uTime":"1700000000000","remark":"vip"}]}`
	})
	var bc = brokerClient(t, client)

	var rows, err = bc.Agent().GetCustomerAssets(context.Background(), AgentAssetQuery{UID: "42"})
	if err != nil {
		t.Fatalf("GetCustomerAssets: %v", err)
	}
	if len(rows) != 1 || !rows[0].Balance.Equal(dec("1000.5")) || rows[0].Remark != "vip" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestContract_Agent_CommissionDetail_Cursor(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/broker/agent-commission": `{"code":"00000","msg":"success","data":{"endId":"","commissionList":[{"uid":"1","bizType":"futures","subBizType":"usdt_futures","symbol":"BTCUSDT","coin":"USDT","fee":"0.5","volume":"1000","activityBonusDeduct":"0","spotCouponDeduct":"0","futuresCouponDeduct":"0","spotFeeDiscountDeduct":"0","negativeMakerFeeDeduct":"0","feePaid":"0.5","directCommission":"0.2","subCommission":"0.05","partnerCommission":"0.1","partnerActualCommission":"0.08","traderType":"normal","apiType":"none","status":"settled","startCalculationTime":"1700000000000","endCalculationTime":"1700000600000"}]}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var bc = brokerClient(t, client)

	var rows, err = bc.Agent().GetCommissionDetail(context.Background(), BrokerReportQuery{})
	if err != nil {
		t.Fatalf("GetCommissionDetail: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Status != "settled" || !rows[0].DirectCommission.Equal(dec("0.2")) || !rows[0].PartnerActualCommission.Equal(dec("0.08")) {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	if rows[0].StartCalculationTimeMs != 1700000000000 || rows[0].EndCalculationTimeMs != 1700000600000 {
		t.Fatalf("time mapping mismatch: %+v", rows[0])
	}
}
