/*
FILE: spot/account_contract_test.go

DESCRIPTION:
Contract tests for the spot M3 account / history endpoints. They
verify that:

  - Each endpoint sends the correct query / body shape (and DOES
    NOT send mix-only fields like productType / marginCoin /
    holdSide / tradeSide on the wire).
  - The response payloads parse into the SDK's typed structs
    (AccountInfo / Balance / OrderInfo / Fill).
  - Pagination correctly walks the idLessThan = lastOrderId cursor
    across multiple mock pages and stops on the standard
    "len < limit" sentinel.
  - Validation errors fail FAST before any HTTP call.

Tests use the shared mockBitget harness (see contract_test.go).
*/

package spot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ---------------------------------------------------------------------
// GetAccountInfo.
// ---------------------------------------------------------------------

func TestContract_Spot_GetAccountInfo_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000001,
		"data":{
			"userId":"12345",
			"inviterId":"67890",
			"ips":"203.0.113.1,203.0.113.2",
			"authorities":["spot","contract"],
			"parentId":"12345",
			"traderType":"market_maker",
			"channelCode":"",
			"regisTime":"1620000000000"
		}
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/account/info": fixture,
	}, nil)

	var info spottypes.AccountInfo
	var err error
	info, err = spotOf(client).Account().GetAccountInfo(context.Background())
	if err != nil {
		t.Fatalf("GetAccountInfo: %v", err)
	}

	if info.UserID != "12345" {
		t.Errorf("UserID: %q", info.UserID)
	}
	if info.InviterID != "67890" {
		t.Errorf("InviterID: %q", info.InviterID)
	}
	if info.IPs != "203.0.113.1,203.0.113.2" {
		t.Errorf("IPs: %q", info.IPs)
	}
	if len(info.Authorities) != 2 || info.Authorities[0] != "spot" || info.Authorities[1] != "contract" {
		t.Errorf("Authorities: %v", info.Authorities)
	}
	if info.TraderType != "market_maker" {
		t.Errorf("TraderType: %q", info.TraderType)
	}
	if info.RegisTimeMs != 1620000000000 {
		t.Errorf("RegisTimeMs: %d", info.RegisTimeMs)
	}
}

// ---------------------------------------------------------------------
// GetAccount.
// ---------------------------------------------------------------------

func TestContract_Spot_GetAccount_AggregatesAllCoins(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000002,
		"data":[
			{"coin":"BTC","available":"0.5","frozen":"0.1","locked":"0","limitAvailable":"","uTime":"1700000000000"},
			{"coin":"USDT","available":"1000","frozen":"500","locked":"0","limitAvailable":"","uTime":"1700000000000"}
		]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/account/assets": fixture,
	}, nil)

	var bal roottypes.Balance
	var err error
	bal, err = spotOf(client).Account().GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if len(bal.Coins) != 2 {
		t.Fatalf("Coins: want 2, got %d", len(bal.Coins))
	}

	var btc roottypes.CoinBalance = bal.Coins[0]
	if btc.Coin != "BTC" {
		t.Errorf("Coins[0].Coin: %q", btc.Coin)
	}
	if !btc.Available.Equal(decimal.RequireFromString("0.5")) {
		t.Errorf("BTC.Available: %s", btc.Available)
	}
	if !btc.Frozen.Equal(decimal.RequireFromString("0.1")) {
		t.Errorf("BTC.Frozen: %s", btc.Frozen)
	}
	// Equity = available + frozen + locked = 0.5 + 0.1 + 0 = 0.6
	if !btc.Equity.Equal(decimal.RequireFromString("0.6")) {
		t.Errorf("BTC.Equity (synthesized): %s", btc.Equity)
	}

	var usdt roottypes.CoinBalance = bal.Coins[1]
	if usdt.Coin != "USDT" {
		t.Errorf("Coins[1].Coin: %q", usdt.Coin)
	}
	if !usdt.Available.Equal(decimal.RequireFromString("1000")) {
		t.Errorf("USDT.Available: %s", usdt.Available)
	}
	if !usdt.Frozen.Equal(decimal.RequireFromString("500")) {
		t.Errorf("USDT.Frozen: %s", usdt.Frozen)
	}

	// Aggregate fields are NOT exposed by spot account/assets and
	// the SDK leaves them zero.
	if !bal.TotalEquity.IsZero() {
		t.Errorf("TotalEquity must stay zero on spot, got %s", bal.TotalEquity)
	}
	if !bal.AvailableBalance.IsZero() {
		t.Errorf("AvailableBalance must stay zero on spot, got %s", bal.AvailableBalance)
	}
}

// ---------------------------------------------------------------------
// GetOpenOrders — pagination.
// ---------------------------------------------------------------------

// pagedOrdersServer mints PageLimit-row pages until the requested
// total is exhausted, then a final short page. Used by both
// GetOpenOrders and GetOrderHistory tests since they share the
// cursor protocol.
type pagedOrdersServer struct {
	mu             sync.Mutex
	receivedCursor []string // captures every idLessThan we observed
	totalRows      int
	limit          int
	hits           int32
}

func (p *pagedOrdersServer) handle(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	atomic.AddInt32(&p.hits, 1)

	var q url.Values = r.URL.Query()
	var idLessThan string = q.Get("idLessThan")

	p.mu.Lock()
	p.receivedCursor = append(p.receivedCursor, idLessThan)
	p.mu.Unlock()

	// Resolve start index from the cursor: empty → 0, otherwise the
	// cursor value is itself the orderId of the previous page's last
	// row (we mint orderIds as zero-padded sequence numbers so we
	// can decode them trivially).
	var startIdx int
	if idLessThan != "" {
		var parsed, _ = strconv.Atoi(idLessThan)
		startIdx = parsed
	}

	// How many rows to ship on this page: up to limit, capped at
	// totalRows.
	var endIdx int = startIdx + p.limit
	if endIdx > p.totalRows {
		endIdx = p.totalRows
	}

	if startIdx >= p.totalRows {
		// Past the end: return an empty array.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":"00000","msg":"success","requestTime":0,"data":[]}`)
		return
	}

	var rows []string
	var i int
	for i = startIdx; i < endIdx; i++ {
		// Mint a row whose orderId equals (i+1) zero-padded so the
		// cursor returned to the client is the index of the LAST row
		// (and matches what the helper expects on the next call).
		rows = append(rows, fmt.Sprintf(
			`{"userId":"u","symbol":"BTCUSDT","orderId":"%d","clientOid":"core-%d","price":"43500","size":"0.001","orderType":"limit","side":"buy","status":"live","priceAvg":"","baseVolume":"0","quoteVolume":"0","force":"post_only","feeDetail":"","cTime":"1700000000000","uTime":"1700000000000"}`,
			i+1, i+1,
		))
	}

	var body string = `{"code":"00000","msg":"success","requestTime":0,"data":[`
	for j, row := range rows {
		if j > 0 {
			body += ","
		}
		body += row
	}
	body += `]}`

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func TestContract_Spot_GetOpenOrders_Pagination(t *testing.T) {
	t.Parallel()

	// Mint 250 orders → ceil(250 / 100) = 3 pages: 100 + 100 + 50.
	var paged pagedOrdersServer = pagedOrdersServer{
		totalRows: 250,
		limit:     bgcommon.OrdersPageLimit, // 100
	}

	var client *bitget.Client
	_, client = mockBitgetCustom(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/spot/trade/unfilled-orders" {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		paged.handle(t, w, r)
	})

	var orders []spottypes.OrderInfo
	var err error
	orders, err = spotOf(client).Account().GetOpenOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if len(orders) != 250 {
		t.Fatalf("len(orders): want 250, got %d", len(orders))
	}
	if got := atomic.LoadInt32(&paged.hits); got != 3 {
		t.Errorf("page hits: want 3 (100+100+50), got %d", got)
	}
	// Cursor protocol: page 0 has empty idLessThan, page 1 = "100",
	// page 2 = "200" (the last orderId of page 1). Page 2 returns
	// only 50 rows < limit, so the helper stops without a 4th call.
	paged.mu.Lock()
	defer paged.mu.Unlock()
	if len(paged.receivedCursor) != 3 {
		t.Fatalf("cursor calls: want 3, got %d (%v)", len(paged.receivedCursor), paged.receivedCursor)
	}
	if paged.receivedCursor[0] != "" {
		t.Errorf("cursor[0]: want empty, got %q", paged.receivedCursor[0])
	}
	if paged.receivedCursor[1] != "100" {
		t.Errorf("cursor[1]: want 100, got %q", paged.receivedCursor[1])
	}
	if paged.receivedCursor[2] != "200" {
		t.Errorf("cursor[2]: want 200, got %q", paged.receivedCursor[2])
	}
}

func TestContract_Spot_GetOpenOrders_NoSymbolReturnsAll(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":[
			{"symbol":"BTCUSDT","orderId":"o1","clientOid":"c1","price":"43500","size":"0.001","orderType":"limit","side":"buy","status":"live","priceAvg":"","baseVolume":"0","quoteVolume":"0","force":"post_only","feeDetail":"","cTime":"1700000000000","uTime":"1700000000000"}
		]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/unfilled-orders": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if q.Get("symbol") != "" {
			t.Errorf("symbol must be omitted when caller passes empty: got %q", q.Get("symbol"))
		}
		if q.Get("productType") != "" {
			t.Errorf("productType must NOT be sent on spot, got %q", q.Get("productType"))
		}
		if q.Get("marginCoin") != "" {
			t.Errorf("marginCoin must NOT be sent on spot, got %q", q.Get("marginCoin"))
		}
	})

	var orders []spottypes.OrderInfo
	var err error
	orders, err = spotOf(client).Account().GetOpenOrders(context.Background(), "")
	if err != nil {
		t.Fatalf("GetOpenOrders(\"\"): %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("len: %d", len(orders))
	}
}

// ---------------------------------------------------------------------
// GetOrderHistory — window parameters.
// ---------------------------------------------------------------------

func TestContract_Spot_GetOrderHistory_WindowAndCursor(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":[
			{"symbol":"BTCUSDT","orderId":"o1","clientOid":"c1","price":"43500","size":"0.001","orderType":"limit","side":"buy","status":"filled","priceAvg":"43499","baseVolume":"0.001","quoteVolume":"43.499","force":"post_only","feeDetail":"","cTime":"1700000000000","uTime":"1700000060000"}
		]
	}`

	var seenStart, seenEnd, seenCursor string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/history-orders": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		seenStart = q.Get("startTime")
		seenEnd = q.Get("endTime")
		seenCursor = q.Get("idLessThan")
	})

	var orders []spottypes.OrderInfo
	var err error
	orders, err = spotOf(client).Account().GetOrderHistory(context.Background(), "BTCUSDT", 1700000000000, 1700099999999)
	if err != nil {
		t.Fatalf("GetOrderHistory: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("len: %d", len(orders))
	}
	if seenStart != "1700000000000" {
		t.Errorf("startTime: %q", seenStart)
	}
	if seenEnd != "1700099999999" {
		t.Errorf("endTime: %q", seenEnd)
	}
	if seenCursor != "" {
		t.Errorf("first-page cursor must be empty, got %q", seenCursor)
	}
	if orders[0].Status != roottypes.OrderStatus("filled") {
		t.Errorf("Status: %q", orders[0].Status)
	}
	if !orders[0].FilledQuantity.Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("FilledQuantity: %s", orders[0].FilledQuantity)
	}
	if !orders[0].AvgFilledPrice.Equal(decimal.RequireFromString("43499")) {
		t.Errorf("AvgFilledPrice: %s", orders[0].AvgFilledPrice)
	}
}

// ---------------------------------------------------------------------
// GetOrderDetail — POST shape.
// ---------------------------------------------------------------------

func TestContract_Spot_GetOrderDetail_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":[{
			"symbol":"BTCUSDT","orderId":"99887766","clientOid":"core-1",
			"price":"43500","size":"0.001","orderType":"limit","side":"buy",
			"status":"live","priceAvg":"","baseVolume":"0","quoteVolume":"0",
			"force":"post_only","feeDetail":"",
			"cTime":"1700000000000","uTime":"1700000000000"
		}]
	}`

	var rec requestRecorder
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/orderInfo": fixture,
	}, func(t *testing.T, r *http.Request) { rec.record(r) })

	var info spottypes.OrderInfo
	var err error
	info, err = spotOf(client).Account().GetOrderDetail(context.Background(), "BTCUSDT", "99887766", "")
	if err != nil {
		t.Fatalf("GetOrderDetail: %v", err)
	}
	if info.OrderID != "99887766" {
		t.Errorf("OrderID: %q", info.OrderID)
	}
	if info.ClientOrderID != "core-1" {
		t.Errorf("ClientOrderID: %q", info.ClientOrderID)
	}

	// Pin the POST body shape — orderInfo is the only spot account
	// endpoint that uses POST. Future revisions of the SDK that
	// accidentally flip it to GET would break callers transparently
	// without this assertion.
	var path string
	var body map[string]any
	path, body, _ = rec.snapshot()
	if path != "/api/v2/spot/trade/orderInfo" {
		t.Fatalf("path: %q", path)
	}
	if body["symbol"] != "BTCUSDT" {
		t.Errorf("body.symbol: %v", body["symbol"])
	}
	if body["orderId"] != "99887766" {
		t.Errorf("body.orderId: %v", body["orderId"])
	}
	if _, ok := body["clientOid"]; ok && body["clientOid"] != "" {
		// omitempty must drop it when caller passed empty.
		t.Errorf("body.clientOid must be omitted when empty, got %v", body["clientOid"])
	}
	if _, ok := body["productType"]; ok {
		t.Errorf("productType must NOT be in spot orderInfo body: %v", body["productType"])
	}
}

func TestContract_Spot_GetOrderDetail_NotFound(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":[]}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/orderInfo": fixture,
	}, nil)

	var err error
	_, err = spotOf(client).Account().GetOrderDetail(context.Background(), "BTCUSDT", "missing", "")
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetOrderDetail empty list: want ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_Spot_GetOrderDetail_FailFastValidation(t *testing.T) {
	t.Parallel()

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{}, nil)
	var s *Client = spotOf(client)

	type stubCall struct {
		name string
		fn   func() error
	}
	var calls = []stubCall{
		{"emptySymbol", func() error {
			_, err := s.Account().GetOrderDetail(context.Background(), "", "1", "")
			return err
		}},
		{"noIdentifier", func() error {
			_, err := s.Account().GetOrderDetail(context.Background(), "BTCUSDT", "", "")
			return err
		}},
	}
	var i int
	for i = 0; i < len(calls); i++ {
		var c stubCall = calls[i]
		t.Run(c.name, func(t *testing.T) {
			var err error = c.fn()
			if !bitget.IsInvalidRequest(err) {
				t.Fatalf("%s: want ErrorKindInvalidRequest, got %v", c.name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------
// GetFills — orderID filter, fee parsing.
// ---------------------------------------------------------------------

func TestContract_Spot_GetFills_PerOrder(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":[
			{
				"userId":"u","symbol":"BTCUSDT","orderId":"o1","tradeId":"t1",
				"orderType":"limit","side":"buy","priceAvg":"43500","size":"0.001",
				"amount":"43.5","feeDetail":{"deduction":"no","feeCoin":"USDT","totalDeduction":"0","totalFee":"-0.0435"},
				"tradeScope":"taker","cTime":"1700000000000","uTime":"1700000000000"
			}
		]
	}`

	var seenOrderID string
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/spot/trade/fills": fixture,
	}, func(t *testing.T, r *http.Request) {
		seenOrderID = r.URL.Query().Get("orderId")
	})

	var fills []spottypes.Fill
	var err error
	fills, err = spotOf(client).Account().GetFills(context.Background(), "BTCUSDT", "o1", 0, 0)
	if err != nil {
		t.Fatalf("GetFills: %v", err)
	}
	if seenOrderID != "o1" {
		t.Errorf("orderId: %q", seenOrderID)
	}
	if len(fills) != 1 {
		t.Fatalf("len: %d", len(fills))
	}
	if fills[0].TradeID != "t1" {
		t.Errorf("TradeID: %q", fills[0].TradeID)
	}
	if !fills[0].FillPrice.Equal(decimal.RequireFromString("43500")) {
		t.Errorf("FillPrice: %s", fills[0].FillPrice)
	}
	if !fills[0].Size.Equal(decimal.RequireFromString("0.001")) {
		t.Errorf("Size: %s", fills[0].Size)
	}
	if !fills[0].Amount.Equal(decimal.RequireFromString("43.5")) {
		t.Errorf("Amount: %s", fills[0].Amount)
	}
	if !fills[0].TotalFee.Equal(decimal.RequireFromString("-0.0435")) {
		t.Errorf("TotalFee: %s", fills[0].TotalFee)
	}
	if fills[0].FeeCoin != "USDT" {
		t.Errorf("FeeCoin: %q", fills[0].FeeCoin)
	}
	if fills[0].TradeScope != "taker" {
		t.Errorf("TradeScope: %q", fills[0].TradeScope)
	}
}

// ---------------------------------------------------------------------
// Test harness — custom handler variant for stateful pagination tests.
// ---------------------------------------------------------------------

// mockBitgetCustom is the variant of mockBitget that gives the test a
// raw handler closure (so the paged-orders test can stitch responses
// together based on the request's idLessThan cursor) and returns the
// underlying httptest.Server alongside the wired *bitget.Client.
func mockBitgetCustom(t *testing.T, handler func(t *testing.T, w http.ResponseWriter, r *http.Request)) (*httptest.Server, *bitget.Client) {
	t.Helper()

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(t, w, r)
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
