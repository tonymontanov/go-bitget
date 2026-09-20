/*
FILE: uta/stream_contract_test.go

DESCRIPTION:
End-to-end contract tests for the PUBLIC side of uta.StreamClient (and the
fan-out / lifecycle shared with the private side) against a local mock of
the Bitget V3 WebSocket. The mock speaks just enough of the protocol:

  - upgrade to a TEXT-frame WS, reply "pong" to the plain-text "ping";
  - accept op=login and answer {"event":"login","code":"0","msg":""};
  - answer subscribe / unsubscribe with the V3 ack (arg echoed, NO code);
  - record every op IN ORDER, keeping the exact arg bytes, so tests can
    pin the wire contract (lower-case instType, topic / symbol keys,
    "UTA" on private topics, login before subscribe);
  - push caller-supplied frames verbatim and force-close the socket.

FIXTURES are the live frames captured 2026-09-21 from
wss://ws.bitget.com/v3/ws/public, pushed byte-for-byte.
*/

package uta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// ---------------------------------------------------------------------
// Live fixtures (verbatim).
// ---------------------------------------------------------------------

const fixtureTickerFutures = `{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"},"data":[{"highPrice24h":"81470","lowPrice24h":"80092.4","openPrice24h":"81003.7","lastPrice":"80810.6","turnover24h":"1682569476.10781","volume24h":"20843.3982","bid1Price":"80810.6","ask1Price":"80810.7","bid1Size":"0.3743","ask1Size":"1.4785","price24hPcnt":"-0.00238","indexPrice":"80852.148","markPrice":"80814.7","fundingRate":"0.000051","openInterest":"32117.3964999999767","deliveryTime":"","deliveryStartTime":"","deliveryStatus":"","nextFundingTime":"1789948800000"}],"ts":1789940561885}`

const fixtureTickerSpot = `{"action":"snapshot","arg":{"instType":"spot","topic":"ticker","symbol":"BTCUSDT"},"data":[{"highPrice24h":"81500.55","lowPrice24h":"80122.15","openPrice24h":"80910.01","lastPrice":"80855.01","turnover24h":"172381085.901288","volume24h":"2134.512901","bid1Price":"80855","ask1Price":"80855.01","bid1Size":"0.119954","ask1Size":"0.381751","price24hPcnt":"-0.00216"}],"ts":1789940562091}`

const fixtureTradesUpdate = `{"action":"update","arg":{"instType":"usdt-futures","topic":"publicTrade","symbol":"BTCUSDT"},"data":[{"i":"1485683898127761410","p":"80810.6","v":"0.0246","S":"sell","T":"1789940564662","L":"1485683898127761411","isRPI":"no"},{"i":"1485683898127761408","p":"80810.6","v":"0.0716","S":"sell","T":"1789940564662","L":"1485683898127761409","isRPI":"no"}],"ts":1789940564662}`

// The history snapshot the venue sends right after subscribe (shortened
// to two rows — the live one carries ~50).
const fixtureTradesSnapshot = `{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"publicTrade","symbol":"BTCUSDT"},"data":[{"i":"1485683898127761400","p":"80810.1","v":"0.5","S":"buy","T":"1789940564000","L":"1485683898127761401","isRPI":"no"},{"i":"1485683898127761398","p":"80810.0","v":"0.1","S":"sell","T":"1789940563990","L":"1485683898127761399","isRPI":"no"}],"ts":1789940564100}`

const fixtureBooks5 = `{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"},"data":[{"a":[["80810.4","0.8523"],["80810.5","0.0014"],["80810.7","0.0093"],["80811","0.0001"],["80811.1","0.0124"]],"b":[["80810.3","1.0943"],["80810.2","0.1194"],["80809.9","0.007"],["80809.6","0.0061"],["80808.8","0.04"]],"seq":993094633676,"pseq":0,"ts":"1789940582101"}],"ts":1789940582102}`

// ---------------------------------------------------------------------
// Mock V3 WS server.
// ---------------------------------------------------------------------

// wsOp — one op received by the mock. For subscribe / unsubscribe `arg`
// holds the EXACT bytes of one args[] element.
type wsOp struct {
	op  string
	arg string
}

type utaMockServer struct {
	t    *testing.T
	srv  *httptest.Server
	upgr websocket.Upgrader

	mu    sync.Mutex
	conns []*websocket.Conn
	ops   []wsOp

	// writeMu serialises writes (gorilla forbids concurrent writers): the
	// handle() goroutine acks while the test pushes frames.
	writeMu sync.Mutex
}

func newUTAMockServer(t *testing.T) *utaMockServer {
	t.Helper()
	var m *utaMockServer = &utaMockServer{
		t:    t,
		upgr: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.close)
	return m
}

func (m *utaMockServer) wsURL() string {
	return "ws" + strings.TrimPrefix(m.srv.URL, "http")
}

func (m *utaMockServer) close() {
	m.srv.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	var i int
	for i = 0; i < len(m.conns); i++ {
		_ = m.conns[i].Close()
	}
}

func (m *utaMockServer) handle(w http.ResponseWriter, r *http.Request) {
	var conn *websocket.Conn
	var err error
	conn, err = m.upgr.Upgrade(w, r, nil)
	if err != nil {
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
			m.write(conn, "pong")
			continue
		}
		var op struct {
			Op   string          `json:"op"`
			Args []codec.RawJSON `json:"args"`
		}
		if err = codec.Unmarshal(body, &op); err != nil {
			continue
		}
		switch op.Op {
		case "login":
			m.record(wsOp{op: "login"})
			m.write(conn, `{"event":"login","code":"0","msg":""}`)
		case "subscribe", "unsubscribe":
			var i int
			for i = 0; i < len(op.Args); i++ {
				m.record(wsOp{op: op.Op, arg: string(op.Args[i])})
				// V3 ack: arg echoed, connId, NO code (live shape).
				m.write(conn, `{"event":"`+op.Op+`","arg":`+string(op.Args[i])+`,"connId":"mock"}`)
			}
		}
	}
}

func (m *utaMockServer) write(conn *websocket.Conn, text string) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	_ = conn.WriteMessage(websocket.TextMessage, []byte(text))
}

func (m *utaMockServer) record(op wsOp) {
	m.mu.Lock()
	m.ops = append(m.ops, op)
	m.mu.Unlock()
}

// opsSnapshot returns a copy of the op log.
func (m *utaMockServer) opsSnapshot() []wsOp {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []wsOp = make([]wsOp, len(m.ops))
	copy(out, m.ops)
	return out
}

// countOps counts ops of one kind with the exact arg.
func (m *utaMockServer) countOps(op, arg string) int {
	var ops []wsOp = m.opsSnapshot()
	var n int = 0
	var i int
	for i = 0; i < len(ops); i++ {
		if ops[i].op == op && ops[i].arg == arg {
			n++
		}
	}
	return n
}

func (m *utaMockServer) connCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.conns)
}

func (m *utaMockServer) activeConn() *websocket.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.conns) == 0 {
		return nil
	}
	return m.conns[len(m.conns)-1]
}

// push writes one frame verbatim to the most recent socket.
func (m *utaMockServer) push(t *testing.T, frame string) {
	t.Helper()
	var conn *websocket.Conn = m.activeConn()
	if conn == nil {
		t.Fatalf("mock: no active connection")
	}
	m.write(conn, frame)
}

// dropActive force-closes the most recent socket (server-side close).
func (m *utaMockServer) dropActive(t *testing.T) {
	t.Helper()
	var conn *websocket.Conn = m.activeConn()
	if conn == nil {
		t.Fatalf("mock: no active connection")
	}
	_ = conn.Close()
}

// waitOps blocks until the mock saw `want` ops of the given kind / arg.
func (m *utaMockServer) waitOps(t *testing.T, op, arg string, want int) {
	t.Helper()
	waitFor(t, 2*time.Second, op+" "+arg, func() bool { return m.countOps(op, arg) >= want })
}

// ---------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------

func newStreamTestClient(t *testing.T, mock *utaMockServer, withCreds bool) *Client {
	t.Helper()
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.WS.UTAPublicURL = mock.wsURL()
	cfg.WS.UTAPrivateURL = mock.wsURL()
	// The V2 URLs must never be dialled by the UTA stream.
	cfg.WS.PublicURL = "ws://127.0.0.1:1/v2-must-not-be-used"
	cfg.WS.PrivateURL = "ws://127.0.0.1:1/v2-must-not-be-used"
	cfg.WS.HandshakeTimeout = 500 * time.Millisecond
	cfg.WS.ReadTimeout = 2 * time.Second
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
	var c *Client = NewClient(parent)
	t.Cleanup(func() {
		_ = c.Stream().Close()
		_ = parent.Close()
	})
	return c
}

func waitFor(t *testing.T, timeout time.Duration, what string, fn func() bool) {
	t.Helper()
	var deadline time.Time = time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}

// collector is a goroutine-safe sink for delivered values.
type collector[T any] struct {
	mu    sync.Mutex
	items []T
}

func (c *collector[T]) add(v T) {
	c.mu.Lock()
	c.items = append(c.items, v)
	c.mu.Unlock()
}

func (c *collector[T]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *collector[T]) snapshot() []T {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []T = make([]T, len(c.items))
	copy(out, c.items)
	return out
}

func requireInvalidRequest(t *testing.T, name string, err error) {
	t.Helper()
	var be *bitget.Error
	if !errors.As(err, &be) {
		t.Fatalf("%s: want *bitget.Error, got %v", name, err)
	}
	if be.Kind != bitget.ErrorKindInvalidRequest {
		t.Fatalf("%s: kind = %s, want invalid_request", name, be.Kind)
	}
}

func levelsString(levels []utatypes.PriceLevel) string {
	var sb strings.Builder
	var i int
	for i = 0; i < len(levels); i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(levels[i].Price.String())
		sb.WriteByte('x')
		sb.WriteString(levels[i].Size.String())
	}
	return sb.String()
}

// ---------------------------------------------------------------------
// WatchTicker.
// ---------------------------------------------------------------------

func TestContract_Stream_WatchTicker_FuturesFixture(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.TickerUpdate]
	var errs collector[error]
	var err error = c.Stream().WatchTicker(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", got.add, errs.add)
	if err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	// Exact wire arg: lower-case instType, topic / symbol keys, nothing else.
	mock.waitOps(t, "subscribe", `{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"}`, 1)

	mock.push(t, fixtureTickerFutures)
	waitFor(t, time.Second, "ticker delivery", func() bool { return got.len() == 1 })

	var tk utatypes.TickerUpdate = got.snapshot()[0]
	if tk.Category != utatypes.CategoryUSDTFutures || tk.Symbol != "BTCUSDT" {
		t.Fatalf("category/symbol = %q/%q", tk.Category, tk.Symbol)
	}
	if tk.LastPrice.String() != "80810.6" {
		t.Fatalf("last = %s", tk.LastPrice)
	}
	if tk.Bid1Price.String() != "80810.6" || tk.Bid1Size.String() != "0.3743" {
		t.Fatalf("bid = %s x %s", tk.Bid1Price, tk.Bid1Size)
	}
	if tk.Ask1Price.String() != "80810.7" || tk.Ask1Size.String() != "1.4785" {
		t.Fatalf("ask = %s x %s", tk.Ask1Price, tk.Ask1Size)
	}
	if tk.MarkPrice.String() != "80814.7" || tk.IndexPrice.String() != "80852.148" {
		t.Fatalf("mark/index = %s/%s", tk.MarkPrice, tk.IndexPrice)
	}
	if tk.FundingRate.String() != "0.000051" {
		t.Fatalf("funding = %s", tk.FundingRate)
	}
	if tk.NextFundingTimeMs != 1789948800000 {
		t.Fatalf("next funding = %d", tk.NextFundingTimeMs)
	}
	if tk.TsMs != 1789940561885 {
		t.Fatalf("ts = %d (want the envelope ts)", tk.TsMs)
	}
	if errs.len() != 0 {
		t.Fatalf("unexpected errors: %v", errs.snapshot())
	}
}

func TestContract_Stream_WatchTicker_SpotFixture(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.TickerUpdate]
	if err := c.Stream().WatchTicker(context.Background(), utatypes.CategorySpot, "BTCUSDT", got.add, nil); err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	mock.waitOps(t, "subscribe", `{"instType":"spot","topic":"ticker","symbol":"BTCUSDT"}`, 1)

	mock.push(t, fixtureTickerSpot)
	waitFor(t, time.Second, "spot ticker delivery", func() bool { return got.len() == 1 })

	var tk utatypes.TickerUpdate = got.snapshot()[0]
	if tk.Category != utatypes.CategorySpot {
		t.Fatalf("category = %q", tk.Category)
	}
	if tk.LastPrice.String() != "80855.01" || tk.Bid1Price.String() != "80855" || tk.Ask1Price.String() != "80855.01" {
		t.Fatalf("prices = %s / %s / %s", tk.LastPrice, tk.Bid1Price, tk.Ask1Price)
	}
	if tk.Bid1Size.String() != "0.119954" || tk.Ask1Size.String() != "0.381751" {
		t.Fatalf("sizes = %s / %s", tk.Bid1Size, tk.Ask1Size)
	}
	if !tk.MarkPrice.IsZero() || !tk.IndexPrice.IsZero() || !tk.FundingRate.IsZero() || tk.NextFundingTimeMs != 0 {
		t.Fatalf("futures-only fields must be zero on spot: %+v", tk)
	}
	if tk.TsMs != 1789940562091 {
		t.Fatalf("ts = %d", tk.TsMs)
	}
}

// A field present in one frame must not leak into the next one through
// the reused decode scratch.
func TestContract_Stream_WatchTicker_ScratchDoesNotLeak(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.TickerUpdate]
	if err := c.Stream().WatchTicker(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", got.add, nil); err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	mock.waitOps(t, "subscribe", `{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"}`, 1)
	mock.push(t, fixtureTickerFutures)
	mock.push(t, `{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"},"data":[{"lastPrice":"80811"}],"ts":1789940561999}`)
	waitFor(t, time.Second, "two tickers", func() bool { return got.len() == 2 })

	var second utatypes.TickerUpdate = got.snapshot()[1]
	if second.LastPrice.String() != "80811" {
		t.Fatalf("last = %s", second.LastPrice)
	}
	if !second.MarkPrice.IsZero() || !second.Bid1Price.IsZero() || second.NextFundingTimeMs != 0 {
		t.Fatalf("stale fields leaked from the previous frame: %+v", second)
	}
}

func TestContract_Stream_WatchTicker_DecodeErrorSurfaced(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.TickerUpdate]
	var errs collector[error]
	if err := c.Stream().WatchTicker(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", got.add, errs.add); err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	mock.waitOps(t, "subscribe", `{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"}`, 1)
	mock.push(t, `{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"},"data":[{"lastPrice":"not-a-number"}],"ts":1}`)
	waitFor(t, time.Second, "decode error", func() bool { return errs.len() == 1 })
	if got.len() != 0 {
		t.Fatalf("a malformed frame must not be delivered: %+v", got.snapshot())
	}
	// The stream survives.
	mock.push(t, fixtureTickerFutures)
	waitFor(t, time.Second, "recovery", func() bool { return got.len() == 1 })
}

// ---------------------------------------------------------------------
// WatchPublicTrades.
// ---------------------------------------------------------------------

func TestContract_Stream_WatchPublicTrades_SnapshotSkippedOldestFirst(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.PublicTradeUpdate]
	var errs collector[error]
	if err := c.Stream().WatchPublicTrades(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", got.add, errs.add); err != nil {
		t.Fatalf("WatchPublicTrades: %v", err)
	}
	mock.waitOps(t, "subscribe", `{"instType":"usdt-futures","topic":"publicTrade","symbol":"BTCUSDT"}`, 1)

	// History first (must be skipped), then the live update fixture.
	mock.push(t, fixtureTradesSnapshot)
	mock.push(t, fixtureTradesUpdate)
	waitFor(t, time.Second, "two trades", func() bool { return got.len() == 2 })
	// Nothing else may trickle in from the snapshot.
	time.Sleep(50 * time.Millisecond)

	var trades []utatypes.PublicTradeUpdate = got.snapshot()
	if len(trades) != 2 {
		t.Fatalf("delivered %d trades, want 2 (snapshot history must be skipped): %+v", len(trades), trades)
	}
	// The venue lists newest first (ids descending) — delivery is oldest first.
	if trades[0].TradeID != "1485683898127761408" || trades[1].TradeID != "1485683898127761410" {
		t.Fatalf("order = %s, %s — want oldest first", trades[0].TradeID, trades[1].TradeID)
	}
	if trades[0].Price.String() != "80810.6" || trades[0].Size.String() != "0.0716" {
		t.Fatalf("trade[0] = %s x %s", trades[0].Price, trades[0].Size)
	}
	if trades[1].Size.String() != "0.0246" {
		t.Fatalf("trade[1] size = %s", trades[1].Size)
	}
	if trades[0].Side != "sell" || trades[0].IsRPI || trades[0].TsMs != 1789940564662 {
		t.Fatalf("trade[0] side/rpi/ts = %q/%v/%d", trades[0].Side, trades[0].IsRPI, trades[0].TsMs)
	}
	if trades[0].Category != utatypes.CategoryUSDTFutures || trades[0].Symbol != "BTCUSDT" {
		t.Fatalf("category/symbol = %q/%q", trades[0].Category, trades[0].Symbol)
	}
	if errs.len() != 0 {
		t.Fatalf("unexpected errors: %v", errs.snapshot())
	}

	// An oldest-first frame (should the venue ever flip) keeps its order;
	// isRPI=yes maps to true.
	mock.push(t, `{"action":"update","arg":{"instType":"usdt-futures","topic":"publicTrade","symbol":"BTCUSDT"},"data":[{"i":"1485683898127761500","p":"1","v":"1","S":"buy","T":"1789940565000","isRPI":"yes"},{"i":"1485683898127761502","p":"2","v":"2","S":"buy","T":"1789940565001","isRPI":"no"}],"ts":1789940565001}`)
	waitFor(t, time.Second, "four trades", func() bool { return got.len() == 4 })
	trades = got.snapshot()
	if trades[2].TradeID != "1485683898127761500" || trades[3].TradeID != "1485683898127761502" {
		t.Fatalf("oldest-first frame reordered: %s, %s", trades[2].TradeID, trades[3].TradeID)
	}
	if !trades[2].IsRPI || trades[3].IsRPI || trades[2].Side != "buy" {
		t.Fatalf("rpi/side: %+v %+v", trades[2], trades[3])
	}
}

// ---------------------------------------------------------------------
// WatchOrderBook — stateless topics.
// ---------------------------------------------------------------------

func TestContract_Stream_WatchOrderBook_Books5Fixture(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.OrderBookUpdate]
	var errs collector[error]
	if err := c.Stream().WatchOrderBook(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", 5, got.add, errs.add); err != nil {
		t.Fatalf("WatchOrderBook: %v", err)
	}
	mock.waitOps(t, "subscribe", `{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"}`, 1)

	mock.push(t, fixtureBooks5)
	waitFor(t, time.Second, "books5 delivery", func() bool { return got.len() == 1 })

	var ob utatypes.OrderBookUpdate = got.snapshot()[0]
	if ob.Category != utatypes.CategoryUSDTFutures || ob.Symbol != "BTCUSDT" {
		t.Fatalf("category/symbol = %q/%q", ob.Category, ob.Symbol)
	}
	if levelsString(ob.Asks) != "80810.4x0.8523 80810.5x0.0014 80810.7x0.0093 80811x0.0001 80811.1x0.0124" {
		t.Fatalf("asks = %s", levelsString(ob.Asks))
	}
	if levelsString(ob.Bids) != "80810.3x1.0943 80810.2x0.1194 80809.9x0.007 80809.6x0.0061 80808.8x0.04" {
		t.Fatalf("bids = %s", levelsString(ob.Bids))
	}
	if ob.Seq != 993094633676 {
		t.Fatalf("seq = %d", ob.Seq)
	}
	if ob.TsMs != 1789940582101 {
		t.Fatalf("ts = %d (want data[].ts, not the envelope ts)", ob.TsMs)
	}

	// STATELESS: the next push is again a full snapshot with pseq=0 — it
	// replaces the view, it is neither merged nor treated as a gap.
	mock.push(t, `{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"},"data":[{"a":[["80812","1"]],"b":[["80811","2"]],"seq":993094633999,"pseq":0,"ts":""}],"ts":1789940582202}`)
	waitFor(t, time.Second, "second books5 delivery", func() bool { return got.len() == 2 })
	var second utatypes.OrderBookUpdate = got.snapshot()[1]
	if levelsString(second.Asks) != "80812x1" || levelsString(second.Bids) != "80811x2" {
		t.Fatalf("second view = %s | %s", levelsString(second.Asks), levelsString(second.Bids))
	}
	if second.TsMs != 1789940582202 {
		t.Fatalf("ts fallback = %d (want the envelope ts when data[].ts is empty)", second.TsMs)
	}
	// The first delivery must be untouched (callers may retain slices).
	if levelsString(got.snapshot()[0].Asks) != "80810.4x0.8523 80810.5x0.0014 80810.7x0.0093 80811x0.0001 80811.1x0.0124" {
		t.Fatalf("retained slice was mutated: %s", levelsString(got.snapshot()[0].Asks))
	}
	if errs.len() != 0 {
		t.Fatalf("unexpected errors: %v", errs.snapshot())
	}
	if mock.countOps("unsubscribe", `{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"}`) != 0 {
		t.Fatal("stateless topic must never resync")
	}
}

func TestContract_Stream_WatchOrderBook_DepthToTopic(t *testing.T) {
	type tc struct {
		depth int
		topic string
	}
	var cases []tc = []tc{
		{-1, "books50"}, {0, "books50"}, {1, "books1"}, {2, "books5"}, {5, "books5"},
		{6, "books50"}, {50, "books50"}, {51, "books"}, {200, "books"}, {1000, "books"},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var topic string
		topic, _ = bookTopicFor(cases[i].depth)
		if topic != cases[i].topic {
			t.Fatalf("depth %d → %q, want %q", cases[i].depth, topic, cases[i].topic)
		}
	}
}

// Two callers sharing books5 with different depths: one wire subscription,
// each handler truncated to ITS depth. A books50 caller is a separate arg.
func TestContract_Stream_WatchOrderBook_PerHandlerDepth(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var deep collector[utatypes.OrderBookUpdate]
	var shallow collector[utatypes.OrderBookUpdate]
	var wide collector[utatypes.OrderBookUpdate]
	if err := c.Stream().WatchOrderBook(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", 5, deep.add, nil); err != nil {
		t.Fatalf("WatchOrderBook(5): %v", err)
	}
	if err := c.Stream().WatchOrderBook(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", 3, shallow.add, nil); err != nil {
		t.Fatalf("WatchOrderBook(3): %v", err)
	}
	if err := c.Stream().WatchOrderBook(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", 20, wide.add, nil); err != nil {
		t.Fatalf("WatchOrderBook(20): %v", err)
	}
	mock.waitOps(t, "subscribe", `{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"}`, 1)
	mock.waitOps(t, "subscribe", `{"instType":"usdt-futures","topic":"books50","symbol":"BTCUSDT"}`, 1)

	mock.push(t, fixtureBooks5)
	waitFor(t, time.Second, "both books5 handlers", func() bool { return deep.len() == 1 && shallow.len() == 1 })
	if len(deep.snapshot()[0].Asks) != 5 || len(deep.snapshot()[0].Bids) != 5 {
		t.Fatalf("depth-5 handler got %d/%d levels", len(deep.snapshot()[0].Asks), len(deep.snapshot()[0].Bids))
	}
	var s3 utatypes.OrderBookUpdate = shallow.snapshot()[0]
	if levelsString(s3.Asks) != "80810.4x0.8523 80810.5x0.0014 80810.7x0.0093" || len(s3.Bids) != 3 {
		t.Fatalf("depth-3 handler = %s | %d bids", levelsString(s3.Asks), len(s3.Bids))
	}
	if cap(s3.Asks) != 3 {
		t.Fatalf("truncated slice must cap its capacity (shared backing array), cap = %d", cap(s3.Asks))
	}
	if wide.len() != 0 {
		t.Fatal("books50 handler received a books5 frame")
	}
	if mock.countOps("subscribe", `{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"}`) != 1 {
		t.Fatal("two books5 callers must share ONE wire subscription")
	}
}

// ---------------------------------------------------------------------
// WatchOrderBook — incremental `books`.
// ---------------------------------------------------------------------

const booksArg = `{"instType":"usdt-futures","topic":"books","symbol":"BTCUSDT"}`

func booksFrame(action, asks, bids string, seq, pseq int64) string {
	return `{"action":"` + action + `","arg":` + booksArg + `,"data":[{"a":` + asks + `,"b":` + bids +
		`,"seq":` + itoa(seq) + `,"pseq":` + itoa(pseq) + `,"ts":"1789940621751","maxdepth":"1000"}],"ts":1789940621755}`
}

func itoa(v int64) string {
	var raw []byte
	raw, _ = codec.Marshal(v)
	return string(raw)
}

func TestContract_Stream_WatchOrderBook_IncrementalApplyGapResync(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.OrderBookUpdate]
	var errs collector[error]
	if err := c.Stream().WatchOrderBook(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", 200, got.add, errs.add); err != nil {
		t.Fatalf("WatchOrderBook: %v", err)
	}
	mock.waitOps(t, "subscribe", booksArg, 1)

	// Snapshot.
	mock.push(t, booksFrame("snapshot",
		`[["80810.4","0.8523"],["80810.5","0.0014"],["80811","0.0001"]]`,
		`[["80810.3","1.0943"],["80810.2","0.1194"],["80809.9","0.007"]]`,
		993096946148, 0))
	waitFor(t, time.Second, "snapshot delivery", func() bool { return got.len() == 1 })
	var ob utatypes.OrderBookUpdate = got.snapshot()[0]
	if levelsString(ob.Asks) != "80810.4x0.8523 80810.5x0.0014 80811x0.0001" ||
		levelsString(ob.Bids) != "80810.3x1.0943 80810.2x0.1194 80809.9x0.007" {
		t.Fatalf("snapshot view = %s | %s", levelsString(ob.Asks), levelsString(ob.Bids))
	}
	if ob.Seq != 993096946148 || ob.TsMs != 1789940621751 {
		t.Fatalf("seq/ts = %d/%d", ob.Seq, ob.TsMs)
	}

	// Update chained by pseq: insert an ask inside the book (different
	// exponent than its neighbours), replace a bid size, delete a level
	// (size "0"), delete a level that does not exist (no-op).
	mock.push(t, booksFrame("update",
		`[["80810.45","2"],["80810.5","0"],["99999","0"]]`,
		`[["80810.3","5"],["80810.25","0.3"]]`,
		993096950501, 993096946148))
	waitFor(t, time.Second, "update delivery", func() bool { return got.len() == 2 })
	ob = got.snapshot()[1]
	if levelsString(ob.Asks) != "80810.4x0.8523 80810.45x2 80811x0.0001" {
		t.Fatalf("asks after update = %s", levelsString(ob.Asks))
	}
	if levelsString(ob.Bids) != "80810.3x5 80810.25x0.3 80810.2x0.1194 80809.9x0.007" {
		t.Fatalf("bids after update = %s", levelsString(ob.Bids))
	}
	if ob.Seq != 993096950501 {
		t.Fatalf("seq after update = %d", ob.Seq)
	}
	// The earlier delivery was not mutated.
	if levelsString(got.snapshot()[0].Bids) != "80810.3x1.0943 80810.2x0.1194 80809.9x0.007" {
		t.Fatalf("retained snapshot slice mutated: %s", levelsString(got.snapshot()[0].Bids))
	}

	// GAP: pseq does not match the last applied seq.
	mock.push(t, booksFrame("update", `[["80812","1"]]`, `[]`, 993096960000, 993096955555))
	waitFor(t, time.Second, "gap error", func() bool { return errs.len() == 1 })
	if !errors.Is(errs.snapshot()[0], ErrOrderBookResync) {
		t.Fatalf("gap error must wrap ErrOrderBookResync: %v", errs.snapshot()[0])
	}
	// While the resync is in flight further updates are dropped silently.
	mock.push(t, booksFrame("update", `[["80813","1"]]`, `[]`, 993096960001, 993096960000))
	mock.waitOps(t, "unsubscribe", booksArg, 1)
	mock.waitOps(t, "subscribe", booksArg, 2)
	if got.len() != 2 {
		t.Fatalf("a frame was delivered from a broken book: %d deliveries", got.len())
	}
	if errs.len() != 1 {
		t.Fatalf("one break must surface ONE error, got %d: %v", errs.len(), errs.snapshot())
	}

	// The venue answers the resubscribe with a fresh snapshot — deliveries
	// resume from IT (nothing of the old state survives).
	mock.push(t, booksFrame("snapshot", `[["80900","1"]]`, `[["80899","2"]]`, 993097000000, 0))
	waitFor(t, time.Second, "post-resync snapshot", func() bool { return got.len() == 3 })
	ob = got.snapshot()[2]
	if levelsString(ob.Asks) != "80900x1" || levelsString(ob.Bids) != "80899x2" {
		t.Fatalf("post-resync view = %s | %s", levelsString(ob.Asks), levelsString(ob.Bids))
	}
	mock.push(t, booksFrame("update", `[["80901","3"]]`, `[]`, 993097000050, 993097000000))
	waitFor(t, time.Second, "post-resync update", func() bool { return got.len() == 4 })
	if levelsString(got.snapshot()[3].Asks) != "80900x1 80901x3" {
		t.Fatalf("post-resync update view = %s", levelsString(got.snapshot()[3].Asks))
	}
}

func TestContract_Stream_WatchOrderBook_PseqZeroAndUpdateBeforeSnapshot(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.OrderBookUpdate]
	var errs collector[error]
	if err := c.Stream().WatchOrderBook(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", 100, got.add, errs.add); err != nil {
		t.Fatalf("WatchOrderBook: %v", err)
	}
	mock.waitOps(t, "subscribe", booksArg, 1)

	// An update BEFORE any snapshot → error + resubscribe.
	mock.push(t, booksFrame("update", `[["80812","1"]]`, `[]`, 11, 10))
	waitFor(t, time.Second, "update-before-snapshot error", func() bool { return errs.len() == 1 })
	mock.waitOps(t, "unsubscribe", booksArg, 1)
	mock.waitOps(t, "subscribe", booksArg, 2)
	if got.len() != 0 {
		t.Fatalf("delivered %d frames without a snapshot", got.len())
	}

	mock.push(t, booksFrame("snapshot", `[["80900","1"]]`, `[["80899","2"]]`, 100, 0))
	waitFor(t, time.Second, "snapshot", func() bool { return got.len() == 1 })

	// pseq=0 on an UPDATE = the venue reset its numbering → resync.
	mock.push(t, booksFrame("update", `[["80901","1"]]`, `[]`, 5, 0))
	waitFor(t, time.Second, "pseq=0 error", func() bool { return errs.len() == 2 })
	if !errors.Is(errs.snapshot()[1], ErrOrderBookResync) {
		t.Fatalf("pseq=0 error must wrap ErrOrderBookResync: %v", errs.snapshot()[1])
	}
	mock.waitOps(t, "unsubscribe", booksArg, 2)
	mock.waitOps(t, "subscribe", booksArg, 3)
	if got.len() != 1 {
		t.Fatalf("pseq=0 update must not be applied, deliveries = %d", got.len())
	}
}

// After a reconnect ws.Conn resets the local book; the venue's snapshot on
// the new socket rebuilds it and the old chain is forgotten (no error).
func TestContract_Stream_WatchOrderBook_ReconnectRebuilds(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.OrderBookUpdate]
	var errs collector[error]
	if err := c.Stream().WatchOrderBook(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", 100, got.add, errs.add); err != nil {
		t.Fatalf("WatchOrderBook: %v", err)
	}
	mock.waitOps(t, "subscribe", booksArg, 1)
	mock.push(t, booksFrame("snapshot", `[["80900","1"],["80901","1"]]`, `[["80899","2"]]`, 100, 0))
	waitFor(t, time.Second, "snapshot", func() bool { return got.len() == 1 })

	mock.dropActive(t)
	mock.waitOps(t, "subscribe", booksArg, 2)
	waitFor(t, time.Second, "second connection", func() bool { return mock.connCount() == 2 })

	mock.push(t, booksFrame("snapshot", `[["70000","9"]]`, `[["69999","8"]]`, 7, 0))
	waitFor(t, time.Second, "snapshot after reconnect", func() bool { return got.len() == 2 })
	var ob utatypes.OrderBookUpdate = got.snapshot()[1]
	if levelsString(ob.Asks) != "70000x9" || levelsString(ob.Bids) != "69999x8" {
		t.Fatalf("view after reconnect = %s | %s (old levels must be gone)", levelsString(ob.Asks), levelsString(ob.Bids))
	}
	mock.push(t, booksFrame("update", `[["70001","1"]]`, `[]`, 8, 7))
	waitFor(t, time.Second, "update after reconnect", func() bool { return got.len() == 3 })
	if errs.len() != 0 {
		t.Fatalf("a reconnect is not a chain break: %v", errs.snapshot())
	}
}

// ---------------------------------------------------------------------
// Multi-handler fan-out.
// ---------------------------------------------------------------------

func TestContract_Stream_FanOut_SharedWireSubscription(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)
	const arg string = `{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"}`

	var order collector[string]
	var first collector[utatypes.TickerUpdate]
	var second collector[utatypes.TickerUpdate]

	var ctx1 context.Context
	var cancel1 context.CancelFunc
	ctx1, cancel1 = context.WithCancel(context.Background())
	defer cancel1()
	var ctx2 context.Context
	var cancel2 context.CancelFunc
	ctx2, cancel2 = context.WithCancel(context.Background())
	defer cancel2()

	if err := c.Stream().WatchTicker(ctx1, utatypes.CategoryUSDTFutures, "BTCUSDT",
		func(u utatypes.TickerUpdate) { order.add("first"); first.add(u) }, nil); err != nil {
		t.Fatalf("WatchTicker #1: %v", err)
	}
	if err := c.Stream().WatchTicker(ctx2, utatypes.CategoryUSDTFutures, "BTCUSDT",
		func(u utatypes.TickerUpdate) { order.add("second"); second.add(u) }, nil); err != nil {
		t.Fatalf("WatchTicker #2: %v", err)
	}
	mock.waitOps(t, "subscribe", arg, 1)

	mock.push(t, fixtureTickerFutures)
	waitFor(t, time.Second, "both handlers", func() bool { return first.len() == 1 && second.len() == 1 })
	if got := order.snapshot(); got[0] != "first" || got[1] != "second" {
		t.Fatalf("handlers must run in registration order, got %v", got)
	}
	if mock.countOps("subscribe", arg) != 1 {
		t.Fatalf("two handlers of one arg must share ONE wire subscription, saw %d subscribes", mock.countOps("subscribe", arg))
	}

	// Cancel the FIRST ctx: the second handler keeps receiving and nothing
	// is unsubscribed on the wire.
	cancel1()
	time.Sleep(60 * time.Millisecond)
	mock.push(t, fixtureTickerFutures)
	waitFor(t, time.Second, "second handler after first detached", func() bool { return second.len() == 2 })
	if first.len() != 1 {
		t.Fatalf("detached handler still receives: %d", first.len())
	}
	if mock.countOps("unsubscribe", arg) != 0 {
		t.Fatal("unsubscribe sent while a handler is still attached")
	}

	// Cancel the LAST ctx: now the wire unsubscribe goes out.
	cancel2()
	mock.waitOps(t, "unsubscribe", arg, 1)

	// A later Watch for the same arg subscribes afresh.
	var third collector[utatypes.TickerUpdate]
	if err := c.Stream().WatchTicker(nil, utatypes.CategoryUSDTFutures, "BTCUSDT", third.add, nil); err != nil { //nolint:staticcheck // nil ctx is part of the contract
		t.Fatalf("WatchTicker #3 (nil ctx): %v", err)
	}
	mock.waitOps(t, "subscribe", arg, 2)
	mock.push(t, fixtureTickerFutures)
	waitFor(t, time.Second, "nil-ctx handler", func() bool { return third.len() == 1 })
	if second.len() != 2 {
		t.Fatalf("detached handler received after resubscribe: %d", second.len())
	}
}

// ---------------------------------------------------------------------
// Reconnect callbacks.
// ---------------------------------------------------------------------

func TestContract_Stream_OnPublicReconnect(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)
	const arg string = `{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"}`

	var calls collector[string]
	// Registered BEFORE the connection exists.
	var removeA func() = c.Stream().OnPublicReconnect(func() { calls.add("a") })
	var removeB func() = c.Stream().OnPublicReconnect(func() { calls.add("b") })
	defer removeB()
	var private collector[string]
	defer c.Stream().OnPrivateReconnect(func() { private.add("x") })()

	if err := c.Stream().WatchTicker(context.Background(), utatypes.CategoryUSDTFutures, "BTCUSDT", func(utatypes.TickerUpdate) {}, nil); err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	mock.waitOps(t, "subscribe", arg, 1)
	time.Sleep(50 * time.Millisecond)
	if calls.len() != 0 {
		t.Fatalf("reconnect callback fired on the FIRST connect: %v", calls.snapshot())
	}

	mock.dropActive(t)
	mock.waitOps(t, "subscribe", arg, 2)
	waitFor(t, time.Second, "reconnect callbacks", func() bool { return calls.len() == 2 })
	if got := calls.snapshot(); got[0] != "a" || got[1] != "b" {
		t.Fatalf("callbacks must run in registration order: %v", got)
	}

	// remove() detaches one callback; it is idempotent.
	removeA()
	removeA()
	mock.dropActive(t)
	mock.waitOps(t, "subscribe", arg, 3)
	waitFor(t, time.Second, "second reconnect callback", func() bool { return calls.len() == 3 })
	if got := calls.snapshot(); got[2] != "b" {
		t.Fatalf("removed callback fired: %v", got)
	}
	if private.len() != 0 {
		t.Fatalf("a public reconnect fired the PRIVATE callbacks: %v", private.snapshot())
	}
	// nil is tolerated.
	c.Stream().OnPublicReconnect(nil)()
}

// ---------------------------------------------------------------------
// Validation / lifecycle.
// ---------------------------------------------------------------------

func TestContract_Stream_Validation(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)
	var s *StreamClient = c.Stream()
	var ctx context.Context = context.Background()

	var tickerFn = func(utatypes.TickerUpdate) {}
	var tradeFn = func(utatypes.PublicTradeUpdate) {}
	var bookFn = func(utatypes.OrderBookUpdate) {}

	type tc struct {
		name string
		run  func() error
	}
	var cases []tc = []tc{
		{"ticker empty category", func() error { return s.WatchTicker(ctx, "", "BTCUSDT", tickerFn, nil) }},
		{"ticker margin category", func() error { return s.WatchTicker(ctx, utatypes.CategoryMargin, "BTCUSDT", tickerFn, nil) }},
		{"ticker unknown category", func() error { return s.WatchTicker(ctx, "OPTIONS", "BTCUSDT", tickerFn, nil) }},
		{"ticker empty symbol", func() error { return s.WatchTicker(ctx, utatypes.CategorySpot, "", tickerFn, nil) }},
		{"ticker nil handler", func() error { return s.WatchTicker(ctx, utatypes.CategorySpot, "BTCUSDT", nil, nil) }},
		{"trades empty category", func() error { return s.WatchPublicTrades(ctx, "", "BTCUSDT", tradeFn, nil) }},
		{"trades margin category", func() error { return s.WatchPublicTrades(ctx, utatypes.CategoryMargin, "BTCUSDT", tradeFn, nil) }},
		{"trades empty symbol", func() error { return s.WatchPublicTrades(ctx, utatypes.CategorySpot, "", tradeFn, nil) }},
		{"trades nil handler", func() error { return s.WatchPublicTrades(ctx, utatypes.CategorySpot, "BTCUSDT", nil, nil) }},
		{"book empty category", func() error { return s.WatchOrderBook(ctx, "", "BTCUSDT", 5, bookFn, nil) }},
		{"book margin category", func() error { return s.WatchOrderBook(ctx, utatypes.CategoryMargin, "BTCUSDT", 5, bookFn, nil) }},
		{"book empty symbol", func() error { return s.WatchOrderBook(ctx, utatypes.CategorySpot, "", 5, bookFn, nil) }},
		{"book nil handler", func() error { return s.WatchOrderBook(ctx, utatypes.CategorySpot, "BTCUSDT", 5, nil, nil) }},
		{"orders nil handler", func() error { return s.WatchOrders(ctx, nil, nil) }},
		{"fills nil handler", func() error { return s.WatchFills(ctx, nil, nil) }},
		{"positions nil handler", func() error { return s.WatchPositions(ctx, nil, nil) }},
		{"account nil handler", func() error { return s.WatchAccount(ctx, nil, nil) }},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		requireInvalidRequest(t, cases[i].name, cases[i].run())
	}
	if mock.connCount() != 0 {
		t.Fatalf("validation failures must not dial: %d connections", mock.connCount())
	}
}

// Every public category maps to its lower-case instType; a lower-case
// category value is accepted and canonicalised.
func TestContract_Stream_CategoryWireMapping(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, false)

	var got collector[utatypes.TickerUpdate]
	var categories []utatypes.Category = []utatypes.Category{
		utatypes.CategorySpot, utatypes.CategoryUSDTFutures, utatypes.CategoryCOINFutures, utatypes.CategoryUSDCFutures,
	}
	var instTypes []string = []string{"spot", "usdt-futures", "coin-futures", "usdc-futures"}
	var i int
	for i = 0; i < len(categories); i++ {
		if err := c.Stream().WatchTicker(context.Background(), categories[i], "BTCUSD", got.add, nil); err != nil {
			t.Fatalf("WatchTicker(%s): %v", categories[i], err)
		}
		mock.waitOps(t, "subscribe", `{"instType":"`+instTypes[i]+`","topic":"ticker","symbol":"BTCUSD"}`, 1)
	}

	// Lower-case input → same wire arg (shared subscription), canonical
	// category in the update.
	if err := c.Stream().WatchTicker(context.Background(), "coin-futures", "BTCUSD", got.add, nil); err != nil {
		t.Fatalf("WatchTicker(lower-case): %v", err)
	}
	mock.push(t, `{"action":"snapshot","arg":{"instType":"coin-futures","topic":"ticker","symbol":"BTCUSD"},"data":[{"lastPrice":"1"}],"ts":1}`)
	waitFor(t, time.Second, "coin-futures deliveries", func() bool { return got.len() == 2 })
	var updates []utatypes.TickerUpdate = got.snapshot()
	if updates[0].Category != utatypes.CategoryCOINFutures || updates[1].Category != utatypes.CategoryCOINFutures {
		t.Fatalf("categories = %q / %q", updates[0].Category, updates[1].Category)
	}
	if mock.countOps("subscribe", `{"instType":"coin-futures","topic":"ticker","symbol":"BTCUSD"}`) != 1 {
		t.Fatal("lower-case category must share the canonical wire subscription")
	}
}

func TestContract_Stream_CloseIdempotentAndWatchAfterClose(t *testing.T) {
	var mock *utaMockServer = newUTAMockServer(t)
	var c *Client = newStreamTestClient(t, mock, true)
	var s *StreamClient = c.Stream()

	if err := s.WatchTicker(context.Background(), utatypes.CategorySpot, "BTCUSDT", func(utatypes.TickerUpdate) {}, nil); err != nil {
		t.Fatalf("WatchTicker: %v", err)
	}
	mock.waitOps(t, "subscribe", `{"instType":"spot","topic":"ticker","symbol":"BTCUSDT"}`, 1)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	requireInvalidRequest(t, "public watch after close",
		s.WatchTicker(context.Background(), utatypes.CategorySpot, "ETHUSDT", func(utatypes.TickerUpdate) {}, nil))
	requireInvalidRequest(t, "private watch after close",
		s.WatchOrders(context.Background(), func(utatypes.Order) {}, nil))
}
