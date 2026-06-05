/*
FILE: earn/sharkfin_contract_test.go

DESCRIPTION:
Contract tests for the EARN Shark Fin sub-client. Fixtures hand-derived
from the Bitget V2 earn/sharkfin docs / tiagosiebler reference types.

KEY INVARIANTS:

  - no call sends productType;
  - assets require status, records require type, both walking the
    resultList/endId cursor;
  - subscribe posts {productId,amount} and returns {orderId};
  - subscribe-result parses {result,msg}.
*/

package earn

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

func TestContract_SharkFin_GetProducts(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","resultList":[{"productId":"1","productName":"BTC SharkFin","productCoin":"BTC",
			"subscribeCoin":"USDT","farmingStartTime":"1700000000000","farmingEndTime":"1700100000000",
			"lowerRate":"0.01","defaultRate":"0.03","upperRate":"0.08","period":"7","interestStartTime":"1700000000000",
			"status":"in_progress","minAmount":"10","limitAmount":"100000","soldAmount":"500","endTime":"1700200000000",
			"startTime":"1699900000000"}]}}`

	var sawProductType bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/sharkfin/product": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.URL.Query().Get("productType") != "" {
			sawProductType = true
		}
	})

	var prods, err = NewClient(client).SharkFin().GetProducts(context.Background(), "")
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if sawProductType {
		t.Error("earn must not send productType")
	}
	if len(prods) != 1 || prods[0].ProductID != "1" || prods[0].FarmingStartTimeMs != 1700000000000 {
		t.Fatalf("products: got %v", prods)
	}
	if !prods[0].UpperRate.Equal(decimal.RequireFromString("0.08")) || !prods[0].SoldAmount.Equal(decimal.RequireFromString("500")) {
		t.Errorf("row: got %+v", prods[0])
	}
}

func TestContract_SharkFin_GetAccount(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"btcSubscribeAmount":"0.5","usdtSubscribeAmount":"1000","btcHistoricalAmount":"1",
			"usdtHistoricalAmount":"2000","btcTotalEarning":"0.01","usdtTotalEarning":"20"}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/sharkfin/account": fixture}, nil)

	var acc, err = NewClient(client).SharkFin().GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if !acc.USDTSubscribeAmount.Equal(decimal.RequireFromString("1000")) || !acc.USDTHistoricalAmount.Equal(decimal.RequireFromString("2000")) {
		t.Errorf("account: got %+v", acc)
	}
}

func TestContract_SharkFin_GetAssets(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","resultList":[{"productId":"1","interestStartTime":"1700000000000",
			"interestEndTime":"1700100000000","productCoin":"BTC","subscribeCoin":"USDT","trend":"up",
			"settleTime":"1700200000000","interestAmount":"1.5","productStatus":"settled"}]}}`

	var gotStatus string
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/sharkfin/assets": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		gotStatus = r.URL.Query().Get("status")
	})

	var assets, err = NewClient(client).SharkFin().GetAssets(context.Background(), "settled")
	if err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
	if gotStatus != "settled" {
		t.Errorf("status: got %q", gotStatus)
	}
	if len(assets) != 1 || assets[0].SettleTimeMs != 1700200000000 || !assets[0].InterestAmount.Equal(decimal.RequireFromString("1.5")) {
		t.Fatalf("assets: got %v", assets)
	}

	if _, err = NewClient(client).SharkFin().GetAssets(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty status: want InvalidRequest, got %v", err)
	}
}

func TestContract_SharkFin_GetRecords(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","resultList":[{"orderId":"7","product":"BTC SharkFin","period":"7","amount":"100",
			"ts":"1700000000000","type":"subscribe"}]}}`

	var gotType string
	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/sharkfin/records": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		gotType = r.URL.Query().Get("type")
	})

	var recs, err = NewClient(client).SharkFin().GetRecords(context.Background(), SharkFinRecordsQuery{Type: "subscribe"})
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if gotType != "subscribe" {
		t.Errorf("type: got %q", gotType)
	}
	if len(recs) != 1 || recs[0].OrderID != "7" || recs[0].TimeMs != 1700000000000 {
		t.Fatalf("records: got %v", recs)
	}

	if _, err = NewClient(client).SharkFin().GetRecords(context.Background(), SharkFinRecordsQuery{}); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty type: want InvalidRequest, got %v", err)
	}
}

func TestContract_SharkFin_GetSubscribeInfo(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"productCoin":"BTC","subscribeCoin":"USDT","interestTime":"1700000000000",
			"expirationTime":"1700100000000","minPrice":"50000","currentPrice":"60000","maxPrice":"70000",
			"minRate":"0.01","defaultRate":"0.03","maxRate":"0.08","period":"7","productMinAmount":"10",
			"availableBalance":"500","userAmount":"0","remainingAmount":"99000","profitPrecision":"8",
			"subscribePrecision":"2"}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/earn/sharkfin/subscribe-info": fixture}, nil)

	var info, err = NewClient(client).SharkFin().GetSubscribeInfo(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetSubscribeInfo: %v", err)
	}
	if !info.CurrentPrice.Equal(decimal.RequireFromString("60000")) || !info.MaxRate.Equal(decimal.RequireFromString("0.08")) {
		t.Errorf("info: got %+v", info)
	}
	if info.InterestTimeMs != 1700000000000 || info.SubscribePrecision != "2" {
		t.Errorf("info meta: got %+v", info)
	}

	if _, err = NewClient(client).SharkFin().GetSubscribeInfo(context.Background(), ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty productId: want InvalidRequest, got %v", err)
	}
}

func TestContract_SharkFin_Subscribe(t *testing.T) {
	t.Parallel()
	const subFixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"999"}}`
	const resFixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"result":"success","msg":""}}`

	var body map[string]any
	var gotOrderID string
	var _, client = mockBitget(t, map[string]string{
		"/api/v2/earn/sharkfin/subscribe":        subFixture,
		"/api/v2/earn/sharkfin/subscribe-result": resFixture,
	}, func(t *testing.T, r *http.Request, raw []byte) {
		switch r.URL.Path {
		case "/api/v2/earn/sharkfin/subscribe":
			_ = json.Unmarshal(raw, &body)
		case "/api/v2/earn/sharkfin/subscribe-result":
			gotOrderID = r.URL.Query().Get("orderId")
		}
	})

	var orderID, err = NewClient(client).SharkFin().Subscribe(context.Background(), "1", "100")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if body["productId"] != "1" || body["amount"] != "100" {
		t.Errorf("body: got %v", body)
	}
	if orderID != "999" {
		t.Errorf("orderId: got %q", orderID)
	}

	var res, err2 = NewClient(client).SharkFin().GetSubscribeResult(context.Background(), "999")
	if err2 != nil {
		t.Fatalf("GetSubscribeResult: %v", err2)
	}
	if gotOrderID != "999" || res.Result != "success" {
		t.Errorf("result: query=%q got %+v", gotOrderID, res)
	}

	if _, err = NewClient(client).SharkFin().Subscribe(context.Background(), "", "1"); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty productId: want InvalidRequest, got %v", err)
	}
}
