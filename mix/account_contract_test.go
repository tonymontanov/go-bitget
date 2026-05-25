/*
FILE: mix/account_contract_test.go

DESCRIPTION:
Contract tests for the M3 account / position endpoints. Each test
spins up the shared `mockBitget` httptest.Server (defined in
contract_test.go) and asserts that the SDK:

  1. targets the correct path;
  2. supplies the pinned productType / marginCoin where required;
  3. parses Bitget's typical V2 envelope (numeric strings, ISO ms
     timestamps, success / failure lists) into the typed domain
     structs in mix/types and root types/.

Coverage:

  - GetAccount         : /accounts (happy + missing marginCoin row)
  - GetPosition        : /single-position (happy + zero-row filter +
                         empty result)
  - GetOpenOrders      : /orders-pending (happy + cursor pagination
                         continues until endId="" + ceiling guard)
  - GetOrderDetail     : /detail (happy + identifier dispatch +
                         "not found" envelope)
  - ClosePosition      : /close-positions (happy + per-row failure
                         surfaces ErrorKindExchange)
  - SetLeverage        : /set-leverage (happy + body fields)
  - SetPositionMode    : /set-position-mode (happy + body fields)

Tests also assert validation errors (zero-value identifiers) live in
contract_test.go's TestStubsAndValidation_ReturnInvalidRequest, so we
do not duplicate them here.
*/

package mix

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ---------------------------------------------------------------------
// GetAccount.
// ---------------------------------------------------------------------

func TestContract_GetAccount_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000010,
		"data":[
			{"marginCoin":"USDT","locked":"10","available":"990","crossedMaxAvailable":"990",
			 "isolatedMaxAvailable":"0","maxTransferOut":"990","accountEquity":"1000",
			 "usdtEquity":"1000","btcEquity":"0.025","unrealizedPL":"5",
			 "crossedRiskRate":"0.05","crossedMarginLeverage":"10",
			 "isolatedLongLever":"0","isolatedShortLever":"0",
			 "marginMode":"crossed","posMode":"one_way_mode"}
		]
	}`

	var srv *httptest.Server
	var client *bitget.Client
	srv, client = mockBitget(t, map[string]string{
		"/api/v2/mix/account/accounts": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("productType"); got != "USDT-FUTURES" {
			t.Errorf("productType: want USDT-FUTURES, got %q", got)
		}
	})
	_ = srv

	var bal roottypes.Balance
	var err error
	bal, err = mixOf(client).Account().GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}

	if bal.MarginCoin != "USDT" {
		t.Errorf("MarginCoin: want USDT, got %q", bal.MarginCoin)
	}
	if !bal.TotalEquity.Equal(decimal.RequireFromString("1000")) {
		t.Errorf("TotalEquity: want 1000, got %s", bal.TotalEquity)
	}
	if !bal.AvailableBalance.Equal(decimal.RequireFromString("990")) {
		t.Errorf("AvailableBalance: want 990, got %s", bal.AvailableBalance)
	}
	if !bal.LockedBalance.Equal(decimal.RequireFromString("10")) {
		t.Errorf("LockedBalance: want 10, got %s", bal.LockedBalance)
	}
	if !bal.UnrealizedPnL.Equal(decimal.RequireFromString("5")) {
		t.Errorf("UnrealizedPnL: want 5, got %s", bal.UnrealizedPnL)
	}
	if len(bal.Coins) != 1 || bal.Coins[0].Coin != "USDT" {
		t.Fatalf("Coins: want 1 USDT entry, got %+v", bal.Coins)
	}
	if !bal.Coins[0].UsdtEquity.Equal(decimal.RequireFromString("1000")) {
		t.Errorf("Coins[0].UsdtEquity: %s", bal.Coins[0].UsdtEquity)
	}
}

func TestContract_GetAccount_MarginCoinNotFound(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":[
		{"marginCoin":"USDC","accountEquity":"100","available":"100","locked":"0","unrealizedPL":"0","usdtEquity":"100","btcEquity":"0"}
	]}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/account/accounts": fixture,
	}, nil)

	var err error
	_, err = mixOf(client).Account().GetAccount(context.Background())
	if err == nil {
		t.Fatal("GetAccount: expected error, got nil")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetAccount: want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// GetPosition.
// ---------------------------------------------------------------------

func TestContract_GetPosition_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":1700000000020,
		"data":[
			{"symbol":"BTCUSDT","marginCoin":"USDT","holdSide":"long",
			 "openDelegateSize":"0","marginSize":"100","available":"0.5","locked":"0","total":"0.5",
			 "leverage":"10","achievedProfits":"5","openPriceAvg":"50000",
			 "marginMode":"crossed","posMode":"one_way_mode","unrealizedPL":"50",
			 "liquidationPrice":"45000","keepMarginRate":"0.005","markPrice":"50100","marginRatio":"0.01",
			 "breakEvenPrice":"50001","totalFee":"0.5","deductedFee":"0",
			 "cTime":"1700000000000","uTime":"1700000000010"}
		]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/position/single-position": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("symbol"); got != "BTCUSDT" {
			t.Errorf("symbol: want BTCUSDT, got %q", got)
		}
		if got := q.Get("marginCoin"); got != "USDT" {
			t.Errorf("marginCoin: want USDT, got %q", got)
		}
	})

	var pos mixtypes.PositionInfo
	var err error
	pos, err = mixOf(client).Account().GetPosition(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}

	if pos.Symbol != "BTCUSDT" {
		t.Errorf("Symbol: want BTCUSDT, got %q", pos.Symbol)
	}
	if pos.HoldSide != mixtypes.HoldSideLong {
		t.Errorf("HoldSide: want long, got %q", pos.HoldSide)
	}
	if !pos.Quantity.Equal(decimal.RequireFromString("0.5")) {
		t.Errorf("Quantity: want 0.5, got %s", pos.Quantity)
	}
	if !pos.AvgOpenPrice.Equal(decimal.RequireFromString("50000")) {
		t.Errorf("AvgOpenPrice: %s", pos.AvgOpenPrice)
	}
	if !pos.UnrealizedPnL.Equal(decimal.RequireFromString("50")) {
		t.Errorf("UnrealizedPnL: %s", pos.UnrealizedPnL)
	}
	if pos.Leverage != 10 {
		t.Errorf("Leverage: want 10, got %d", pos.Leverage)
	}
	if pos.CreatedAtMs != 1700000000000 {
		t.Errorf("CreatedAtMs: %d", pos.CreatedAtMs)
	}
}

func TestContract_GetPosition_FiltersZeroRows(t *testing.T) {
	t.Parallel()
	// Bitget keeps a row with all-zero numerics for symbols the
	// account once traded but currently has no exposure on. The SDK
	// must skip it and return a clean zero PositionInfo.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":[
			{"symbol":"BTCUSDT","marginCoin":"USDT","holdSide":"long","total":"0",
			 "available":"0","locked":"0","leverage":"10","openPriceAvg":"0",
			 "marginMode":"crossed","posMode":"one_way_mode","unrealizedPL":"0",
			 "liquidationPrice":"0","markPrice":"0","achievedProfits":"0",
			 "cTime":"0","uTime":"0"}
		]
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/position/single-position": fixture,
	}, nil)

	var pos mixtypes.PositionInfo
	var err error
	pos, err = mixOf(client).Account().GetPosition(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if pos.Symbol != "BTCUSDT" {
		t.Errorf("Symbol echo: want BTCUSDT, got %q", pos.Symbol)
	}
	if !pos.Quantity.IsZero() {
		t.Errorf("Quantity: want 0 (no exposure), got %s", pos.Quantity)
	}
	if pos.HoldSide != "" {
		t.Errorf("HoldSide: want empty (no exposure), got %q", pos.HoldSide)
	}
}

// ---------------------------------------------------------------------
// GetOpenOrders — pagination.
// ---------------------------------------------------------------------

func TestContract_GetOpenOrders_PaginatesUntilEmptyEndId(t *testing.T) {
	t.Parallel()
	// Two pages: first reports endId="page2-cursor", second reports
	// endId="" (terminal). The SDK must follow the cursor and stop
	// without a third call.

	// Build 100-row pages with sequential orderIds. Bitget's V2 page
	// limit is 100, so this exercises the "full page → fetch next"
	// branch.
	type row struct {
		Symbol     string `json:"symbol"`
		Size       string `json:"size"`
		OrderID    string `json:"orderId"`
		ClientOid  string `json:"clientOid"`
		BaseVolume string `json:"baseVolume"`
		Fee        string `json:"fee"`
		Price      string `json:"price"`
		PriceAvg   string `json:"priceAvg"`
		State      string `json:"state"`
		Side       string `json:"side"`
		Force      string `json:"force"`
		PosSide    string `json:"posSide"`
		MarginCoin string `json:"marginCoin"`
		MarginMode string `json:"marginMode"`
		TradeSide  string `json:"tradeSide"`
		Leverage   string `json:"leverage"`
		OrderType  string `json:"orderType"`
		CTime      string `json:"cTime"`
		UTime      string `json:"uTime"`
		ReduceOnly string `json:"reduceOnly"`
	}
	var page1 []row = make([]row, 100)
	var page2 []row = make([]row, 50)
	var i int
	for i = 0; i < 100; i++ {
		page1[i] = row{Symbol: "BTCUSDT", OrderID: "p1-" + strconv.Itoa(i), ClientOid: "c1-" + strconv.Itoa(i),
			Size: "0.01", Price: "50000", State: "live", Side: "buy", Force: "gtc",
			PosSide: "long", MarginCoin: "USDT", MarginMode: "crossed", OrderType: "limit",
			CTime: "1700000000000", UTime: "1700000000000"}
	}
	for i = 0; i < 50; i++ {
		page2[i] = row{Symbol: "BTCUSDT", OrderID: "p2-" + strconv.Itoa(i), ClientOid: "c2-" + strconv.Itoa(i),
			Size: "0.02", Price: "50100", State: "live", Side: "sell", Force: "gtc",
			PosSide: "short", MarginCoin: "USDT", MarginMode: "crossed", OrderType: "limit",
			CTime: "1700000000000", UTime: "1700000000000"}
	}
	var p1 []byte
	var p2 []byte
	var err error
	p1, err = json.Marshal(map[string]any{"endId": "p1-99", "entrustedList": page1})
	if err != nil {
		t.Fatal(err)
	}
	p2, err = json.Marshal(map[string]any{"endId": "", "entrustedList": page2})
	if err != nil {
		t.Fatal(err)
	}

	// Mock with custom dispatcher: respond with page1 the first time,
	// page2 every subsequent call. Track call count to assert exactly
	// two roundtrips.
	var calls int32
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c int32 = atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		var envelope []byte
		if c == 1 {
			if got := r.URL.Query().Get("idLessThan"); got != "" {
				t.Errorf("first call: idLessThan should be empty, got %q", got)
			}
			envelope = []byte(`{"code":"00000","msg":"success","requestTime":0,"data":` + string(p1) + `}`)
		} else {
			if got := r.URL.Query().Get("idLessThan"); got != "p1-99" {
				t.Errorf("second call: idLessThan: want p1-99, got %q", got)
			}
			envelope = []byte(`{"code":"00000","msg":"success","requestTime":0,"data":` + string(p2) + `}`)
		}
		_, _ = io.WriteString(w, string(envelope))
	}))
	t.Cleanup(srv.Close)

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	var client *bitget.Client
	client, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var orders []mixtypes.OrderInfo
	orders, err = mixOf(client).Account().GetOpenOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if len(orders) != 150 {
		t.Errorf("len(orders): want 150 (100+50), got %d", len(orders))
	}
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Errorf("HTTP calls: want 2, got %d", c)
	}
	// First and last orderIds should match the two pages.
	if orders[0].OrderID != "p1-0" {
		t.Errorf("orders[0].OrderID: want p1-0, got %q", orders[0].OrderID)
	}
	if orders[149].OrderID != "p2-49" {
		t.Errorf("orders[149].OrderID: want p2-49, got %q", orders[149].OrderID)
	}
}

func TestContract_GetOpenOrders_StopsOnPartialPage(t *testing.T) {
	t.Parallel()
	// A short page (<100 rows) implies "no more pages" even if endId
	// is non-empty, because asking again only loops back to the same
	// rows. The SDK must stop after the first call.
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"endId":"some-cursor",
			"entrustedList":[
				{"symbol":"BTCUSDT","orderId":"o1","clientOid":"c1","size":"0.01","price":"50000",
				 "state":"live","side":"buy","force":"gtc","posSide":"long","marginCoin":"USDT",
				 "marginMode":"crossed","orderType":"limit","cTime":"1700000000000","uTime":"1700000000000"}
			]
		}
	}`

	var calls int32
	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/orders-pending": fixture,
	}, func(t *testing.T, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	})

	var orders []mixtypes.OrderInfo
	var err error
	orders, err = mixOf(client).Account().GetOpenOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("len: want 1, got %d", len(orders))
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Errorf("HTTP calls: want 1 (short page is terminal), got %d", c)
	}
}

func TestContract_GetOpenOrders_EmptyResult(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,
		"data":{"endId":"","entrustedList":[]}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/orders-pending": fixture,
	}, nil)

	var orders []mixtypes.OrderInfo
	var err error
	orders, err = mixOf(client).Account().GetOpenOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("len: want 0, got %d", len(orders))
	}
}

// ---------------------------------------------------------------------
// GetOrderDetail.
// ---------------------------------------------------------------------

func TestContract_GetOrderDetail_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{
		"code":"00000","msg":"success","requestTime":0,
		"data":{
			"symbol":"BTCUSDT","orderId":"123","clientOid":"core-uuid-7",
			"size":"0.01","price":"50000","priceAvg":"50000.5",
			"baseVolume":"0.005","fee":"0.001",
			"state":"partially_filled","side":"buy","force":"gtc","posSide":"long",
			"marginCoin":"USDT","marginMode":"crossed","tradeSide":"open",
			"leverage":"10","orderType":"limit","cTime":"1700000000000","uTime":"1700000000010","reduceOnly":"NO"
		}
	}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/detail": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("orderId"); got != "123" {
			t.Errorf("orderId: want 123, got %q", got)
		}
		if got := q.Get("clientOid"); got != "" {
			t.Errorf("clientOid: should be omitted when orderId is set, got %q", got)
		}
	})

	var info mixtypes.OrderInfo
	var err error
	info, err = mixOf(client).Account().GetOrderDetail(context.Background(), "BTCUSDT", "123", "")
	if err != nil {
		t.Fatalf("GetOrderDetail: %v", err)
	}
	if info.OrderID != "123" {
		t.Errorf("OrderID: want 123, got %q", info.OrderID)
	}
	if info.ClientOrderID != "core-uuid-7" {
		t.Errorf("ClientOrderID: want core-uuid-7, got %q", info.ClientOrderID)
	}
	if info.Status != roottypes.OrderStatusPartiallyFilled {
		t.Errorf("Status: want partially_filled, got %q", info.Status)
	}
	if !info.FilledQuantity.Equal(decimal.RequireFromString("0.005")) {
		t.Errorf("FilledQuantity: %s", info.FilledQuantity)
	}
	if !info.AvgFilledPrice.Equal(decimal.RequireFromString("50000.5")) {
		t.Errorf("AvgFilledPrice: %s", info.AvgFilledPrice)
	}
}

func TestContract_GetOrderDetail_DispatchesByClientOid(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{
		"symbol":"BTCUSDT","orderId":"42","clientOid":"core-uuid-8",
		"size":"0.01","price":"50000","priceAvg":"0","baseVolume":"0","fee":"0",
		"state":"live","side":"buy","force":"gtc","posSide":"long","marginCoin":"USDT",
		"marginMode":"crossed","orderType":"limit","cTime":"0","uTime":"0"}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/detail": fixture,
	}, func(t *testing.T, r *http.Request) {
		var q url.Values = r.URL.Query()
		if got := q.Get("orderId"); got != "" {
			t.Errorf("orderId: should be omitted when only clientOid is set, got %q", got)
		}
		if got := q.Get("clientOid"); got != "core-uuid-8" {
			t.Errorf("clientOid: want core-uuid-8, got %q", got)
		}
	})

	var err error
	_, err = mixOf(client).Account().GetOrderDetail(context.Background(), "BTCUSDT", "", "core-uuid-8")
	if err != nil {
		t.Fatalf("GetOrderDetail: %v", err)
	}
}

func TestContract_GetOrderDetail_NotFoundEnvelope(t *testing.T) {
	t.Parallel()
	// Bitget occasionally returns a "found nothing" envelope with a
	// success status code but blank ids inside data. The SDK must
	// surface a typed InvalidRequest rather than silently returning
	// a zero OrderInfo.
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{
		"symbol":"","orderId":"","clientOid":"",
		"size":"","price":"","priceAvg":"","baseVolume":"","fee":"",
		"state":"","side":"","force":"","posSide":"","marginCoin":"",
		"marginMode":"","orderType":"","cTime":"","uTime":""}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/detail": fixture,
	}, nil)

	var err error
	_, err = mixOf(client).Account().GetOrderDetail(context.Background(), "BTCUSDT", "999", "")
	if err == nil {
		t.Fatal("GetOrderDetail: expected error, got nil")
	}
	if !bitget.IsInvalidRequest(err) {
		t.Errorf("GetOrderDetail: want ErrorKindInvalidRequest, got %v", err)
	}
}

// ---------------------------------------------------------------------
// ClosePosition.
// ---------------------------------------------------------------------

func TestContract_ClosePosition_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{
		"successList":[{"orderId":"close-1","clientOid":""}],
		"failureList":[]}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/close-positions": fixture,
	}, func(t *testing.T, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if body["symbol"] != "BTCUSDT" {
			t.Errorf("symbol: want BTCUSDT, got %v", body["symbol"])
		}
		if body["productType"] != "USDT-FUTURES" {
			t.Errorf("productType: want USDT-FUTURES, got %v", body["productType"])
		}
		if _, ok := body["holdSide"]; ok {
			t.Errorf("holdSide should be omitted in one-way mode, got %v", body["holdSide"])
		}
	})

	var err error
	err = mixOf(client).Account().ClosePosition(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}
}

func TestContract_ClosePosition_PerRowFailureSurfaces(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{
		"successList":[],
		"failureList":[{"orderId":"","clientOid":"","errorMsg":"no position","errorCode":"40768"}]}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/order/close-positions": fixture,
	}, nil)

	var err error
	err = mixOf(client).Account().ClosePosition(context.Background(), "BTCUSDT")
	if err == nil {
		t.Fatal("ClosePosition: expected error, got nil")
	}
	if !bitget.IsExchange(err) {
		t.Errorf("ClosePosition: want ErrorKindExchange, got %v", err)
	}
}

// ---------------------------------------------------------------------
// SetLeverage.
// ---------------------------------------------------------------------

func TestContract_SetLeverage_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{
		"symbol":"BTCUSDT","marginCoin":"USDT","longLeverage":"10","shortLeverage":"10","crossMarginLeverage":"10","marginMode":"crossed"}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/account/set-leverage": fixture,
	}, func(t *testing.T, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if body["symbol"] != "BTCUSDT" {
			t.Errorf("symbol: want BTCUSDT, got %v", body["symbol"])
		}
		if body["productType"] != "USDT-FUTURES" {
			t.Errorf("productType: want USDT-FUTURES, got %v", body["productType"])
		}
		if body["marginCoin"] != "USDT" {
			t.Errorf("marginCoin: want USDT, got %v", body["marginCoin"])
		}
		if body["leverage"] != "10" {
			t.Errorf("leverage: want \"10\", got %v", body["leverage"])
		}
		if _, ok := body["holdSide"]; ok {
			t.Errorf("holdSide should be omitted in one-way mode, got %v", body["holdSide"])
		}
	})

	var err error
	err = mixOf(client).Account().SetLeverage(context.Background(), "BTCUSDT", 10)
	if err != nil {
		t.Fatalf("SetLeverage: %v", err)
	}
}

// ---------------------------------------------------------------------
// SetPositionMode.
// ---------------------------------------------------------------------

func TestContract_SetPositionMode_Happy(t *testing.T) {
	t.Parallel()
	const fixture = `{"code":"00000","msg":"success","requestTime":0,"data":{"posMode":"one_way_mode"}}`

	var client *bitget.Client
	_, client = mockBitget(t, map[string]string{
		"/api/v2/mix/account/set-position-mode": fixture,
	}, func(t *testing.T, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if body["productType"] != "USDT-FUTURES" {
			t.Errorf("productType: want USDT-FUTURES, got %v", body["productType"])
		}
		if body["posMode"] != "one_way_mode" {
			t.Errorf("posMode: want one_way_mode, got %v", body["posMode"])
		}
	})

	var err error
	err = mixOf(client).Account().SetPositionMode(context.Background(), roottypes.PositionModeOneWay)
	if err != nil {
		t.Fatalf("SetPositionMode: %v", err)
	}
}
