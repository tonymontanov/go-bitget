/*
FILE: mix/stream_private_contract_test.go

DESCRIPTION:
End-to-end tests for the private streams (orders / positions / account).
The mock exposed by stream_contract_test.go already understands
{"op":"login",...}, so these tests only need to plug into the same
infrastructure and exercise the field-mapping for each channel.
*/

package mix

import (
	"context"
	"sync"
	"testing"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// makePrivateStreamClient is the private-WS analogue of
// makeStreamClient: it points BOTH PublicURL and PrivateURL at the
// mock so tests do not need to know which socket the SDK opens, AND
// it pre-fills API credentials so signerEnabled() returns true and
// ws.Conn issues a real login frame.
func makePrivateStreamClient(t *testing.T, mock *streamMockServer) *Client {
	t.Helper()
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.WS.PublicURL = mock.wsURL()
	cfg.WS.PrivateURL = mock.wsURL()
	cfg.WS.HandshakeTimeout = 500 * time.Millisecond
	cfg.WS.ReadTimeout = 500 * time.Millisecond
	cfg.WS.WriteTimeout = 500 * time.Millisecond
	cfg.WS.PingInterval = 5 * time.Second
	cfg.WS.LoginTimeout = 500 * time.Millisecond
	cfg.WS.ReconnectInitialBackoff = 10 * time.Millisecond
	cfg.WS.ReconnectMaxBackoff = 50 * time.Millisecond
	cfg.WS.ReconnectJitter = 0
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	return NewClient(parent)
}

// ---------------------------------------------------------------------
// WatchOrders.
// ---------------------------------------------------------------------

func TestContract_WatchOrders_FieldMapping(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan mixtypes.OrderInfo, 4)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "BTCUSDT",
		func(o mixtypes.OrderInfo) {
			select {
			case got <- o:
			default:
			}
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}

	select {
	case sub := <-mock.subs:
		// CRITICAL CONTRACT (regressed twice already):
		// Bitget V2 private orders channel rejects per-symbol
		// subscriptions with code=30001. The SDK MUST always
		// subscribe with instId="default"; per-symbol filtering
		// happens client-side inside handleOrdersFrame.
		if sub["channel"] != "orders" || sub["instId"] != "default" {
			t.Fatalf("unexpected subscribe arg (expected instId=default): %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "orders", "default",
		[]map[string]any{{
			"instId":        "BTCUSDT",
			"orderId":       "ord-1",
			"clientOid":     "cli-1",
			"side":          "buy",
			"tradeSide":     "open",
			"posSide":       "long",
			"orderType":     "limit",
			"force":         "gtc",
			"status":        "partially_filled",
			"size":          "0.01",
			"price":         "50000",
			"accBaseVolume": "0.003",
			"priceAvg":      "50001",
			"fee":           "0.0001",
			"marginCoin":    "USDT",
			"marginMode":    "crossed",
			"leverage":      "10",
			"cTime":         "1700000000000",
			"uTime":         "1700000000050",
		}}, 1700000000050)

	select {
	case o := <-got:
		if o.OrderID != "ord-1" || o.ClientOrderID != "cli-1" {
			t.Fatalf("ids: %#v", o)
		}
		if o.Symbol != "BTCUSDT" || o.Side != roottypes.SideTypeBuy {
			t.Fatalf("symbol/side: %#v", o)
		}
		if string(o.Status) != "partially_filled" {
			t.Fatalf("status: %s", o.Status)
		}
		if o.Quantity.String() != "0.01" || o.Price.String() != "50000" {
			t.Fatalf("qty/price: %s/%s", o.Quantity, o.Price)
		}
		if o.FilledQuantity.String() != "0.003" || o.AvgFilledPrice.String() != "50001" {
			t.Fatalf("filled/avg: %s/%s", o.FilledQuantity, o.AvgFilledPrice)
		}
		if o.CumFee.String() != "0.0001" {
			t.Fatalf("fee: %s", o.CumFee)
		}
		if o.CreatedAtMs != 1700000000000 || o.UpdatedAtMs != 1700000000050 {
			t.Fatalf("ts: %d/%d", o.CreatedAtMs, o.UpdatedAtMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("orders handler not invoked")
	}
}

// TestContract_WatchOrders_FilterDropsForeignSymbol locks down the
// per-symbol filter for orders. SDK subscribes globally with
// instId="default", but the user handler must only see rows whose
// row.InstID matches the requested symbol. Symmetric with the
// existing positions test (which catches the same regression class
// — once the SDK ships per-symbol filtering, EVERY private channel
// that overrides instId="default" must filter).
func TestContract_WatchOrders_FilterDropsForeignSymbol(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []mixtypes.OrderInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "BTCUSDT",
		func(o mixtypes.OrderInfo) {
			gotMu.Lock()
			got = append(got, o)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	<-mock.subs

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "orders", "default",
		[]map[string]any{
			{
				"instId": "ETHUSDT", "orderId": "ord-eth", "clientOid": "cli-eth",
				"side": "buy", "tradeSide": "open", "posSide": "long",
				"orderType": "limit", "force": "gtc", "status": "live",
				"size": "1", "price": "2500", "marginCoin": "USDT",
				"marginMode": "crossed", "leverage": "5",
			},
			{
				"instId": "BTCUSDT", "orderId": "ord-btc", "clientOid": "cli-btc",
				"side": "buy", "tradeSide": "open", "posSide": "long",
				"orderType": "limit", "force": "gtc", "status": "live",
				"size": "0.01", "price": "50000", "marginCoin": "USDT",
				"marginMode": "crossed", "leverage": "10",
			},
		}, 1700000000050)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) >= 1
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 row (BTCUSDT only), got %d: %#v", len(got), got)
	}
	if got[0].Symbol != "BTCUSDT" {
		t.Fatalf("expected BTCUSDT, got %q (filter regression)", got[0].Symbol)
	}
}

// TestContract_WatchOrders_DefaultSymbolReceivesAll covers the
// caller-friendly opt-out for orders: pass "default" → no filter,
// every order on the account reaches the handler.
func TestContract_WatchOrders_DefaultSymbolReceivesAll(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []mixtypes.OrderInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "default",
		func(o mixtypes.OrderInfo) {
			gotMu.Lock()
			got = append(got, o)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	<-mock.subs

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "orders", "default",
		[]map[string]any{
			{"instId": "ETHUSDT", "orderId": "o1", "clientOid": "c1", "side": "buy", "orderType": "limit", "force": "gtc", "status": "live", "size": "1", "price": "2500", "marginCoin": "USDT", "marginMode": "crossed"},
			{"instId": "BTCUSDT", "orderId": "o2", "clientOid": "c2", "side": "buy", "orderType": "limit", "force": "gtc", "status": "live", "size": "0.01", "price": "50000", "marginCoin": "USDT", "marginMode": "crossed"},
		}, 1700000000050)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) == 2
	})
}

// TestContract_WatchOrders_AcceptsNumericFields locks down the
// flexString regression observed on prod for positions (PARTIUSDT,
// May 27 2026, v1.2.1) but applied to the orders channel: the wire
// MAY ship every numeric field as a JSON number instead of the
// documented quoted string. flexString accepts both shapes; this
// test pins it for orders so a future strict-typed regression on
// this channel is caught at CI time.
func TestContract_WatchOrders_AcceptsNumericFields(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan mixtypes.OrderInfo, 4)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "BTCUSDT",
		func(o mixtypes.OrderInfo) {
			select {
			case got <- o:
			default:
			}
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	<-mock.subs

	// Every numeric field below is shipped as a JSON number — the
	// failure mode that abort-decoded the entire push pre-v1.2.1.
	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "orders", "default",
		[]map[string]any{{
			"instId":        "BTCUSDT",
			"orderId":       "ord-num",
			"clientOid":     "cli-num",
			"side":          "buy",
			"tradeSide":     "open",
			"posSide":       "long",
			"orderType":     "limit",
			"force":         "gtc",
			"status":        "partially_filled",
			"size":          0.01,
			"price":         50000,
			"accBaseVolume": 0.003,
			"priceAvg":      50001,
			"fee":           0.0001,
			"marginCoin":    "USDT",
			"marginMode":    "crossed",
			"leverage":      10,
			"cTime":         1700000000000,
			"uTime":         1700000000050,
		}}, 1700000000050)

	select {
	case o := <-got:
		if o.OrderID != "ord-num" {
			t.Fatalf("orderId: %q (decode regression)", o.OrderID)
		}
		if o.Quantity.String() != "0.01" || o.Price.String() != "50000" {
			t.Fatalf("qty/price: %s/%s (numeric-field regression)", o.Quantity, o.Price)
		}
		if o.FilledQuantity.String() != "0.003" || o.AvgFilledPrice.String() != "50001" {
			t.Fatalf("filled/avg: %s/%s", o.FilledQuantity, o.AvgFilledPrice)
		}
		if o.UpdatedAtMs != 1700000000050 {
			t.Fatalf("uTime: %d", o.UpdatedAtMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("orders handler not invoked (numeric-field regression)")
	}
}

// ---------------------------------------------------------------------
// WatchPositions.
// ---------------------------------------------------------------------

func TestContract_WatchPositions_FieldMapping(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []mixtypes.PositionInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchPositions(ctx, "BTCUSDT",
		func(p mixtypes.PositionInfo) {
			gotMu.Lock()
			got = append(got, p)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchPositions: %v", err)
	}

	select {
	case sub := <-mock.subs:
		// CRITICAL CONTRACT (see WatchOrders test for the long story).
		// Bitget V2 positions channel ONLY accepts instId="default";
		// per-symbol subscribe yields code=30001.
		if sub["channel"] != "positions" || sub["instId"] != "default" {
			t.Fatalf("unexpected subscribe arg (expected instId=default): %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "positions", "default",
		[]map[string]any{{
			"instId":           "BTCUSDT",
			"marginCoin":       "USDT",
			"holdSide":         "long",
			"marginMode":       "crossed",
			"total":            "0.5",
			"available":        "0.5",
			"frozen":           "0.0",
			"openPriceAvg":     "50000",
			"markPrice":        "50100",
			"liquidationPrice": "30000",
			"leverage":         "10",
			"unrealizedPL":     "50",
			"achievedProfits":  "0",
			"cTime":            "1700000000000",
			"uTime":            "1700000000050",
		}}, 1700000000050)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) == 1
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	var p mixtypes.PositionInfo = got[0]
	if p.Symbol != "BTCUSDT" || p.MarginCoin != "USDT" {
		t.Fatalf("symbol/marginCoin: %#v", p)
	}
	if string(p.HoldSide) != "long" || string(p.MarginMode) != "crossed" {
		t.Fatalf("holdSide/marginMode: %s/%s", p.HoldSide, p.MarginMode)
	}
	if p.Quantity.String() != "0.5" || p.Available.String() != "0.5" {
		t.Fatalf("qty/avail: %s/%s", p.Quantity, p.Available)
	}
	if p.AvgOpenPrice.String() != "50000" || p.MarkPrice.String() != "50100" {
		t.Fatalf("openAvg/mark: %s/%s", p.AvgOpenPrice, p.MarkPrice)
	}
	if p.LiquidationPrice.String() != "30000" || p.Leverage != 10 {
		t.Fatalf("liq/lev: %s/%d", p.LiquidationPrice, p.Leverage)
	}
	if p.UnrealizedPnL.String() != "50" {
		t.Fatalf("unrealized: %s", p.UnrealizedPnL)
	}
	if p.CreatedAtMs != 1700000000000 || p.UpdatedAtMs != 1700000000050 {
		t.Fatalf("ts: %d/%d", p.CreatedAtMs, p.UpdatedAtMs)
	}
}

// TestContract_WatchPositions_FilterDropsForeignSymbol locks down
// the symbol-filter behaviour: when the caller subscribes for
// "BTCUSDT" the SDK still subscribes globally with instId="default",
// but rows for an unrelated symbol must NOT reach the user handler.
// This is the per-symbol contract that the v1.0 wire bug
// accidentally satisfied (server filtered by rejecting subscribe).
func TestContract_WatchPositions_FilterDropsForeignSymbol(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []mixtypes.PositionInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchPositions(ctx, "BTCUSDT",
		func(p mixtypes.PositionInfo) {
			gotMu.Lock()
			got = append(got, p)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchPositions: %v", err)
	}
	<-mock.subs

	// One push carrying TWO rows: the requested symbol and a
	// foreign one. Only the requested one should reach the handler.
	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "positions", "default",
		[]map[string]any{
			{
				"instId":     "ETHUSDT",
				"marginCoin": "USDT",
				"holdSide":   "long",
				"marginMode": "crossed",
				"total":      "1",
				"available":  "1",
				"frozen":     "0",
				"leverage":   "5",
			},
			{
				"instId":     "BTCUSDT",
				"marginCoin": "USDT",
				"holdSide":   "long",
				"marginMode": "crossed",
				"total":      "0.1",
				"available":  "0.1",
				"frozen":     "0",
				"leverage":   "5",
			},
		}, 1700000000050)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) >= 1
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 row (BTCUSDT only), got %d: %#v", len(got), got)
	}
	if got[0].Symbol != "BTCUSDT" {
		t.Fatalf("expected BTCUSDT, got %q (filter regression)", got[0].Symbol)
	}
}

// TestContract_WatchPositions_DefaultSymbolReceivesAll covers the
// caller-friendly opt-out: pass "default" → no filter, all rows.
func TestContract_WatchPositions_DefaultSymbolReceivesAll(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []mixtypes.PositionInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchPositions(ctx, "default",
		func(p mixtypes.PositionInfo) {
			gotMu.Lock()
			got = append(got, p)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchPositions: %v", err)
	}
	<-mock.subs

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "positions", "default",
		[]map[string]any{
			{"instId": "ETHUSDT", "marginCoin": "USDT", "holdSide": "long", "marginMode": "crossed", "total": "1", "available": "1", "frozen": "0", "leverage": "5"},
			{"instId": "BTCUSDT", "marginCoin": "USDT", "holdSide": "long", "marginMode": "crossed", "total": "0.1", "available": "0.1", "frozen": "0", "leverage": "5"},
		}, 1700000000050)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) == 2
	})
}

// TestContract_WatchPositions_AcceptsNumericLeverage locks down the
// flexString regression observed on prod (PARTIUSDT, May 27 2026):
// Bitget V2 positions snapshot shipped `leverage` as a JSON number
// (`"leverage":5`) instead of the documented quoted string
// (`"leverage":"5"`), and the strict string-typed wire row caused
// jsoniter to abort decoding with
//
//	`mix.wsPositionRow.Leverage: ReadString: expects " or n, but found 5`.
//
// The handler then dropped the entire push, breaking inventory
// updates for any account with an open position. flexString accepts
// both shapes; this test pins it.
func TestContract_WatchPositions_AcceptsNumericLeverage(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []mixtypes.PositionInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchPositions(ctx, "PARTIUSDT",
		func(p mixtypes.PositionInfo) {
			gotMu.Lock()
			got = append(got, p)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchPositions: %v", err)
	}
	<-mock.subs

	// IMPORTANT: every numeric field below is sent as a JSON number,
	// not a quoted string. This mirrors the exact wire shape captured
	// from the live Bitget V2 server (production app.log, v1.2.0).
	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "positions", "default",
		[]map[string]any{{
			"instId":           "PARTIUSDT",
			"marginCoin":       "USDT",
			"holdSide":         "short",
			"marginMode":       "crossed",
			"total":            0.5,
			"available":        0.5,
			"frozen":           0,
			"openPriceAvg":     0.084501465025,
			"markPrice":        0.05627,
			"liquidationPrice": 0.12,
			"leverage":         5, // <-- the actual regression
			"unrealizedPL":     -1128.50454,
			"achievedProfits":  0,
			"cTime":            1779897892000,
			"uTime":            1779897893000,
		}}, 1779897893000)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) == 1
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 PARTIUSDT row, got %d (numeric-field regression)", len(got))
	}
	var p mixtypes.PositionInfo = got[0]
	if p.Symbol != "PARTIUSDT" {
		t.Fatalf("symbol: %q", p.Symbol)
	}
	if p.Leverage != 5 {
		t.Fatalf("leverage: got %d, want 5", p.Leverage)
	}
	if p.Quantity.String() != "0.5" {
		t.Fatalf("total: %s", p.Quantity)
	}
	if p.UpdatedAtMs != 1779897893000 {
		t.Fatalf("uTime: %d", p.UpdatedAtMs)
	}
}

// ---------------------------------------------------------------------
// WatchAccount.
// ---------------------------------------------------------------------

func TestContract_WatchAccount_FieldMapping(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan roottypes.Balance, 4)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchAccount(ctx,
		func(b roottypes.Balance) {
			select {
			case got <- b:
			default:
			}
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchAccount: %v", err)
	}

	select {
	case sub := <-mock.subs:
		if sub["channel"] != "account" || sub["coin"] != "USDT" {
			t.Fatalf("unexpected subscribe arg: %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	// Bitget keys account pushes by coin, not by instId. The envelope
	// arg therefore carries `coin` and the SDK's registry dispatch
	// (env.Arg.Key()) must match the originating subscription on
	// `instType:channel::coin`.
	mock.pushFrameWithCoin(t, "snapshot", "USDT-FUTURES", "account", "", "USDT",
		[]map[string]any{{
			"marginCoin":   "USDT",
			"available":    "1000",
			"frozen":       "5",
			"equity":       "1100",
			"usdtEquity":   "1100",
			"btcEquity":    "0",
			"unrealizedPL": "100",
		}}, 1700000000050)

	select {
	case b := <-got:
		if b.MarginCoin != "USDT" {
			t.Fatalf("marginCoin: %s", b.MarginCoin)
		}
		if b.TotalEquity.String() != "1100" {
			t.Fatalf("equity: %s", b.TotalEquity)
		}
		if b.AvailableBalance.String() != "1000" {
			t.Fatalf("avail: %s", b.AvailableBalance)
		}
		if b.LockedBalance.String() != "5" {
			t.Fatalf("locked: %s", b.LockedBalance)
		}
		if b.UnrealizedPnL.String() != "100" {
			t.Fatalf("upnl: %s", b.UnrealizedPnL)
		}
		if len(b.Coins) != 1 {
			t.Fatalf("coins arity: %d", len(b.Coins))
		}
		var coin roottypes.CoinBalance = b.Coins[0]
		if coin.Coin != "USDT" || coin.Equity.String() != "1100" || coin.Available.String() != "1000" {
			t.Fatalf("coin: %#v", coin)
		}
		if coin.Frozen.String() != "5" || coin.UsdtEquity.String() != "1100" {
			t.Fatalf("coin frozen/usdtEq: %s/%s", coin.Frozen, coin.UsdtEquity)
		}
	case <-time.After(time.Second):
		t.Fatalf("account handler not invoked")
	}
}

// ---------------------------------------------------------------------
// WatchFills (v2.0.0-m6).
// ---------------------------------------------------------------------

// TestContract_WatchFills_FieldMapping pins the per-execution wire →
// SDK conversion for the mix `fill` channel including derivatives-
// only fields (clientOid / posMode / tradeSide / profit) and the
// mix-vs-spot field-name divergence (price vs priceAvg, baseVolume
// vs size, quoteVolume vs amount). Subscribe arg pins instId="default".
func TestContract_WatchFills_FieldMapping(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan mixtypes.FillUpdate, 4)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchFills(ctx, "BTCUSDT",
		func(f mixtypes.FillUpdate) {
			select {
			case got <- f:
			default:
			}
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchFills: %v", err)
	}

	select {
	case sub := <-mock.subs:
		if sub["channel"] != "fill" || sub["instId"] != "default" {
			t.Fatalf("unexpected subscribe arg (expected instId=default): %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "fill", "default",
		[]map[string]any{{
			"orderId":     "ord-1",
			"clientOid":   "cli-1",
			"tradeId":     "trd-1",
			"symbol":      "BTCUSDT",
			"side":        "buy",
			"orderType":   "market",
			"posMode":     "one_way_mode",
			"tradeSide":   "open",
			"price":       "51000.5",
			"baseVolume":  "0.01",
			"quoteVolume": "510.005",
			"profit":      "0",
			"tradeScope":  "taker",
			"feeDetail": []map[string]any{{
				"feeCoin":           "USDT",
				"deduction":         "no",
				"totalDeductionFee": "0",
				"totalFee":          "-0.183717",
			}},
			"cTime": "1703577336606",
			"uTime": "1703577336606",
		}}, 1703577336700)

	select {
	case f := <-got:
		if f.OrderID != "ord-1" || f.ClientOrderID != "cli-1" || f.TradeID != "trd-1" {
			t.Fatalf("ids: %#v", f)
		}
		if f.Symbol != "BTCUSDT" {
			t.Fatalf("symbol: %s", f.Symbol)
		}
		if f.PosMode != "one_way_mode" {
			t.Fatalf("posMode: %s", f.PosMode)
		}
		if string(f.TradeSide) != "open" {
			t.Fatalf("tradeSide: %s", f.TradeSide)
		}
		if f.Price.String() != "51000.5" {
			t.Fatalf("price: %s", f.Price)
		}
		if f.BaseVolume.String() != "0.01" || f.QuoteVolume.String() != "510.005" {
			t.Fatalf("base/quote: %s/%s", f.BaseVolume, f.QuoteVolume)
		}
		if !f.Profit.IsZero() {
			t.Fatalf("profit: %s (expected 0)", f.Profit)
		}
		if len(f.FeeDetail) != 1 || f.FeeDetail[0].TotalFee.String() != "-0.183717" {
			t.Fatalf("feeDetail: %#v", f.FeeDetail)
		}
		if f.CreatedAtMs != 1703577336606 {
			t.Fatalf("cTime: %d", f.CreatedAtMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("fill handler not invoked")
	}
}

// TestContract_WatchFills_FilterDropsForeignSymbol — same per-symbol
// semantics as orders / positions: subscribe with concrete symbol →
// rows for other symbols never reach the handler.
func TestContract_WatchFills_FilterDropsForeignSymbol(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []mixtypes.FillUpdate
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchFills(ctx, "BTCUSDT",
		func(f mixtypes.FillUpdate) {
			gotMu.Lock()
			got = append(got, f)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchFills: %v", err)
	}
	<-mock.subs

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "fill", "default",
		[]map[string]any{
			{"orderId": "o1", "clientOid": "c1", "tradeId": "t1", "symbol": "ETHUSDT", "side": "buy", "orderType": "limit", "posMode": "one_way_mode", "tradeSide": "open", "price": "2500", "baseVolume": "0.1", "quoteVolume": "250"},
			{"orderId": "o2", "clientOid": "c2", "tradeId": "t2", "symbol": "BTCUSDT", "side": "sell", "orderType": "limit", "posMode": "one_way_mode", "tradeSide": "close", "price": "50000", "baseVolume": "0.001", "quoteVolume": "50"},
		}, 1700000000050)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) >= 1
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 || got[0].Symbol != "BTCUSDT" {
		t.Fatalf("expected exactly 1 BTCUSDT row, got %d: %#v (filter regression)", len(got), got)
	}
}

// TestContract_WatchFills_AcceptsNumericFields — pre-emptive
// flexString regression guard for the new mix fills channel.
func TestContract_WatchFills_AcceptsNumericFields(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan mixtypes.FillUpdate, 1)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchFills(ctx, "BTCUSDT",
		func(f mixtypes.FillUpdate) {
			select {
			case got <- f:
			default:
			}
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchFills: %v", err)
	}
	<-mock.subs

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "fill", "default",
		[]map[string]any{{
			"orderId":     "ord-num",
			"clientOid":   "cli-num",
			"tradeId":     "trd-num",
			"symbol":      "BTCUSDT",
			"side":        "buy",
			"orderType":   "market",
			"posMode":     "one_way_mode",
			"tradeSide":   "open",
			"price":       51000.5,
			"baseVolume":  0.01,
			"quoteVolume": 510.005,
			"profit":      0,
			"tradeScope":  "taker",
			"feeDetail": []map[string]any{{
				"feeCoin":           "USDT",
				"deduction":         "no",
				"totalDeductionFee": 0,
				"totalFee":          -0.183717,
			}},
			"cTime": 1703577336606,
			"uTime": 1703577336606,
		}}, 1703577336700)

	select {
	case f := <-got:
		if f.Price.String() != "51000.5" {
			t.Fatalf("price from JSON-number: %s", f.Price)
		}
		if f.BaseVolume.String() != "0.01" || f.QuoteVolume.String() != "510.005" {
			t.Fatalf("base/quote from JSON-number: %s/%s", f.BaseVolume, f.QuoteVolume)
		}
		if f.UpdatedAtMs != 1703577336606 {
			t.Fatalf("uTime from JSON-number: %d", f.UpdatedAtMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("fill handler not invoked (numeric-field regression)")
	}
}

// ---------------------------------------------------------------------
// Auth guard.
// ---------------------------------------------------------------------

func TestContract_PrivateChannels_RequireSigner(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()
	// makeStreamClient leaves credentials empty → signer disabled.
	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	type tc struct {
		name string
		run  func() error
	}
	var cases = []tc{
		{"orders", func() error {
			return c.Stream().WatchOrders(context.Background(), "BTCUSDT",
				func(mixtypes.OrderInfo) {}, nil)
		}},
		{"positions", func() error {
			return c.Stream().WatchPositions(context.Background(), "BTCUSDT",
				func(mixtypes.PositionInfo) {}, nil)
		}},
		{"account", func() error {
			return c.Stream().WatchAccount(context.Background(),
				func(roottypes.Balance) {}, nil)
		}},
		{"fills", func() error {
			return c.Stream().WatchFills(context.Background(), "BTCUSDT",
				func(mixtypes.FillUpdate) {}, nil)
		}},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var sc tc = cases[i]
		t.Run(sc.name, func(t *testing.T) {
			var err error = sc.run()
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			var be *bitget.Error
			if !asBitgetError(err, &be) {
				t.Fatalf("not a *bitget.Error: %v", err)
			}
			if be.Kind != bitget.ErrorKindAuth {
				t.Fatalf("kind = %s", be.Kind)
			}
		})
	}
}

// ---------------------------------------------------------------------
// Validation cases for private channels.
// ---------------------------------------------------------------------

func TestContract_StreamPrivateValidation(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()
	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	type tc struct {
		name string
		run  func() error
	}
	var cases = []tc{
		{"orders empty symbol", func() error {
			return c.Stream().WatchOrders(context.Background(), "",
				func(mixtypes.OrderInfo) {}, nil)
		}},
		{"orders nil handler", func() error {
			return c.Stream().WatchOrders(context.Background(), "BTCUSDT", nil, nil)
		}},
		{"positions empty symbol", func() error {
			return c.Stream().WatchPositions(context.Background(), "",
				func(mixtypes.PositionInfo) {}, nil)
		}},
		{"positions nil handler", func() error {
			return c.Stream().WatchPositions(context.Background(), "BTCUSDT", nil, nil)
		}},
		{"account nil handler", func() error {
			return c.Stream().WatchAccount(context.Background(), nil, nil)
		}},
		{"fills empty symbol", func() error {
			return c.Stream().WatchFills(context.Background(), "",
				func(mixtypes.FillUpdate) {}, nil)
		}},
		{"fills nil handler", func() error {
			return c.Stream().WatchFills(context.Background(), "BTCUSDT", nil, nil)
		}},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var sc tc = cases[i]
		t.Run(sc.name, func(t *testing.T) {
			var err error = sc.run()
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			var be *bitget.Error
			if !asBitgetError(err, &be) {
				t.Fatalf("not a *bitget.Error: %v", err)
			}
			if be.Kind != bitget.ErrorKindInvalidRequest {
				t.Fatalf("kind = %s", be.Kind)
			}
		})
	}
}
