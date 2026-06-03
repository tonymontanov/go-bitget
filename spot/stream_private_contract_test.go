/*
FILE: spot/stream_private_contract_test.go

DESCRIPTION:
End-to-end tests for the spot private streams (M5 ships only the
"orders" channel — see stream-private.go). The mock exposed by
stream_contract_test.go already understands {"op":"login",...}, so
these tests only need to plug into the same infrastructure and
exercise the field-mapping for the orders channel plus the private-
side guards (auth required, instId="default" enforcement, symbol
filter, validation).
*/

package spot

import (
	"context"
	"sync"
	"testing"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
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

func TestContract_Spot_WatchOrders_FieldMapping(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan spottypes.OrderInfo, 4)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "BTCUSDT",
		func(o spottypes.OrderInfo) {
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
		// CRITICAL CONTRACT (mirrors mix regression):
		// Bitget V2 private orders channel rejects per-symbol
		// subscriptions with code=30001. The SDK MUST always
		// subscribe with instType=SPOT, channel=orders, instId=default;
		// per-symbol filtering happens client-side inside
		// handleOrdersFrame.
		if sub["instType"] != SpotInstType {
			t.Fatalf("instType: expected %q, got %#v", SpotInstType, sub)
		}
		if sub["channel"] != channelOrders || sub["instId"] != instIDDefaultPrivate {
			t.Fatalf("unexpected subscribe arg (expected channel=orders, instId=default): %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", channelOrders, instIDDefaultPrivate,
		[]map[string]any{{
			"instId":        "BTCUSDT",
			"orderId":       "ord-1",
			"clientOid":     "cli-1",
			"side":          "buy",
			"orderType":     "limit",
			"force":         "gtc",
			"status":        "partially_filled",
			"size":          "0.01",
			"price":         "50000",
			"accBaseVolume": "0.003",
			"priceAvg":      "50001",
			"fee":           "0.0001",
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
		if string(o.OrderType) != "limit" || string(o.TimeInForce) != "gtc" {
			t.Fatalf("orderType/force: %s/%s", o.OrderType, o.TimeInForce)
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

// TestContract_Spot_WatchOrders_FilterDropsForeignSymbol locks down
// the symbol-filter behaviour: when the caller subscribes for
// "BTCUSDT" the SDK still subscribes globally with instId="default",
// but rows for an unrelated symbol must NOT reach the user handler.
func TestContract_Spot_WatchOrders_FilterDropsForeignSymbol(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []spottypes.OrderInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "BTCUSDT",
		func(o spottypes.OrderInfo) {
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

	// One push carrying TWO rows: the requested symbol and a
	// foreign one. Only the requested one should reach the handler.
	mock.pushFrame(t, "snapshot", channelOrders, instIDDefaultPrivate,
		[]map[string]any{
			{
				"instId":    "ETHUSDT",
				"orderId":   "ord-eth",
				"clientOid": "cli-eth",
				"side":      "buy",
				"orderType": "limit",
				"force":     "gtc",
				"status":    "live",
				"size":      "0.1",
				"price":     "3000",
				"cTime":     "1700000000000",
				"uTime":     "1700000000010",
			},
			{
				"instId":    "BTCUSDT",
				"orderId":   "ord-btc",
				"clientOid": "cli-btc",
				"side":      "sell",
				"orderType": "limit",
				"force":     "gtc",
				"status":    "live",
				"size":      "0.01",
				"price":     "50000",
				"cTime":     "1700000000000",
				"uTime":     "1700000000010",
			},
		}, 1700000000010)

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
	if got[0].OrderID != "ord-btc" {
		t.Fatalf("expected ord-btc, got %q", got[0].OrderID)
	}
}

// TestContract_Spot_WatchOrders_DefaultSymbolReceivesAll covers the
// caller-friendly opt-out: pass "default" → no filter, all rows.
func TestContract_Spot_WatchOrders_DefaultSymbolReceivesAll(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []spottypes.OrderInfo
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, instIDDefaultPrivate,
		func(o spottypes.OrderInfo) {
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

	mock.pushFrame(t, "snapshot", channelOrders, instIDDefaultPrivate,
		[]map[string]any{
			{"instId": "ETHUSDT", "orderId": "ord-eth", "clientOid": "cli-eth", "side": "buy", "orderType": "limit", "force": "gtc", "status": "live", "size": "0.1", "price": "3000", "cTime": "1700000000000", "uTime": "1700000000010"},
			{"instId": "BTCUSDT", "orderId": "ord-btc", "clientOid": "cli-btc", "side": "sell", "orderType": "limit", "force": "gtc", "status": "live", "size": "0.01", "price": "50000", "cTime": "1700000000000", "uTime": "1700000000010"},
		}, 1700000000010)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) == 2
	})
}

// TestContract_Spot_WatchOrders_AcceptsNumericFields locks down the
// flexString contract on the spot side, mirroring the mix
// PARTIUSDT regression captured in 2026-05-27 prod logs (Bitget V2
// occasionally ships numeric fields as JSON numbers instead of the
// documented quoted strings — the strict string-typed wire row
// would abort the entire push).
func TestContract_Spot_WatchOrders_AcceptsNumericFields(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan spottypes.OrderInfo, 1)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "PEPEUSDT",
		func(o spottypes.OrderInfo) {
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

	// Every numeric field below is sent as a JSON number, NOT a
	// quoted string. flexString must accept both shapes.
	mock.pushFrame(t, "snapshot", channelOrders, instIDDefaultPrivate,
		[]map[string]any{{
			"instId":        "PEPEUSDT",
			"orderId":       "ord-1",
			"clientOid":     "cli-1",
			"side":          "buy",
			"orderType":     "limit",
			"force":         "gtc",
			"status":        "live",
			"size":          1234567.89,
			"price":         0.0000012,
			"accBaseVolume": 0,
			"priceAvg":      0,
			"fee":           0,
			"cTime":         1779897892000,
			"uTime":         1779897893000,
		}}, 1779897893000)

	select {
	case o := <-got:
		if o.Symbol != "PEPEUSDT" {
			t.Fatalf("symbol: %q", o.Symbol)
		}
		if o.Quantity.String() != "1234567.89" {
			t.Fatalf("size: %s", o.Quantity)
		}
		if o.Price.String() != "0.0000012" {
			t.Fatalf("price: %s", o.Price)
		}
		if o.UpdatedAtMs != 1779897893000 {
			t.Fatalf("uTime: %d", o.UpdatedAtMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("orders handler not invoked (numeric-field regression)")
	}
}

// ---------------------------------------------------------------------
// Auth guard.
// ---------------------------------------------------------------------

func TestContract_Spot_PrivateChannels_RequireSigner(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()
	// makeStreamClient leaves credentials empty → signer disabled.
	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var err error = c.Stream().WatchOrders(context.Background(), "BTCUSDT",
		func(spottypes.OrderInfo) {}, nil)
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
}

// ---------------------------------------------------------------------
// Validation cases.
// ---------------------------------------------------------------------

func TestContract_Spot_StreamPrivateValidation(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()
	var c *Client = makePrivateStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	type tc struct {
		name string
		run  func() error
	}
	var cases = []tc{
		{"empty symbol", func() error {
			return c.Stream().WatchOrders(context.Background(), "",
				func(spottypes.OrderInfo) {}, nil)
		}},
		{"nil handler", func() error {
			return c.Stream().WatchOrders(context.Background(), "BTCUSDT", nil, nil)
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
