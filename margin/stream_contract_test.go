/*
FILE: margin/stream_contract_test.go

DESCRIPTION:
End-to-end tests for the MARGIN private WS streams against a mock
Bitget V2 WS server. The mock implements just enough of the protocol
for the SDK to log in, subscribe and receive push frames.

KEY INVARIANTS:

  - channel names carry the crossed/isolated suffix matching the
    client's pinned mode (orders-crossed / account-isolated / ...);
  - instType is always "MARGIN";
  - orders subscribe with instId="default", account with coin="default"
    (per-symbol / per-coin filtering is client-side);
  - field mapping (loanType, baseVolume, fee aggregation, balances);
  - private channels require API credentials (ErrorKindAuth);
  - validation (empty symbol / nil handler) is pre-flight.
*/

package margin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ---------------------------------------------------------------------
// Mock Bitget V2 private-WS endpoint (instType="MARGIN").
// ---------------------------------------------------------------------

type wsMock struct {
	t      *testing.T
	srv    *httptest.Server
	upgr   websocket.Upgrader
	subs   chan map[string]string
	unsubs chan map[string]string

	mu      sync.Mutex
	conns   []*websocket.Conn
	writeMu sync.Mutex
}

func newWSMock(t *testing.T) *wsMock {
	t.Helper()
	var m *wsMock = &wsMock{
		t:      t,
		upgr:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		subs:   make(chan map[string]string, 32),
		unsubs: make(chan map[string]string, 32),
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *wsMock) wsURL() string { return "ws" + strings.TrimPrefix(m.srv.URL, "http") }

func (m *wsMock) close() {
	m.srv.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	var i int
	for i = 0; i < len(m.conns); i++ {
		_ = m.conns[i].Close()
	}
}

func (m *wsMock) activeConn() *websocket.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.conns) == 0 {
		return nil
	}
	return m.conns[len(m.conns)-1]
}

func (m *wsMock) handle(w http.ResponseWriter, r *http.Request) {
	var conn *websocket.Conn
	var err error
	conn, err = m.upgr.Upgrade(w, r, nil)
	if err != nil {
		m.t.Errorf("upgrade: %v", err)
		return
	}
	m.mu.Lock()
	m.conns = append(m.conns, conn)
	m.mu.Unlock()
	for {
		var msgType int
		var body []byte
		msgType, body, err = conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.TextMessage {
			continue
		}
		if string(body) == "ping" {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("pong"))
			continue
		}
		if strings.HasPrefix(string(body), `{"op":"login"`) {
			m.writeMu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"login","code":"0"}`))
			m.writeMu.Unlock()
			continue
		}
		var op struct {
			Op   string              `json:"op"`
			Args []map[string]string `json:"args"`
		}
		if err = codec.Unmarshal(body, &op); err != nil {
			continue
		}
		switch op.Op {
		case "subscribe":
			var i int
			for i = 0; i < len(op.Args); i++ {
				m.subs <- op.Args[i]
				m.ack(conn, "subscribe", op.Args[i])
			}
		case "unsubscribe":
			var i int
			for i = 0; i < len(op.Args); i++ {
				m.unsubs <- op.Args[i]
				m.ack(conn, "unsubscribe", op.Args[i])
			}
		}
	}
}

func (m *wsMock) ack(conn *websocket.Conn, event string, arg map[string]string) {
	var raw []byte
	var err error
	raw, err = codec.Marshal(map[string]any{"event": event, "arg": arg, "code": "0"})
	if err != nil {
		return
	}
	m.writeMu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, raw)
	m.writeMu.Unlock()
}

// pushFrame ships one push frame keyed by instId. instType is hard-
// coded to "MARGIN".
func (m *wsMock) pushFrame(t *testing.T, action, channel, instID string, data any, tsMs int64) {
	t.Helper()
	m.pushFrameKeyed(t, action, channel, instID, "", data, tsMs)
}

// pushFrameKeyed ships one push frame; coin is set when non-empty (used
// by the account channel which the registry keys by coin).
func (m *wsMock) pushFrameKeyed(t *testing.T, action, channel, instID, coin string, data any, tsMs int64) {
	t.Helper()
	var conn *websocket.Conn = m.activeConn()
	if conn == nil {
		t.Fatalf("no active connection")
	}
	var arg = map[string]string{"instType": MarginInstType, "channel": channel}
	if instID != "" {
		arg["instId"] = instID
	}
	if coin != "" {
		arg["coin"] = coin
	}
	var raw []byte
	var err error
	raw, err = codec.Marshal(map[string]any{"action": action, "arg": arg, "data": data, "ts": tsMs})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if err = conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// makeStreamClient wires a margin.Client (pinned to mode) whose private
// StreamClient points at the mock URL and carries API credentials so
// signerEnabled() is true.
func makeStreamClient(t *testing.T, mock *wsMock, mode roottypes.MarginMode, withCreds bool) *Client {
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
	if withCreds {
		cfg.APIKey = "k"
		cfg.SecretKey = "s"
		cfg.Passphrase = "p"
	}
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	return NewClientWithMode(parent, mode)
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	var deadline time.Time = time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

// ---------------------------------------------------------------------
// WatchOrders.
// ---------------------------------------------------------------------

func TestContract_Margin_WatchOrders_FieldMapping(t *testing.T) {
	var mock *wsMock = newWSMock(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock, roottypes.MarginModeCrossed, true)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan margintypes.OrderInfo, 4)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "BTCUSDT",
		func(o margintypes.OrderInfo) {
			select {
			case got <- o:
			default:
			}
		}, nil)
	if err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}

	select {
	case sub := <-mock.subs:
		if sub["instType"] != MarginInstType {
			t.Fatalf("instType: %#v", sub)
		}
		if sub["channel"] != "orders-crossed" || sub["instId"] != "default" {
			t.Fatalf("unexpected subscribe arg (want channel=orders-crossed, instId=default): %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "orders-crossed", "default",
		[]map[string]any{{
			"instId":     "BTCUSDT",
			"symbol":     "BTCUSDT",
			"orderId":    "ord-1",
			"clientOid":  "cli-1",
			"side":       "buy",
			"orderType":  "limit",
			"force":      "gtc",
			"status":     "partially_filled",
			"loanType":   "autoLoan",
			"price":      "50000",
			"baseSize":   "0.01",
			"baseVolume": "0.003",
			"priceAvg":   "50001",
			"feeDetail": []map[string]any{{
				"feeCoin":           "USDT",
				"deduction":         "no",
				"totalDeductionFee": "0",
				"totalFee":          "-0.15",
			}},
			"cTime": "1700000000000",
			"uTime": "1700000000050",
		}}, 1700000000050)

	select {
	case o := <-got:
		if o.OrderID != "ord-1" || o.ClientOrderID != "cli-1" {
			t.Fatalf("ids: %#v", o)
		}
		if o.Symbol != "BTCUSDT" || o.Side != roottypes.SideTypeBuy {
			t.Fatalf("symbol/side: %#v", o)
		}
		if o.LoanType != margintypes.LoanTypeAutoLoan {
			t.Fatalf("loanType: %q", o.LoanType)
		}
		if o.Quantity.String() != "0.01" || o.Price.String() != "50000" {
			t.Fatalf("qty/price: %s/%s", o.Quantity, o.Price)
		}
		if o.FilledQuantity.String() != "0.003" || o.AvgFilledPrice.String() != "50001" {
			t.Fatalf("filled/avg: %s/%s", o.FilledQuantity, o.AvgFilledPrice)
		}
		if o.CumFee.String() != "-0.15" {
			t.Fatalf("cumFee: %s", o.CumFee)
		}
		if o.CreatedAtMs != 1700000000000 || o.UpdatedAtMs != 1700000000050 {
			t.Fatalf("ts: %d/%d", o.CreatedAtMs, o.UpdatedAtMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("orders handler not invoked")
	}
}

func TestContract_Margin_WatchOrders_FilterDropsForeignSymbol(t *testing.T) {
	var mock *wsMock = newWSMock(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock, roottypes.MarginModeIsolated, true)
	defer func() { _ = c.Stream().Close() }()

	var got []margintypes.OrderInfo
	var gotMu sync.Mutex
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrders(ctx, "BTCUSDT",
		func(o margintypes.OrderInfo) {
			gotMu.Lock()
			got = append(got, o)
			gotMu.Unlock()
		}, nil)
	if err != nil {
		t.Fatalf("WatchOrders: %v", err)
	}
	select {
	case sub := <-mock.subs:
		if sub["channel"] != "orders-isolated" {
			t.Fatalf("channel: %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "orders-isolated", "default",
		[]map[string]any{
			{"symbol": "ETHUSDT", "orderId": "o-eth", "side": "buy", "orderType": "limit", "force": "gtc", "status": "live", "baseSize": "0.1", "price": "3000", "cTime": "1", "uTime": "2"},
			{"symbol": "BTCUSDT", "orderId": "o-btc", "side": "sell", "orderType": "limit", "force": "gtc", "status": "live", "baseSize": "0.01", "price": "50000", "cTime": "1", "uTime": "2"},
		}, 2)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) >= 1
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 || got[0].Symbol != "BTCUSDT" || got[0].OrderID != "o-btc" {
		t.Fatalf("filter regression: %#v", got)
	}
}

// ---------------------------------------------------------------------
// WatchAccount.
// ---------------------------------------------------------------------

func TestContract_Margin_WatchAccount_FieldMapping(t *testing.T) {
	var mock *wsMock = newWSMock(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock, roottypes.MarginModeIsolated, true)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan margintypes.AccountUpdate, 8)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchAccount(ctx, "",
		func(b margintypes.AccountUpdate) {
			select {
			case got <- b:
			default:
			}
		}, nil)
	if err != nil {
		t.Fatalf("WatchAccount: %v", err)
	}

	select {
	case sub := <-mock.subs:
		if sub["channel"] != "account-isolated" || sub["coin"] != "default" {
			t.Fatalf("unexpected subscribe arg (want channel=account-isolated, coin=default): %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrameKeyed(t, "snapshot", "account-isolated", "", "default",
		[]map[string]any{{
			"coin":      "USDT",
			"symbol":    "BTCUSDT",
			"available": "9607383.17",
			"frozen":    "0",
			"borrow":    "150",
			"interest":  "0.5",
			"coupon":    "0",
			"uTime":     "1700000000050",
		}}, 1700000000050)

	select {
	case b := <-got:
		if b.Coin != "USDT" || b.Symbol != "BTCUSDT" {
			t.Fatalf("coin/symbol: %#v", b)
		}
		if b.Available.String() != "9607383.17" {
			t.Fatalf("available: %s", b.Available)
		}
		if b.Borrow.String() != "150" || b.Interest.String() != "0.5" {
			t.Fatalf("borrow/interest: %s/%s", b.Borrow, b.Interest)
		}
		if b.UpdatedAtMs != 1700000000050 {
			t.Fatalf("uTime: %d", b.UpdatedAtMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("account handler not invoked")
	}
}

func TestContract_Margin_WatchAccount_FilterDropsForeignCoin(t *testing.T) {
	var mock *wsMock = newWSMock(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock, roottypes.MarginModeCrossed, true)
	defer func() { _ = c.Stream().Close() }()

	var got []margintypes.AccountUpdate
	var gotMu sync.Mutex
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchAccount(ctx, "USDT",
		func(b margintypes.AccountUpdate) {
			gotMu.Lock()
			got = append(got, b)
			gotMu.Unlock()
		}, nil)
	if err != nil {
		t.Fatalf("WatchAccount: %v", err)
	}
	select {
	case sub := <-mock.subs:
		if sub["channel"] != "account-crossed" {
			t.Fatalf("channel: %#v", sub)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrameKeyed(t, "snapshot", "account-crossed", "", "default",
		[]map[string]any{
			{"coin": "BTC", "available": "0.1", "borrow": "0", "uTime": "1"},
			{"coin": "USDT", "available": "1000", "borrow": "0", "uTime": "1"},
			{"coin": "ETH", "available": "5", "borrow": "0", "uTime": "1"},
		}, 1)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) >= 1
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 || got[0].Coin != "USDT" {
		t.Fatalf("filter regression: %#v", got)
	}
}

// ---------------------------------------------------------------------
// Auth guard + validation.
// ---------------------------------------------------------------------

func TestContract_Margin_PrivateChannels_RequireSigner(t *testing.T) {
	var mock *wsMock = newWSMock(t)
	defer mock.close()
	var c *Client = makeStreamClient(t, mock, roottypes.MarginModeCrossed, false)
	defer func() { _ = c.Stream().Close() }()

	var cases = []struct {
		name string
		run  func() error
	}{
		{"orders", func() error {
			return c.Stream().WatchOrders(context.Background(), "BTCUSDT", func(margintypes.OrderInfo) {}, nil)
		}},
		{"account", func() error {
			return c.Stream().WatchAccount(context.Background(), "", func(margintypes.AccountUpdate) {}, nil)
		}},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var sc = cases[i]
		t.Run(sc.name, func(t *testing.T) {
			var err error = sc.run()
			if !bitget.IsAuth(err) {
				t.Fatalf("want ErrorKindAuth, got %v", err)
			}
		})
	}
}

func TestContract_Margin_StreamValidation(t *testing.T) {
	var mock *wsMock = newWSMock(t)
	defer mock.close()
	var c *Client = makeStreamClient(t, mock, roottypes.MarginModeCrossed, true)
	defer func() { _ = c.Stream().Close() }()

	var cases = []struct {
		name string
		run  func() error
	}{
		{"orders empty symbol", func() error {
			return c.Stream().WatchOrders(context.Background(), "", func(margintypes.OrderInfo) {}, nil)
		}},
		{"orders nil handler", func() error {
			return c.Stream().WatchOrders(context.Background(), "BTCUSDT", nil, nil)
		}},
		{"account nil handler", func() error {
			return c.Stream().WatchAccount(context.Background(), "USDT", nil, nil)
		}},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var sc = cases[i]
		t.Run(sc.name, func(t *testing.T) {
			var err error = sc.run()
			if !bitget.IsInvalidRequest(err) {
				t.Fatalf("want ErrorKindInvalidRequest, got %v", err)
			}
		})
	}
}
