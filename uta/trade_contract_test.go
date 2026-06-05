/*
FILE: uta/trade_contract_test.go

DESCRIPTION:
Contract tests for the V3 UTA Trade sub-client: single place / modify /
cancel acks + guards, batch ARRAY body shaping and lenient response decode
(bare array vs {list}), cancel-symbol / close-positions, countdown, and the
order-info / unfilled / history / fills parsing with feeDetail.
*/

package uta

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func TestContract_Trade_PlaceModifyCancel(t *testing.T) {
	t.Parallel()
	var placeBody []byte
	var sawSign bool
	var _, client = mockBitgetDynamic(t, false, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Header.Get("ACCESS-SIGN") != "" {
			sawSign = true
		}
		if r.URL.Path == "/api/v3/trade/place-order" {
			placeBody = body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"orderId":"o1","clientOid":"c1"}}`))
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var ack, err = uc.Trade().PlaceOrder(ctx, PlaceOrderRequest{
		Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", Qty: "0.01", Side: "buy", OrderType: "limit",
		Price: "60000", TimeInForce: "gtc", PosSide: "long",
	})
	if err != nil || ack.OrderID != "o1" || ack.ClientOID != "c1" {
		t.Fatalf("PlaceOrder: %v %+v", err, ack)
	}
	if !sawSign {
		t.Error("trade calls must be signed")
	}
	var pb map[string]any
	if jerr := json.Unmarshal(placeBody, &pb); jerr != nil {
		t.Fatalf("place body: %v", jerr)
	}
	if pb["category"] != "USDT-FUTURES" || pb["side"] != "buy" || pb["posSide"] != "long" || pb["price"] != "60000" {
		t.Fatalf("unexpected place body: %v", pb)
	}

	if _, err = uc.Trade().ModifyOrder(ctx, ModifyOrderRequest{OrderID: "o1", Price: "60500"}); err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	if _, err = uc.Trade().CancelOrder(ctx, CancelOrderRequest{ClientOID: "c1"}); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	// Guards.
	if _, err = uc.Trade().PlaceOrder(ctx, PlaceOrderRequest{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT"}); err == nil {
		t.Error("PlaceOrder(no qty/side): want guard")
	}
	if _, err = uc.Trade().ModifyOrder(ctx, ModifyOrderRequest{}); err == nil {
		t.Error("ModifyOrder(no id): want guard")
	}
	if _, err = uc.Trade().CancelOrder(ctx, CancelOrderRequest{}); err == nil {
		t.Error("CancelOrder(no id): want guard")
	}
}

func TestContract_Trade_Batch(t *testing.T) {
	t.Parallel()
	var placeBatchBody []byte
	var _, client = mockBitgetDynamic(t, false, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/trade/place-batch":
			placeBatchBody = body
			// bare-array response
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":[{"orderId":"o1","clientOid":"c1"},{"orderId":"","clientOid":"c2","code":"40762","msg":"insufficient"}]}`))
		case "/api/v3/trade/cancel-batch":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"list":[{"orderId":"o1","clientOid":"c1"}]}}`))
		default:
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":null}`))
		}
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var res, err = uc.Trade().PlaceBatchOrders(ctx, []PlaceOrderRequest{
		{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", Qty: "0.01", Side: "buy", OrderType: "limit", Price: "60000"},
		{Category: utatypes.CategoryUSDTFutures, Symbol: "ETHUSDT", Qty: "0.1", Side: "buy", OrderType: "market"},
	})
	if err != nil || len(res) != 2 || res[0].OrderID != "o1" || res[1].Code != "40762" {
		t.Fatalf("PlaceBatchOrders: %v %+v", err, res)
	}
	// Body must be a top-level JSON array.
	var arr []map[string]any
	if jerr := json.Unmarshal(placeBatchBody, &arr); jerr != nil {
		t.Fatalf("place-batch body must be a JSON array: %v (%s)", jerr, placeBatchBody)
	}
	if len(arr) != 2 || arr[1]["orderType"] != "market" {
		t.Fatalf("unexpected batch body: %v", arr)
	}

	var cres, cerr = uc.Trade().CancelBatchOrders(ctx, []CancelBatchOrder{
		{Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", OrderID: "o1"},
	})
	if cerr != nil || len(cres) != 1 || cres[0].OrderID != "o1" {
		t.Fatalf("CancelBatchOrders: %v %+v", cerr, cres)
	}

	// Guards.
	if _, err = uc.Trade().PlaceBatchOrders(ctx, nil); err == nil {
		t.Error("PlaceBatchOrders(empty): want guard")
	}
	if _, err = uc.Trade().CancelBatchOrders(ctx, []CancelBatchOrder{{Symbol: "BTCUSDT", OrderID: "o1"}}); err == nil {
		t.Error("CancelBatchOrders(no category): want guard")
	}
}

func TestContract_Trade_CancelSymbolCloseCountdown(t *testing.T) {
	t.Parallel()
	var countdownBody []byte
	var _, client = mockBitgetDynamic(t, false, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/trade/cancel-symbol-order":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"list":[{"orderId":"o1","clientOid":"c1","code":"00000","msg":""}]}}`))
		case "/api/v3/trade/close-positions":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"list":[{"orderId":"o9","clientOid":"","code":"00000","msg":""}]}}`))
		case "/api/v3/trade/countdown-cancel-all":
			countdownBody = body
			_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":null}`))
		}
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var cs, err = uc.Trade().CancelSymbolOrders(ctx, utatypes.CategoryUSDTFutures, "BTCUSDT")
	if err != nil || len(cs) != 1 || cs[0].OrderID != "o1" {
		t.Fatalf("CancelSymbolOrders: %v %+v", err, cs)
	}
	var cp, perr = uc.Trade().CloseAllPositions(ctx, ClosePositionsRequest{Category: utatypes.CategoryUSDTFutures, PosSide: "long"})
	if perr != nil || len(cp) != 1 || cp[0].OrderID != "o9" {
		t.Fatalf("CloseAllPositions: %v %+v", perr, cp)
	}
	if err = uc.Trade().CountdownCancelAll(ctx, 30); err != nil {
		t.Fatalf("CountdownCancelAll: %v", err)
	}
	var cb map[string]any
	if jerr := json.Unmarshal(countdownBody, &cb); jerr != nil || cb["countdown"] != "30" {
		t.Fatalf("unexpected countdown body: %v (%v)", cb, jerr)
	}

	// Guards.
	if _, err = uc.Trade().CancelSymbolOrders(ctx, "", ""); err == nil {
		t.Error("CancelSymbolOrders(no category): want guard")
	}
	if _, err = uc.Trade().CloseAllPositions(ctx, ClosePositionsRequest{}); err == nil {
		t.Error("CloseAllPositions(no category): want guard")
	}
}

func TestContract_Trade_OrderQueries(t *testing.T) {
	t.Parallel()
	var orderJSON = `{"orderId":"o1","clientOid":"c1","category":"USDT-FUTURES","symbol":"BTCUSDT","orderType":"limit","side":"buy","price":"60000","qty":"0.01","amount":"600","cumExecQty":"0.005","cumExecValue":"300","avgPrice":"60000","timeInForce":"gtc","orderStatus":"partially_filled","posSide":"long","holdMode":"hedge_mode","reduceOnly":"no","feeDetail":[{"feeCoin":"USDT","fee":"-0.18"}],"cancelReason":"","execType":"T","createdTime":"1700000000000","updatedTime":"1700000001000"}`
	var routes = map[string]string{
		"/api/v3/trade/order-info":      `{"code":"00000","msg":"success","data":` + orderJSON + `}`,
		"/api/v3/trade/unfilled-orders": `{"code":"00000","msg":"success","data":{"list":[` + orderJSON + `],"cursor":"u123"}}`,
		"/api/v3/trade/history-orders":  `{"code":"00000","msg":"success","data":{"list":[` + orderJSON + `],"cursor":""}}`,
		"/api/v3/trade/fills":           `{"code":"00000","msg":"success","data":{"list":[{"execId":"e1","orderId":"o1","category":"USDT-FUTURES","symbol":"BTCUSDT","orderType":"limit","side":"buy","execPrice":"60000","execQty":"0.005","execValue":"300","tradeScope":"taker","feeDetail":[{"feeCoin":"USDT","fee":"-0.18"}],"execPnl":"0","tradeSide":"open","createdTime":"1700000000500","updatedTime":"1700000000500"}],"cursor":"f99"}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var o, err = uc.Trade().GetOrderInfo(ctx, "o1", "")
	if err != nil || o.OrderID != "o1" || !o.CumExecQty.Equal(decv("0.005")) || o.HoldMode != "hedge_mode" {
		t.Fatalf("GetOrderInfo: %v %+v", err, o)
	}
	if len(o.FeeDetail) != 1 || !o.FeeDetail[0].Fee.Equal(decv("-0.18")) || o.CreatedTime != 1700000000000 {
		t.Fatalf("GetOrderInfo feeDetail: %+v", o.FeeDetail)
	}

	var uos, ucur, uerr = uc.Trade().GetUnfilledOrders(ctx, OrdersQuery{Symbol: "BTCUSDT"})
	if uerr != nil || len(uos) != 1 || ucur != "u123" {
		t.Fatalf("GetUnfilledOrders: %v %+v cur=%q", uerr, uos, ucur)
	}

	var hos, _, herr = uc.Trade().GetHistoryOrders(ctx, OrdersQuery{Category: utatypes.CategoryUSDTFutures})
	if herr != nil || len(hos) != 1 {
		t.Fatalf("GetHistoryOrders: %v %+v", herr, hos)
	}

	var fills, fcur, ferr = uc.Trade().GetFills(ctx, FillsQuery{OrderID: "o1"})
	if ferr != nil || len(fills) != 1 || fcur != "f99" || !fills[0].ExecQty.Equal(decv("0.005")) || fills[0].TradeSide != "open" {
		t.Fatalf("GetFills: %v %+v cur=%q", ferr, fills, fcur)
	}

	// Guards.
	if _, err = uc.Trade().GetOrderInfo(ctx, "", ""); err == nil {
		t.Error("GetOrderInfo(no id): want guard")
	}
	if _, _, err := uc.Trade().GetHistoryOrders(ctx, OrdersQuery{}); err == nil {
		t.Error("GetHistoryOrders(no category): want guard")
	}
}
