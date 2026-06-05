/*
FILE: convert/convert_contract_test.go

DESCRIPTION:
Contract tests for the CONVERT profile. Fixtures are hand-derived from
the Bitget V2 convert docs / tiagosiebler reference types.

KEY INVARIANTS:

  - no call sends productType (convert is account-level);
  - quoted-price requires fromCoin+toCoin and exactly one size, and sends
    the venue `fromCoinSz`/`toCoinSz` GET keys; trade posts `...Size`;
  - the RFQ (traceId/cnvtPrice) round-trips quoted-price -> trade;
  - convert-record walks the idLessThan/endId cursor and needs a window;
  - BGB coin-list / convert / records parse the nested fee details.
*/

package convert

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	convtypes "github.com/tonymontanov/go-bitget/v2/convert/types"
)

func TestContract_GetCurrencies(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":[{"coin":"ETH","available":"0.9994","maxAmount":"5","minAmount":"0.0005"}]}`

	var sawProductType bool
	var _, client = mockBitget(t, map[string]string{"/api/v2/convert/currencies": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		if r.URL.Query().Get("productType") != "" {
			sawProductType = true
		}
	})

	var cur, err = NewClient(client).GetCurrencies(context.Background())
	if err != nil {
		t.Fatalf("GetCurrencies: %v", err)
	}
	if sawProductType {
		t.Error("convert must not send productType")
	}
	if len(cur) != 1 || cur[0].Coin != "ETH" {
		t.Fatalf("currencies: got %v", cur)
	}
	if !cur[0].MaxAmount.Equal(decimal.RequireFromString("5")) || !cur[0].MinAmount.Equal(decimal.RequireFromString("0.0005")) {
		t.Errorf("bounds: got %s/%s", cur[0].MaxAmount, cur[0].MinAmount)
	}
}

func TestContract_GetQuotedPrice(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"fee":"0","fromCoinSize":"100","fromCoin":"USDT","cnvtPrice":"0.0005226794534969",
			"toCoinSize":"0.23206967","toCoin":"ETH","traceId":"42"}}`

	var gotFrom, gotTo, gotFromSz, gotFromSize string
	var _, client = mockBitget(t, map[string]string{"/api/v2/convert/quoted-price": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		gotFrom = r.URL.Query().Get("fromCoin")
		gotTo = r.URL.Query().Get("toCoin")
		gotFromSz = r.URL.Query().Get("fromCoinSz")
		gotFromSize = r.URL.Query().Get("fromCoinSize")
	})

	var q, err = NewClient(client).GetQuotedPrice(context.Background(), "USDT", "ETH", "100", "")
	if err != nil {
		t.Fatalf("GetQuotedPrice: %v", err)
	}
	if gotFrom != "USDT" || gotTo != "ETH" {
		t.Errorf("coins: got %s->%s", gotFrom, gotTo)
	}
	if gotFromSz != "100" {
		t.Errorf("expected fromCoinSz=100 on the GET wire, got %q (fromCoinSize=%q)", gotFromSz, gotFromSize)
	}
	if q.TraceID != "42" || !q.CnvtPrice.Equal(decimal.RequireFromString("0.0005226794534969")) {
		t.Errorf("rfq: got traceId=%q price=%s", q.TraceID, q.CnvtPrice)
	}
	if !q.ToCoinSize.Equal(decimal.RequireFromString("0.23206967")) {
		t.Errorf("toCoinSize: got %s", q.ToCoinSize)
	}
}

func TestContract_GetQuotedPrice_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var c = NewClient(client)
	if _, err := c.GetQuotedPrice(context.Background(), "", "ETH", "1", ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty fromCoin: want InvalidRequest, got %v", err)
	}
	if _, err := c.GetQuotedPrice(context.Background(), "USDT", "ETH", "", ""); !bitget.IsInvalidRequest(err) {
		t.Errorf("no size: want InvalidRequest, got %v", err)
	}
	if _, err := c.GetQuotedPrice(context.Background(), "USDT", "ETH", "1", "1"); !bitget.IsInvalidRequest(err) {
		t.Errorf("both sizes: want InvalidRequest, got %v", err)
	}
}

func TestContract_Trade(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"ts":"1688527221603","cnvtPrice":"0.00052268","toCoinSize":"0.23206967","toCoin":"ETH"}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/convert/trade": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var res, err = NewClient(client).Trade(context.Background(), convtypes.TradeRequest{
		FromCoin: "USDT", ToCoin: "ETH", FromCoinSize: "444", ToCoinSize: "0.23206967",
		CnvtPrice: "0.0005226794534969", TraceID: "1",
	})
	if err != nil {
		t.Fatalf("Trade: %v", err)
	}
	if body["fromCoinSize"] != "444" || body["toCoinSize"] != "0.23206967" || body["traceId"] != "1" {
		t.Errorf("body: got %v", body)
	}
	if _, present := body["productType"]; present {
		t.Errorf("convert must not send productType: %v", body)
	}
	if res.ToCoin != "ETH" || res.TimeMs != 1688527221603 || !res.ToCoinSize.Equal(decimal.RequireFromString("0.23206967")) {
		t.Errorf("result: got %+v", res)
	}
}

func TestContract_Trade_Guards(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var c = NewClient(client)
	var base = convtypes.TradeRequest{FromCoin: "USDT", ToCoin: "ETH", FromCoinSize: "1", ToCoinSize: "1", CnvtPrice: "1", TraceID: "1"}

	var noTrace = base
	noTrace.TraceID = ""
	if _, err := c.Trade(context.Background(), noTrace); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty traceId: want InvalidRequest, got %v", err)
	}
	var noPrice = base
	noPrice.CnvtPrice = ""
	if _, err := c.Trade(context.Background(), noPrice); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty cnvtPrice: want InvalidRequest, got %v", err)
	}
}

func TestContract_GetHistory(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"endId":"","dataList":[{"id":"1","ts":"1688527512229","cnvtPrice":"0.00052268","fee":"0",
			"fromCoinSize":"100","fromCoin":"USDT","toCoinSize":"0.23206967","toCoin":"ETH"}]}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/convert/convert-record": fixture}, nil)

	var recs, err = NewClient(client).GetHistory(context.Background(), 1686128558000, 1686214958000)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(recs) != 1 || recs[0].ID != "1" {
		t.Fatalf("records: got %v", recs)
	}
	if recs[0].FromCoin != "USDT" || recs[0].ToCoin != "ETH" || recs[0].TimeMs != 1688527512229 {
		t.Errorf("row: got %+v", recs[0])
	}
}

func TestContract_GetHistory_Guard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	if _, err := NewClient(client).GetHistory(context.Background(), 0, 0); !bitget.IsInvalidRequest(err) {
		t.Errorf("no window: want InvalidRequest, got %v", err)
	}
}

func TestContract_GetBGBCoins(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"coinList":[{"coin":"EOS","available":"12.3","bgbEstAmount":"1.05","precision":"4",
			"feeDetail":[{"feeRate":"0.001","fee":"0.012"}],"cTime":"1700000000000"}]}}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/convert/bgb-convert-coin-list": fixture}, nil)

	var coins, err = NewClient(client).GetBGBCoins(context.Background())
	if err != nil {
		t.Fatalf("GetBGBCoins: %v", err)
	}
	if len(coins) != 1 || coins[0].Coin != "EOS" {
		t.Fatalf("coins: got %v", coins)
	}
	if !coins[0].BGBEstAmount.Equal(decimal.RequireFromString("1.05")) {
		t.Errorf("bgbEstAmount: got %s", coins[0].BGBEstAmount)
	}
	if len(coins[0].FeeDetail) != 1 || !coins[0].FeeDetail[0].FeeRate.Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("feeDetail: got %v", coins[0].FeeDetail)
	}
}

func TestContract_ConvertBGB(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":{"orderList":[{"coin":"EOS","orderId":"1233431213"},{"coin":"GROK","orderId":"1233431214"}]}}`

	var body map[string]any
	var _, client = mockBitget(t, map[string]string{"/api/v2/convert/bgb-convert": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		_ = json.Unmarshal(raw, &body)
	})

	var orders, err = NewClient(client).ConvertBGB(context.Background(), []string{"EOS", "GROK"})
	if err != nil {
		t.Fatalf("ConvertBGB: %v", err)
	}
	var list, ok = body["coinList"].([]any)
	if !ok || len(list) != 2 || list[0] != "EOS" {
		t.Errorf("coinList body: got %v", body["coinList"])
	}
	if len(orders) != 2 || orders[1].Coin != "GROK" || orders[1].OrderID != "1233431214" {
		t.Errorf("orders: got %v", orders)
	}
	if _, err = NewClient(client).ConvertBGB(context.Background(), nil); !bitget.IsInvalidRequest(err) {
		t.Errorf("empty coins: want InvalidRequest, got %v", err)
	}
}

func TestContract_GetBGBHistory(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,
		"data":[{"orderId":"1","fromCoin":"EOS","fromAmount":"12.3","fromCoinPrice":"0.8","toCoin":"BGB",
			"toAmount":"1.05","toCoinPrice":"0.5","feeDetail":[{"feeCoin":"BGB","fee":"0.001"}],
			"status":"success","ctime":"1700000000000"}]}`

	var _, client = mockBitget(t, map[string]string{"/api/v2/convert/bgb-convert-records": fixture}, nil)

	var hist, err = NewClient(client).GetBGBHistory(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("GetBGBHistory: %v", err)
	}
	if len(hist) != 1 || hist[0].FromCoin != "EOS" || hist[0].ToCoin != "BGB" {
		t.Fatalf("history: got %v", hist)
	}
	if !hist[0].ToAmount.Equal(decimal.RequireFromString("1.05")) || hist[0].Status != "success" {
		t.Errorf("row: got %+v", hist[0])
	}
	if len(hist[0].FeeDetail) != 1 || hist[0].FeeDetail[0].FeeCoin != "BGB" {
		t.Errorf("feeDetail: got %v", hist[0].FeeDetail)
	}
}
