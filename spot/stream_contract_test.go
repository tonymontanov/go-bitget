/*
FILE: spot/stream_contract_test.go

DESCRIPTION:
End-to-end stream tests against a mock Bitget V2 WS server. The mock
implements just enough of the protocol for the SDK to subscribe and
receive push frames:

  - upgrade to a TEXT-frame WS;
  - reply "pong" to plain-text "ping";
  - acknowledge subscribe/unsubscribe ops;
  - ship caller-injected push frames on demand.

Coverage:

  - Every Watch* call subscribes with instType="SPOT" (the regression
    guard against accidentally re-using the mix product type).
  - WatchOrderbook delivers a snapshot through the user handler and
    properly applies a delta on top.
  - WatchOrderbook surfaces a CRC mismatch through errHandler and
    triggers an Unsubscribe→Subscribe resync round-trip.
  - WatchTicker decodes the spot-specific 24h roll-up fields
    (open24h / high24h / low24h / change24h / ...) — and does NOT
    materialise mark/index/funding fields (those have no equivalent
    on the spot wire).
  - WatchTrades fans batches out and normalises buy/sell sides.
  - WatchKline shape conversion matches the same 7-element wire
    array the mix profile uses.
  - Client-side validation (empty symbol, nil handler, empty
    timeframe) returns ErrorKindInvalidRequest before touching the
    network.
*/

package spot

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
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// ---------------------------------------------------------------------
// Mock Bitget V2 public-WS endpoint.
// ---------------------------------------------------------------------
//
// Structurally identical to mix/stream_contract_test.go's mock — the
// public endpoint is byte-for-byte the same protocol across products,
// so the harness is too. We keep an in-package copy rather than
// extracting a shared test helper because:
//
//   - Tests across packages can't share *_test.go files (Go test rule);
//   - the mock is small (~100 lines) and stable;
//   - extracting it to a non-test package would expose a public
//     surface no production code needs.

type streamMockServer struct {
	t      *testing.T
	srv    *httptest.Server
	upgr   websocket.Upgrader
	subs   chan map[string]string
	unsubs chan map[string]string

	mu      sync.Mutex
	conns   []*websocket.Conn
	writeMu sync.Mutex
}

func newStreamMockServer(t *testing.T) *streamMockServer {
	t.Helper()
	var m *streamMockServer = &streamMockServer{
		t:      t,
		upgr:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		subs:   make(chan map[string]string, 32),
		unsubs: make(chan map[string]string, 32),
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *streamMockServer) wsURL() string {
	return "ws" + strings.TrimPrefix(m.srv.URL, "http")
}

func (m *streamMockServer) close() {
	m.srv.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	var i int
	for i = 0; i < len(m.conns); i++ {
		_ = m.conns[i].Close()
	}
}

func (m *streamMockServer) activeConn() *websocket.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.conns) == 0 {
		return nil
	}
	return m.conns[len(m.conns)-1]
}

func (m *streamMockServer) handle(w http.ResponseWriter, r *http.Request) {
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
				var ack = map[string]any{
					"event": "subscribe",
					"arg":   op.Args[i],
					"code":  "0",
				}
				var raw []byte
				raw, err = codec.Marshal(ack)
				if err == nil {
					m.writeMu.Lock()
					_ = conn.WriteMessage(websocket.TextMessage, raw)
					m.writeMu.Unlock()
				}
			}
		case "unsubscribe":
			var i int
			for i = 0; i < len(op.Args); i++ {
				m.unsubs <- op.Args[i]
				var ack = map[string]any{
					"event": "unsubscribe",
					"arg":   op.Args[i],
					"code":  "0",
				}
				var raw []byte
				raw, err = codec.Marshal(ack)
				if err == nil {
					m.writeMu.Lock()
					_ = conn.WriteMessage(websocket.TextMessage, raw)
					m.writeMu.Unlock()
				}
			}
		}
	}
}

// pushFrame sends a single push frame to the most recently opened
// socket. instType is hard-coded to "SPOT" because that's what the
// SDK subscribes with on this profile and the dispatcher expects a
// matching arg key.
func (m *streamMockServer) pushFrame(t *testing.T, action, channel, instID string, data any, tsMs int64) {
	t.Helper()
	var conn *websocket.Conn = m.activeConn()
	if conn == nil {
		t.Fatalf("no active connection")
	}
	var arg = map[string]string{
		"instType": "SPOT",
		"channel":  channel,
		"instId":   instID,
	}
	var frame = map[string]any{
		"action": action,
		"arg":    arg,
		"data":   data,
		"ts":     tsMs,
	}
	var raw []byte
	var err error
	raw, err = codec.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if err = conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// makeStreamClient wires a spot.Client whose StreamClient points at
// the given mock URL. Reconnect knobs are tightened so tests stay
// snappy.
func makeStreamClient(t *testing.T, mock *streamMockServer) *Client {
	t.Helper()
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.WS.PublicURL = mock.wsURL()
	cfg.WS.HandshakeTimeout = 500 * time.Millisecond
	cfg.WS.ReadTimeout = 500 * time.Millisecond
	cfg.WS.WriteTimeout = 500 * time.Millisecond
	cfg.WS.PingInterval = 5 * time.Second
	cfg.WS.LoginTimeout = 500 * time.Millisecond
	cfg.WS.ReconnectInitialBackoff = 10 * time.Millisecond
	cfg.WS.ReconnectMaxBackoff = 50 * time.Millisecond
	cfg.WS.ReconnectJitter = 0
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	return NewClient(parent)
}

// waitFor polls fn() every 10ms until it returns true or timeout.
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
// WatchOrderbook.
// ---------------------------------------------------------------------

func TestContract_Spot_WatchOrderbook_SnapshotAndUpdate(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var snaps []roottypes.OrderBookSnapshot
	var snapsMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchOrderbook(ctx, "BTCUSDT",
		func(ob roottypes.OrderBookSnapshot) {
			snapsMu.Lock()
			snaps = append(snaps, ob)
			snapsMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchOrderbook: %v", err)
	}

	// Verify the subscribe arg lands with instType="SPOT" — the
	// regression guard preventing a future hand-edit from copy-
	// pasting the mix product type into spot.
	select {
	case got := <-mock.subs:
		if got["instType"] != "SPOT" {
			t.Fatalf("instType = %q (want SPOT)", got["instType"])
		}
		if got["channel"] != "books" || got["instId"] != "BTCUSDT" {
			t.Fatalf("unexpected subscribe arg: %#v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("subscribe not received by mock")
	}

	mock.pushFrame(t, "snapshot", "books", "BTCUSDT",
		[]map[string]any{{
			"asks":     [][]string{{"50001", "1.5"}, {"50002", "2.0"}},
			"bids":     [][]string{{"49999", "1.0"}, {"49998", "0.5"}},
			"checksum": 0,
			"ts":       "1700000000000",
		}}, 1700000000000)

	waitFor(t, time.Second, func() bool {
		snapsMu.Lock()
		defer snapsMu.Unlock()
		return len(snaps) == 1
	})
	snapsMu.Lock()
	var first roottypes.OrderBookSnapshot = snaps[0]
	snapsMu.Unlock()
	if len(first.Asks) != 2 || first.Asks[0].Price.String() != "50001" {
		t.Fatalf("snapshot asks: %#v", first.Asks)
	}
	if len(first.Bids) != 2 || first.Bids[0].Price.String() != "49999" {
		t.Fatalf("snapshot bids: %#v", first.Bids)
	}

	// Push a delta: remove bid 49998, add ask 50003.
	mock.pushFrame(t, "update", "books", "BTCUSDT",
		[]map[string]any{{
			"asks":     [][]string{{"50003", "1.0"}},
			"bids":     [][]string{{"49998", "0"}},
			"checksum": 0,
			"ts":       "1700000000050",
		}}, 1700000000050)

	waitFor(t, time.Second, func() bool {
		snapsMu.Lock()
		defer snapsMu.Unlock()
		return len(snaps) == 2
	})
	snapsMu.Lock()
	var second roottypes.OrderBookSnapshot = snaps[1]
	snapsMu.Unlock()
	if len(second.Asks) != 3 {
		t.Fatalf("after delta asks=%d", len(second.Asks))
	}
	if len(second.Bids) != 1 || second.Bids[0].Price.String() != "49999" {
		t.Fatalf("after delta bids: %#v", second.Bids)
	}
}

func TestContract_Spot_WatchOrderbook_ChecksumMismatchTriggersResync(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var errs []error
	var errsMu sync.Mutex

	var err error = c.Stream().WatchOrderbook(ctx, "BTCUSDT",
		func(ob roottypes.OrderBookSnapshot) {},
		func(e error) {
			errsMu.Lock()
			errs = append(errs, e)
			errsMu.Unlock()
		},
	)
	if err != nil {
		t.Fatalf("WatchOrderbook: %v", err)
	}
	<-mock.subs

	// Snapshot with no checksum — engine accepts; not dirty afterwards.
	mock.pushFrame(t, "snapshot", "books", "BTCUSDT",
		[]map[string]any{{
			"asks":     [][]string{{"50001", "1.5"}},
			"bids":     [][]string{{"49999", "1.0"}},
			"checksum": 0,
			"ts":       "1700000000000",
		}}, 1700000000000)

	// Update with a deliberately-wrong checksum.
	mock.pushFrame(t, "update", "books", "BTCUSDT",
		[]map[string]any{{
			"asks":     [][]string{{"50002", "1.0"}},
			"bids":     [][]string{},
			"checksum": 12345,
			"ts":       "1700000000050",
		}}, 1700000000050)

	// Expect: an unsubscribe followed by a fresh subscribe.
	select {
	case <-mock.unsubs:
	case <-time.After(time.Second):
		t.Fatalf("resync did not unsubscribe")
	}
	select {
	case <-mock.subs:
	case <-time.After(time.Second):
		t.Fatalf("resync did not re-subscribe")
	}

	errsMu.Lock()
	defer errsMu.Unlock()
	if len(errs) == 0 {
		t.Fatalf("errHandler not called on checksum mismatch")
	}
}

// ---------------------------------------------------------------------
// WatchTicker — spot-specific 24h roll-ups.
// ---------------------------------------------------------------------

func TestContract_Spot_WatchTicker_FieldMapping(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan spottypes.MarketTicker, 1)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchTicker(ctx, "BTCUSDT",
		func(tk spottypes.MarketTicker) {
			select {
			case got <- tk:
			default:
			}
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	select {
	case sub := <-mock.subs:
		if sub["instType"] != "SPOT" {
			t.Fatalf("instType = %q (want SPOT)", sub["instType"])
		}
		if sub["channel"] != "ticker" {
			t.Fatalf("channel = %q", sub["channel"])
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "ticker", "BTCUSDT",
		[]map[string]any{{
			"instId":       "BTCUSDT",
			"lastPr":       "50000.5",
			"open24h":      "49500",
			"high24h":      "50500",
			"low24h":       "49000",
			"openUtc":      "49800",
			"change24h":    "0.0101",
			"changeUtc24h": "0.0040",
			"bidPr":        "50000",
			"bidSz":        "1.2",
			"askPr":        "50001",
			"askSz":        "0.7",
			"baseVolume":   "1234.5",
			"quoteVolume":  "61725000",
			"usdtVolume":   "61725000",
			"ts":           "1700000000000",
		}}, 1700000000000)

	select {
	case tk := <-got:
		if tk.Symbol != "BTCUSDT" {
			t.Fatalf("symbol = %q", tk.Symbol)
		}
		if tk.LastPrice.String() != "50000.5" {
			t.Fatalf("last = %s", tk.LastPrice.String())
		}
		if tk.High24h.String() != "50500" {
			t.Fatalf("high24h = %s", tk.High24h.String())
		}
		if tk.Low24h.String() != "49000" {
			t.Fatalf("low24h = %s", tk.Low24h.String())
		}
		if tk.Open.String() != "49500" {
			t.Fatalf("open24h = %s", tk.Open.String())
		}
		if tk.OpenUtc.String() != "49800" {
			t.Fatalf("openUtc = %s", tk.OpenUtc.String())
		}
		if tk.Change24h.String() != "0.0101" {
			t.Fatalf("change24h = %s", tk.Change24h.String())
		}
		if tk.ChangeUtc24h.String() != "0.004" {
			t.Fatalf("changeUtc24h = %s", tk.ChangeUtc24h.String())
		}
		if tk.AskPrice.String() != "50001" || tk.AskSize.String() != "0.7" {
			t.Fatalf("ask = %s / size %s", tk.AskPrice, tk.AskSize)
		}
		if tk.BidPrice.String() != "50000" || tk.BidSize.String() != "1.2" {
			t.Fatalf("bid = %s / size %s", tk.BidPrice, tk.BidSize)
		}
		if tk.BaseVolume.String() != "1234.5" {
			t.Fatalf("baseVolume = %s", tk.BaseVolume.String())
		}
		if tk.QuoteVolume.String() != "61725000" {
			t.Fatalf("quoteVolume = %s", tk.QuoteVolume.String())
		}
		if tk.UsdtVolume.String() != "61725000" {
			t.Fatalf("usdtVolume = %s", tk.UsdtVolume.String())
		}
		if tk.TsMs != 1700000000000 {
			t.Fatalf("ts = %d", tk.TsMs)
		}
	case <-time.After(time.Second):
		t.Fatalf("ticker handler not invoked")
	}
}

// ---------------------------------------------------------------------
// WatchTrades — fan-out + side normalisation.
// ---------------------------------------------------------------------

func TestContract_Spot_WatchTrades_FanOutAndSideMapping(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got []roottypes.TradeUpdate
	var gotMu sync.Mutex

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchTrades(ctx, "BTCUSDT",
		func(u roottypes.TradeUpdate) {
			gotMu.Lock()
			got = append(got, u)
			gotMu.Unlock()
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchTrades: %v", err)
	}
	select {
	case sub := <-mock.subs:
		if sub["instType"] != "SPOT" {
			t.Fatalf("instType = %q (want SPOT)", sub["instType"])
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "trade", "BTCUSDT",
		[]map[string]any{
			{"ts": "1700000000000", "price": "50000", "size": "0.001", "side": "buy", "tradeId": "t1"},
			{"ts": "1700000000050", "price": "50001", "size": "0.002", "side": "sell", "tradeId": "t2"},
		}, 1700000000000)

	waitFor(t, time.Second, func() bool {
		gotMu.Lock()
		defer gotMu.Unlock()
		return len(got) == 2
	})
	gotMu.Lock()
	defer gotMu.Unlock()
	if got[0].Side != roottypes.SideTypeBuy || got[0].TradeID != "t1" {
		t.Fatalf("trade[0]: %#v", got[0])
	}
	if got[0].Price.String() != "50000" || got[0].Size.String() != "0.001" {
		t.Fatalf("trade[0] price/size: %s/%s", got[0].Price.String(), got[0].Size.String())
	}
	if got[1].Side != roottypes.SideTypeSell || got[1].TradeID != "t2" {
		t.Fatalf("trade[1]: %#v", got[1])
	}
}

// ---------------------------------------------------------------------
// WatchKline — 7-element row decoding.
// ---------------------------------------------------------------------

func TestContract_Spot_WatchKline_RowDecoding(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()

	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	var got = make(chan roottypes.KlineUpdate, 4)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var err error = c.Stream().WatchKline(ctx, "BTCUSDT", roottypes.Timeframe1m,
		func(u roottypes.KlineUpdate) {
			select {
			case got <- u:
			default:
			}
		},
		nil,
	)
	if err != nil {
		t.Fatalf("WatchKline: %v", err)
	}
	select {
	case sub := <-mock.subs:
		if sub["instType"] != "SPOT" {
			t.Fatalf("instType = %q (want SPOT)", sub["instType"])
		}
		if sub["channel"] != "candle1m" {
			t.Fatalf("channel = %q", sub["channel"])
		}
	case <-time.After(time.Second):
		t.Fatalf("subscribe not received")
	}

	mock.pushFrame(t, "snapshot", "candle1m", "BTCUSDT",
		[][]string{
			{"1700000000000", "50000", "50100", "49900", "50050", "10.5", "525000"},
		}, 1700000000000)

	select {
	case u := <-got:
		if u.Symbol != "BTCUSDT" {
			t.Fatalf("symbol = %q", u.Symbol)
		}
		if u.Interval != roottypes.Timeframe1m {
			t.Fatalf("interval = %q", u.Interval)
		}
		if u.StartMs != 1700000000000 {
			t.Fatalf("start = %d", u.StartMs)
		}
		if u.Open.String() != "50000" || u.High.String() != "50100" || u.Low.String() != "49900" || u.Close.String() != "50050" {
			t.Fatalf("ohlc: %s/%s/%s/%s", u.Open, u.High, u.Low, u.Close)
		}
		if u.Volume.String() != "10.5" || u.Turnover.String() != "525000" {
			t.Fatalf("vol/turnover: %s/%s", u.Volume, u.Turnover)
		}
	case <-time.After(time.Second):
		t.Fatalf("kline handler not invoked")
	}
}

// ---------------------------------------------------------------------
// Client-side validation.
// ---------------------------------------------------------------------

func TestContract_Spot_StreamValidation(t *testing.T) {
	var mock *streamMockServer = newStreamMockServer(t)
	defer mock.close()
	var c *Client = makeStreamClient(t, mock)
	defer func() { _ = c.Stream().Close() }()

	type tc struct {
		name string
		run  func() error
	}
	var cases = []tc{
		{"orderbook empty symbol", func() error {
			return c.Stream().WatchOrderbook(context.Background(), "", func(roottypes.OrderBookSnapshot) {}, nil)
		}},
		{"orderbook nil handler", func() error {
			return c.Stream().WatchOrderbook(context.Background(), "BTCUSDT", nil, nil)
		}},
		{"ticker empty symbol", func() error {
			return c.Stream().WatchTicker(context.Background(), "", func(spottypes.MarketTicker) {}, nil)
		}},
		{"ticker nil handler", func() error {
			return c.Stream().WatchTicker(context.Background(), "BTCUSDT", nil, nil)
		}},
		{"trades empty symbol", func() error {
			return c.Stream().WatchTrades(context.Background(), "", func(roottypes.TradeUpdate) {}, nil)
		}},
		{"trades nil handler", func() error {
			return c.Stream().WatchTrades(context.Background(), "BTCUSDT", nil, nil)
		}},
		{"kline empty symbol", func() error {
			return c.Stream().WatchKline(context.Background(), "", roottypes.Timeframe1m, func(roottypes.KlineUpdate) {}, nil)
		}},
		{"kline nil handler", func() error {
			return c.Stream().WatchKline(context.Background(), "BTCUSDT", roottypes.Timeframe1m, nil, nil)
		}},
		{"kline empty timeframe", func() error {
			return c.Stream().WatchKline(context.Background(), "BTCUSDT", "", func(roottypes.KlineUpdate) {}, nil)
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

// asBitgetError unwraps err into *bitget.Error. Tiny helper used only
// by the validation table above to keep cases compact.
func asBitgetError(err error, dst **bitget.Error) bool {
	if be, ok := err.(*bitget.Error); ok {
		*dst = be
		return true
	}
	return false
}
