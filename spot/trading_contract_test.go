/*
FILE: spot/trading_contract_test.go

DESCRIPTION:
Contract tests for the spot M2 trading endpoints. They verify that:

  - The request body is built correctly from the typed
    CreateOrderRequest / ModifyOrderRequest / CancelOrderRequest
    (no productType / marginMode / marginCoin / tradeSide on spot).
  - The response payload is parsed into the SDK's typed OrderInfo /
    BatchOrderResult.
  - Per-row outcomes on batch endpoints land in the correct slot
    (matched by clientOid; positional fallback when clientOid is empty).
  - Native spot batch-cancel-replace-order is hit with a SINGLE REST
    call (no client-side fan-out, in contrast to the mix profile).
  - CancelAllOrders rejects empty-symbol calls (Bitget V2 spot has
    no cross-symbol cancel-all endpoint).
  - Validation errors fail FAST before any HTTP call.

Tests use the same httptest server harness as the M2 market-data
tests (see contract_test.go::mockBitget).
*/

package spot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// requestRecorder captures the latest POST body + path for assertions.
type requestRecorder struct {
	mu      sync.Mutex
	path    string
	body    map[string]any
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

func TestContract_Spot_CreateOrder_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000010,
		"data":{"orderId":"99887766","clientOid":"core-uuid-1"}
	}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/place-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var req spottypes.CreateOrderRequest = spottypes.CreateOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          roottypes.SideTypeBuy,
		OrderType:     roottypes.OrderTypeLimit,
		TimeInForce:   roottypes.TimeInForcePostOnly,
		Quantity:      decimal.RequireFromString("0.001"),
		Price:         decimal.RequireFromString("43500.5"),
		ClientOrderID: "core-uuid-1",
	}
	var info spottypes.OrderInfo
	var err error
	info, err = spotOf(client).Trading().CreateOrder(context.Background(), req)
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
	if path != "/api/v2/spot/trade/place-order" {
		t.Fatalf("path: %q", path)
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("symbol: %v", body["symbol"])
	}
	// Spot must NOT send productType / marginMode / marginCoin /
	// tradeSide / reduceOnly. Pin against accidental regression.
	if _, ok := body["productType"]; ok {
		t.Errorf("productType must not be in spot place-order body: %v", body["productType"])
	}
	if _, ok := body["marginMode"]; ok {
		t.Errorf("marginMode must not be in spot place-order body: %v", body["marginMode"])
	}
	if _, ok := body["marginCoin"]; ok {
		t.Errorf("marginCoin must not be in spot place-order body: %v", body["marginCoin"])
	}
	if _, ok := body["tradeSide"]; ok {
		t.Errorf("tradeSide must not be in spot place-order body: %v", body["tradeSide"])
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

func TestContract_Spot_CreateOrder_MarketOrderOmitsPrice(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"orderId":"42","clientOid":""}}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/place-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	// Market BUY: Quantity in QUOTE (USDT). Price field omitted.
	var req spottypes.CreateOrderRequest = spottypes.CreateOrderRequest{
		Symbol:    "BTCUSDT",
		Side:      roottypes.SideTypeBuy,
		OrderType: roottypes.OrderTypeMarket,
		Quantity:  decimal.RequireFromString("100"),
	}
	var err error
	_, err = spotOf(client).Trading().CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateOrder market: %v", err)
	}
	var _, body, _ = rec.snapshot()
	if _, ok := body["price"]; ok {
		t.Errorf("price must be omitted on market orders, got %v", body["price"])
	}
	if body["orderType"] != "market" {
		t.Errorf("orderType: %v", body["orderType"])
	}
}

// ---------------------------------------------------------------------
// ModifyOrder.
// ---------------------------------------------------------------------

func TestContract_Spot_ModifyOrder_AutoFillsNewClientOid(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{"orderId":"new-id","clientOid":"s-deadbeef"}
	}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/cancel-replace-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var req spottypes.ModifyOrderRequest = spottypes.ModifyOrderRequest{
		Symbol:        "BTCUSDT",
		ClientOrderID: "core-uuid-1",
		NewQuantity:   decimal.RequireFromString("0.002"),
		NewPrice:      decimal.RequireFromString("43400"),
	}
	var info spottypes.OrderInfo
	var err error
	info, err = spotOf(client).Trading().ModifyOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	if info.OrderID != "new-id" {
		t.Errorf("OrderID: %q", info.OrderID)
	}

	var _, body, _ = rec.snapshot()
	var newOid, ok = body["newClientOid"].(string)
	if !ok || newOid == "" {
		t.Fatalf("newClientOid missing: %v", body)
	}
	if !strings.HasPrefix(newOid, "s-") {
		t.Errorf("newClientOid: want s- prefix (generated), got %q", newOid)
	}
	if newOid == "core-uuid-1" {
		t.Errorf("newClientOid must differ from clientOid; got %q", newOid)
	}
	if body["clientOid"] != "core-uuid-1" {
		t.Errorf("clientOid: %v", body["clientOid"])
	}
	if body["newSize"] != "0.002" {
		t.Errorf("newSize: %v", body["newSize"])
	}
	if body["newPrice"] != "43400" {
		t.Errorf("newPrice: %v", body["newPrice"])
	}
}

func TestContract_Spot_ModifyOrder_RejectsDuplicateClientOid(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var err error
	_, err = spotOf(client).Trading().ModifyOrder(context.Background(), spottypes.ModifyOrderRequest{
		Symbol:           "BTCUSDT",
		ClientOrderID:    "same",
		NewClientOrderID: "same",
		NewQuantity:      decimal.RequireFromString("0.002"),
	})
	if !bitget.IsInvalidRequest(err) {
		t.Fatalf("ModifyOrder dup-oid: want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// CancelOrder.
// ---------------------------------------------------------------------

func TestContract_Spot_CancelOrder_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"orderId":"99887766","clientOid":"core-uuid-1"}}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/cancel-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var err error
	err = spotOf(client).Trading().CancelOrder(context.Background(), roottypes.CancelOrderRequest{
		Symbol:        "BTCUSDT",
		ClientOrderID: "core-uuid-1",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	var path string
	var body map[string]any
	path, body, _ = rec.snapshot()
	if path != "/api/v2/spot/trade/cancel-order" {
		t.Fatalf("path: %q", path)
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("symbol: %v", body["symbol"])
	}
	if _, ok := body["productType"]; ok {
		t.Errorf("productType must not be in spot cancel-order body: %v", body["productType"])
	}
	if _, ok := body["marginCoin"]; ok {
		t.Errorf("marginCoin must not be in spot cancel-order body: %v", body["marginCoin"])
	}
	if body["clientOid"] != "core-uuid-1" {
		t.Errorf("clientOid: %v", body["clientOid"])
	}
}

// ---------------------------------------------------------------------
// CreateBatchOrders.
// ---------------------------------------------------------------------

func TestContract_Spot_CreateBatchOrders_HappyAndMixed(t *testing.T) {
	t.Parallel()
	// One success, one failure — mixed envelope is the trickiest
	// case for the collator.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"successList":[{"orderId":"o1","clientOid":"core-1"}],
			"failureList":[{"orderId":"","clientOid":"core-2","errorMsg":"insufficient balance","errorCode":"43012"}]
		}
	}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/batch-orders": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var reqs []spottypes.CreateOrderRequest = []spottypes.CreateOrderRequest{
		{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, TimeInForce: roottypes.TimeInForcePostOnly,
			Quantity: decimal.RequireFromString("0.001"), Price: decimal.RequireFromString("43500"), ClientOrderID: "core-1"},
		{Symbol: "BTCUSDT", Side: roottypes.SideTypeSell, OrderType: roottypes.OrderTypeLimit, TimeInForce: roottypes.TimeInForcePostOnly,
			Quantity: decimal.RequireFromString("0.001"), Price: decimal.RequireFromString("43600"), ClientOrderID: "core-2"},
	}
	var results []spottypes.BatchOrderResult
	var err error
	results, err = spotOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CreateBatchOrders: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("len(results): want 2, got %d", len(results))
	}
	if results[0].Order == nil || results[0].Err != nil {
		t.Fatalf("results[0] expected success: %+v", results[0])
	}
	if results[0].Order.OrderID != "o1" {
		t.Errorf("results[0].OrderID: %q", results[0].Order.OrderID)
	}
	if results[1].Order != nil || results[1].Err == nil {
		t.Fatalf("results[1] expected failure: %+v", results[1])
	}
	if !errors.Is(results[1].Err, results[1].Err) || !strings.Contains(results[1].Err.Error(), "insufficient balance") {
		t.Errorf("results[1].Err: %v", results[1].Err)
	}

	var path string
	var body map[string]any
	path, body, _ = rec.snapshot()
	if path != "/api/v2/spot/trade/batch-orders" {
		t.Fatalf("path: %q", path)
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("body.symbol: %v", body["symbol"])
	}
	var orderList, ok = body["orderList"].([]any)
	if !ok || len(orderList) != 2 {
		t.Fatalf("orderList: %v", body["orderList"])
	}
}

func TestContract_Spot_CreateBatchOrders_HeterogeneousSymbol(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var reqs []spottypes.CreateOrderRequest = []spottypes.CreateOrderRequest{
		{Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, Quantity: decimal.RequireFromString("0.001"), Price: decimal.RequireFromString("1")},
		{Symbol: "ETHUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, Quantity: decimal.RequireFromString("0.001"), Price: decimal.RequireFromString("1")},
	}
	var _, err = spotOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if !bitget.IsInvalidRequest(err) {
		t.Fatalf("CreateBatchOrders mixed symbols: want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// ModifyBatchOrders — NATIVE on spot, single REST call.
// ---------------------------------------------------------------------

func TestContract_Spot_ModifyBatchOrders_NativeSingleRPC(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"successList":[
				{"orderId":"new-1","clientOid":"s-aaaa"},
				{"orderId":"new-2","clientOid":"s-bbbb"}
			],
			"failureList":[]
		}
	}`

	var hits int32
	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/batch-cancel-replace-order": fixture,
	}, func(t *testing.T, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		rec.record(r)
	})

	var reqs []spottypes.ModifyOrderRequest = []spottypes.ModifyOrderRequest{
		{Symbol: "BTCUSDT", ClientOrderID: "core-1", NewClientOrderID: "s-aaaa", NewPrice: decimal.RequireFromString("43500")},
		{Symbol: "BTCUSDT", ClientOrderID: "core-2", NewClientOrderID: "s-bbbb", NewPrice: decimal.RequireFromString("43600")},
	}
	var results []spottypes.BatchOrderResult
	var err error
	results, err = spotOf(client).Trading().ModifyBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("ModifyBatchOrders: %v", err)
	}

	// Pin the central architectural promise: native endpoint = ONE
	// HTTP hit, regardless of batch size. (Mix has client-side
	// fan-out and would generate len(reqs) hits.)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("ModifyBatchOrders should hit the venue exactly once, got %d", got)
	}
	if len(results) != 2 {
		t.Fatalf("len(results): want 2, got %d", len(results))
	}
	if results[0].Order == nil || results[0].Order.OrderID != "new-1" {
		t.Errorf("results[0]: %+v", results[0])
	}
	if results[1].Order == nil || results[1].Order.OrderID != "new-2" {
		t.Errorf("results[1]: %+v", results[1])
	}

	var path string
	var body map[string]any
	path, body, _ = rec.snapshot()
	if path != "/api/v2/spot/trade/batch-cancel-replace-order" {
		t.Fatalf("path: %q", path)
	}
	var orderList, ok = body["orderList"].([]any)
	if !ok || len(orderList) != 2 {
		t.Fatalf("orderList: %v", body["orderList"])
	}
}

func TestContract_Spot_ModifyBatchOrders_AutoFillsNewClientOid(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"successList":[],"failureList":[]}}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/batch-cancel-replace-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var reqs []spottypes.ModifyOrderRequest = []spottypes.ModifyOrderRequest{
		{Symbol: "BTCUSDT", ClientOrderID: "core-1", NewQuantity: decimal.RequireFromString("0.001")},
		{Symbol: "BTCUSDT", ClientOrderID: "core-2", NewQuantity: decimal.RequireFromString("0.002")},
	}
	var _, err = spotOf(client).Trading().ModifyBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("ModifyBatchOrders: %v", err)
	}
	var _, body, _ = rec.snapshot()
	var orderList, ok = body["orderList"].([]any)
	if !ok || len(orderList) != 2 {
		t.Fatalf("orderList: %v", body["orderList"])
	}
	for i, row := range orderList {
		var rowMap map[string]any = row.(map[string]any)
		var newOid, _ = rowMap["newClientOid"].(string)
		if newOid == "" {
			t.Fatalf("row %d missing newClientOid", i)
		}
		if !strings.HasPrefix(newOid, "s-") {
			t.Errorf("row %d newClientOid prefix: %q", i, newOid)
		}
	}
}

// ---------------------------------------------------------------------
// CancelBatchOrders.
// ---------------------------------------------------------------------

func TestContract_Spot_CancelBatchOrders_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"successList":[
				{"orderId":"o1","clientOid":"core-1"}
			],
			"failureList":[
				{"orderId":"o2","clientOid":"core-2","errorMsg":"order not exist","errorCode":"43025"}
			]
		}
	}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/cancel-batch-orders": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var reqs []roottypes.CancelOrderRequest = []roottypes.CancelOrderRequest{
		{Symbol: "BTCUSDT", ClientOrderID: "core-1"},
		{Symbol: "BTCUSDT", ClientOrderID: "core-2"},
	}
	var results []spottypes.BatchOrderResult
	var err error
	results, err = spotOf(client).Trading().CancelBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CancelBatchOrders: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results): want 2, got %d", len(results))
	}
	if results[0].Order == nil || results[0].Order.Status != roottypes.OrderStatusCancelled {
		t.Errorf("results[0]: %+v", results[0])
	}
	if results[1].Err == nil {
		t.Errorf("results[1] expected failure: %+v", results[1])
	}

	var path string
	var body map[string]any
	path, body, _ = rec.snapshot()
	if path != "/api/v2/spot/trade/cancel-batch-orders" {
		t.Fatalf("path: %q", path)
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("body.symbol: %v", body["symbol"])
	}
}

// ---------------------------------------------------------------------
// CancelAllOrders.
// ---------------------------------------------------------------------

func TestContract_Spot_CancelAllOrders_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"successList":[],"failureList":[]}}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/cancel-symbol-order": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var err error
	err = spotOf(client).Trading().CancelAllOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("CancelAllOrders: %v", err)
	}
	var _, body, _ = rec.snapshot()
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("body.symbol: %v", body["symbol"])
	}
	if _, ok := body["productType"]; ok {
		t.Errorf("productType must NOT be in spot cancel-symbol-order: %v", body["productType"])
	}
}

func TestContract_Spot_CancelAllOrders_RejectsEmptySymbol(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)

	var err error
	err = spotOf(client).Trading().CancelAllOrders(context.Background(), "")
	if !bitget.IsInvalidRequest(err) {
		t.Fatalf("CancelAllOrders empty: want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// Validation fail-fast (no HTTP call).
// ---------------------------------------------------------------------

func TestContract_Spot_Trading_FailFastValidation(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)
	var s *Client = spotOf(client)
	var ctx context.Context = context.Background()

	type stubCall struct {
		name string
		fn   func() error
	}
	var calls = []stubCall{
		{"Trading.CreateOrder_zeroQty", func() error {
			_, err := s.Trading().CreateOrder(ctx, spottypes.CreateOrderRequest{
				Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit,
			})
			return err
		}},
		{"Trading.CreateOrder_emptySymbol", func() error {
			_, err := s.Trading().CreateOrder(ctx, spottypes.CreateOrderRequest{
				Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, Quantity: decimal.RequireFromString("1"), Price: decimal.RequireFromString("1"),
			})
			return err
		}},
		{"Trading.CreateOrder_limitNoPrice", func() error {
			_, err := s.Trading().CreateOrder(ctx, spottypes.CreateOrderRequest{
				Symbol: "BTCUSDT", Side: roottypes.SideTypeBuy, OrderType: roottypes.OrderTypeLimit, Quantity: decimal.RequireFromString("1"),
			})
			return err
		}},
		{"Trading.ModifyOrder_noIdentifier", func() error {
			_, err := s.Trading().ModifyOrder(ctx, spottypes.ModifyOrderRequest{
				Symbol: "BTCUSDT", NewQuantity: decimal.RequireFromString("1"),
			})
			return err
		}},
		{"Trading.ModifyOrder_noChange", func() error {
			_, err := s.Trading().ModifyOrder(ctx, spottypes.ModifyOrderRequest{
				Symbol: "BTCUSDT", ClientOrderID: "core-1",
			})
			return err
		}},
		{"Trading.CancelOrder_noIdentifier", func() error {
			return s.Trading().CancelOrder(ctx, roottypes.CancelOrderRequest{Symbol: "BTCUSDT"})
		}},
		{"Trading.CreateBatchOrders_empty", func() error {
			_, err := s.Trading().CreateBatchOrders(ctx, nil)
			return err
		}},
		{"Trading.ModifyBatchOrders_empty", func() error {
			_, err := s.Trading().ModifyBatchOrders(ctx, nil)
			return err
		}},
		{"Trading.CancelBatchOrders_empty", func() error {
			_, err := s.Trading().CancelBatchOrders(ctx, nil)
			return err
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
