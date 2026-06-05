/*
FILE: uta/public_contract_test.go

DESCRIPTION:
Contract tests for the V3 UTA Public sub-client: the unsigned invariant on
market reads, the demo `paptrading: 1` header, the category guards, and
the instrument / ticker / orderbook / candle / fill decoding (incl. the
array-of-arrays candle shape).
*/

package uta

import (
	"context"
	"net/http"
	"testing"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func TestContract_Public_ServerTime_UnsignedAndDemo(t *testing.T) {
	t.Parallel()
	var sawSign bool
	var sawPap string
	var _, client = mockBitgetDynamic(t, true, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Header.Get("ACCESS-SIGN") != "" {
			sawSign = true
		}
		sawPap = r.Header.Get("paptrading")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"serverTime":"1700000000123"}}`))
	})
	var uc = utaClient(t, client)

	var ms, err = uc.Public().GetServerTime(context.Background())
	if err != nil {
		t.Fatalf("GetServerTime: %v", err)
	}
	if ms != 1700000000123 {
		t.Fatalf("want 1700000000123, got %d", ms)
	}
	if sawSign {
		t.Error("public/time must be unsigned")
	}
	if sawPap != "1" {
		t.Errorf("demo mode must send paptrading:1, got %q", sawPap)
	}
}

func TestContract_Public_NoDemoNoHeader(t *testing.T) {
	t.Parallel()
	var sawPap string
	var _, client = mockBitgetDynamic(t, false, func(w http.ResponseWriter, r *http.Request, body []byte) {
		sawPap = r.Header.Get("paptrading")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"serverTime":"1"}}`))
	})
	var uc = utaClient(t, client)
	if _, err := uc.Public().GetServerTime(context.Background()); err != nil {
		t.Fatalf("GetServerTime: %v", err)
	}
	if sawPap != "" {
		t.Errorf("non-demo must not send paptrading, got %q", sawPap)
	}
}

func TestContract_Public_Instruments(t *testing.T) {
	t.Parallel()
	var sawCat string
	var routes = map[string]string{
		"/api/v3/market/instruments": `{"code":"00000","msg":"success","data":[{"symbol":"BTCUSDT","category":"USDT-FUTURES","baseCoin":"BTC","quoteCoin":"USDT","status":"online","pricePrecision":"1","quantityPrecision":"3","quotePrecision":"6","minOrderQty":"0.001","maxOrderQty":"1000","maxMarketOrderQty":"100","minOrderAmount":"5","buyLimitPriceRatio":"0.02","sellLimitPriceRatio":"0.02","makerFeeRate":"0.0002","takerFeeRate":"0.0006","symbolType":"perpetual","minLeverage":"1","maxLeverage":"125","fundInterval":"8","launchTime":"1600000000000","deliveryTime":"0"}]}`,
	}
	var _, client = mockBitget(t, routes, func(t *testing.T, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v3/market/instruments" {
			sawCat = r.URL.Query().Get("category")
		}
	})
	var uc = utaClient(t, client)

	var insts, err = uc.Public().GetInstruments(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT")
	if err != nil {
		t.Fatalf("GetInstruments: %v", err)
	}
	if sawCat != "USDT-FUTURES" {
		t.Errorf("category query mismatch: %q", sawCat)
	}
	if len(insts) != 1 {
		t.Fatalf("want 1 instrument, got %d", len(insts))
	}
	var in = insts[0]
	if in.Symbol != "BTCUSDT" || in.PricePrecision != 1 || in.QuantityPrecision != 3 {
		t.Fatalf("unexpected instrument: %+v", in)
	}
	if !in.MinOrderQty.Equal(decv("0.001")) || !in.MaxLeverage.Equal(decv("125")) || in.LaunchTimeMs != 1600000000000 {
		t.Fatalf("unexpected instrument decimals: %+v", in)
	}

	// Guard.
	if _, err = uc.Public().GetInstruments(context.Background(), "", ""); err == nil {
		t.Error("GetInstruments(no category): want guard error")
	}
}

func TestContract_Public_TickersAndBook(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v3/market/tickers":   `{"code":"00000","msg":"success","data":[{"category":"USDT-FUTURES","symbol":"BTCUSDT","lastPrice":"60000","openPrice24h":"59000","highPrice24h":"61000","lowPrice24h":"58000","ask1Price":"60001","bid1Price":"59999","bid1Size":"2","ask1Size":"3","price24hPcnt":"0.017","volume24h":"1234","turnover24h":"74000000","indexPrice":"60005","markPrice":"60002","fundingRate":"0.0001","openInterest":"5000"}]}`,
		"/api/v3/market/orderbook": `{"code":"00000","msg":"success","data":{"a":[["60001","3"],["60002","1"]],"b":[["59999","2"]],"ts":"1700000000000"}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var tks, terr = uc.Public().GetTickers(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT")
	if terr != nil || len(tks) != 1 {
		t.Fatalf("GetTickers: %v %+v", terr, tks)
	}
	if !tks[0].LastPrice.Equal(decv("60000")) || !tks[0].FundingRate.Equal(decv("0.0001")) {
		t.Fatalf("unexpected ticker: %+v", tks[0])
	}

	var ob, oerr = uc.Public().GetOrderBook(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT", 5)
	if oerr != nil {
		t.Fatalf("GetOrderBook: %v", oerr)
	}
	if len(ob.Asks) != 2 || len(ob.Bids) != 1 || !ob.Asks[0].Price.Equal(decv("60001")) || ob.TimeMs != 1700000000000 {
		t.Fatalf("unexpected book: %+v", ob)
	}

	// Guards.
	if _, err := uc.Public().GetOrderBook(ctx, utatypes.CategoryUSDTFutures, "", 0); err == nil {
		t.Error("GetOrderBook(no symbol): want guard")
	}
}

func TestContract_Public_CandlesAndFills(t *testing.T) {
	t.Parallel()
	var routes = map[string]string{
		"/api/v3/market/candles":         `{"code":"00000","msg":"success","data":[["1700000000000","59000","61000","58000","60000","1234","74000000"],["1700000060000","60000","60500","59800","60200","500","30000000"]]}`,
		"/api/v3/market/history-candles": `{"code":"00000","msg":"success","data":[["1699990000000","58000","58500","57000","58200","999","58000000"]]}`,
		"/api/v3/market/fills":           `{"code":"00000","msg":"success","data":[{"execId":"e1","price":"60000","size":"0.5","side":"buy","ts":"1700000000000"}]}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var ks, kerr = uc.Public().GetCandles(ctx, CandlesQuery{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", Interval: utatypes.Interval1m})
	if kerr != nil || len(ks) != 2 {
		t.Fatalf("GetCandles: %v %+v", kerr, ks)
	}
	if ks[0].TimeMs != 1700000000000 || !ks[0].Close.Equal(decv("60000")) || !ks[0].Turnover.Equal(decv("74000000")) {
		t.Fatalf("unexpected candle: %+v", ks[0])
	}

	var hk, herr = uc.Public().GetHistoryCandles(ctx, CandlesQuery{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", Interval: utatypes.Interval1m})
	if herr != nil || len(hk) != 1 || !hk[0].Open.Equal(decv("58000")) {
		t.Fatalf("GetHistoryCandles: %v %+v", herr, hk)
	}

	var fills, ferr = uc.Public().GetPublicFills(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT", 10)
	if ferr != nil || len(fills) != 1 || fills[0].ExecID != "e1" || !fills[0].Size.Equal(decv("0.5")) {
		t.Fatalf("GetPublicFills: %v %+v", ferr, fills)
	}

	// Guard.
	if _, err := uc.Public().GetCandles(ctx, CandlesQuery{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT"}); err == nil {
		t.Error("GetCandles(no interval): want guard")
	}
}
