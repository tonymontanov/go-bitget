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
		if sub["channel"] != "orders" || sub["instId"] != "BTCUSDT" {
			t.Fatalf("unexpected subscribe arg: %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "orders", "BTCUSDT",
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
		if sub["channel"] != "positions" || sub["instId"] != "BTCUSDT" {
			t.Fatalf("unexpected subscribe arg: %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "USDT-FUTURES", "positions", "BTCUSDT",
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
