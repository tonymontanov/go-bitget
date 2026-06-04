/*
FILE: margin/trading_contract_test.go

DESCRIPTION:
Contract tests for the MARGIN profile trading sub-client. They pin the
on-the-wire shape of every place / cancel call against hand-derived
Bitget V2 fixtures (https://www.bitget.com/api-doc/margin/...), with no
network calls (a local httptest.Server backs every test).

KEY INVARIANTS LOCKED HERE:

  - The crossed / isolated MODE selects the URL path segment
    (/api/v2/margin/<mode>/...). A matrix runs every primitive in both
    modes to prove the segment tracks ClientSettings.Mode.
  - loanType is ALWAYS on the wire (defaults to "normal").
  - Size is side-dependent: BaseSize for limit / market-sell, QuoteSize
    for market-buy — and the OTHER field is omitted.
  - force is emitted only for limit orders.
  - stpMode is forwarded when set, omitted otherwise.
  - batch-place / batch-cancel are per-symbol with the standard
    {successList, failureList} envelope collation.
*/

package margin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// mockBitget starts an httptest.Server that routes by path to a canned
// JSON envelope. Unknown paths return a Bitget-style 404 so a test
// fails loudly when the SDK targets an unexpected endpoint. The
// optional inspect callback runs per request before the canned reply.
func mockBitget(
	t *testing.T,
	routes map[string]string,
	inspect func(t *testing.T, r *http.Request, body []byte),
) (*httptest.Server, *bitget.Client) {
	t.Helper()

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		if r.Body != nil {
			raw, _ = io.ReadAll(r.Body)
		}
		if inspect != nil {
			inspect(t, r, raw)
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

// marginOf returns the default (crossed) margin.Client owned by the
// bitget.Client. The factory is registered by margin.init().
func marginOf(c *bitget.Client) *Client { return c.Margin().(*Client) }

// marginWithMode builds a margin.Client pinned to the given mode against
// the same mock server the root client targets.
func marginWithMode(c *bitget.Client, mode roottypes.MarginMode) *Client {
	return NewClientWithMode(c, mode)
}

// decodeBody unmarshals a captured request body into a generic map for
// field-level assertions.
func decodeBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode body %q: %v", string(raw), err)
	}
	return m
}

// ---------------------------------------------------------------------
// Mode → path segment matrix.
// ---------------------------------------------------------------------

func TestContract_Margin_PlaceOrder_ModeSegment(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"111","clientOid":"c-1"}}`

	var cases = []struct {
		mode roottypes.MarginMode
		path string
	}{
		{roottypes.MarginModeCrossed, "/api/v2/margin/crossed/place-order"},
		{roottypes.MarginModeIsolated, "/api/v2/margin/isolated/place-order"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()
			var seenPath string
			var client *bitget.Client
			_, client = mockBitget(t, map[string]string{tc.path: fixture}, func(t *testing.T, r *http.Request, _ []byte) {
				seenPath = r.URL.Path
			})

			var _, err = marginWithMode(client, tc.mode).Trading().CreateOrder(context.Background(), margintypes.CreateOrderRequest{
				Symbol:      "BTCUSDT",
				Side:        roottypes.SideTypeBuy,
				OrderType:   roottypes.OrderTypeLimit,
				TimeInForce: roottypes.TimeInForceGTC,
				Price:       decimal.RequireFromString("43000"),
				BaseSize:    decimal.RequireFromString("0.01"),
			})
			if err != nil {
				t.Fatalf("CreateOrder: %v", err)
			}
			if seenPath != tc.path {
				t.Errorf("path: want %q, got %q", tc.path, seenPath)
			}
		})
	}
}

// ---------------------------------------------------------------------
// CreateOrder — body shape.
// ---------------------------------------------------------------------

func TestContract_Margin_CreateOrder_LimitBuy_BaseSize(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"oid-1","clientOid":"cid-1"}}`

	var body map[string]any
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/place-order": fixture}, func(t *testing.T, _ *http.Request, raw []byte) {
		body = decodeBody(t, raw)
	})

	var info margintypes.OrderInfo
	var err error
	info, err = marginOf(client).Trading().CreateOrder(context.Background(), margintypes.CreateOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          roottypes.SideTypeBuy,
		OrderType:     roottypes.OrderTypeLimit,
		TimeInForce:   roottypes.TimeInForceGTC,
		Price:         decimal.RequireFromString("43000"),
		BaseSize:      decimal.RequireFromString("0.01"),
		ClientOrderID: "cid-1",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	assertField(t, body, "loanType", "normal")
	assertField(t, body, "force", "gtc")
	assertField(t, body, "price", "43000")
	assertField(t, body, "baseSize", "0.01")
	if _, present := body["quoteSize"]; present {
		t.Errorf("quoteSize: must be omitted for limit orders, got %v", body["quoteSize"])
	}
	if _, present := body["stpMode"]; present {
		t.Errorf("stpMode: must be omitted when unset, got %v", body["stpMode"])
	}

	if info.OrderID != "oid-1" {
		t.Errorf("OrderID: want oid-1, got %q", info.OrderID)
	}
	if info.ClientOrderID != "cid-1" {
		t.Errorf("ClientOrderID: want cid-1, got %q", info.ClientOrderID)
	}
	if info.LoanType != margintypes.LoanTypeNormal {
		t.Errorf("LoanType: want normal, got %q", info.LoanType)
	}
	if info.Status != roottypes.OrderStatusLive {
		t.Errorf("Status: want live, got %q", info.Status)
	}
	if !info.Quantity.Equal(decimal.RequireFromString("0.01")) {
		t.Errorf("Quantity: want 0.01, got %s", info.Quantity)
	}
}

func TestContract_Margin_CreateOrder_MarketBuy_QuoteSize(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"oid-2","clientOid":"cid-2"}}`

	var body map[string]any
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/place-order": fixture}, func(t *testing.T, _ *http.Request, raw []byte) {
		body = decodeBody(t, raw)
	})

	var _, err = marginOf(client).Trading().CreateOrder(context.Background(), margintypes.CreateOrderRequest{
		Symbol:    "BTCUSDT",
		Side:      roottypes.SideTypeBuy,
		OrderType: roottypes.OrderTypeMarket,
		QuoteSize: decimal.RequireFromString("10000"),
		LoanType:  margintypes.LoanTypeAutoLoan,
		STPMode:   margintypes.STPModeCancelTaker,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	assertField(t, body, "loanType", "autoLoan")
	assertField(t, body, "quoteSize", "10000")
	assertField(t, body, "stpMode", "cancel_taker")
	if _, present := body["baseSize"]; present {
		t.Errorf("baseSize: must be omitted for market buy, got %v", body["baseSize"])
	}
	if _, present := body["force"]; present {
		t.Errorf("force: must be omitted for market orders, got %v", body["force"])
	}
	if _, present := body["price"]; present {
		t.Errorf("price: must be omitted for market orders, got %v", body["price"])
	}
}

func TestContract_Margin_CreateOrder_MarketSell_BaseSize(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"oid-3","clientOid":""}}`

	var body map[string]any
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/isolated/place-order": fixture}, func(t *testing.T, _ *http.Request, raw []byte) {
		body = decodeBody(t, raw)
	})

	var _, err = marginWithMode(client, roottypes.MarginModeIsolated).Trading().CreateOrder(context.Background(), margintypes.CreateOrderRequest{
		Symbol:    "BTCUSDT",
		Side:      roottypes.SideTypeSell,
		OrderType: roottypes.OrderTypeMarket,
		BaseSize:  decimal.RequireFromString("0.5"),
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	assertField(t, body, "baseSize", "0.5")
	assertField(t, body, "loanType", "normal")
	if _, present := body["quoteSize"]; present {
		t.Errorf("quoteSize: must be omitted for market sell, got %v", body["quoteSize"])
	}
}

// ---------------------------------------------------------------------
// CreateOrder — validation.
// ---------------------------------------------------------------------

func TestContract_Margin_CreateOrder_Validation(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)
	var tr = marginOf(client).Trading()

	var cases = []struct {
		name string
		req  margintypes.CreateOrderRequest
	}{
		{"emptySymbol", margintypes.CreateOrderRequest{Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, Price: decimal.New(1, 0), BaseSize: decimal.New(1, 0)}},
		{"limitMissingPrice", margintypes.CreateOrderRequest{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, BaseSize: decimal.New(1, 0)}},
		{"limitMissingBaseSize", margintypes.CreateOrderRequest{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, Price: decimal.New(1, 0)}},
		{"marketBuyMissingQuoteSize", margintypes.CreateOrderRequest{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeMarket}},
		{"marketSellMissingBaseSize", margintypes.CreateOrderRequest{Symbol: "BTCUSDT", Side: roottypes.SideTypeSell, OrderType: roottypes.OrderTypeMarket}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var _, err = tr.CreateOrder(context.Background(), tc.req)
			if !bitget.IsInvalidRequest(err) {
				t.Errorf("%s: want ErrorKindInvalidRequest, got %v", tc.name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------
// CancelOrder.
// ---------------------------------------------------------------------

func TestContract_Margin_CancelOrder_ModeAndBody(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":1,"data":{"orderId":"oid","clientOid":"cid"}}`

	var body map[string]any
	var seenPath string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/isolated/cancel-order": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		seenPath = r.URL.Path
		body = decodeBody(t, raw)
	})

	var err = marginWithMode(client, roottypes.MarginModeIsolated).Trading().CancelOrder(context.Background(), roottypes.CancelOrderRequest{
		Symbol:        "BTCUSDT",
		ClientOrderID: "cid",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if seenPath != "/api/v2/margin/isolated/cancel-order" {
		t.Errorf("path: got %q", seenPath)
	}
	assertField(t, body, "symbol", "BTCUSDT")
	assertField(t, body, "clientOid", "cid")
	if _, present := body["orderId"]; present {
		t.Errorf("orderId: must be omitted when empty, got %v", body["orderId"])
	}
}

func TestContract_Margin_CancelOrder_Validation(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var err = marginOf(client).Trading().CancelOrder(context.Background(), roottypes.CancelOrderRequest{Symbol: "BTCUSDT"})
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("CancelOrder(no id): want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// CreateBatchOrders.
// ---------------------------------------------------------------------

func TestContract_Margin_CreateBatchOrders_Collation(t *testing.T) {
	t.Parallel()
	// Row 0 (cid-a) succeeds, row 1 (cid-b) fails — verify per-row
	// collation maps each outcome back to its request position.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"successList":[{"orderId":"o-a","clientOid":"cid-a"}],
			"failureList":[{"orderId":"","clientOid":"cid-b","errorMsg":"insufficient","errorCode":"43012"}]
		}
	}`

	var body map[string]any
	var seenPath string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/batch-place-order": fixture}, func(t *testing.T, r *http.Request, raw []byte) {
		seenPath = r.URL.Path
		body = decodeBody(t, raw)
	})

	var reqs = []margintypes.CreateOrderRequest{
		{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, TimeInForce: roottypes.TimeInForceGTC, Price: decimal.RequireFromString("43000"), BaseSize: decimal.RequireFromString("0.01"), ClientOrderID: "cid-a"},
		{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, TimeInForce: roottypes.TimeInForceGTC, Price: decimal.RequireFromString("42000"), BaseSize: decimal.RequireFromString("0.02"), ClientOrderID: "cid-b"},
	}
	var res, err = marginOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CreateBatchOrders: %v", err)
	}
	if seenPath != "/api/v2/margin/crossed/batch-place-order" {
		t.Errorf("path: got %q", seenPath)
	}
	// symbol pinned at the top level, orderList carries the rows.
	assertField(t, body, "symbol", "BTCUSDT")
	if _, ok := body["orderList"].([]any); !ok {
		t.Fatalf("orderList: want array, got %T", body["orderList"])
	}

	if len(res) != 2 {
		t.Fatalf("results: want 2, got %d", len(res))
	}
	if res[0].Err != nil || res[0].Order == nil {
		t.Fatalf("row 0: want success, got err=%v order=%v", res[0].Err, res[0].Order)
	}
	if res[0].Order.OrderID != "o-a" {
		t.Errorf("row 0 OrderID: want o-a, got %q", res[0].Order.OrderID)
	}
	if res[0].Order.LoanType != margintypes.LoanTypeNormal {
		t.Errorf("row 0 LoanType: want normal, got %q", res[0].Order.LoanType)
	}
	if res[1].Err == nil {
		t.Fatalf("row 1: want error, got nil")
	}
	if res[1].ClientOrderID != "cid-b" {
		t.Errorf("row 1 ClientOrderID: want cid-b, got %q", res[1].ClientOrderID)
	}
}

func TestContract_Margin_CreateBatchOrders_MixedSymbol(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var reqs = []margintypes.CreateOrderRequest{
		{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeMarket, QuoteSize: decimal.New(100, 0)},
		{Symbol: "ETHUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeMarket, QuoteSize: decimal.New(100, 0)},
	}
	var _, err = marginOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("mixed symbol: want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// CancelBatchOrders.
// ---------------------------------------------------------------------

func TestContract_Margin_CancelBatchOrders_Collation(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1,
		"data":{
			"successList":[{"orderId":"o-1","clientOid":"cid-1"}],
			"failureList":[{"orderId":"","clientOid":"cid-2","errorMsg":"not found","errorCode":"43001"}]
		}
	}`

	var seenPath string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{"/api/v2/margin/crossed/batch-cancel-order": fixture}, func(t *testing.T, r *http.Request, _ []byte) {
		seenPath = r.URL.Path
	})

	var reqs = []roottypes.CancelOrderRequest{
		{Symbol: "BTCUSDT", ClientOrderID: "cid-1"},
		{Symbol: "BTCUSDT", ClientOrderID: "cid-2"},
	}
	var res, err = marginOf(client).Trading().CancelBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CancelBatchOrders: %v", err)
	}
	if seenPath != "/api/v2/margin/crossed/batch-cancel-order" {
		t.Errorf("path: got %q", seenPath)
	}
	if len(res) != 2 {
		t.Fatalf("results: want 2, got %d", len(res))
	}
	if res[0].Order == nil || res[0].Order.Status != roottypes.OrderStatusCancelled {
		t.Errorf("row 0: want cancelled order, got %+v", res[0])
	}
	if res[1].Err == nil {
		t.Errorf("row 1: want error, got nil")
	}
}

// ---------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------

// assertField checks that body[key] equals want (string-typed JSON
// value). Fails the test with a clear message otherwise.
func assertField(t *testing.T, body map[string]any, key, want string) {
	t.Helper()
	var got, ok = body[key]
	if !ok {
		t.Errorf("%s: missing (want %q)", key, want)
		return
	}
	if gs, _ := got.(string); gs != want {
		t.Errorf("%s: want %q, got %v", key, want, got)
	}
}
