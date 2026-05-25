/*
FILE: mix/contract_test.go

DESCRIPTION:
Contract tests for the MIX profile. They verify that the parser maps
real Bitget V2 JSON envelopes into domain structs. Each fixture is
hand-derived from the public Bitget V2 documentation
(https://www.bitget.com/api-doc/contract/...) and trimmed to the
fields the SDK consumes.

Coverage (M1):

  - GetSymbolInfo         : /api/v2/mix/market/contracts (happy + 404)
  - GetOrderBook          : /api/v2/mix/market/merge-depth (happy + depth clamp)
  - GetMarketTicker       : /api/v2/mix/market/ticker
  - GetHistoricalCandles  : /api/v2/mix/market/candles (preserves ASC order)

Tests use a local httptest.Server; no network calls are made. Trading
and account stubs are exercised separately to confirm they fail with
the expected ErrorKindInvalidRequest.
*/

package mix

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// mockBitget starts an httptest.Server that routes requests by path to
// a canned JSON envelope. Unknown paths return a Bitget-style 404
// envelope so the test fails loudly when the SDK targets an unexpected
// endpoint. Rate-limit headers are set on every reply so the observer
// pipeline is exercised.
//
// The optional `inspect` callback runs per-request before the canned
// response is written and lets a test assert query parameters.
func mockBitget(
	t *testing.T,
	routes map[string]string,
	inspect func(t *testing.T, r *http.Request),
) (*httptest.Server, *bitget.Client) {
	t.Helper()

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inspect != nil {
			inspect(t, r)
		}
		var body string
		var ok bool
		body, ok = routes[r.URL.Path]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":"40404","msg":"no fixture for `+r.URL.Path+`","data":null,"requestTime":0}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "20")
		w.Header().Set("X-RateLimit-Remaining", "19")
		w.Header().Set("X-RateLimit-Used", "1")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	cfg.REST.RequestTimeout = 3 * time.Second

	var client *bitget.Client
	var err error
	client, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return srv, client
}

// mixOf returns the mix.Client owned by the bitget.Client. The factory
// is registered by mix.init() — importing this test package is enough
// to wire it.
func mixOf(c *bitget.Client) *Client { return c.Mix().(*Client) }

// ---------------------------------------------------------------------
// MarketData — GetSymbolInfo.
// ---------------------------------------------------------------------

func TestContract_GetSymbolInfo_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000001,
		"data":[{
			"symbol":"BTCUSDT",
			"baseCoin":"BTC",
			"quoteCoin":"USDT",
			"buyLimitPriceRatio":"0.05",
			"sellLimitPriceRatio":"0.05",
			"feeRateUpRatio":"0.005",
			"makerFeeRate":"0.0002",
			"takerFeeRate":"0.0006",
			"openCostUpRatio":"0.01",
			"supportMarginCoins":["USDT"],
			"minTradeNum":"0.001",
			"priceEndStep":"5",
			"volumePlace":"3",
			"pricePlace":"1",
			"sizeMultiplier":"0.001",
			"symbolType":"perpetual",
			"minTradeUSDT":"5",
			"maxSymbolOrderNum":"500",
			"maxProductOrderNum":"5000",
			"maxPositionNum":"150",
			"symbolStatus":"normal",
			"offTime":"-1",
			"limitOpenTime":"-1",
			"deliveryTime":"",
			"deliveryStartTime":"",
			"launchTime":"",
			"fundInterval":"8",
			"minLever":"1",
			"maxLever":"125",
			"posLimit":"0.05",
			"maintainTime":""
		}]
	}`

	var srv *httptest.Server
	var client *bitget.Client
	srv, client = mockBitget(t, map[string]string{
		"/api/v2/mix/market/contracts": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("productType"); got != "USDT-FUTURES" {
			t.Errorf("productType: want USDT-FUTURES, got %q", got)
		}
		if got := q.Get("symbol"); got != "BTCUSDT" {
			t.Errorf("symbol: want BTCUSDT, got %q", got)
		}
	})
	_ = srv

	var info mixtypes.SymbolInfo
	var err error
	info, err = mixOf(client).MarketData().GetSymbolInfo(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetSymbolInfo: %v", err)
	}

	if info.Symbol != "BTCUSDT" {
		t.Errorf("Symbol: want BTCUSDT, got %q", info.Symbol)
	}
	if info.BaseCoin != "BTC" {
		t.Errorf("BaseCoin: want BTC, got %q", info.BaseCoin)
	}
	if info.QuoteCoin != "USDT" {
		t.Errorf("QuoteCoin: want USDT, got %q", info.QuoteCoin)
	}
	if info.ProductType != roottypes.ProductTypeUSDTFutures {
		t.Errorf("ProductType: want USDT-FUTURES, got %q", info.ProductType)
	}

	// pricePlace=1, priceEndStep=5 → tick = 0.5
	var wantTick decimal.Decimal = decimal.RequireFromString("0.5")
	if !info.PriceTick.Equal(wantTick) {
		t.Errorf("PriceTick: want %s, got %s", wantTick, info.PriceTick)
	}
	// volumePlace=3 → step = 0.001
	var wantStep decimal.Decimal = decimal.RequireFromString("0.001")
	if !info.SizeStep.Equal(wantStep) {
		t.Errorf("SizeStep: want %s, got %s", wantStep, info.SizeStep)
	}

	if info.MaxLever != 125 {
		t.Errorf("MaxLever: want 125, got %d", info.MaxLever)
	}
	if info.MinLever != 1 {
		t.Errorf("MinLever: want 1, got %d", info.MinLever)
	}
	if !info.MinTradeNum.Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("MinTradeNum: want 0.001, got %s", info.MinTradeNum)
	}
	if !info.MinTradeUSDT.Equal(decimal.RequireFromString("5")) {
		t.Errorf("MinTradeUSDT: want 5, got %s", info.MinTradeUSDT)
	}
	if info.SymbolStatus != "normal" {
		t.Errorf("SymbolStatus: want normal, got %q", info.SymbolStatus)
	}
}

func TestContract_GetSymbolInfo_NotFound(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1700000000001,"data":[]}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/market/contracts": fixture,
	}, nil)

	var err error
	_, err = mixOf(client).MarketData().GetSymbolInfo(context.Background(), "XYZUSDT")
	if err == nil {
		t.Fatal("GetSymbolInfo: expected error, got nil")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetSymbolInfo: want ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_GetSymbolInfo_EmptySymbol(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var err error
	_, err = mixOf(client).MarketData().GetSymbolInfo(context.Background(), "")
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetSymbolInfo(\"\"): want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// MarketData — GetOrderBook.
// ---------------------------------------------------------------------

func TestContract_GetOrderBook_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000002,
		"data":{
			"asks":[
				["43500.5","0.123"],
				["43501.0","0.500"]
			],
			"bids":[
				["43499.5","0.250"],
				["43499.0","1.000"]
			],
			"ts":"1700000000002",
			"precision":"scale0",
			"scale":"0.5"
		}
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/market/merge-depth": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("limit"); got != "max50" {
			t.Errorf("limit: want max50 (default for depth=0), got %q", got)
		}
		if got := q.Get("precision"); got != "scale0" {
			t.Errorf("precision: want scale0, got %q", got)
		}
	})

	var snap roottypes.OrderBookSnapshot
	var err error
	snap, err = mixOf(client).MarketData().GetOrderBook(context.Background(), "BTCUSDT", 0)
	if err != nil {
		t.Fatalf("GetOrderBook: %v", err)
	}

	if snap.Symbol != "BTCUSDT" {
		t.Errorf("Symbol: want BTCUSDT, got %q", snap.Symbol)
	}
	if len(snap.Asks) != 2 {
		t.Fatalf("Asks: want 2 levels, got %d", len(snap.Asks))
	}
	if !snap.Asks[0].Price.Equal(decimal.RequireFromString("43500.5")) {
		t.Errorf("Asks[0].Price: want 43500.5, got %s", snap.Asks[0].Price)
	}
	if !snap.Asks[0].Size.Equal(decimal.RequireFromString("0.123")) {
		t.Errorf("Asks[0].Size: want 0.123, got %s", snap.Asks[0].Size)
	}
	if len(snap.Bids) != 2 {
		t.Fatalf("Bids: want 2 levels, got %d", len(snap.Bids))
	}
	if !snap.Bids[0].Price.Equal(decimal.RequireFromString("43499.5")) {
		t.Errorf("Bids[0].Price: want 43499.5, got %s", snap.Bids[0].Price)
	}
	if snap.TsMs != 1700000000002 {
		t.Errorf("TsMs: want 1700000000002, got %d", snap.TsMs)
	}
	if snap.Checksum != 0 {
		t.Errorf("Checksum: want 0 (REST endpoint never includes one), got %d", snap.Checksum)
	}
}

func TestContract_GetOrderBook_DepthClamp(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"asks":[],"bids":[],"ts":"0","precision":"scale0","scale":"0.5"}}`

	var cases = []struct {
		name string
		in   int
		want string
	}{
		{"negative", -10, "max50"},
		{"zero", 0, "max50"},
		{"one", 1, "max15"},
		{"fifteen", 15, "max15"},
		{"sixteen", 16, "max50"},
		{"fifty", 50, "max50"},
		{"hundred", 100, "max100"},
		{"twohundred", 200, "max200"},
		{"oversized", 1000, "max200"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var seen string
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{
				"/api/v2/mix/market/merge-depth": fixture,
			}, func(t *testing.T, r *http.Request) {
				seen = r.URL.Query().Get("limit")
			})

			var err error
			_, err = mixOf(client).MarketData().GetOrderBook(context.Background(), "BTCUSDT", tc.in)
			if err != nil {
				t.Fatalf("GetOrderBook(%d): %v", tc.in, err)
			}
			if seen != tc.want {
				t.Errorf("depth %d: want limit %q, got %q", tc.in, tc.want, seen)
			}
		})
	}
}

// ---------------------------------------------------------------------
// MarketData — GetMarketTicker.
// ---------------------------------------------------------------------

func TestContract_GetMarketTicker_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000003,
		"data":[{
			"symbol":"BTCUSDT",
			"lastPr":"43500.5",
			"markPrice":"43500.4",
			"indexPrice":"43500.0",
			"askPr":"43501.0",
			"bidPr":"43500.0",
			"high24h":"44000",
			"low24h":"43000",
			"baseVolume":"15000",
			"quoteVolume":"650000000",
			"usdtVolume":"650000000",
			"holdingAmount":"50000",
			"open24h":"43200",
			"changeUtc24h":"0.0023",
			"change24h":"0.0069",
			"fundingRate":"0.0001",
			"nextFundingTime":"1700000288000",
			"ts":"1700000000003",
			"deliveryStartTime":"",
			"deliveryTime":"",
			"deliveryStatus":""
		}]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/market/ticker": fixture,
	}, nil)

	var tk mixtypes.MarketTicker
	var err error
	tk, err = mixOf(client).MarketData().GetMarketTicker(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetMarketTicker: %v", err)
	}

	if tk.Symbol != "BTCUSDT" {
		t.Errorf("Symbol: want BTCUSDT, got %q", tk.Symbol)
	}
	if !tk.LastPrice.Equal(decimal.RequireFromString("43500.5")) {
		t.Errorf("LastPrice: %s", tk.LastPrice)
	}
	if !tk.MarkPrice.Equal(decimal.RequireFromString("43500.4")) {
		t.Errorf("MarkPrice: %s", tk.MarkPrice)
	}
	if !tk.IndexPrice.Equal(decimal.RequireFromString("43500.0")) {
		t.Errorf("IndexPrice: %s", tk.IndexPrice)
	}
	if !tk.AskPrice.Equal(decimal.RequireFromString("43501.0")) {
		t.Errorf("AskPrice: %s", tk.AskPrice)
	}
	if !tk.BidPrice.Equal(decimal.RequireFromString("43500.0")) {
		t.Errorf("BidPrice: %s", tk.BidPrice)
	}
	if !tk.FundingRate.Equal(decimal.RequireFromString("0.0001")) {
		t.Errorf("FundingRate: %s", tk.FundingRate)
	}
	if tk.NextFundingTimeMs != 1700000288000 {
		t.Errorf("NextFundingTimeMs: %d", tk.NextFundingTimeMs)
	}
	if tk.TsMs != 1700000000003 {
		t.Errorf("TsMs: %d", tk.TsMs)
	}
}

// ---------------------------------------------------------------------
// MarketData — GetHistoricalCandles.
// ---------------------------------------------------------------------

func TestContract_GetHistoricalCandles_PreservesAscendingOrder(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000004,
		"data":[
			["1700000000000","43000","43100","42950","43050","12.34","531000.5"],
			["1700000060000","43050","43200","43040","43180","8.0","344800.0"],
			["1700000120000","43180","43300","43170","43290","11.5","497000.0"]
		]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/market/candles": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("granularity"); got != "1m" {
			t.Errorf("granularity: want 1m, got %q", got)
		}
		if got := q.Get("limit"); got != "3" {
			t.Errorf("limit: want 3, got %q", got)
		}
	})

	var rows roottypes.Candles
	var err error
	rows, err = mixOf(client).MarketData().GetHistoricalCandles(context.Background(), "BTCUSDT", roottypes.Timeframe1m, 3)
	if err != nil {
		t.Fatalf("GetHistoricalCandles: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("len: want 3, got %d", len(rows))
	}
	if rows[0].OpenTimeMs != 1700000000000 {
		t.Errorf("rows[0].OpenTimeMs: want 1700000000000, got %d", rows[0].OpenTimeMs)
	}
	if rows[1].OpenTimeMs != 1700000060000 {
		t.Errorf("rows[1].OpenTimeMs: want 1700000060000, got %d", rows[1].OpenTimeMs)
	}
	if rows[2].OpenTimeMs != 1700000120000 {
		t.Errorf("rows[2].OpenTimeMs: want 1700000120000, got %d", rows[2].OpenTimeMs)
	}
	if !rows[2].Close.Equal(decimal.RequireFromString("43290")) {
		t.Errorf("rows[2].Close: %s", rows[2].Close)
	}
}

func TestContract_GetHistoricalCandles1m_DefaultLength(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":[]}`

	var seen string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/market/candles": fixture,
	}, func(t *testing.T, r *http.Request) {
		seen = r.URL.Query().Get("limit")
	})

	var err error
	_, err = mixOf(client).MarketData().GetHistoricalCandles1m(context.Background(), "BTCUSDT", 0)
	if err != nil {
		t.Fatalf("GetHistoricalCandles1m: %v", err)
	}
	// length=0 → default 100.
	if seen != "100" {
		t.Errorf("limit: want 100, got %q", seen)
	}
}

// ---------------------------------------------------------------------
// Trading / Account / Stream — stubs.
// ---------------------------------------------------------------------

func TestStubs_ReturnInvalidRequest(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)
	var m *Client = mixOf(client)

	var ctx context.Context = context.Background()

	type stubCall struct {
		name string
		fn   func() error
	}
	var calls = []stubCall{
		{"Trading.CreateOrder", func() error {
			_, err := m.Trading().CreateOrder(ctx, mixtypes.CreateOrderRequest{})
			return err
		}},
		{"Trading.ModifyOrder", func() error {
			_, err := m.Trading().ModifyOrder(ctx, mixtypes.ModifyOrderRequest{})
			return err
		}},
		{"Trading.CancelOrder", func() error {
			return m.Trading().CancelOrder(ctx, roottypes.CancelOrderRequest{})
		}},
		{"Trading.CreateBatchOrders", func() error {
			_, err := m.Trading().CreateBatchOrders(ctx, nil)
			return err
		}},
		{"Trading.ModifyBatchOrders", func() error {
			_, err := m.Trading().ModifyBatchOrders(ctx, nil)
			return err
		}},
		{"Trading.CancelBatchOrders", func() error {
			_, err := m.Trading().CancelBatchOrders(ctx, nil)
			return err
		}},
		{"Trading.CancelAllOrders", func() error {
			return m.Trading().CancelAllOrders(ctx, "BTCUSDT")
		}},
		{"Account.GetAccount", func() error {
			_, err := m.Account().GetAccount(ctx)
			return err
		}},
		{"Account.GetPosition", func() error {
			_, err := m.Account().GetPosition(ctx, "BTCUSDT")
			return err
		}},
		{"Account.GetOpenOrders", func() error {
			_, err := m.Account().GetOpenOrders(ctx, "BTCUSDT")
			return err
		}},
		{"Account.GetOrderDetail", func() error {
			_, err := m.Account().GetOrderDetail(ctx, "BTCUSDT", "1", "")
			return err
		}},
		{"Account.ClosePosition", func() error {
			return m.Account().ClosePosition(ctx, "BTCUSDT")
		}},
		{"Account.SetLeverage", func() error {
			return m.Account().SetLeverage(ctx, "BTCUSDT", 5)
		}},
		{"Account.SetPositionMode", func() error {
			return m.Account().SetPositionMode(ctx, roottypes.PositionModeOneWay)
		}},
		{"Stream.WatchOrderbook", func() error {
			return m.Stream().WatchOrderbook(ctx, "BTCUSDT", nil, nil)
		}},
		{"Stream.WatchTicker", func() error {
			return m.Stream().WatchTicker(ctx, "BTCUSDT", nil, nil)
		}},
		{"Stream.WatchTrades", func() error {
			return m.Stream().WatchTrades(ctx, "BTCUSDT", nil, nil)
		}},
		{"Stream.WatchKline", func() error {
			return m.Stream().WatchKline(ctx, "BTCUSDT", roottypes.Timeframe1m, nil, nil)
		}},
		{"Stream.WatchOrders", func() error {
			return m.Stream().WatchOrders(ctx, "BTCUSDT", nil, nil)
		}},
		{"Stream.WatchPositions", func() error {
			return m.Stream().WatchPositions(ctx, "BTCUSDT", nil, nil)
		}},
		{"Stream.WatchAccount", func() error {
			return m.Stream().WatchAccount(ctx, nil, nil)
		}},
	}

	var i int
	for i = 0; i < len(calls); i++ {
		var c stubCall = calls[i]
		t.Run(c.name, func(t *testing.T) {
			var err error = c.fn()
			if err == nil {
				t.Fatalf("%s: want error, got nil", c.name)
			}
			if !bitget.IsInvalidRequest(err) {
				t.Fatalf("%s: want ErrorKindInvalidRequest, got %v", c.name, err)
			}
		})
	}
}
