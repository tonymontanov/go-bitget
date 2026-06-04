/*
FILE: mix/producttype_contract_test.go

DESCRIPTION:
Product-type matrix contract tests for the MIX profile. The trading /
account / market sub-clients are parameterised by the ClientSettings
trio (productType, marginMode, marginCoin) pinned at construction
(see mix/client.go). Until v2.5 every contract test exercised only the
default USDT-FUTURES combo, so the COIN-FUTURES and USDC-FUTURES wire
shapes were reachable but unpinned. These tests close that gap: they
assert, per product type, the venue-visible deltas the desk relies on:

  - productType on the wire matches the pinned setting;
  - marginCoin handling per product type:
      USDT-FUTURES → "USDT" on every place / cancel / batch / position;
      USDC-FUTURES → "USDC";
      COIN-FUTURES → OMITTED. Coin-margined contracts use a per-symbol
        margin coin (e.g. BTC for BTCUSD); the SDK leaves the field
        empty and lets Bitget infer it from the symbol. This is the
        SDK's documented assumption (mix/client.go defaultMarginCoinFor)
        and is pinned here so a regression is caught at CI; it is
        confirmed on the live venue by the consumer's smoke run.

The harness (mockBitget / requestRecorder) is shared with the other
mix contract tests; this file only adds the product-type dimension.
*/

package mix

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// productTypeCase describes one row of the product-type matrix: the
// settings to pin on the client and the venue-visible deltas the wire
// body / query must carry.
type productTypeCase struct {
	name string
	// settings pinned on the mix.Client under test.
	settings roottypes.ProductType
	// symbol representative of the product type (purely cosmetic for
	// the wire assertions; uses the venue's symbol convention).
	symbol string
	// wantProductType is the value expected on the wire.
	wantProductType string
	// wantMarginCoinPresent is false for COIN-FUTURES (the SDK omits
	// the field and lets Bitget infer it from the symbol).
	wantMarginCoinPresent bool
	// wantMarginCoin is the value expected when present.
	wantMarginCoin string
}

// productTypeMatrix is the shared set of rows exercised by every
// wire-shape test below.
func productTypeMatrix() []productTypeCase {
	return []productTypeCase{
		{"USDT-FUTURES", roottypes.ProductTypeUSDTFutures, "BTCUSDT", "USDT-FUTURES", true, "USDT"},
		{"USDC-FUTURES", roottypes.ProductTypeUSDCFutures, "BTCPERP", "USDC-FUTURES", true, "USDC"},
		{"COIN-FUTURES", roottypes.ProductTypeCoinFutures, "BTCUSD", "COIN-FUTURES", false, ""},
	}
}

// mixWithProductType builds a *Client pinned to the given product type
// (margin mode / margin coin fall back to the SDK defaults) against the
// shared mock root client.
func mixWithProductType(c *bitget.Client, pt roottypes.ProductType) *Client {
	return NewClientWithSettings(c, ClientSettings{ProductType: pt})
}

// assertMarginCoin checks the marginCoin field of a decoded wire body
// against the product-type expectation (present+value, or omitted).
func assertMarginCoin(t *testing.T, body map[string]any, c productTypeCase) {
	t.Helper()
	var mc, present = body["marginCoin"]
	if c.wantMarginCoinPresent {
		if !present {
			t.Errorf("%s: marginCoin must be present on the wire, got omitted", c.name)
			return
		}
		if mc != c.wantMarginCoin {
			t.Errorf("%s: marginCoin: want %q, got %v", c.name, c.wantMarginCoin, mc)
		}
		return
	}
	if present {
		t.Errorf("%s: COIN-FUTURES must OMIT marginCoin (Bitget infers it from the symbol), got %v", c.name, mc)
	}
}

// ---------------------------------------------------------------------
// place-order wire shape across product types.
// ---------------------------------------------------------------------

func TestContract_ProductType_PlaceOrder_WireShape(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"orderId":"o","clientOid":""}}`

	var cases []productTypeCase = productTypeMatrix()
	var i int
	for i = 0; i < len(cases); i++ {
		var c productTypeCase = cases[i]
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var rec requestRecorder
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{
				"/api/v2/mix/order/place-order": fixture,
			}, func(t *testing.T, r *http.Request) { rec.record(r) })

			var _, err = mixWithProductType(client, c.settings).Trading().CreateOrder(context.Background(), mixtypes.CreateOrderRequest{
				Symbol:    c.symbol,
				Side:      roottypes.SideTypeBuy,
				OrderType: roottypes.OrderTypeLimit,
				Quantity:  decimal.RequireFromString("1"),
				Price:     decimal.RequireFromString("50000"),
			})
			if err != nil {
				t.Fatalf("CreateOrder: %v", err)
			}

			var _, body, _ = rec.snapshot()
			if body["productType"] != c.wantProductType {
				t.Errorf("%s: productType: want %q, got %v", c.name, c.wantProductType, body["productType"])
			}
			if body["marginMode"] != "crossed" {
				t.Errorf("%s: marginMode: want crossed (default), got %v", c.name, body["marginMode"])
			}
			assertMarginCoin(t, body, c)
		})
	}
}

// ---------------------------------------------------------------------
// cancel-order wire shape across product types.
// ---------------------------------------------------------------------

func TestContract_ProductType_CancelOrder_WireShape(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"orderId":"o","clientOid":""}}`

	var cases []productTypeCase = productTypeMatrix()
	var i int
	for i = 0; i < len(cases); i++ {
		var c productTypeCase = cases[i]
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var rec requestRecorder
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{
				"/api/v2/mix/order/cancel-order": fixture,
			}, func(t *testing.T, r *http.Request) { rec.record(r) })

			var err = mixWithProductType(client, c.settings).Trading().CancelOrder(context.Background(), roottypes.CancelOrderRequest{
				Symbol:  c.symbol,
				OrderID: "o-1",
			})
			if err != nil {
				t.Fatalf("CancelOrder: %v", err)
			}

			var _, body, _ = rec.snapshot()
			if body["productType"] != c.wantProductType {
				t.Errorf("%s: productType: want %q, got %v", c.name, c.wantProductType, body["productType"])
			}
			assertMarginCoin(t, body, c)
		})
	}
}

// ---------------------------------------------------------------------
// batch-place-order wire shape across product types.
// ---------------------------------------------------------------------

func TestContract_ProductType_BatchPlace_WireShape(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,
		"data":{"successList":[{"orderId":"o1","clientOid":"c-1"}],"failureList":[]}}`

	var cases []productTypeCase = productTypeMatrix()
	var i int
	for i = 0; i < len(cases); i++ {
		var c productTypeCase = cases[i]
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var rec requestRecorder
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{
				"/api/v2/mix/order/batch-place-order": fixture,
			}, func(t *testing.T, r *http.Request) { rec.record(r) })

			var _, err = mixWithProductType(client, c.settings).Trading().CreateBatchOrders(context.Background(), []mixtypes.CreateOrderRequest{
				{Symbol: c.symbol, Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(50000), ClientOrderID: "c-1"},
			})
			if err != nil {
				t.Fatalf("CreateBatchOrders: %v", err)
			}

			var _, body, _ = rec.snapshot()
			if body["productType"] != c.wantProductType {
				t.Errorf("%s: productType: want %q, got %v", c.name, c.wantProductType, body["productType"])
			}
			if body["marginMode"] != "crossed" {
				t.Errorf("%s: marginMode: want crossed (default), got %v", c.name, body["marginMode"])
			}
			if body["symbol"] != c.symbol {
				t.Errorf("%s: symbol: want %q, got %v", c.name, c.symbol, body["symbol"])
			}
			assertMarginCoin(t, body, c)
		})
	}
}

// ---------------------------------------------------------------------
// account / position query shape across product types.
//
// GetAccount sends only productType (it filters the returned rows by
// marginCoin client-side). GetPosition sends productType plus
// marginCoin — except for COIN-FUTURES, where the marginCoin query
// param is omitted (same per-symbol inference rationale as trading).
// ---------------------------------------------------------------------

func TestContract_ProductType_AccountQuery_ProductType(t *testing.T) {
	t.Parallel()

	var cases []productTypeCase = productTypeMatrix()
	var i int
	for i = 0; i < len(cases); i++ {
		var c productTypeCase = cases[i]
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// The accounts row must carry a marginCoin the SDK will
			// accept: for USDT/USDC it filters by the pinned coin, for
			// COIN it takes the first row (pinned coin is empty).
			var rowCoin string = c.wantMarginCoin
			if rowCoin == "" {
				rowCoin = "BTC"
			}
			var accountsFixture string = `{"code":"00000","msg":"success","requestTime":0,
				"data":[{"marginCoin":"` + rowCoin + `","available":"100","locked":"0","accountEquity":"100","usdtEquity":"100","btcEquity":"0","unrealizedPL":"0"}]}`

			var seenProductType string
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{
				"/api/v2/mix/account/accounts": accountsFixture,
			}, func(t *testing.T, r *http.Request) {
				seenProductType = r.URL.Query().Get("productType")
			})

			var _, err = mixWithProductType(client, c.settings).Account().GetAccount(context.Background())
			if err != nil {
				t.Fatalf("GetAccount: %v", err)
			}
			if seenProductType != c.wantProductType {
				t.Errorf("%s: GetAccount productType query: want %q, got %q", c.name, c.wantProductType, seenProductType)
			}
		})
	}
}

func TestContract_ProductType_PositionQuery_MarginCoin(t *testing.T) {
	t.Parallel()
	// Empty position list → GetPosition returns a zero PositionInfo
	// with no parse work; we only care about the outbound query shape.
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":[]}`

	var cases []productTypeCase = productTypeMatrix()
	var i int
	for i = 0; i < len(cases); i++ {
		var c productTypeCase = cases[i]
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var seen url.Values
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{
				"/api/v2/mix/position/single-position": fixture,
			}, func(t *testing.T, r *http.Request) {
				seen = r.URL.Query()
			})

			var _, err = mixWithProductType(client, c.settings).Account().GetPosition(context.Background(), c.symbol)
			if err != nil {
				t.Fatalf("GetPosition: %v", err)
			}
			if got := seen.Get("productType"); got != c.wantProductType {
				t.Errorf("%s: productType query: want %q, got %q", c.name, c.wantProductType, got)
			}
			var _, present = seen["marginCoin"]
			if c.wantMarginCoinPresent {
				if got := seen.Get("marginCoin"); got != c.wantMarginCoin {
					t.Errorf("%s: marginCoin query: want %q, got %q", c.name, c.wantMarginCoin, got)
				}
			} else if present {
				t.Errorf("%s: COIN-FUTURES position query must OMIT marginCoin, got %q", c.name, seen.Get("marginCoin"))
			}
		})
	}
}

// ---------------------------------------------------------------------
// defaultMarginCoinFor — unit coverage of the coin-derivation table
// for every product type, including the demo (testnet) variants.
// ---------------------------------------------------------------------

func TestDefaultMarginCoinFor_AllProductTypes(t *testing.T) {
	t.Parallel()
	type tc struct {
		pt   roottypes.ProductType
		want string
	}
	var cases []tc = []tc{
		{roottypes.ProductTypeUSDTFutures, "USDT"},
		{roottypes.ProductTypeUSDCFutures, "USDC"},
		{roottypes.ProductTypeCoinFutures, ""},
		{roottypes.ProductTypeSusdtFutures, "USDT"},
		{roottypes.ProductTypeSusdcFutures, "USDC"},
		{roottypes.ProductTypeScoinFutures, ""},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var c tc = cases[i]
		if got := defaultMarginCoinFor(c.pt); got != c.want {
			t.Errorf("defaultMarginCoinFor(%q): want %q, got %q", c.pt, c.want, got)
		}
	}
}

// ---------------------------------------------------------------------
// public WebSocket subscribe instType across product types.
//
// The public-stream subscribe arg carries instType = the pinned
// productType (mix/stream.go uses string(s.c.productType)). A COIN /
// USDC client must therefore subscribe on the COIN-FUTURES /
// USDC-FUTURES instType, not the USDT-FUTURES default.
// ---------------------------------------------------------------------

// makeStreamClientWithProductType mirrors makeStreamClient (see
// stream_contract_test.go) but pins a product type so the WS subscribe
// instType can be asserted per type.
func makeStreamClientWithProductType(t *testing.T, mock *streamMockServer, pt roottypes.ProductType) *Client {
	t.Helper()
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.WS.PublicURL = mock.wsURL()
	cfg.WS.HandshakeTimeout = 500 * time.Millisecond
	cfg.WS.ReadTimeout = 500 * time.Millisecond
	cfg.WS.WriteTimeout = 500 * time.Millisecond
	cfg.WS.PingInterval = 5 * time.Second
	cfg.WS.LoginTimeout = 500 * time.Millisecond
	cfg.WS.ReconnectInitialBackoff = 10 * time.Millisecond
	cfg.WS.ReconnectMaxBackoff = 50 * time.Millisecond
	cfg.WS.ReconnectJitter = 0
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	return NewClientWithSettings(parent, ClientSettings{ProductType: pt})
}

func TestContract_ProductType_WatchTicker_InstType(t *testing.T) {
	var cases []productTypeCase = productTypeMatrix()
	var i int
	for i = 0; i < len(cases); i++ {
		var c productTypeCase = cases[i]
		t.Run(c.name, func(t *testing.T) {
			var mock *streamMockServer = newStreamMockServer(t)
			defer mock.close()

			var sc *Client = makeStreamClientWithProductType(t, mock, c.settings)
			defer func() { _ = sc.Stream().Close() }()

			var ctx context.Context
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(context.Background())
			defer cancel()

			var err error = sc.Stream().WatchTicker(ctx, c.symbol,
				func(tk mixtypes.MarketTicker) {}, nil)
			if err != nil {
				t.Fatalf("WatchTicker: %v", err)
			}

			select {
			case sub := <-mock.subs:
				if sub["instType"] != c.wantProductType {
					t.Errorf("%s: instType: want %q, got %q", c.name, c.wantProductType, sub["instType"])
				}
				if sub["channel"] != "ticker" {
					t.Errorf("%s: channel: want ticker, got %q", c.name, sub["channel"])
				}
				if sub["instId"] != c.symbol {
					t.Errorf("%s: instId: want %q, got %q", c.name, c.symbol, sub["instId"])
				}
			case <-time.After(time.Second):
				t.Fatalf("%s: no subscribe frame arrived", c.name)
			}
		})
	}
}

// TestClientSettings_DemoProductTypes confirms the demo (testnet)
// product types resolve their margin coin and survive client
// construction (the desk uses these against the Bitget demo host
// shipped in v2.5).
func TestClientSettings_DemoProductTypes(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	type tc struct {
		pt       roottypes.ProductType
		wantCoin string
	}
	var cases []tc = []tc{
		{roottypes.ProductTypeSusdtFutures, "USDT"},
		{roottypes.ProductTypeSusdcFutures, "USDC"},
		{roottypes.ProductTypeScoinFutures, ""},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var c tc = cases[i]
		var m *Client = NewClientWithSettings(client, ClientSettings{ProductType: c.pt})
		if m == nil {
			t.Fatalf("NewClientWithSettings(%q): nil", c.pt)
		}
		if m.ProductType() != c.pt {
			t.Errorf("ProductType(): want %q, got %q", c.pt, m.ProductType())
		}
		if m.MarginCoin() != c.wantCoin {
			t.Errorf("%q MarginCoin(): want %q, got %q", c.pt, c.wantCoin, m.MarginCoin())
		}
	}
}
