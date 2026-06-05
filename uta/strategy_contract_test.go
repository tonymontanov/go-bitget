/*
FILE: uta/strategy_contract_test.go

DESCRIPTION:
Contract tests for the V3 UTA Strategy (plan-order) sub-client: place /
modify / cancel acks + guards, POST body shaping and the open / history
query parsing.
*/

package uta

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

func TestContract_Strategy_PlaceModifyCancel(t *testing.T) {
	t.Parallel()
	var placeBody []byte
	var _, client = mockBitgetDynamic(t, false, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.URL.Path == "/api/v3/trade/place-strategy-order" {
			placeBody = body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"orderId":"s1","clientOid":"sc1"}}`))
	})
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var ack, err = uc.Strategy().PlaceOrder(ctx, PlaceStrategyOrderRequest{
		Category: utatypes.CategoryUSDTFutures, Symbol: "BTCUSDT", Type: "tpsl", TpslMode: "full",
		PosSide: "long", TakeProfit: "65000", StopLoss: "55000", TpTriggerBy: "mark",
	})
	if err != nil || ack.OrderID != "s1" {
		t.Fatalf("PlaceOrder: %v %+v", err, ack)
	}
	var pb map[string]any
	if jerr := json.Unmarshal(placeBody, &pb); jerr != nil {
		t.Fatalf("place body: %v", jerr)
	}
	if pb["type"] != "tpsl" || pb["takeProfit"] != "65000" || pb["tpTriggerBy"] != "mark" {
		t.Fatalf("unexpected strategy place body: %v", pb)
	}

	if _, err = uc.Strategy().ModifyOrder(ctx, ModifyStrategyOrderRequest{OrderID: "s1", Qty: "0.02", TakeProfit: "66000"}); err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	if _, err = uc.Strategy().CancelOrder(ctx, CancelStrategyOrderRequest{ClientOID: "sc1"}); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	// Guards.
	if _, err = uc.Strategy().PlaceOrder(ctx, PlaceStrategyOrderRequest{Category: utatypes.CategoryUSDTFutures}); err == nil {
		t.Error("PlaceOrder(no symbol): want guard")
	}
	if _, err = uc.Strategy().ModifyOrder(ctx, ModifyStrategyOrderRequest{OrderID: "s1"}); err == nil {
		t.Error("ModifyOrder(no qty): want guard")
	}
	if _, err = uc.Strategy().CancelOrder(ctx, CancelStrategyOrderRequest{}); err == nil {
		t.Error("CancelOrder(no id): want guard")
	}
}

func TestContract_Strategy_Queries(t *testing.T) {
	t.Parallel()
	var orderJSON = `{"orderId":"s1","clientOid":"sc1","category":"USDT-FUTURES","symbol":"BTCUSDT","qty":"0.01","posSide":"long","status":"pending","triggerType":"takeProfit","tpTriggerBy":"mark","takeProfit":"65000","stopLoss":"55000","tpOrderType":"market","triggerBy":"mark","triggerPrice":"64000","triggerOrderType":"limit","triggerOrderPrice":"64010","createdTime":"1700000000000","updatedTime":"1700000001000"}`
	var routes = map[string]string{
		"/api/v3/trade/unfilled-strategy-orders": `{"code":"00000","msg":"success","data":{"list":[` + orderJSON + `]}}`,
		"/api/v3/trade/history-strategy-orders":  `{"code":"00000","msg":"success","data":{"list":[` + orderJSON + `],"cursor":"sh9"}}`,
	}
	var _, client = mockBitget(t, routes, nil)
	var uc = utaClient(t, client)
	var ctx = context.Background()

	var open, err = uc.Strategy().GetUnfilledOrders(ctx, utatypes.CategoryUSDTFutures, "tpsl")
	if err != nil || len(open) != 1 || !open[0].TakeProfit.Equal(decv("65000")) || open[0].TriggerType != "takeProfit" {
		t.Fatalf("GetUnfilledOrders: %v %+v", err, open)
	}

	var hist, cur, herr = uc.Strategy().GetHistoryOrders(ctx, StrategyHistoryQuery{Category: utatypes.CategoryUSDTFutures, Limit: 50})
	if herr != nil || len(hist) != 1 || cur != "sh9" || !hist[0].TriggerOrderPrice.Equal(decv("64010")) {
		t.Fatalf("GetHistoryOrders: %v %+v cur=%q", herr, hist, cur)
	}

	// Guards.
	if _, err = uc.Strategy().GetUnfilledOrders(ctx, "", ""); err == nil {
		t.Error("GetUnfilledOrders(no category): want guard")
	}
	if _, _, err := uc.Strategy().GetHistoryOrders(ctx, StrategyHistoryQuery{}); err == nil {
		t.Error("GetHistoryOrders(no category): want guard")
	}
}
