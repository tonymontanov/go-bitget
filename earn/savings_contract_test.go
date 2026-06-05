/*
FILE: earn/savings_contract_test.go

DESCRIPTION:
Contract tests for the EARN Savings sub-client + the Earn account
overview. Fixtures are hand-derived from the Bitget V2 earn docs /
tiagosiebler reference types.

KEY INVARIANTS:

  - no call sends productType (earn is account-level);
  - assets / records require a periodType and walk the resultList/endId
    cursor;
  - subscribe / redeem post the documented body and parse the small
    {orderId} / {orderId,status} responses;
  - subscribe-result / redeem-result parse {result,msg}.
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

func TestContract_Account_GetAssets(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":[{"coin":"BTC","amount":"0.10000000"},{"coin":"USDT","amount":"400.00000000"}]}`

	var sawProductType bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/account/assets": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.URL.Query().Get("productType") != "" {
			sawProductType = true
		}
	})

	var assets, err = NewClient(client).Account().GetAssets(context.Background(), "")
	if err != nil {
		t.Fatalf("Account.GetAssets: %v", err)
	}
	if sawProductType {
		t.Error("earn must not send productType")
	}
	if len(assets) != 2 || assets[1].Coin != "USDT" || !assets[1].Amount.Equal(decimal.RequireFromString("400")) {
		t.Fatalf("assets: got %v", assets)
	}
}

func TestContract_Savings_GetProducts(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":[{"productId":"1","coin":"USDT","periodType":"flexible","period":"","apyType":"ladder",
			"advanceRedeem":"Yes","settleMethod":"daily","status":"in_progress","productLevel":"normal",
			"apyList":[{"rateLevel":"1","minStepVal":"0","maxStepVal":"1000","currentApy":"0.05"}]}]}`

	var gotFilter string
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/savings/product": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		gotFilter = r.URL.Query().Get("filter")
	})

	var prods, err = NewClient(client).Savings().GetProducts(context.Background(), "", "available_and_held")
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if gotFilter != "available_and_held" {
		t.Errorf("filter: got %q", gotFilter)
	}
	if len(prods) != 1 || prods[0].ProductID != "1" || prods[0].PeriodType != "flexible" {
		t.Fatalf("products: got %v", prods)
	}
	if len(prods[0].ApyList) != 1 || !prods[0].ApyList[0].CurrentApy.Equal(decimal.RequireFromString("0.05")) {
		t.Errorf("apyList: got %v", prods[0].ApyList)
	}
}

func TestContract_Savings_GetAccount(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"btcAmount":"0.5","usdtAmount":"1000","btc24hEarning":"0.001","usdt24hEarning":"2",
			"btcTotalEarning":"0.01","usdtTotalEarning":"20"}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/savings/account": fixture}, nil)

	var acc, err = NewClient(client).Savings().GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if !acc.USDTAmount.Equal(decimal.RequireFromString("1000")) || !acc.BTCTotalEarning.Equal(decimal.RequireFromString("0.01")) {
		t.Errorf("account: got %+v", acc)
	}
}

func TestContract_Savings_GetAssets(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","resultList":[{"productId":"1","orderId":"9","productCoin":"USDT","interestCoin":"USDT",
			"periodType":"flexible","period":"","holdAmount":"100","lastProfit":"0.01","totalProfit":"0.5",
			"holdDays":"12","status":"in_progress","allowRedemption":"Yes","productLevel":"normal",
			"apy":[{"rateLevel":"1","minApy":"0.01","maxApy":"0.08","currentApy":"0.05"}]}]}}`

	var gotPeriod string
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/savings/assets": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		gotPeriod = r.URL.Query().Get("periodType")
	})

	var assets, err = NewClient(client).Savings().GetAssets(context.Background(), "flexible")
	if err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
	if gotPeriod != "flexible" {
		t.Errorf("periodType: got %q", gotPeriod)
	}
	if len(assets) != 1 || assets[0].OrderID != "9" || assets[0].HoldDays != 12 {
		t.Fatalf("assets: got %v", assets)
	}
	if !assets[0].HoldAmount.Equal(decimal.RequireFromString("100")) || len(assets[0].Apy) != 1 {
		t.Errorf("row: got %+v", assets[0])
	}

	if _, err = NewClient(client).Savings().GetAssets(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty periodType: want InvalidRequest, got %v", err)
	}
}

func TestContract_Savings_GetRecords(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","resultList":[{"orderId":"7","coinName":"USDT","settleCoinName":"USDT",
			"productType":"flexible","period":"","productLevel":"normal","amount":"50","ts":"1700000000000",
			"orderType":"subscribe"}]}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/savings/records": fixture}, nil)

	var recs, err = NewClient(client).Savings().GetRecords(context.Background(), SavingsRecordsQuery{PeriodType: "flexible"})
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(recs) != 1 || recs[0].OrderID != "7" || recs[0].OrderType != "subscribe" || recs[0].TimeMs != 1700000000000 {
		t.Fatalf("records: got %v", recs)
	}

	if _, err = NewClient(client).Savings().GetRecords(context.Background(), SavingsRecordsQuery{}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty periodType: want InvalidRequest, got %v", err)
	}
}

func TestContract_Savings_GetSubscribeInfo(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"singleMinAmount":"1","singleMaxAmount":"100000","remainingAmount":"5000",
			"subscribePrecision":"2","profitPrecision":"8","subscribeTime":"1","interestTime":"2",
			"settleTime":"3","expireTime":"4","redeemTime":"5","settleMethod":"daily","redeemDelay":"0",
			"apyList":[{"rateLevel":"1","minStepVal":"0","maxStepVal":"1000","currentApy":"0.05"}]}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/savings/subscribe-info": fixture}, nil)

	var info, err = NewClient(client).Savings().GetSubscribeInfo(context.Background(), "1", "flexible")
	if err != nil {
		t.Fatalf("GetSubscribeInfo: %v", err)
	}
	if !info.SingleMinAmount.Equal(decimal.RequireFromString("1")) || !info.RemainingAmount.Equal(decimal.RequireFromString("5000")) {
		t.Errorf("amounts: got %+v", info)
	}
	if info.SubscribePrecision != "2" || len(info.ApyList) != 1 {
		t.Errorf("info: got %+v", info)
	}

	if _, err = NewClient(client).Savings().GetSubscribeInfo(context.Background(), "", "flexible"); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty productId: want InvalidRequest, got %v", err)
	}
}

func TestContract_Savings_Subscribe(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"1313060074239184896"}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/savings/subscribe": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var orderID, err = NewClient(client).Savings().Subscribe(context.Background(), "23123123", "flexible", "99999999")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if body["productId"] != "23123123" || body["periodType"] != "flexible" || body["amount"] != "99999999" {
		t.Errorf("body: got %v", body)
	}
	if orderID != "1313060074239184896" {
		t.Errorf("orderId: got %q", orderID)
	}

	if _, err = NewClient(client).Savings().Subscribe(context.Background(), "", "flexible", "1"); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty productId: want InvalidRequest, got %v", err)
	}
}

func TestContract_Savings_Redeem(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"123123123","status":"2000.000000"}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/savings/redeem": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var res, err = NewClient(client).Savings().Redeem(context.Background(), earntypes.SavingsRedeemRequest{
		ProductID: "23123123", PeriodType: "flexible", Amount: "99999999",
	})
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if _, present := body["orderId"]; present {
		t.Errorf("orderId must be omitted when empty: %v", body)
	}
	if res.OrderID != "123123123" || res.Status != "2000.000000" {
		t.Errorf("result: got %+v", res)
	}

	if _, err = NewClient(client).Savings().Redeem(context.Background(), earntypes.SavingsRedeemRequest{ProductID: "1", PeriodType: "flexible"}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty amount: want InvalidRequest, got %v", err)
	}
}

func TestContract_Savings_Results(t *testing.T) {
	t.Parallel()
	const subFixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"result":"success","msg":""}}`
	const redFixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"result":"fail","msg":"too soon"}}`

	var gotSubQuery, gotRedQuery string
	var _, client = mockBitget(t, map[string]string{
		"/api/v2/earn/savings/subscribe-result": subFixture,
		"/api/v2/earn/savings/redeem-result":    redFixture,
	}, func(t *testing.T, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/api/v2/earn/savings/subscribe-result":
			gotSubQuery = r.URL.Query().Get("productId")
		case "/api/v2/earn/savings/redeem-result":
			gotRedQuery = r.URL.Query().Get("orderId")
		}
	})

	var sub, err = NewClient(client).Savings().GetSubscribeResult(context.Background(), "23123123", "flexible")
	if err != nil {
		t.Fatalf("GetSubscribeResult: %v", err)
	}
	if gotSubQuery != "23123123" || sub.Result != "success" {
		t.Errorf("subscribe-result: query=%q got %+v", gotSubQuery, sub)
	}

	var red earntypes.OpResult
	red, err = NewClient(client).Savings().GetRedeemResult(context.Background(), "123123", "flexible")
	if err != nil {
		t.Fatalf("GetRedeemResult: %v", err)
	}
	if gotRedQuery != "123123" || red.Result != "fail" || red.Msg != "too soon" {
		t.Errorf("redeem-result: query=%q got %+v", gotRedQuery, red)
	}

	if _, err = NewClient(client).Savings().GetRedeemResult(context.Background(), "", "flexible"); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty orderId: want InvalidRequest, got %v", err)
	}
}
