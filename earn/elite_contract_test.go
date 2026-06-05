/*
FILE: earn/elite_contract_test.go

DESCRIPTION:
Contract tests for the EARN On-Chain Elite sub-client. Fixtures
hand-derived from the Bitget V2 earn/elite reference types.

KEY INVARIANTS:

  - no call sends productType;
  - records require a type and walk the cursor/endId cursor over
    recordList; assets is a single resultList;
  - redeemType / paymentAccount decode from either string or array;
  - subscribe / redeem post the documented body and guard required fields.
*/

package earn

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	earntypes "github.com/tonymontanov/go-bitget/v2/earn/types"
)

func TestContract_Elite_GetProducts(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":[{"productId":"1","coin":"BGUSD","minApr":"0.05","maxApr":"0.12","sellOut":"NO",
			"subscriptionCoinList":[{"subscriptionCoin":"USDT","precision":"2","feeRate":"0.001",
				"exchangeRate":"1","remainQuota":"100000","minAmount":"10"}]}]}`

	var sawProductType bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/elite/product": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.URL.Query().Get("productType") != "" {
			sawProductType = true
		}
	})

	var prods, err = NewClient(client).Elite().GetProducts(context.Background())
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if sawProductType {
		t.Error("earn must not send productType")
	}
	if len(prods) != 1 || prods[0].SellOut != "NO" || !prods[0].MaxApr.Equal(decimal.RequireFromString("0.12")) {
		t.Fatalf("products: got %v", prods)
	}
	if len(prods[0].SubscriptionCoinList) != 1 || prods[0].SubscriptionCoinList[0].SubscriptionCoin != "USDT" {
		t.Errorf("subCoins: got %v", prods[0].SubscriptionCoinList)
	}
}

func TestContract_Elite_GetAssets(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"resultList":[{"productId":"1","productCoin":"BGUSD","holdingAmount":"100",
			"usdtHoldingAmount":"100","exchangeRate":"1","apr":"0.08","minApy":"0.05","maxApy":"0.12",
			"subscriptionCoin":"USDT","exchangeAmount":"100","projectList":[{"projectName":"alpha"}],
			"unsettledBGPoints":"3","interestCoin":"USDT","totalProfit":"2.5"}]}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/elite/assets": fixture}, nil)

	var assets, err = NewClient(client).Elite().GetAssets(context.Background())
	if err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
	if len(assets) != 1 || !assets[0].TotalProfit.Equal(decimal.RequireFromString("2.5")) {
		t.Fatalf("assets: got %v", assets)
	}
	if len(assets[0].ProjectList) != 1 || assets[0].ProjectList[0] != "alpha" {
		t.Errorf("projectList: got %v", assets[0].ProjectList)
	}
}

func TestContract_Elite_GetRecords(t *testing.T) {
	t.Parallel()
	// redeemType as array, paymentAccount as array on one row; the other
	// row uses a bare-string redeemType to exercise flexStringList.
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","recordList":[
			{"recordId":"1","productId":"1","coin":"USDT","status":"done","exchangeRate":"1","receivedCoin":"USDT",
			 "receivedAmount":"10","investAmount":"100","feeRate":"0.001","redeemType":["fast"],
			 "receivingAccount":"spot","actualReceivingAccount":"spot","paymentAccount":["spot"],
			 "settlePoints":"1","fee":"0.01"},
			{"recordId":"2","productId":"1","coin":"USDT","status":"done","exchangeRate":"1","receivedCoin":"USDT",
			 "receivedAmount":"5","investAmount":"50","feeRate":"0.001","redeemType":"standard",
			 "receivingAccount":"unified","actualReceivingAccount":"unified","paymentAccount":"unified",
			 "settlePoints":"0","fee":"0"}]}}`

	var gotType string
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/elite/records": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		gotType = r.URL.Query().Get("type")
	})

	var recs, err = NewClient(client).Elite().GetRecords(context.Background(), EliteRecordsQuery{Type: "redeem"})
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if gotType != "redeem" {
		t.Errorf("type: got %q", gotType)
	}
	if len(recs) != 2 {
		t.Fatalf("records: got %d", len(recs))
	}
	if len(recs[0].RedeemType) != 1 || recs[0].RedeemType[0] != "fast" || len(recs[0].PaymentAccount) != 1 {
		t.Errorf("row0 unions: got redeem=%v pay=%v", recs[0].RedeemType, recs[0].PaymentAccount)
	}
	if len(recs[1].RedeemType) != 1 || recs[1].RedeemType[0] != "standard" {
		t.Errorf("row1 redeemType (bare string): got %v", recs[1].RedeemType)
	}

	if _, err = NewClient(client).Elite().GetRecords(context.Background(), EliteRecordsQuery{}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty type: want InvalidRequest, got %v", err)
	}
}

func TestContract_Elite_GetSubscribeInfo(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"productSubId":"11","minAmount":"10","remainQuota":"5000","exchangeRate":"1","productCoin":"BGUSD",
			"interestTime":"1700000000000","settleTime":"1700100000000","precision":"2","feeRate":"0.001",
			"subscriptionCoinList":[{"subscriptionCoin":"USDT","precision":"2","feeRate":"0.001","exchangeRate":"1"}]}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/elite/subscribe-info": fixture}, nil)

	var info, err = NewClient(client).Elite().GetSubscribeInfo(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetSubscribeInfo: %v", err)
	}
	if info.ProductSubID != "11" || !info.RemainQuota.Equal(decimal.RequireFromString("5000")) {
		t.Errorf("info: got %+v", info)
	}
	if len(info.SubscriptionCoinList) != 1 || info.SubscriptionCoinList[0].SubscriptionCoin != "USDT" {
		t.Errorf("subCoins: got %v", info.SubscriptionCoinList)
	}

	if _, err = NewClient(client).Elite().GetSubscribeInfo(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty productId: want InvalidRequest, got %v", err)
	}
}

func TestContract_Elite_Subscribe(t *testing.T) {
	t.Parallel()
	const subFixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"5001"}}`
	const resFixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"result":"settled"}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{
		"/api/v2/earn/elite/subscribe":        subFixture,
		"/api/v2/earn/elite/subscribe-result": resFixture,
	}, func(t *testing.T, r *http.Request, raw []byte) {
		if r.URL.Path == "/api/v2/earn/elite/subscribe" {
			_ = json.Unmarshal(raw, &body)
		}
	})

	var orderID, err = NewClient(client).Elite().Subscribe(context.Background(), earntypes.EliteSubscribeRequest{
		ProductSubID: "11", Amount: "100", PaymentAccount: "spot",
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if body["productSubId"] != "11" || body["amount"] != "100" || body["paymentAccount"] != "spot" {
		t.Errorf("body: got %v", body)
	}
	if _, present := body["coin"]; present {
		t.Errorf("coin must be omitted when empty: %v", body)
	}
	if orderID != "5001" {
		t.Errorf("orderId: got %q", orderID)
	}

	var status, err2 = NewClient(client).Elite().GetSubscribeResult(context.Background(), "5001")
	if err2 != nil {
		t.Fatalf("GetSubscribeResult: %v", err2)
	}
	if status != "settled" {
		t.Errorf("status: got %q", status)
	}

	if _, err = NewClient(client).Elite().Subscribe(context.Background(), earntypes.EliteSubscribeRequest{Amount: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty productSubId: want InvalidRequest, got %v", err)
	}
}

func TestContract_Elite_RedeemFlow(t *testing.T) {
	t.Parallel()
	const infoFixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"productId":"1","productSubId":"11","productCoin":"BGUSD","subscriptionCoin":"USDT",
			"profitCoin":"USDT","exchangeRate":"1","totalUnPayInterestAmount":"0.5","preSettleApr":"0.08",
			"receivedCoin":"USDT","unsettledPoints":"3",
			"bgusdReceiveCoinList":[{"bgusdReceiveCoin":"USDT","bgusdExchangeRate":"1"}],
			"redeemModeList":[{"redeemFeeRate":"0.002","remainQuota":"100","redeemType":"fast","redeemScale":"0.5",
				"redeemDelayDate":"0","minRedeemAmount":"1","redeemTime":"now"}]}}`
	const redeemFixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"6001"}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{
		"/api/v2/earn/elite/redeem-info": infoFixture,
		"/api/v2/earn/elite/redeem":      redeemFixture,
	}, func(t *testing.T, r *http.Request, raw []byte) {
		if r.URL.Path == "/api/v2/earn/elite/redeem" {
			_ = json.Unmarshal(raw, &body)
		}
	})

	var info, err = NewClient(client).Elite().GetRedeemInfo(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetRedeemInfo: %v", err)
	}
	if !info.TotalUnPayInterestAmount.Equal(decimal.RequireFromString("0.5")) || len(info.RedeemModeList) != 1 {
		t.Fatalf("redeem-info: got %+v", info)
	}
	if info.RedeemModeList[0].RedeemType != "fast" || !info.RedeemModeList[0].RedeemFeeRate.Equal(decimal.RequireFromString("0.002")) {
		t.Errorf("redeemMode: got %+v", info.RedeemModeList[0])
	}
	if len(info.BgusdReceiveCoinList) != 1 || info.BgusdReceiveCoinList[0].BgusdReceiveCoin != "USDT" {
		t.Errorf("bgusdList: got %v", info.BgusdReceiveCoinList)
	}

	var orderID string
	orderID, err = NewClient(client).Elite().Redeem(context.Background(), earntypes.EliteRedeemRequest{
		ProductID: "1", ProductSubID: "11", RedeemType: "fast", Amount: "50", ReceiveAccount: "spot",
	})
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if body["productId"] != "1" || body["redeemType"] != "fast" || body["receiveAccount"] != "spot" {
		t.Errorf("body: got %v", body)
	}
	if orderID != "6001" {
		t.Errorf("orderId: got %q", orderID)
	}

	if _, err = NewClient(client).Elite().Redeem(context.Background(), earntypes.EliteRedeemRequest{ProductID: "1", ProductSubID: "11", RedeemType: "fast", Amount: "1"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty receiveAccount: want InvalidRequest, got %v", err)
	}
}
