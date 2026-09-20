/*
FILE: uta/stream_private_contract_test.go

DESCRIPTION:
Contract tests for the PRIVATE topics of uta.StreamClient (order / fill /
position / account) against the mock V3 WS server of
stream_contract_test.go.

The private channels could not be captured live (no UTA key), so the
fixtures are the push samples of the official V3 docs, byte-for-byte.
Beyond the field mapping the tests pin the tolerance the decoders promise:
feeDetail as an array AND as a string, bare numbers where the docs promise
quoted strings, REST spellings as fallbacks.

Wire contract pinned here: login BEFORE the first subscribe,
{"instType":"UTA","topic":"<topic>"} with no symbol, one wire subscription
per topic no matter how many consumers, ErrorKindAuth without credentials
before anything is dialled, OnPrivateReconnect only from the second
connection on.
*/

package uta

import (
	"context"
	"errors"
	"testing"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// ---------------------------------------------------------------------
// Docs fixtures (verbatim push samples).
// ---------------------------------------------------------------------

const fixtureOrderPush = `{"action":"snapshot","arg":{"instType":"UTA","topic":"order"},"data":[{"category":"usdt-futures","symbol":"BTCUSDT","orderId":"xxx","clientOid":"xxx","price":"","qty":"0.001","amount":"1000","holdMode":"hedge_mode","holdSide":"long","delegateType":"normal","tradeSide":"open","orderType":"market","timeInForce":"gtc","side":"buy","marginMode":"crossed","marginCoin":"USDT","reduceOnly":"no","cumExecQty":"0.001","cumExecValue":"83.1315","avgPrice":"83131.5","totalProfit":"0","orderStatus":"filled","cancelReason":"","leverage":"20","feeDetail":[{"feeCoin":"USDT","fee":"0.0332526"}],"createdTime":"1742367838101","updatedTime":"1742367838115","stpMode":"none","requestId":12345678}],"ts":1742367838124}`

const fixtureFillPush = `{"data":[{"symbol":"BTCUSDT","orderType":"market","updatedTime":"1736378720623","side":"buy","orderId":"1288888888888888888","execPnl":"0","feeDetail":[{"feeCoin":"USDT","fee":"0.569958"}],"execTime":"1736378720623","tradeScope":"taker","tradeSide":"open","execId":"1288888888888888888","execLinkId":"1288888888888888888","execPrice":"94993","holdSide":"long","execValue":"949.93","category":"usdt-futures","execQty":"0.01","clientOid":"1288888888888888889","isRPI":"no"}],"arg":{"instType":"UTA","topic":"fill"},"action":"snapshot","ts":1733904123981}`

const fixturePositionPush = `{"data":[{"symbol":"BTCUSDT","leverage":"20","openFeeTotal":"","mmr":"","breakEvenPrice":"","available":"0","liqPrice":"","marginMode":"crossed","unrealisedPnl":"0","markPrice":"94987.1","createdTime":"1736378720620","avgPrice":"0","totalFundingFee":"0","cashDividend":"0","updatedTime":"1736378720620","marginCoin":"USDT","frozen":"0","profitRate":"","closeFeeTotal":"","marginSize":"0","curRealisedPnl":"0","size":"0","positionStatus":"ended","posSide":"long","holdMode":"hedge_mode"}],"arg":{"instType":"UTA","topic":"position"},"action":"snapshot","ts":1730711666652}`

const fixtureAccountPush = `{"data":[{"unrealisedPnL":"-10116.55","totalEquity":"4976919.05","positionMgnRatio":"0","mmr":"408.08","effEquity":"4847952.35","imr":"17795.97","mgnRatio":"0","coin":[{"debts":"0","balance":"0.9992","available":"0.9992","borrow":"0","locked":"0","equity":"0.9992","coin":"ETH","usdValue":"2488.667472","bonus":"0"},{"debts":"0","balance":"52.00819","available":"52.00819","borrow":"0","locked":"0","equity":"52.00819","coin":"BTC","usdValue":"4630564.31304974","bonus":"0"},{"debts":"0","balance":"354411.45536458","available":"344282.65536458","borrow":"0","locked":"0","equity":"344282.65536458","coin":"USDT","usdValue":"343866.07335159","bonus":"10"}]}],"arg":{"instType":"UTA","topic":"account"},"action":"snapshot","ts":1740546523244}`

const (
	argOrder    string = `{"instType":"UTA","topic":"order"}`
	argFill     string = `{"instType":"UTA","topic":"fill"}`
	argPosition string = `{"instType":"UTA","topic":"position"}`
	argAccount  string = `{"instType":"UTA","topic":"account"}`
)

// ---------------------------------------------------------------------
// Wire contract: login first, UTA args, no symbol.
// ---------------------------------------------------------------------

func TestContract_StreamPrivate_LoginBeforeSubscribeAndArgs(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)
	var s *StreamClient = c.Stream()
	var ctx context.Context = context.Background()

	if err := s.WatchOrders(ctx, func(utatypes.Order) {}, nil); err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	if err := s.WatchFills(ctx, func(utatypes.Fill) {}, nil); err != nil {
		t.Fatalf("WatchFills: %v", err)
	}
	if err := s.WatchPositions(ctx, func(utatypes.CurrentPosition) {}, nil); err != nil {
		t.Fatalf("WatchPositions: %v", err)
	}
	if err := s.WatchAccount(ctx, func(utatypes.AccountAssets) {}, nil); err != nil {
		t.Fatalf("WatchAccount: %v", err)
	}
	mock.waitOps(t, "subscribe", argOrder, 1)
	mock.waitOps(t, "subscribe", argFill, 1)
	mock.waitOps(t, "subscribe", argPosition, 1)
	mock.waitOps(t, "subscribe", argAccount, 1)

	var ops []wsOp = mock.opsSnapshot()
	if len(ops) != 5 {
		t.Fatalf("ops = %+v, want login + 4 subscribes", ops)
	}
	if ops[0].op != "login" {
		t.Fatalf("first op = %q, want login BEFORE any subscribe: %+v", ops[0].op, ops)
	}
	if mock.connCount() != 1 {
		t.Fatalf("private topics must share ONE connection, got %d", mock.connCount())
	}
}

func TestContract_StreamPrivate_RequiresCredentials(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)
	var s *StreamClient = c.Stream()
	var ctx context.Context = context.Background()

	type tc struct {
		name string
		run  func() error
	}
	var cases []tc = []tc{
		{"orders", func() error { return s.WatchOrders(ctx, func(utatypes.Order) {}, nil) }},
		{"fills", func() error { return s.WatchFills(ctx, func(utatypes.Fill) {}, nil) }},
		{"positions", func() error { return s.WatchPositions(ctx, func(utatypes.CurrentPosition) {}, nil) }},
		{"account", func() error { return s.WatchAccount(ctx, func(utatypes.AccountAssets) {}, nil) }},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var err error = cases[i].run()
		var be *bitget.Error
		if !errors.As(err, &be) || be.Kind != bitget.ErrorKindAuth {
			t.Fatalf("%s: want ErrorKindAuth, got %v", cases[i].name, err)
		}
	}
	time.Sleep(30 * time.Millisecond)
	if mock.connCount() != 0 {
		t.Fatalf("missing credentials must fail BEFORE dialling: %d connections", mock.connCount())
	}
}

// ---------------------------------------------------------------------
// WatchOrders.
// ---------------------------------------------------------------------

func TestContract_StreamPrivate_WatchOrders_DocsFixture(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var got collector[utatypes.Order]
	var errs collector[error]
	if err := c.Stream().WatchOrders(context.Background(), got.add, errs.add); err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	mock.waitOps(t, "subscribe", argOrder, 1)
	mock.push(t, fixtureOrderPush)
	waitFor(t, time.Second, "order delivery", func() bool { return got.len() == 1 })

	var o utatypes.Order = got.snapshot()[0]
	if o.OrderID != "xxx" || o.ClientOID != "xxx" || o.Symbol != "BTCUSDT" {
		t.Fatalf("ids/symbol: %+v", o)
	}
	if o.Category != string(utatypes.CategoryUSDTFutures) {
		t.Fatalf("category = %q, want the canonical upper-case form", o.Category)
	}
	if o.OrderStatus != "filled" || o.OrderType != "market" || o.Side != "buy" || o.TimeInForce != "gtc" {
		t.Fatalf("status/type/side/tif: %+v", o)
	}
	if o.PosSide != "long" {
		t.Fatalf("PosSide = %q, want the WS holdSide", o.PosSide)
	}
	if o.HoldMode != "hedge_mode" || o.DelegateType != "normal" || o.ReduceOnly != "no" || o.StpMode != "none" {
		t.Fatalf("modes: %+v", o)
	}
	if o.TradeSide != "open" || o.MarginMode != "crossed" || o.MarginCoin != "USDT" || o.Leverage.String() != "20" {
		t.Fatalf("WS-only fields: trade=%q mode=%q coin=%q lev=%s", o.TradeSide, o.MarginMode, o.MarginCoin, o.Leverage)
	}
	if !o.Price.IsZero() || o.Qty.String() != "0.001" || o.Amount.String() != "1000" {
		t.Fatalf("price/qty/amount = %s/%s/%s", o.Price, o.Qty, o.Amount)
	}
	if o.CumExecQty.String() != "0.001" || o.CumExecValue.String() != "83.1315" || o.AvgPrice.String() != "83131.5" {
		t.Fatalf("exec = %s/%s/%s", o.CumExecQty, o.CumExecValue, o.AvgPrice)
	}
	if !o.TotalProfit.IsZero() {
		t.Fatalf("totalProfit = %s", o.TotalProfit)
	}
	if len(o.FeeDetail) != 1 || o.FeeDetail[0].FeeCoin != "USDT" || o.FeeDetail[0].Fee.String() != "0.0332526" {
		t.Fatalf("feeDetail = %+v", o.FeeDetail)
	}
	if o.CreatedTime != 1742367838101 || o.UpdatedTime != 1742367838115 {
		t.Fatalf("times = %d/%d", o.CreatedTime, o.UpdatedTime)
	}
	if errs.len() != 0 {
		t.Fatalf("unexpected errors: %v", errs.snapshot())
	}
}

// orderStatus values pass through verbatim; one handler call per row.
func TestContract_StreamPrivate_WatchOrders_StatusVerbatimAndRowFanOut(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var got collector[utatypes.Order]
	if err := c.Stream().WatchOrders(context.Background(), got.add, nil); err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	mock.waitOps(t, "subscribe", argOrder, 1)
	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"order"},"data":[`+
		`{"category":"spot","symbol":"ETHUSDT","orderId":"1","orderStatus":"new"},`+
		`{"category":"usdt-futures","symbol":"BTCUSDT","orderId":"2","orderStatus":"partially_filled"},`+
		`{"category":"coin-futures","symbol":"BTCUSD","orderId":"3","orderStatus":"filled"},`+
		`{"category":"margin","symbol":"BTCUSDT","orderId":"4","orderStatus":"cancelled"}],"ts":1}`)
	waitFor(t, time.Second, "four orders", func() bool { return got.len() == 4 })

	var orders []utatypes.Order = got.snapshot()
	var wantStatus []string = []string{"new", "partially_filled", "filled", "cancelled"}
	var wantCategory []string = []string{"SPOT", "USDT-FUTURES", "COIN-FUTURES", "MARGIN"}
	var i int
	for i = 0; i < len(orders); i++ {
		if orders[i].OrderStatus != wantStatus[i] || orders[i].Category != wantCategory[i] {
			t.Fatalf("order[%d] = %q / %q", i, orders[i].OrderStatus, orders[i].Category)
		}
	}
}

// feeDetail arrives as an array of objects, as a JSON string (empty, or
// wrapping the array / one object), as one object, or null. None of them
// may drop the order row (the v2.5.1 incident on the V2 orders channel).
func TestContract_StreamPrivate_WatchOrders_FeeDetailShapes(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var got collector[utatypes.Order]
	var errs collector[error]
	if err := c.Stream().WatchOrders(context.Background(), got.add, errs.add); err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	mock.waitOps(t, "subscribe", argOrder, 1)

	type tc struct {
		name     string
		fee      string
		wantLen  int
		wantCoin string
		wantFee  string
	}
	var cases []tc = []tc{
		{"array", `[{"feeCoin":"USDT","fee":"0.0332526"}]`, 1, "USDT", "0.0332526"},
		{"array numeric fee", `[{"feeCoin":"USDT","fee":-0.18}]`, 1, "USDT", "-0.18"},
		{"empty string", `""`, 0, "", ""},
		{"string wrapping array", `"[{\"feeCoin\":\"BGB\",\"fee\":\"0.5\"}]"`, 1, "BGB", "0.5"},
		{"string wrapping object", `"{\"feeCoin\":\"BTC\",\"fee\":\"0.00001\"}"`, 1, "BTC", "0.00001"},
		{"single object", `{"feeCoin":"ETH","fee":"0.002"}`, 1, "ETH", "0.002"},
		{"null", `null`, 0, "", ""},
		{"empty array", `[]`, 0, "", ""},
		{"garbage string", `"n/a"`, 0, "", ""},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"order"},"data":[{"category":"usdt-futures","symbol":"BTCUSDT","orderId":"o`+cases[i].name+`","orderStatus":"filled","feeDetail":`+cases[i].fee+`}],"ts":1}`)
		var want int = i + 1
		waitFor(t, time.Second, "order with feeDetail "+cases[i].name, func() bool { return got.len() == want })
		var o utatypes.Order = got.snapshot()[i]
		if len(o.FeeDetail) != cases[i].wantLen {
			t.Fatalf("%s: feeDetail = %+v", cases[i].name, o.FeeDetail)
		}
		if cases[i].wantLen == 1 && (o.FeeDetail[0].FeeCoin != cases[i].wantCoin || o.FeeDetail[0].Fee.String() != cases[i].wantFee) {
			t.Fatalf("%s: feeDetail = %+v", cases[i].name, o.FeeDetail)
		}
	}
	// A frame WITHOUT feeDetail decodes as well.
	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"order"},"data":[{"category":"spot","symbol":"BTCUSDT","orderId":"nofee","orderStatus":"new"}],"ts":1}`)
	waitFor(t, time.Second, "order without feeDetail", func() bool { return got.len() == len(cases)+1 })
	if errs.len() != 0 {
		t.Fatalf("a feeDetail shape must never surface an error: %v", errs.snapshot())
	}
}

// Bare numbers (and a bool) where the docs promise quoted strings.
func TestContract_StreamPrivate_NumericInsteadOfString(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var orders collector[utatypes.Order]
	var fills collector[utatypes.Fill]
	var positions collector[utatypes.CurrentPosition]
	var accounts collector[utatypes.AccountAssets]
	var errs collector[error]
	var ctx context.Context = context.Background()
	if err := c.Stream().WatchOrders(ctx, orders.add, errs.add); err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	if err := c.Stream().WatchFills(ctx, fills.add, errs.add); err != nil {
		t.Fatalf("WatchFills: %v", err)
	}
	if err := c.Stream().WatchPositions(ctx, positions.add, errs.add); err != nil {
		t.Fatalf("WatchPositions: %v", err)
	}
	if err := c.Stream().WatchAccount(ctx, accounts.add, errs.add); err != nil {
		t.Fatalf("WatchAccount: %v", err)
	}
	mock.waitOps(t, "subscribe", argAccount, 1)
	mock.waitOps(t, "subscribe", argOrder, 1)
	mock.waitOps(t, "subscribe", argFill, 1)
	mock.waitOps(t, "subscribe", argPosition, 1)

	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"order"},"data":[{"category":"usdt-futures","symbol":"BTCUSDT","orderId":1288888888888888888,"clientOid":"c1","price":60000.5,"qty":0.01,"cumExecQty":0,"avgPrice":null,"leverage":20,"reduceOnly":false,"orderStatus":"new","createdTime":1742367838101,"updatedTime":1742367838115}],"ts":1}`)
	waitFor(t, time.Second, "numeric order", func() bool { return orders.len() == 1 })
	var o utatypes.Order = orders.snapshot()[0]
	if o.OrderID != "1288888888888888888" || o.Price.String() != "60000.5" || o.Qty.String() != "0.01" {
		t.Fatalf("order ids/price/qty: %+v", o)
	}
	if o.Leverage.String() != "20" || !o.AvgPrice.IsZero() || o.ReduceOnly != "false" {
		t.Fatalf("order lev/avg/reduceOnly: %s/%s/%q", o.Leverage, o.AvgPrice, o.ReduceOnly)
	}
	if o.CreatedTime != 1742367838101 || o.UpdatedTime != 1742367838115 {
		t.Fatalf("order times = %d/%d", o.CreatedTime, o.UpdatedTime)
	}

	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"fill"},"data":[{"category":"spot","symbol":"BTCUSDT","execId":77,"orderId":78,"execPrice":94993,"execQty":0.01,"execValue":949.93,"execTime":1736378720623,"isRPI":true}],"ts":1}`)
	waitFor(t, time.Second, "numeric fill", func() bool { return fills.len() == 1 })
	var f utatypes.Fill = fills.snapshot()[0]
	if f.ExecID != "77" || f.OrderID != "78" || f.ExecPrice.String() != "94993" || f.ExecQty.String() != "0.01" {
		t.Fatalf("fill: %+v", f)
	}
	if f.ExecTime != 1736378720623 || !f.IsRPI {
		t.Fatalf("fill execTime/rpi = %d/%v", f.ExecTime, f.IsRPI)
	}

	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"position"},"data":[{"symbol":"BTCUSDT","posSide":"short","size":0.5,"available":0.5,"frozen":0,"avgPrice":81000.1,"leverage":10,"markPrice":80990,"unrealisedPnl":5.05,"liqPrice":120000,"updatedTime":1736378720620}],"ts":1}`)
	waitFor(t, time.Second, "numeric position", func() bool { return positions.len() == 1 })
	var p utatypes.CurrentPosition = positions.snapshot()[0]
	if p.Total.String() != "0.5" || p.AvgPrice.String() != "81000.1" || p.LiquidationPrice.String() != "120000" || p.PosSide != "short" {
		t.Fatalf("position: %+v", p)
	}

	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"account"},"data":[{"totalEquity":1000.5,"unrealisedPnL":-1.5,"coin":[{"coin":"USDT","balance":1000.5,"available":900,"debts":0,"locked":100.5}]}],"ts":1}`)
	waitFor(t, time.Second, "numeric account", func() bool { return accounts.len() == 1 })
	var a utatypes.AccountAssets = accounts.snapshot()[0]
	if a.AccountEquity.String() != "1000.5" || a.UnrealisedPnl.String() != "-1.5" {
		t.Fatalf("account: %+v", a)
	}
	if len(a.Assets) != 1 || a.Assets[0].Balance.String() != "1000.5" || a.Assets[0].Locked.String() != "100.5" {
		t.Fatalf("account assets: %+v", a.Assets)
	}
	if errs.len() != 0 {
		t.Fatalf("numeric fields must be tolerated: %v", errs.snapshot())
	}
}

// A NUMERIC field holding non-numeric garbage rejects the row (one error)
// instead of delivering a silent zero; the other rows still arrive.
func TestContract_StreamPrivate_GarbageNumericRejectsRow(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var got collector[utatypes.Order]
	var errs collector[error]
	if err := c.Stream().WatchOrders(context.Background(), got.add, errs.add); err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	mock.waitOps(t, "subscribe", argOrder, 1)
	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"order"},"data":[`+
		`{"category":"spot","symbol":"BTCUSDT","orderId":"bad","qty":"abc"},`+
		`{"category":"spot","symbol":"BTCUSDT","orderId":"good","qty":"1"}],"ts":1}`)
	waitFor(t, time.Second, "good row + error", func() bool { return got.len() == 1 && errs.len() == 1 })
	if got.snapshot()[0].OrderID != "good" {
		t.Fatalf("delivered = %+v", got.snapshot())
	}
}

// ---------------------------------------------------------------------
// WatchFills.
// ---------------------------------------------------------------------

func TestContract_StreamPrivate_WatchFills_DocsFixture(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var got collector[utatypes.Fill]
	var errs collector[error]
	if err := c.Stream().WatchFills(context.Background(), got.add, errs.add); err != nil {
		t.Fatalf("WatchFills: %v", err)
	}
	mock.waitOps(t, "subscribe", argFill, 1)
	mock.push(t, fixtureFillPush)
	waitFor(t, time.Second, "fill delivery", func() bool { return got.len() == 1 })

	var f utatypes.Fill = got.snapshot()[0]
	if f.ExecID != "1288888888888888888" || f.OrderID != "1288888888888888888" || f.ClientOID != "1288888888888888889" {
		t.Fatalf("ids: %+v", f)
	}
	if f.ExecLinkID != "1288888888888888888" {
		t.Fatalf("execLinkId = %q", f.ExecLinkID)
	}
	if f.Category != "USDT-FUTURES" || f.Symbol != "BTCUSDT" || f.OrderType != "market" || f.Side != "buy" {
		t.Fatalf("category/symbol/type/side: %+v", f)
	}
	if f.PosSide != "long" || f.TradeSide != "open" || f.TradeScope != "taker" || f.IsRPI {
		t.Fatalf("posSide/tradeSide/scope/rpi: %+v", f)
	}
	if f.ExecPrice.String() != "94993" || f.ExecQty.String() != "0.01" || f.ExecValue.String() != "949.93" || !f.ExecPnl.IsZero() {
		t.Fatalf("exec: %s/%s/%s/%s", f.ExecPrice, f.ExecQty, f.ExecValue, f.ExecPnl)
	}
	if len(f.FeeDetail) != 1 || f.FeeDetail[0].FeeCoin != "USDT" || f.FeeDetail[0].Fee.String() != "0.569958" {
		t.Fatalf("feeDetail = %+v", f.FeeDetail)
	}
	if f.ExecTime != 1736378720623 || f.UpdatedTime != 1736378720623 {
		t.Fatalf("times = %d/%d", f.ExecTime, f.UpdatedTime)
	}
	if f.CreatedTime != f.ExecTime {
		t.Fatalf("CreatedTime = %d, want the ExecTime fallback (the WS row has no createdTime)", f.CreatedTime)
	}

	// feeDetail as a string on the fill topic is tolerated too.
	mock.push(t, `{"action":"snapshot","arg":{"instType":"UTA","topic":"fill"},"data":[{"category":"spot","symbol":"ETHUSDT","execId":"e2","feeDetail":"","execPrice":"1","execQty":"2","isRPI":"yes"}],"ts":1}`)
	waitFor(t, time.Second, "second fill", func() bool { return got.len() == 2 })
	if !got.snapshot()[1].IsRPI || got.snapshot()[1].FeeDetail != nil {
		t.Fatalf("second fill: %+v", got.snapshot()[1])
	}
	if errs.len() != 0 {
		t.Fatalf("unexpected errors: %v", errs.snapshot())
	}
}

// ---------------------------------------------------------------------
// WatchPositions.
// ---------------------------------------------------------------------

func TestContract_StreamPrivate_WatchPositions_DocsFixture(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var got collector[utatypes.CurrentPosition]
	var errs collector[error]
	if err := c.Stream().WatchPositions(context.Background(), got.add, errs.add); err != nil {
		t.Fatalf("WatchPositions: %v", err)
	}
	mock.waitOps(t, "subscribe", argPosition, 1)
	mock.push(t, fixturePositionPush)
	waitFor(t, time.Second, "position delivery", func() bool { return got.len() == 1 })

	var p utatypes.CurrentPosition = got.snapshot()[0]
	if p.Category != "" {
		t.Fatalf("category = %q — the WS position row carries none", p.Category)
	}
	if p.Symbol != "BTCUSDT" || p.MarginCoin != "USDT" || p.MarginMode != "crossed" {
		t.Fatalf("symbol/coin/mode: %+v", p)
	}
	if p.PosSide != "long" || p.HoldMode != "hedge_mode" || p.PositionStatus != "ended" {
		t.Fatalf("side/mode/status: %+v", p)
	}
	if !p.Total.IsZero() || !p.Available.IsZero() || !p.Frozen.IsZero() {
		t.Fatalf("closed position sizes: %s/%s/%s", p.Total, p.Available, p.Frozen)
	}
	if p.Leverage.String() != "20" || p.MarkPrice.String() != "94987.1" {
		t.Fatalf("lev/mark = %s/%s", p.Leverage, p.MarkPrice)
	}
	if !p.LiquidationPrice.IsZero() || !p.MMR.IsZero() || !p.BreakEvenPrice.IsZero() || !p.ProfitRate.IsZero() {
		t.Fatalf("empty-string numerics must decode to zero: %+v", p)
	}
	if p.CreatedTime != 1736378720620 || p.UpdatedTime != 1736378720620 {
		t.Fatalf("times = %d/%d", p.CreatedTime, p.UpdatedTime)
	}

	// An OPEN position: WS spellings size / liqPrice / totalFundingFee /
	// marginSize land in Total / LiquidationPrice / TotalFunding / MarginSize.
	mock.push(t, `{"action":"update","arg":{"instType":"UTA","topic":"position"},"data":[{"symbol":"ETHUSDT","marginCoin":"USDT","marginMode":"isolated","posSide":"short","holdMode":"hedge_mode","positionStatus":"opening","size":"1.5","available":"1","frozen":"0.5","avgPrice":"3000.25","leverage":"10","marginSize":"450.0375","curRealisedPnl":"-0.9","unrealisedPnl":"12.5","liqPrice":"3290.1","mmr":"0.005","markPrice":"2991.9","breakEvenPrice":"2998.4","profitRate":"0.0277","totalFundingFee":"-0.12","openFeeTotal":"-0.9","closeFeeTotal":"0","createdTime":"1736378720620","updatedTime":"1736378999999"}],"ts":1}`)
	waitFor(t, time.Second, "open position", func() bool { return got.len() == 2 })
	p = got.snapshot()[1]
	if p.Total.String() != "1.5" || p.Available.String() != "1" || p.Frozen.String() != "0.5" {
		t.Fatalf("sizes = %s/%s/%s", p.Total, p.Available, p.Frozen)
	}
	if p.LiquidationPrice.String() != "3290.1" || p.TotalFunding.String() != "-0.12" || p.MarginSize.String() != "450.0375" {
		t.Fatalf("liq/funding/margin = %s/%s/%s", p.LiquidationPrice, p.TotalFunding, p.MarginSize)
	}
	if p.AvgPrice.String() != "3000.25" || p.UnrealisedPnl.String() != "12.5" || p.CurRealisedPnl.String() != "-0.9" {
		t.Fatalf("avg/upnl/rpnl = %s/%s/%s", p.AvgPrice, p.UnrealisedPnl, p.CurRealisedPnl)
	}
	if p.MMR.String() != "0.005" || p.BreakEvenPrice.String() != "2998.4" || p.ProfitRate.String() != "0.0277" {
		t.Fatalf("mmr/bep/rate = %s/%s/%s", p.MMR, p.BreakEvenPrice, p.ProfitRate)
	}
	if p.OpenFeeTotal.String() != "-0.9" || p.PosSide != "short" || p.MarginMode != "isolated" {
		t.Fatalf("fees/side/mode: %+v", p)
	}

	// REST spellings are read as fallbacks (should the venue align them).
	mock.push(t, `{"action":"update","arg":{"instType":"UTA","topic":"position"},"data":[{"category":"usdt-futures","symbol":"SOLUSDT","holdSide":"long","total":"7","liquidationPrice":"10","totalFunding":"-1","positionBalance":"99"}],"ts":1}`)
	waitFor(t, time.Second, "REST-spelled position", func() bool { return got.len() == 3 })
	p = got.snapshot()[2]
	if p.Category != "USDT-FUTURES" || p.PosSide != "long" || p.Total.String() != "7" ||
		p.LiquidationPrice.String() != "10" || p.TotalFunding.String() != "-1" || p.PositionBalance.String() != "99" {
		t.Fatalf("REST-spelled position: %+v", p)
	}
	if errs.len() != 0 {
		t.Fatalf("unexpected errors: %v", errs.snapshot())
	}
}

// ---------------------------------------------------------------------
// WatchAccount.
// ---------------------------------------------------------------------

func TestContract_StreamPrivate_WatchAccount_DocsFixture(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var got collector[utatypes.AccountAssets]
	var errs collector[error]
	if err := c.Stream().WatchAccount(context.Background(), got.add, errs.add); err != nil {
		t.Fatalf("WatchAccount: %v", err)
	}
	mock.waitOps(t, "subscribe", argAccount, 1)
	mock.push(t, fixtureAccountPush)
	waitFor(t, time.Second, "account delivery", func() bool { return got.len() == 1 })

	var a utatypes.AccountAssets = got.snapshot()[0]
	if a.AccountEquity.String() != "4976919.05" {
		t.Fatalf("AccountEquity = %s, want the WS totalEquity", a.AccountEquity)
	}
	if a.UnrealisedPnl.String() != "-10116.55" || a.EffEquity.String() != "4847952.35" {
		t.Fatalf("upnl/eff = %s/%s", a.UnrealisedPnl, a.EffEquity)
	}
	if a.MMR.String() != "408.08" || a.IMR.String() != "17795.97" || !a.MgnRatio.IsZero() || !a.PositionMgnRatio.IsZero() {
		t.Fatalf("mmr/imr/ratios = %s/%s/%s/%s", a.MMR, a.IMR, a.MgnRatio, a.PositionMgnRatio)
	}
	if len(a.Assets) != 3 {
		t.Fatalf("assets = %d, want 3 (the WS coin[] list)", len(a.Assets))
	}
	var usdt utatypes.AccountAsset = a.Assets[2]
	if usdt.Coin != "USDT" || usdt.Balance.String() != "354411.45536458" || usdt.Available.String() != "344282.65536458" {
		t.Fatalf("USDT asset: %+v", usdt)
	}
	if usdt.Equity.String() != "344282.65536458" || usdt.USDValue.String() != "343866.07335159" {
		t.Fatalf("USDT equity/usd = %s/%s", usdt.Equity, usdt.USDValue)
	}
	if !usdt.Debt.IsZero() || !usdt.Locked.IsZero() || !usdt.Borrow.IsZero() || usdt.Bonus.String() != "10" {
		t.Fatalf("USDT debt/locked/borrow/bonus = %s/%s/%s/%s", usdt.Debt, usdt.Locked, usdt.Borrow, usdt.Bonus)
	}
	if a.Assets[0].Coin != "ETH" || a.Assets[1].Coin != "BTC" {
		t.Fatalf("asset order: %s, %s", a.Assets[0].Coin, a.Assets[1].Coin)
	}
	if errs.len() != 0 {
		t.Fatalf("unexpected errors: %v", errs.snapshot())
	}
}

// ---------------------------------------------------------------------
// Fan-out on a private topic.
// ---------------------------------------------------------------------

func TestContract_StreamPrivate_FanOut_ThreeConsumers(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var consumers [3]collector[utatypes.Order]
	var cancels [3]context.CancelFunc
	var i int
	for i = 0; i < 3; i++ {
		var ctx context.Context
		ctx, cancels[i] = context.WithCancel(context.Background())
		defer cancels[i]()
		if err := c.Stream().WatchOrders(ctx, consumers[i].add, nil); err != nil {
			t.Fatalf("WatchOrders #%d: %v", i, err)
		}
	}
	mock.waitOps(t, "subscribe", argOrder, 1)

	mock.push(t, fixtureOrderPush)
	waitFor(t, time.Second, "all three consumers", func() bool {
		return consumers[0].len() == 1 && consumers[1].len() == 1 && consumers[2].len() == 1
	})
	if mock.countOps("subscribe", argOrder) != 1 {
		t.Fatalf("three consumers must share ONE wire subscription, saw %d", mock.countOps("subscribe", argOrder))
	}

	// One consumer leaves — the others keep receiving, no unsubscribe.
	cancels[1]()
	time.Sleep(60 * time.Millisecond)
	mock.push(t, fixtureOrderPush)
	waitFor(t, time.Second, "remaining consumers", func() bool { return consumers[0].len() == 2 && consumers[2].len() == 2 })
	if consumers[1].len() != 1 {
		t.Fatalf("detached consumer still receives: %d", consumers[1].len())
	}
	if mock.countOps("unsubscribe", argOrder) != 0 {
		t.Fatal("unsubscribe sent while consumers remain")
	}

	cancels[0]()
	time.Sleep(60 * time.Millisecond)
	if mock.countOps("unsubscribe", argOrder) != 0 {
		t.Fatal("unsubscribe sent while the last consumer remains")
	}
	cancels[2]()
	mock.waitOps(t, "unsubscribe", argOrder, 1)
}

// ---------------------------------------------------------------------
// OnPrivateReconnect.
// ---------------------------------------------------------------------

func TestContract_StreamPrivate_OnPrivateReconnect_OnlyFromSecondConnection(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)

	var calls collector[int]
	var public collector[int]
	var remove func() = c.Stream().OnPrivateReconnect(func() { calls.add(mock.connCount()) })
	defer c.Stream().OnPublicReconnect(func() { public.add(1) })()

	var got collector[utatypes.Order]
	if err := c.Stream().WatchOrders(context.Background(), got.add, nil); err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	mock.waitOps(t, "subscribe", argOrder, 1)
	time.Sleep(50 * time.Millisecond)
	if calls.len() != 0 {
		t.Fatalf("OnPrivateReconnect fired on the FIRST connection: %v", calls.snapshot())
	}

	// Server-side close → relogin → resubscribe → callback.
	mock.dropActive(t)
	mock.waitOps(t, "subscribe", argOrder, 2)
	waitFor(t, time.Second, "reconnect callback", func() bool { return calls.len() == 1 })
	if calls.snapshot()[0] != 2 {
		t.Fatalf("callback fired on connection #%d, want #2", calls.snapshot()[0])
	}
	// The second connection logged in again before resubscribing.
	var ops []wsOp = mock.opsSnapshot()
	var logins int = 0
	var lastLogin int = -1
	var lastSubscribe int = -1
	var i int
	for i = 0; i < len(ops); i++ {
		if ops[i].op == "login" {
			logins++
			lastLogin = i
		}
		if ops[i].op == "subscribe" {
			lastSubscribe = i
		}
	}
	if logins != 2 || lastLogin > lastSubscribe {
		t.Fatalf("relogin must precede the resubscribe: %+v", ops)
	}
	// The stream keeps delivering on the new socket.
	mock.push(t, fixtureOrderPush)
	waitFor(t, time.Second, "delivery after reconnect", func() bool { return got.len() == 1 })

	remove()
	mock.dropActive(t)
	mock.waitOps(t, "subscribe", argOrder, 3)
	time.Sleep(50 * time.Millisecond)
	if calls.len() != 1 {
		t.Fatalf("removed callback fired: %v", calls.snapshot())
	}
	if public.len() != 0 {
		t.Fatalf("a private reconnect fired the PUBLIC callbacks")
	}
}
