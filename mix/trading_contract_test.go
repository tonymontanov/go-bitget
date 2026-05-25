/*
FILE: mix/trading_contract_test.go

DESCRIPTION:
Contract tests for the M2 trading endpoints. They verify that:

  - The request body is built correctly from the typed
    CreateOrderRequest / ModifyOrderRequest / CancelOrderRequest
    plus the parent client's pinned settings (productType, marginMode,
    marginCoin).
  - The response payload is parsed into the SDK's typed OrderInfo /
    BatchOrderResult.
  - Per-row outcomes on batch endpoints land in the correct slot
    (matched by clientOid; positional fallback when clientOid is empty).
  - CancelAllOrders rejects per-symbol calls (Bitget V2 doesn't
    support that endpoint variant).
  - Validation errors fail FAST before any HTTP call.

Tests use the same httptest server harness as the M1 contract tests
(see contract_test.go::mockBitget).
*/

package mix

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// requestRecorder captures the latest POST body + path for assertions.
// Concurrent-safe (sync.Mutex) because httptest.Server may serve
// requests on different goroutines.
type requestRecorder struct {
	mu     sync.Mutex
	path   string
	body   map[string]any
	rawBody string
}

func (r *requestRecorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.path = req.URL.Path
	if req.Body == nil {
		return
	}
	var raw []byte
	raw, _ = io.ReadAll(req.Body)
	r.rawBody = string(raw)
	if len(raw) == 0 {
		return
	}
	var decoded map[string]any = map[string]any{}
	_ = json.Unmarshal(raw, &decoded)
	r.body = decoded
}

func (r *requestRecorder) snapshot() (string, map[string]any, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.path, r.body, r.rawBody
}

// ---------------------------------------------------------------------
// CreateOrder.
// ---------------------------------------------------------------------

func TestContract_CreateOrder_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000010,
		"data":{"orderId":"99887766","clientOid":"core-uuid-1"}
	}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/place-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var req mixtypes.CreateOrderRequest = mixtypes.CreateOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          roottypes.SideTypeBuy,
		OrderType:     roottypes.OrderTypeLimit,
		TimeInForce:   roottypes.TimeInForcePostOnly,
		Quantity:      decimal.RequireFromString("0.001"),
		Price:         decimal.RequireFromString("43500.5"),
		ClientOrderID: "core-uuid-1",
	}
	var info mixtypes.OrderInfo
	var err error
	info, err = mixOf(client).Trading().CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	if info.OrderID != "99887766" {
		t.Errorf("OrderID: want 99887766, got %q", info.OrderID)
	}
	if info.ClientOrderID != "core-uuid-1" {
		t.Errorf("ClientOrderID: want core-uuid-1, got %q", info.ClientOrderID)
	}
	if info.Status != roottypes.OrderStatusLive {
		t.Errorf("Status: want live, got %q", info.Status)
	}
	if !info.Quantity.Equal(req.Quantity) || !info.Price.Equal(req.Price) {
		t.Errorf("echoed Quantity/Price wrong: got %s / %s", info.Quantity, info.Price)
	}

	var path string
	var body map[string]any
	path, body, _ = rec.snapshot()
	if path != "/api/v2/mix/order/place-order" {
		t.Fatalf("path: %q", path)
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("symbol: %v", body["symbol"])
	}
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType: %v", body["productType"])
	}
	if body["marginMode"] != "crossed" {
		t.Errorf("marginMode: want crossed, got %v", body["marginMode"])
	}
	if body["marginCoin"] != "USDT" {
		t.Errorf("marginCoin: %v", body["marginCoin"])
	}
	if body["side"] != "buy" {
		t.Errorf("side: %v", body["side"])
	}
	if body["orderType"] != "limit" {
		t.Errorf("orderType: %v", body["orderType"])
	}
	if body["force"] != "post_only" {
		t.Errorf("force: %v", body["force"])
	}
	if body["size"] != "0.001" {
		t.Errorf("size: %v", body["size"])
	}
	if body["price"] != "43500.5" {
		t.Errorf("price: %v", body["price"])
	}
	if body["clientOid"] != "core-uuid-1" {
		t.Errorf("clientOid: %v", body["clientOid"])
	}
}

func TestContract_CreateOrder_Market_NoPriceField(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000011,
		"data":{"orderId":"42","clientOid":""}
	}`
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/place-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var req mixtypes.CreateOrderRequest = mixtypes.CreateOrderRequest{
		Symbol:    "BTCUSDT",
		Side:      roottypes.SideTypeSell,
		OrderType: roottypes.OrderTypeMarket,
		Quantity:  decimal.RequireFromString("0.05"),
	}
	var _, err = mixOf(client).Trading().CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateOrder market: %v", err)
	}

	var _, body, _ = rec.snapshot()
	if _, ok := body["price"]; ok {
		t.Errorf("market order should NOT include `price` field, got %v", body["price"])
	}
	if body["orderType"] != "market" {
		t.Errorf("orderType: %v", body["orderType"])
	}
}

func TestContract_CreateOrder_ReduceOnly(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"orderId":"x","clientOid":""}}`
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/place-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var _, err = mixOf(client).Trading().CreateOrder(context.Background(), mixtypes.CreateOrderRequest{
		Symbol:     "BTCUSDT",
		Side:       roottypes.SideTypeSell,
		OrderType:  roottypes.OrderTypeLimit,
		Quantity:   decimal.RequireFromString("1"),
		Price:      decimal.RequireFromString("50000"),
		ReduceOnly: true,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	var _, body, _ = rec.snapshot()
	if body["reduceOnly"] != "YES" {
		t.Errorf("reduceOnly: want YES, got %v", body["reduceOnly"])
	}
}

func TestContract_CreateOrder_ValidationErrors(t *testing.T) {
	t.Parallel()
	type tc struct {
		name string
		req  mixtypes.CreateOrderRequest
	}
	var cases []tc = []tc{
		{"empty symbol", mixtypes.CreateOrderRequest{Side: "buy", OrderType: "limit", Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(1)}},
		{"empty side", mixtypes.CreateOrderRequest{Symbol: "BTCUSDT", OrderType: "limit", Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(1)}},
		{"empty type", mixtypes.CreateOrderRequest{Symbol: "BTCUSDT", Side: "buy", Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(1)}},
		{"zero qty", mixtypes.CreateOrderRequest{Symbol: "BTCUSDT", Side: "buy", OrderType: "limit", Price: decimal.NewFromInt(1)}},
		{"limit no price", mixtypes.CreateOrderRequest{Symbol: "BTCUSDT", Side: "buy", OrderType: "limit", Quantity: decimal.NewFromInt(1)}},
	}
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var i int
	for i = 0; i < len(cases); i++ {
		var c tc = cases[i]
		t.Run(c.name, func(t *testing.T) {
			var _, err = mixOf(client).Trading().CreateOrder(context.Background(), c.req)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !bitget.IsInvalidRequest(err) {
				t.Fatalf("want ErrorKindInvalidRequest, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------
// ModifyOrder.
// ---------------------------------------------------------------------

func TestContract_ModifyOrder_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000020,
		"data":{"orderId":"NEW-9999","clientOid":"core-uuid-2"}
	}`
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/modify-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var info mixtypes.OrderInfo
	var err error
	info, err = mixOf(client).Trading().ModifyOrder(context.Background(), mixtypes.ModifyOrderRequest{
		Symbol:        "BTCUSDT",
		OrderID:       "OLD-1111",
		ClientOrderID: "core-uuid-2",
		NewQuantity:   decimal.RequireFromString("0.002"),
		NewPrice:      decimal.RequireFromString("43501.0"),
	})
	if err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	if info.OrderID != "NEW-9999" {
		t.Errorf("OrderID: %q", info.OrderID)
	}
	if info.ClientOrderID != "core-uuid-2" {
		t.Errorf("ClientOrderID: %q", info.ClientOrderID)
	}

	var _, body, _ = rec.snapshot()
	if body["orderId"] != "OLD-1111" {
		t.Errorf("orderId: %v", body["orderId"])
	}
	if body["newClientOid"] != "core-uuid-2" {
		t.Errorf("newClientOid: want core-uuid-2 (Bitget requires NEW clientOid), got %v", body["newClientOid"])
	}
	if body["newSize"] != "0.002" || body["newPrice"] != "43501" {
		t.Errorf("new size/price: %v / %v", body["newSize"], body["newPrice"])
	}
}

func TestContract_ModifyOrder_Validation(t *testing.T) {
	t.Parallel()
	type tc struct {
		name string
		req  mixtypes.ModifyOrderRequest
	}
	var cases []tc = []tc{
		{"empty symbol", mixtypes.ModifyOrderRequest{OrderID: "1", ClientOrderID: "x", NewPrice: decimal.NewFromInt(1)}},
		{"no identifier", mixtypes.ModifyOrderRequest{Symbol: "BTCUSDT", NewPrice: decimal.NewFromInt(1)}},
		{"missing newClientOid", mixtypes.ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: "1", NewPrice: decimal.NewFromInt(1)}},
		{"no new size or price", mixtypes.ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: "1", ClientOrderID: "x"}},
	}
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var i int
	for i = 0; i < len(cases); i++ {
		var c tc = cases[i]
		t.Run(c.name, func(t *testing.T) {
			// Empty symbol triggers the same check; we only assert
			// that we land on a typed validation error.
			var _, err = mixOf(client).Trading().ModifyOrder(context.Background(), c.req)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !bitget.IsInvalidRequest(err) {
				t.Fatalf("want ErrorKindInvalidRequest, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------
// CancelOrder.
// ---------------------------------------------------------------------

func TestContract_CancelOrder_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{"orderId":"OLD-1","clientOid":""}
	}`
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/cancel-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var err = mixOf(client).Trading().CancelOrder(context.Background(), roottypes.CancelOrderRequest{
		Symbol:  "BTCUSDT",
		OrderID: "OLD-1",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	var _, body, _ = rec.snapshot()
	if body["orderId"] != "OLD-1" {
		t.Errorf("orderId: %v", body["orderId"])
	}
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType: %v", body["productType"])
	}
	if body["marginCoin"] != "USDT" {
		t.Errorf("marginCoin: %v", body["marginCoin"])
	}
}

// ---------------------------------------------------------------------
// CancelAllOrders.
// ---------------------------------------------------------------------

func TestContract_CancelAllOrders_Global(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{"successList":[{"orderId":"1","clientOid":""}],"failureList":[]}
	}`
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/cancel-all-orders": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var err = mixOf(client).Trading().CancelAllOrders(context.Background(), "")
	if err != nil {
		t.Fatalf("CancelAllOrders: %v", err)
	}
	var _, body, _ = rec.snapshot()
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType: %v", body["productType"])
	}
	if body["marginCoin"] != "USDT" {
		t.Errorf("marginCoin: %v", body["marginCoin"])
	}
	if _, ok := body["symbol"]; ok {
		t.Errorf("body must NOT carry a symbol filter, got %v", body["symbol"])
	}
}

func TestContract_CancelAllOrders_RejectsPerSymbol(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var err = mixOf(client).Trading().CancelAllOrders(context.Background(), "BTCUSDT")
	if err == nil {
		t.Fatal("expected error for per-symbol cancel")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Fatalf("want ErrorKindInvalidRequest, got %v", err)
	}
	if !strings.Contains(err.Error(), "BTCUSDT") {
		t.Fatalf("error message must mention symbol, got %v", err)
	}
}

// ---------------------------------------------------------------------
// CreateBatchOrders.
// ---------------------------------------------------------------------

func TestContract_CreateBatchOrders_Happy(t *testing.T) {
	t.Parallel()
	// Two requests; venue accepts the first and rejects the second
	// with a typed error code.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"successList":[{"orderId":"o1","clientOid":"c-1"}],
			"failureList":[{"orderId":"","clientOid":"c-2","errorMsg":"price not in range","errorCode":"40808"}]
		}
	}`
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/batch-place-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var reqs []mixtypes.CreateOrderRequest = []mixtypes.CreateOrderRequest{
		{
			Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit,
			Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(50000), ClientOrderID: "c-1",
		},
		{
			Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit,
			Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(99999), ClientOrderID: "c-2",
		},
	}
	var results []mixtypes.BatchOrderResult
	var err error
	results, err = mixOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CreateBatchOrders: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results)=%d, want 2", len(results))
	}

	// Row 0 — success.
	if results[0].Order == nil {
		t.Fatalf("results[0].Order is nil; err=%v", results[0].Err)
	}
	if results[0].Order.OrderID != "o1" {
		t.Errorf("results[0].OrderID: %q", results[0].Order.OrderID)
	}
	if results[0].ClientOrderID != "c-1" {
		t.Errorf("results[0].ClientOrderID: %q", results[0].ClientOrderID)
	}

	// Row 1 — failure with typed error.
	if results[1].Err == nil {
		t.Fatal("results[1].Err is nil")
	}
	var bgErr *bitget.Error
	if !errors.As(results[1].Err, &bgErr) {
		t.Fatalf("results[1].Err: want *bitget.Error, got %T", results[1].Err)
	}
	if bgErr.BitgetCode != "40808" {
		t.Errorf("BitgetCode: want 40808, got %q", bgErr.BitgetCode)
	}
	if results[1].ClientOrderID != "c-2" {
		t.Errorf("results[1].ClientOrderID: %q", results[1].ClientOrderID)
	}

	// Verify wire body shape.
	var _, body, _ = rec.snapshot()
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType: %v", body["productType"])
	}
	if body["marginMode"] != "crossed" {
		t.Errorf("marginMode: %v", body["marginMode"])
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("symbol: %v", body["symbol"])
	}
	var orderList, ok = body["orderList"].([]any)
	if !ok || len(orderList) != 2 {
		t.Fatalf("orderList not a 2-elem array: %v", body["orderList"])
	}
}

func TestContract_CreateBatchOrders_HeterogeneousSymbol(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var _, err = mixOf(client).Trading().CreateBatchOrders(context.Background(), []mixtypes.CreateOrderRequest{
		{Symbol: "BTCUSDT", Side: "buy", OrderType: "limit", Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(1), ClientOrderID: "a"},
		{Symbol: "ETHUSDT", Side: "buy", OrderType: "limit", Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(1), ClientOrderID: "b"},
	})
	if err == nil {
		t.Fatal("expected error for heterogeneous symbol")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Fatalf("want ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_CreateBatchOrders_TooLarge(t *testing.T) {
	t.Parallel()
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var reqs []mixtypes.CreateOrderRequest = make([]mixtypes.CreateOrderRequest, 51)
	var i int
	for i = 0; i < len(reqs); i++ {
		reqs[i] = mixtypes.CreateOrderRequest{
			Symbol:        "BTCUSDT",
			Side:          roottypes.SideTypeBuy,
			OrderType:     roottypes.OrderTypeLimit,
			Quantity:      decimal.NewFromInt(1),
			Price:         decimal.NewFromInt(50000),
			ClientOrderID: "c-" + strings.Repeat("x", 1),
		}
	}
	var _, err = mixOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if err == nil {
		t.Fatal("expected error for batch>50")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Fatalf("want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// ModifyBatchOrders.
// ---------------------------------------------------------------------

func TestContract_ModifyBatchOrders_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"successList":[{"orderId":"NEW-1","clientOid":"c-1"}],
			"failureList":[]
		}
	}`
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/batch-modify-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var results []mixtypes.BatchOrderResult
	var err error
	results, err = mixOf(client).Trading().ModifyBatchOrders(context.Background(), []mixtypes.ModifyOrderRequest{
		{Symbol: "BTCUSDT", OrderID: "OLD-1", ClientOrderID: "c-1", NewPrice: decimal.NewFromInt(50100)},
	})
	if err != nil {
		t.Fatalf("ModifyBatchOrders: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d, want 1", len(results))
	}
	if results[0].Order == nil {
		t.Fatalf("results[0].Order is nil; err=%v", results[0].Err)
	}
	if results[0].Order.OrderID != "NEW-1" {
		t.Errorf("OrderID: %q", results[0].Order.OrderID)
	}

	var _, body, _ = rec.snapshot()
	if body["productType"] != "USDT-FUTURES" {
		t.Errorf("productType: %v", body["productType"])
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("symbol: %v", body["symbol"])
	}
}

// ---------------------------------------------------------------------
// CancelBatchOrders.
// ---------------------------------------------------------------------

func TestContract_CancelBatchOrders_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"successList":[{"orderId":"o-1","clientOid":"c-1"}],
			"failureList":[{"orderId":"","clientOid":"c-2","errorMsg":"order not exist","errorCode":"40768"}]
		}
	}`
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/batch-cancel-orders": fixture,
	}, nil)

	var results []mixtypes.BatchOrderResult
	var err error
	results, err = mixOf(client).Trading().CancelBatchOrders(context.Background(), []roottypes.CancelOrderRequest{
		{Symbol: "BTCUSDT", OrderID: "o-1", ClientOrderID: "c-1"},
		{Symbol: "BTCUSDT", ClientOrderID: "c-2"},
	})
	if err != nil {
		t.Fatalf("CancelBatchOrders: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results)=%d, want 2", len(results))
	}
	if results[0].Order == nil || results[0].Order.Status != roottypes.OrderStatusCancelled {
		t.Errorf("results[0]: want cancelled status, got %+v", results[0])
	}
	if results[1].Err == nil {
		t.Errorf("results[1].Err: want non-nil")
	}
	if results[1].ClientOrderID != "c-2" {
		t.Errorf("results[1].ClientOrderID: %q", results[1].ClientOrderID)
	}
}
