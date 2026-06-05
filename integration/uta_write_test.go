//go:build integration

/*
FILE: integration/uta_write_test.go

DESCRIPTION:
Live PAPER-TRADING write checks for the V3 UTA order flow. Gated behind
BITGET_ITEST_WRITE=1 AND demo mode. They place limit orders FAR below the
market (≈50% of last) so they cannot fill, assert the acks, then cancel
them immediately. Their real purpose is to confirm two Phase-7 open items
on live data:

  - the single place / cancel ack shape, and
  - the BATCH place + cancel-symbol response shape (bare array vs
    {list|successList|failureList}) — decoded leniently by the SDK.

A best-effort cancel-symbol cleanup runs at the end regardless.
*/

package integration

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/uta"
)

// farLimit derives a safe (won't-fill) limit price and a minimal qty for
// the configured futures symbol.
func farLimit(t *testing.T, u *uta.Client) (price, qty string, posSide string) {
	t.Helper()
	var ctx = testCtx(t)

	var insts, err = u.Public().GetInstruments(ctx, futures, symbol())
	if err != nil || len(insts) == 0 {
		t.Fatalf("GetInstruments(%s): %v", symbol(), err)
	}
	var inst = insts[0]

	var tks, terr = u.Public().GetTickers(ctx, futures, symbol())
	if terr != nil || len(tks) == 0 || tks[0].LastPrice.IsZero() {
		t.Fatalf("GetTickers(%s): %v", symbol(), terr)
	}
	var last = tks[0].LastPrice

	var far = last.Mul(decimal.NewFromFloat(0.5)).Truncate(inst.PricePrecision)
	var minQty = inst.MinOrderQty
	if minQty.IsZero() {
		minQty = decimal.NewFromFloat(0.001)
	}

	// Hedge mode requires a posSide on opening orders.
	var st, serr = u.Account().GetSettings(ctx)
	if serr == nil && st.HoldMode == "hedge_mode" {
		posSide = "long"
	}
	return far.String(), minQty.String(), posSide
}

func TestLive_UTA_PaperPlaceCancel(t *testing.T) {
	requireWrite(t)
	var u = utaClient(t)
	var ctx = testCtx(t)
	var price, qty, posSide = farLimit(t, u)
	t.Logf("paper order: %s qty=%s price=%s posSide=%q", symbol(), qty, price, posSide)

	// Best-effort cleanup.
	t.Cleanup(func() {
		var res, err = u.Trade().CancelSymbolOrders(context.Background(), futures, symbol())
		if err != nil {
			t.Logf("[cleanup cancel-symbol note] %v", err)
		} else {
			t.Logf("cleanup cancelled %d orders", len(res))
		}
	})

	var ack, err = u.Trade().PlaceOrder(ctx, uta.PlaceOrderRequest{
		Category: futures, Symbol: symbol(), Qty: qty, Price: price,
		Side: "buy", OrderType: "limit", TimeInForce: "gtc", PosSide: posSide,
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if ack.OrderID == "" {
		t.Fatalf("PlaceOrder returned empty orderId: %+v", ack)
	}
	t.Logf("placed orderId=%s clientOid=%s", ack.OrderID, ack.ClientOID)

	var info, ierr = u.Trade().GetOrderInfo(ctx, ack.OrderID, "")
	if ierr != nil {
		t.Logf("[GetOrderInfo note] %v", ierr)
	} else {
		t.Logf("order status=%s price=%s qty=%s posSide=%s holdMode=%s",
			info.OrderStatus, info.Price, info.Qty, info.PosSide, info.HoldMode)
	}

	var cack, cerr = u.Trade().CancelOrder(ctx, uta.CancelOrderRequest{OrderID: ack.OrderID})
	if cerr != nil {
		t.Fatalf("CancelOrder: %v", cerr)
	}
	t.Logf("cancelled orderId=%s", cack.OrderID)
}

func TestLive_UTA_PaperBatchShape(t *testing.T) {
	requireWrite(t)
	var u = utaClient(t)
	var ctx = testCtx(t)
	var price, qty, posSide = farLimit(t, u)

	t.Cleanup(func() {
		var res, err = u.Trade().CancelSymbolOrders(context.Background(), futures, symbol())
		if err != nil {
			t.Logf("[cleanup cancel-symbol note] %v", err)
		} else {
			t.Logf("cleanup cancelled %d orders", len(res))
		}
	})

	var orders = []uta.PlaceOrderRequest{
		{Category: futures, Symbol: symbol(), Qty: qty, Price: price, Side: "buy", OrderType: "limit", TimeInForce: "gtc", PosSide: posSide},
		{Category: futures, Symbol: symbol(), Qty: qty, Price: price, Side: "buy", OrderType: "limit", TimeInForce: "gtc", PosSide: posSide},
	}
	var res, err = u.Trade().PlaceBatchOrders(ctx, orders)
	if err != nil {
		t.Fatalf("PlaceBatchOrders: %v", err)
	}
	t.Logf("BATCH PLACE confirmed: %d results", len(res))
	var i int
	for i = 0; i < len(res); i++ {
		t.Logf("  [%d] orderId=%q clientOid=%q code=%q msg=%q",
			i, res[i].OrderID, res[i].ClientOID, res[i].Code, res[i].Msg)
	}
	if len(res) == 0 {
		t.Error("batch place returned no results — investigate response shape")
	}

	var cancelled, cerr = u.Trade().CancelSymbolOrders(ctx, futures, symbol())
	if cerr != nil {
		t.Fatalf("CancelSymbolOrders: %v", cerr)
	}
	t.Logf("CANCEL-SYMBOL confirmed: %d results", len(cancelled))
}
