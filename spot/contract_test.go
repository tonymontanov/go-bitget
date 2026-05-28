/*
FILE: spot/contract_test.go

DESCRIPTION:
Contract tests for the SPOT profile market-data sub-client. They
verify that the parser maps real Bitget V2 JSON envelopes into domain
structs. Each fixture is hand-derived from the public V2 spot
documentation (https://www.bitget.com/api-doc/spot/...) and trimmed
to the fields the SDK consumes.

Coverage (M2 — market data):

  - GetSymbolInfo        : /api/v2/spot/public/symbols (happy + 404 + empty arg)
  - GetOrderBook         : /api/v2/spot/market/orderbook (happy + depth clamp)
  - GetMarketTicker      : /api/v2/spot/market/tickers
  - GetHistoricalCandles : /api/v2/spot/market/candles (preserves ASC order;
                           tolerates the 8-element row spot ships)
  - GetHistoricalCandles1m default-length

Trading contract tests live in trading_contract_test.go.

Tests use a local httptest.Server; no network calls are made.
*/

package spot

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
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// mockBitget starts an httptest.Server that routes requests by path
// to a canned JSON envelope. Unknown paths return a Bitget-style 404
// envelope so the test fails loudly when the SDK targets an
// unexpected endpoint. Rate-limit headers are set on every reply so
// the observer pipeline is exercised.
//
// The optional `inspect` callback runs per-request before the canned
// response is written and lets a test assert query parameters or
// request bodies.
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

// spotOf returns the spot.Client owned by the bitget.Client. The
// factory is registered by spot.init() — importing this test package
// is enough to wire it.
func spotOf(c *bitget.Client) *Client { return c.Spot().(*Client) }

// ---------------------------------------------------------------------
// MarketData — GetSymbolInfo.
// ---------------------------------------------------------------------

func TestContract_Spot_GetSymbolInfo_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000001,
		"data":[{
			"symbol":"BTCUSDT",
			"baseCoin":"BTC",
			"quoteCoin":"USDT",
			"minTradeAmount":"0.0001",
			"maxTradeAmount":"10000",
			"takerFeeRate":"0.002",
			"makerFeeRate":"0.002",
			"pricePrecision":"2",
			"quantityPrecision":"6",
			"quotePrecision":"8",
			"status":"online",
			"minTradeUSDT":"5"
		}]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/public/symbols": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("symbol"); got != "BTCUSDT" {
			t.Errorf("symbol: want BTCUSDT, got %q", got)
		}
		// Spot has no productType — guard against accidentally
		// adding one (the mix profile uses it; spot must not).
		if got := q.Get("productType"); got != "" {
			t.Errorf("productType: must NOT be sent on spot, got %q", got)
		}
	})

	var info spottypes.SymbolInfo
	var err error
	info, err = spotOf(client).MarketData().GetSymbolInfo(context.Background(), "BTCUSDT")
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

	// pricePrecision=2 → tick = 0.01
	var wantTick decimal.Decimal = decimal.RequireFromString("0.01")
	if !info.PriceTick.Equal(wantTick) {
		t.Errorf("PriceTick: want %s, got %s", wantTick, info.PriceTick)
	}
	// quantityPrecision=6 → step = 0.000001
	var wantStep decimal.Decimal = decimal.RequireFromString("0.000001")
	if !info.SizeStep.Equal(wantStep) {
		t.Errorf("SizeStep: want %s, got %s", wantStep, info.SizeStep)
	}
	// quotePrecision=8 → quote step = 0.00000001
	var wantQuoteStep decimal.Decimal = decimal.RequireFromString("0.00000001")
	if !info.QuoteStep.Equal(wantQuoteStep) {
		t.Errorf("QuoteStep: want %s, got %s", wantQuoteStep, info.QuoteStep)
	}

	if !info.MinTradeAmount.Equal(decimal.RequireFromString("0.0001")) {
		t.Errorf("MinTradeAmount: %s", info.MinTradeAmount)
	}
	if !info.MaxTradeAmount.Equal(decimal.RequireFromString("10000")) {
		t.Errorf("MaxTradeAmount: %s", info.MaxTradeAmount)
	}
	if !info.MinTradeUSDT.Equal(decimal.RequireFromString("5")) {
		t.Errorf("MinTradeUSDT: %s", info.MinTradeUSDT)
	}
	if !info.MakerFeeRate.Equal(decimal.RequireFromString("0.002")) {
		t.Errorf("MakerFeeRate: %s", info.MakerFeeRate)
	}
	if !info.TakerFeeRate.Equal(decimal.RequireFromString("0.002")) {
		t.Errorf("TakerFeeRate: %s", info.TakerFeeRate)
	}
	if info.Status != "online" {
		t.Errorf("Status: want online, got %q", info.Status)
	}
}

func TestContract_Spot_GetSymbolInfo_NotFound(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1700000000001,"data":[]}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/public/symbols": fixture,
	}, nil)

	var err error
	_, err = spotOf(client).MarketData().GetSymbolInfo(context.Background(), "XYZUSDT")
	if err == nil {
		t.Fatal("GetSymbolInfo: expected error, got nil")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetSymbolInfo: want ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_Spot_GetSymbolInfo_EmptySymbol(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var err error
	_, err = spotOf(client).MarketData().GetSymbolInfo(context.Background(), "")
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetSymbolInfo(\"\"): want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// MarketData — GetOrderBook.
// ---------------------------------------------------------------------

func TestContract_Spot_GetOrderBook_Happy(t *testing.T) {
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
			"ts":"1700000000002"
		}
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/market/orderbook": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("limit"); got != "50" {
			t.Errorf("limit: want 50 (default for depth=0), got %q", got)
		}
		if got := q.Get("type"); got != "step0" {
			t.Errorf("type: want step0, got %q", got)
		}
	})

	var snap roottypes.OrderBookSnapshot
	var err error
	snap, err = spotOf(client).MarketData().GetOrderBook(context.Background(), "BTCUSDT", 0)
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
	if snap.TsMs != 1700000000002 {
		t.Errorf("TsMs: want 1700000000002, got %d", snap.TsMs)
	}
	if snap.Checksum != 0 {
		t.Errorf("Checksum: want 0 (REST endpoint never includes one), got %d", snap.Checksum)
	}
}

func TestContract_Spot_GetOrderBook_DepthClamp(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"asks":[],"bids":[],"ts":"0"}}`

	var cases = []struct {
		name string
		in   int
		want string
	}{
		{"negative", -10, "50"},
		{"zero", 0, "50"},
		{"one", 1, "1"},
		{"fifty", 50, "50"},
		{"hundred", 100, "100"},
		{"atCap", 150, "150"},
		{"oversized", 1000, "150"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var seen string
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{
				"/api/v2/spot/market/orderbook": fixture,
			}, func(t *testing.T, r *http.Request) {
				seen = r.URL.Query().Get("limit")
			})

			var err error
			_, err = spotOf(client).MarketData().GetOrderBook(context.Background(), "BTCUSDT", tc.in)
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

func TestContract_Spot_GetMarketTicker_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000003,
		"data":[{
			"symbol":"BTCUSDT",
			"lastPr":"43500.5",
			"askPr":"43501.0",
			"askSz":"0.5",
			"bidPr":"43500.0",
			"bidSz":"0.25",
			"high24h":"44000",
			"low24h":"43000",
			"open":"43200",
			"openUtc":"43150",
			"baseVolume":"15000",
			"quoteVolume":"650000000",
			"usdtVolume":"650000000",
			"change24h":"0.0069",
			"changeUtc24h":"0.0080",
			"ts":"1700000000003"
		}]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/market/tickers": fixture,
	}, nil)

	var tk spottypes.MarketTicker
	var err error
	tk, err = spotOf(client).MarketData().GetMarketTicker(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetMarketTicker: %v", err)
	}

	if tk.Symbol != "BTCUSDT" {
		t.Errorf("Symbol: want BTCUSDT, got %q", tk.Symbol)
	}
	if !tk.LastPrice.Equal(decimal.RequireFromString("43500.5")) {
		t.Errorf("LastPrice: %s", tk.LastPrice)
	}
	if !tk.AskPrice.Equal(decimal.RequireFromString("43501.0")) {
		t.Errorf("AskPrice: %s", tk.AskPrice)
	}
	if !tk.AskSize.Equal(decimal.RequireFromString("0.5")) {
		t.Errorf("AskSize: %s", tk.AskSize)
	}
	if !tk.BidPrice.Equal(decimal.RequireFromString("43500.0")) {
		t.Errorf("BidPrice: %s", tk.BidPrice)
	}
	if !tk.BidSize.Equal(decimal.RequireFromString("0.25")) {
		t.Errorf("BidSize: %s", tk.BidSize)
	}
	if !tk.High24h.Equal(decimal.RequireFromString("44000")) {
		t.Errorf("High24h: %s", tk.High24h)
	}
	if !tk.Low24h.Equal(decimal.RequireFromString("43000")) {
		t.Errorf("Low24h: %s", tk.Low24h)
	}
	if !tk.Open.Equal(decimal.RequireFromString("43200")) {
		t.Errorf("Open: %s", tk.Open)
	}
	if !tk.OpenUtc.Equal(decimal.RequireFromString("43150")) {
		t.Errorf("OpenUtc: %s", tk.OpenUtc)
	}
	if !tk.Change24h.Equal(decimal.RequireFromString("0.0069")) {
		t.Errorf("Change24h: %s", tk.Change24h)
	}
	if !tk.ChangeUtc24h.Equal(decimal.RequireFromString("0.0080")) {
		t.Errorf("ChangeUtc24h: %s", tk.ChangeUtc24h)
	}
	if tk.TsMs != 1700000000003 {
		t.Errorf("TsMs: %d", tk.TsMs)
	}
}

// ---------------------------------------------------------------------
// MarketData — GetHistoricalCandles.
// ---------------------------------------------------------------------

func TestContract_Spot_GetHistoricalCandles_PreservesAscendingOrder(t *testing.T) {
	t.Parallel()
	// Spot ships an 8-element row (extra usdtVolume column). The
	// shared bgcommon.ParseCandles helper ignores the 8th column so
	// the same parser works for both mix and spot.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000004,
		"data":[
			["1700000000000","43000","43100","42950","43050","12.34","531000.5","531000.5"],
			["1700000060000","43050","43200","43040","43180","8.0","344800.0","344800.0"],
			["1700000120000","43180","43300","43170","43290","11.5","497000.0","497000.0"]
		]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/market/candles": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("granularity"); got != "1m" {
			t.Errorf("granularity: want 1m, got %q", got)
		}
		if got := q.Get("limit"); got != "3" {
			t.Errorf("limit: want 3, got %q", got)
		}
		if got := q.Get("productType"); got != "" {
			t.Errorf("productType: must NOT be sent on spot, got %q", got)
		}
	})

	var rows roottypes.Candles
	var err error
	rows, err = spotOf(client).MarketData().GetHistoricalCandles(context.Background(), "BTCUSDT", roottypes.Timeframe1m, 3)
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

func TestContract_Spot_GetHistoricalCandles1m_DefaultLength(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":[]}`

	var seen string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/market/candles": fixture,
	}, func(t *testing.T, r *http.Request) {
		seen = r.URL.Query().Get("limit")
	})

	var err error
	_, err = spotOf(client).MarketData().GetHistoricalCandles1m(context.Background(), "BTCUSDT", 0)
	if err != nil {
		t.Fatalf("GetHistoricalCandles1m: %v", err)
	}
	// length=0 → default 100.
	if seen != "100" {
		t.Errorf("limit: want 100, got %q", seen)
	}
}
