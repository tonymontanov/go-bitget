/*
FILE: common/tax_p2p_contract_test.go

DESCRIPTION:
Contract tests for the common Tax + P2P sub-clients: window guards, the
idLessThan cursor stitching for the flat tax arrays and the enveloped P2P
lists, and the nested order/ad decoding.
*/

package common

import (
	"context"
	"net/http"
	"strconv"
	"testing"
)

func TestContract_Tax_Spot_WindowGuard(t *testing.T) {
	t.Parallel()
	var _, client = mockBitget(t, map[string]string{}, nil)
	var cc = commonClient(t, client)
	if _, err := cc.Tax().GetSpotRecords(context.Background(), TaxQuery{}); err == nil {
		t.Error("GetSpotRecords(no window): want guard error")
	}
}

func TestContract_Tax_Spot_CursorStitch(t *testing.T) {
	t.Parallel()
	// Page 0 returns a full 100-row page (forces a second fetch); page 1
	// returns a short page (stops). We synthesise 100 rows then 1 row.
	var calls int
	var routes = map[string]string{} // unused; dynamic handler below
	_ = routes
	var _, client = mockBitgetDynamic(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.URL.Path != "/api/v2/tax/spot-record" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"40404","msg":"x","data":null}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var ilt = r.URL.Query().Get("idLessThan")
		if calls == 0 {
			if ilt != "" {
				t.Errorf("page0 idLessThan should be empty, got %q", ilt)
			}
			// 100 rows, ids 200..101 descending; last id 101.
			var b = `{"code":"00000","msg":"success","data":[`
			var i int
			for i = 0; i < 100; i++ {
				if i > 0 {
					b += ","
				}
				var id = strconv.Itoa(200 - i)
				b += `{"id":"` + id + `","coin":"USDT","spotTaxType":"trade","amount":"1","fee":"0.1","balance":"10","ts":"1700000000000"}`
			}
			b += `]}`
			calls++
			_, _ = w.Write([]byte(b))
			return
		}
		if ilt != "101" {
			t.Errorf("page1 idLessThan want 101, got %q", ilt)
		}
		calls++
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":[{"id":"100","coin":"BTC","spotTaxType":"trade","amount":"2","fee":"0.2","balance":"5","ts":"1700000000001"}]}`))
	})
	var cc = commonClient(t, client)

	var recs, err = cc.Tax().GetSpotRecords(context.Background(), TaxQuery{StartTimeMs: 1, EndTimeMs: 2})
	if err != nil {
		t.Fatalf("GetSpotRecords: %v", err)
	}
	if len(recs) != 101 {
		t.Fatalf("want 101 stitched rows, got %d", len(recs))
	}
	if recs[100].ID != "100" || !recs[100].Amount.Equal(dec("2")) {
		t.Fatalf("unexpected last row: %+v", recs[100])
	}
	if calls != 2 {
		t.Fatalf("want 2 fetches, got %d", calls)
	}
}

func TestContract_Tax_FuturesMarginP2P(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/tax/future-record": `{"code":"00000","msg":"success","data":[{"id":"1","symbol":"BTCUSDT","marginCoin":"USDT","futureTaxType":"fee","amount":"3","fee":"0.3","ts":"1700000000000"}]}`,
		"/api/v2/tax/margin-record": `{"code":"00000","msg":"success","data":[{"id":"2","coin":"USDT","marginTaxType":"interest","amount":"4","fee":"0.4","total":"4.4","symbol":"BTCUSDT","ts":"1700000000001"}]}`,
		"/api/v2/tax/p2p-record":    `{"code":"00000","msg":"success","data":[{"id":"3","coin":"USDT","p2pTaxType":"sell","total":"5","ts":"1700000000002"}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var cc = commonClient(t, client)
	var ctx = context.Background()

	var f, ferr = cc.Tax().GetFuturesRecords(ctx, FuturesTaxQuery{StartTimeMs: 1, EndTimeMs: 2, ProductType: "USDT-FUTURES"})
	if ferr != nil || len(f) != 1 || f[0].Symbol != "BTCUSDT" || !f[0].Fee.Equal(dec("0.3")) {
		t.Fatalf("futures: %v %+v", ferr, f)
	}
	var m, merr = cc.Tax().GetMarginRecords(ctx, MarginTaxQuery{StartTimeMs: 1, EndTimeMs: 2, MarginType: "isolated"})
	if merr != nil || len(m) != 1 || !m[0].Total.Equal(dec("4.4")) {
		t.Fatalf("margin: %v %+v", merr, m)
	}
	var p, perr = cc.Tax().GetP2PRecords(ctx, TaxQuery{StartTimeMs: 1, EndTimeMs: 2})
	if perr != nil || len(p) != 1 || !p[0].Total.Equal(dec("5")) || p[0].TaxType != "sell" {
		t.Fatalf("p2p: %v %+v", perr, p)
	}
}

func TestContract_P2P_MerchantsAndInfo(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/p2p/merchantList": `{"code":"00000","msg":"success","data":{"minMerchantId":"","merchantList":[{"registerTime":"1700000000000","nickName":"alice","isOnline":"yes","avgPaymentTime":"5","avgReleaseTime":"6","totalTrades":"100","totalBuy":"60","totalSell":"40","totalCompletionRate":"0.99","trades30d":"10","sell30d":"4","buy30d":"6","completionRate30d":"1.0"}]}}`,
		"/api/v2/p2p/merchantInfo": `{"code":"00000","msg":"success","data":{"registerTime":"1700000000000","nickName":"me","merchantId":"m1","avgPaymentTime":"5","avgReleaseTime":"6","totalTrades":"100","totalBuy":"60","totalSell":"40","totalCompletionRate":"0.99","trades30d":"10","sell30d":"4","buy30d":"6","completionRate30d":"1.0","kycStatus":true,"emailBindStatus":true,"mobileBindStatus":false,"email":"a@b.c","mobile":""}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var cc = commonClient(t, client)
	var ctx = context.Background()

	var ms, err = cc.P2P().GetMerchants(ctx, "yes")
	if err != nil {
		t.Fatalf("GetMerchants: %v", err)
	}
	if len(ms) != 1 || ms[0].NickName != "alice" || ms[0].TotalTrades != 100 || !ms[0].TotalCompletionRate.Equal(dec("0.99")) {
		t.Fatalf("unexpected merchants: %+v", ms)
	}

	var info, ierr = cc.P2P().GetMerchantInfo(ctx)
	if ierr != nil {
		t.Fatalf("GetMerchantInfo: %v", ierr)
	}
	if info.MerchantID != "m1" || !info.KycStatus || info.MobileBindStatus || info.Email != "a@b.c" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestContract_P2P_OrdersAndAds(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v2/p2p/orderList": `{"code":"00000","msg":"success","data":{"minOrderId":"","orderList":[{"orderId":"o1","orderNo":"no1","advNo":"a1","side":"buy","count":"1.5","coin":"USDT","price":"1.01","fiat":"EUR","withdrawTime":"0","representTime":"0","releaseTime":"1700000000002","paymentTime":"1700000000001","amount":"1.515","status":"completed","buyerRealName":"B","sellerRealName":"S","ctime":"1700000000000","utime":"1700000000003","paymentInfo":{"paymethodName":"SEPA","paymethodId":"pm1","paymethodInfo":[{"name":"IBAN","required":"true","type":"text","value":"DE..."}]}}]}}`,
		"/api/v2/p2p/advList":   `{"code":"00000","msg":"success","data":{"minAdvId":"","advList":[{"advId":"ad1","advNo":"an1","side":"sell","advSize":"100","size":"50","coin":"USDT","price":"1.02","coinPrecision":"2","fiat":"EUR","fiatPrecision":"2","fiatSymbol":"€","status":"online","hide":"no","maxTradeAmount":"500","minTradeAmount":"10","payDuration":"15","turnoverNum":"7","turnoverRate":"0.5","label":null,"userLimitList":{"minCompleteNum":"0","maxCompleteNum":"0","placeOrderNum":"0","allowMerchantPlace":"yes","completeRate30d":"0.9","country":"DE"},"paymentMethodList":[{"paymentMethod":"SEPA","paymentId":"pm1","paymentInfo":[{"name":"IBAN","required":true,"type":"text"}]}],"merchantCertifiedList":[{"imageUrl":"http://x","desc":"verified"}],"utime":"1700000000003","ctime":"1700000000000"}]}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var cc = commonClient(t, client)
	var ctx = context.Background()

	var orders, oerr = cc.P2P().GetOrders(ctx, P2POrdersQuery{StartTimeMs: 1, AdvNo: "a1", Language: "en_US"})
	if oerr != nil {
		t.Fatalf("GetOrders: %v", oerr)
	}
	if len(orders) != 1 {
		t.Fatalf("want 1 order, got %d", len(orders))
	}
	var o = orders[0]
	if o.OrderID != "o1" || !o.Amount.Equal(dec("1.515")) || o.ReleaseTimeMs != 1700000000002 {
		t.Fatalf("unexpected order: %+v", o)
	}
	if o.PaymentInfo.PaymethodName != "SEPA" || len(o.PaymentInfo.PaymethodInfo) != 1 || o.PaymentInfo.PaymethodInfo[0].Value != "DE..." {
		t.Fatalf("unexpected paymentInfo: %+v", o.PaymentInfo)
	}

	var ads, aerr = cc.P2P().GetAdvertisements(ctx, P2PAdsQuery{StartTimeMs: 1, Status: "online", Side: "sell", Coin: "USDT", Fiat: "EUR"})
	if aerr != nil {
		t.Fatalf("GetAdvertisements: %v", aerr)
	}
	if len(ads) != 1 {
		t.Fatalf("want 1 ad, got %d", len(ads))
	}
	var ad = ads[0]
	if ad.AdvID != "ad1" || !ad.Price.Equal(dec("1.02")) || ad.TurnoverNum != 7 || ad.FiatSymbol != "€" {
		t.Fatalf("unexpected ad: %+v", ad)
	}
	if ad.UserLimit.Country != "DE" || len(ad.PaymentMethods) != 1 || !ad.PaymentMethods[0].PaymentInfo[0].Required {
		t.Fatalf("unexpected ad nested: %+v", ad)
	}
	if len(ad.Certified) != 1 || ad.Certified[0].Desc != "verified" {
		t.Fatalf("unexpected certified: %+v", ad.Certified)
	}

	// Guards.
	if _, err := cc.P2P().GetOrders(ctx, P2POrdersQuery{AdvNo: "a", Language: "en"}); err == nil {
		t.Error("GetOrders(no startTime): want guard")
	}
	if _, err := cc.P2P().GetAdvertisements(ctx, P2PAdsQuery{StartTimeMs: 1, Status: "online", Side: "sell", Coin: "USDT"}); err == nil {
		t.Error("GetAdvertisements(no fiat): want guard")
	}
}
