/*
FILE: internal/ws/conn_test.go

DESCRIPTION:
Mock-server tests for the Bitget WebSocket connection wrapper. The mock
implements just enough of the Bitget protocol to validate the SDK
behaviour:

  - upgrades to a WS connection;
  - replies "pong" to plain-text "ping";
  - replies {"event":"login","code":"0"} to {"op":"login",...};
  - replies {"event":"subscribe","arg":{...},"code":"0"} to a subscribe op;
  - lets the test inject push frames at will.

Coverage:

  - public connect → subscribe → receive push → handler invoked;
  - private login round-trip;
  - reconnect: server closes the socket, client reconnects, the same
    subscription is sent again automatically;
  - V3 (UTA) args: topic / symbol subscribe op on the wire and push
    dispatch by the V3 registry key;
  - Config.OnConnect: reconnect=false on the first connection, true after
    a forced server-side close; on private endpoints it fires after login.
*/

package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/tonymontanov/go-bitget/v2/internal/auth"
	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
)

// mockServer is a minimal Bitget-compatible WS endpoint.
type mockServer struct {
	t      *testing.T
	srv    *httptest.Server
	upgr   websocket.Upgrader
	subs   chan SubscriptionArg
	logins chan struct{}
	conns  chan *websocket.Conn
	// writeMu serialises every write on every captured *websocket.Conn.
	// gorilla/websocket forbids concurrent writes; tests that inject
	// push frames from the test goroutine must take the same lock the
	// handle() goroutine uses for ack writes.
	writeMu sync.Mutex
	// loginAck — the frame sent back on a login op. Empty → the classic
	// {"event":"login","code":"0"}. Tests override it to exercise the
	// code-less V3 form and the rejection frame.
	loginAck string
}

func newMockServer(t *testing.T) *mockServer {
	t.Helper()
	var m *mockServer = &mockServer{
		t:      t,
		upgr:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		subs:   make(chan SubscriptionArg, 16),
		logins: make(chan struct{}, 4),
		conns:  make(chan *websocket.Conn, 4),
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *mockServer) wsURL() string {
	return "ws" + strings.TrimPrefix(m.srv.URL, "http")
}

func (m *mockServer) close() {
	m.srv.Close()
}

func (m *mockServer) handle(w http.ResponseWriter, r *http.Request) {
	var conn *websocket.Conn
	var err error
	conn, err = m.upgr.Upgrade(w, r, nil)
	if err != nil {
		m.t.Errorf("upgrade: %v", err)
		return
	}
	m.conns <- conn
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
		// Plain-text ping → reply "pong".
		if string(body) == "ping" {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`pong`))
			continue
		}
		// JSON ops.
		var op struct {
			Op   string            `json:"op"`
			Args []SubscriptionArg `json:"args"`
		}
		if err = codec.Unmarshal(body, &op); err != nil {
			continue
		}
		switch op.Op {
		case "login":
			m.logins <- struct{}{}
			m.writeMu.Lock()
			var ack string = m.loginAck
			if ack == "" {
				ack = `{"event":"login","code":"0"}`
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(ack))
			m.writeMu.Unlock()
		case "subscribe":
			var i int
			for i = 0; i < len(op.Args); i++ {
				m.subs <- op.Args[i]
				var ack []byte
				ack, _ = codec.Marshal(map[string]any{
					"event": "subscribe",
					"arg":   op.Args[i],
					"code":  "0",
				})
				m.writeMu.Lock()
				_ = conn.WriteMessage(websocket.TextMessage, ack)
				m.writeMu.Unlock()
			}
		case "unsubscribe":
			// no-op for tests
		}
	}
}

func TestConnPublicSubscribePush(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()

	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             3 * time.Second,
		WriteTimeout:            2 * time.Second,
		PingInterval:            500 * time.Millisecond,
		ReconnectInitialBackoff: 50 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
	}, nil, nil, nil)
	defer c.Close()

	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	var pushCh chan struct {
		arg SubscriptionArg
		act string
		ts  int64
	} = make(chan struct {
		arg SubscriptionArg
		act string
		ts  int64
	}, 4)
	var sub *Subscription = &Subscription{
		Arg: SubscriptionArg{InstType: "USDT-FUTURES", Channel: "books5", InstID: "BTCUSDT"},
		Handler: func(arg SubscriptionArg, action string, payload []byte, tsMs int64, checksum int64) {
			pushCh <- struct {
				arg SubscriptionArg
				act string
				ts  int64
			}{arg, action, tsMs}
		},
	}
	if err := c.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Wait for the server to receive the subscribe op.
	select {
	case got := <-srv.subs:
		if got.Channel != "books5" || got.InstID != "BTCUSDT" {
			t.Fatalf("subscribe arg = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive subscribe op")
	}

	// Inject a push frame on the client-bound conn.
	var conn *websocket.Conn
	select {
	case conn = <-srv.conns:
	case <-time.After(time.Second):
		t.Fatal("no client conn captured")
	}
	var push []byte
	push, _ = codec.Marshal(map[string]any{
		"action": "snapshot",
		"arg":    sub.Arg,
		"ts":     1700000000123,
		"data":   []map[string]string{{"price": "100", "size": "1"}},
	})
	srv.writeMu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, push)
	srv.writeMu.Unlock()

	select {
	case ev := <-pushCh:
		if ev.act != "snapshot" || ev.arg.Channel != "books5" || ev.ts != 1700000000123 {
			t.Fatalf("unexpected push event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not invoked")
	}
}

func TestConnPrivateLogin(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()

	var signer *auth.Signer = auth.NewSigner("k", "s", "p")
	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		IsPrivate:               true,
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             3 * time.Second,
		WriteTimeout:            2 * time.Second,
		LoginTimeout:            2 * time.Second,
		PingInterval:            500 * time.Millisecond,
		ReconnectInitialBackoff: 50 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
	}, signer, nil, nil)
	defer c.Close()

	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	select {
	case <-srv.logins:
	case <-time.After(2 * time.Second):
		t.Fatal("login op not received by server")
	}
}

// privateTestConfig is the Config shared by the login-ack tests below.
func privateTestConfig(url string, onConnect func(bool)) Config {
	return Config{
		URL:                     url,
		IsPrivate:               true,
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             3 * time.Second,
		WriteTimeout:            2 * time.Second,
		LoginTimeout:            2 * time.Second,
		PingInterval:            500 * time.Millisecond,
		ReconnectInitialBackoff: 30 * time.Millisecond,
		ReconnectMaxBackoff:     100 * time.Millisecond,
		OnConnect:               onConnect,
	}
}

// TestConnPrivateLoginAckWithoutCode — the V3 endpoint omits `code` on
// its acks; a code-less {"event":"login"} must count as a successful
// login (OnConnect fires), not as a rejection.
func TestConnPrivateLoginAckWithoutCode(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	srv.loginAck = `{"event":"login","msg":"","connId":"0649f8fffee700ab"}`
	defer srv.close()

	var connected chan bool = make(chan bool, 4)
	var c *Conn = NewConn(privateTestConfig(srv.wsURL(), func(reconnect bool) { connected <- reconnect }),
		auth.NewSigner("k", "s", "p"), nil, nil)
	defer c.Close()

	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	select {
	case reconnect := <-connected:
		if reconnect {
			t.Fatal("first connection reported as reconnect")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("code-less login ack was not accepted: OnConnect never fired")
	}
}

// TestConnPrivateLoginRejected — a rejected login arrives as
// {"event":"error",...}; the conn must NOT report itself connected and
// must keep retrying the login.
func TestConnPrivateLoginRejected(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	srv.loginAck = `{"event":"error","code":"30005","msg":"error"}`
	defer srv.close()

	var connected chan bool = make(chan bool, 4)
	var c *Conn = NewConn(privateTestConfig(srv.wsURL(), func(reconnect bool) { connected <- reconnect }),
		auth.NewSigner("k", "s", "p"), nil, nil)
	defer c.Close()

	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	var i int
	for i = 0; i < 2; i++ {
		select {
		case <-srv.logins:
		case <-connected:
			t.Fatal("OnConnect fired although the login was rejected")
		case <-time.After(2 * time.Second):
			t.Fatalf("login attempt #%d not seen: the conn stopped retrying", i+1)
		}
	}
	select {
	case <-connected:
		t.Fatal("OnConnect fired although the login was rejected")
	default:
	}
}

func TestConnReconnectResubscribe(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()

	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            2 * time.Second,
		PingInterval:            300 * time.Millisecond,
		ReconnectInitialBackoff: 30 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
	}, nil, nil, nil)
	defer c.Close()

	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	var handlerHits atomic.Int32
	var sub *Subscription = &Subscription{
		Arg: SubscriptionArg{InstType: "USDT-FUTURES", Channel: "ticker", InstID: "BTCUSDT"},
		Handler: func(SubscriptionArg, string, []byte, int64, int64) {
			handlerHits.Add(1)
		},
	}
	if err := c.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// First subscribe op.
	var firstSubArg SubscriptionArg
	select {
	case firstSubArg = <-srv.subs:
	case <-time.After(2 * time.Second):
		t.Fatal("initial subscribe missing")
	}
	if firstSubArg.Channel != "ticker" {
		t.Fatalf("initial sub arg = %+v", firstSubArg)
	}

	// Drain the initial conn from the channel and force-close it to trigger
	// a reconnect.
	var oldConn *websocket.Conn
	select {
	case oldConn = <-srv.conns:
	case <-time.After(time.Second):
		t.Fatal("no captured conn")
	}
	_ = oldConn.Close()

	// We expect a re-subscribe op from the next connect.
	select {
	case got := <-srv.subs:
		if got.Channel != "ticker" {
			t.Fatalf("re-subscribe arg = %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not resubscribe after reconnect")
	}

	// Sanity: handler not invoked because server never sent a push.
	if handlerHits.Load() != 0 {
		t.Fatalf("unexpected handler hits: %d", handlerHits.Load())
	}
}

// TestSubscribeBeforeStart ensures Subscribe is allowed before Start: the
// arg is queued and dispatched on the first connect.
func TestSubscribeBeforeStart(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()
	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            2 * time.Second,
		PingInterval:            500 * time.Millisecond,
		ReconnectInitialBackoff: 30 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
	}, nil, nil, nil)
	defer c.Close()

	var sub *Subscription = &Subscription{
		Arg:     SubscriptionArg{InstType: "USDT-FUTURES", Channel: "trade", InstID: "BTCUSDT"},
		Handler: func(SubscriptionArg, string, []byte, int64, int64) {},
	}
	if err := c.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	c.Start(context.Background())
	select {
	case <-srv.subs:
	case <-time.After(2 * time.Second):
		t.Fatal("queued subscribe was not dispatched on connect")
	}
}

// TestCloseStopsSupervisor ensures Close stops the reconnect loop promptly.
func TestCloseStopsSupervisor(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()
	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            2 * time.Second,
		PingInterval:            500 * time.Millisecond,
		ReconnectInitialBackoff: 30 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
	}, nil, nil, nil)
	c.Start(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = c.Close()
	}()
	var done = make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung")
	}
}

// TestConnV3SubscribePush covers the V3 (UTA) coordinates end to end:
// the subscribe op carries topic / symbol (and no channel / instId), and
// a push keyed by the same V3 arg reaches the handler.
func TestConnV3SubscribePush(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()

	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             3 * time.Second,
		WriteTimeout:            2 * time.Second,
		PingInterval:            500 * time.Millisecond,
		ReconnectInitialBackoff: 50 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
	}, nil, nil, nil)
	defer c.Close()
	c.Start(context.Background())

	type pushEvent struct {
		arg     SubscriptionArg
		action  string
		payload string
		ts      int64
	}
	var pushCh chan pushEvent = make(chan pushEvent, 4)
	var sub *Subscription = &Subscription{
		Arg: SubscriptionArg{InstType: "usdt-futures", Topic: "books5", Symbol: "BTCUSDT"},
		Handler: func(arg SubscriptionArg, action string, payload []byte, tsMs int64, _ int64) {
			pushCh <- pushEvent{arg: arg, action: action, payload: string(payload), ts: tsMs}
		},
	}
	if err := c.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	select {
	case got := <-srv.subs:
		if got.InstType != "usdt-futures" || got.Topic != "books5" || got.Symbol != "BTCUSDT" {
			t.Fatalf("V3 subscribe arg = %+v", got)
		}
		if got.Channel != "" || got.InstID != "" || got.Coin != "" {
			t.Fatalf("V2 coordinates leaked into a V3 subscribe: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the V3 subscribe op")
	}

	var conn *websocket.Conn
	select {
	case conn = <-srv.conns:
	case <-time.After(time.Second):
		t.Fatal("no client conn captured")
	}
	// Live frame shape (captured 2026-09-21), trimmed to one level a side.
	var push []byte = []byte(`{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"},"data":[{"a":[["80810.4","0.8523"]],"b":[["80810.3","1.0943"]],"seq":993094633676,"pseq":0,"ts":"1789940582101"}],"ts":1789940582102}`)
	srv.writeMu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, push)
	srv.writeMu.Unlock()

	select {
	case ev := <-pushCh:
		if ev.action != "snapshot" || ev.arg.Topic != "books5" || ev.arg.Symbol != "BTCUSDT" || ev.ts != 1789940582102 {
			t.Fatalf("unexpected V3 push event: %+v", ev)
		}
		if !strings.Contains(ev.payload, `"seq":993094633676`) {
			t.Fatalf("payload not forwarded verbatim: %s", ev.payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("V3 handler not invoked")
	}
}

// TestSubscribeValidation: an arg needs a V2 channel OR a V3 topic.
func TestSubscribeValidation(t *testing.T) {
	var c *Conn = NewConn(Config{URL: "ws://127.0.0.1:1"}, nil, nil, nil)
	defer c.Close()
	var noop = func(SubscriptionArg, string, []byte, int64, int64) {}

	var err error = c.Subscribe(&Subscription{Arg: SubscriptionArg{InstType: "usdt-futures", Symbol: "BTCUSDT"}, Handler: noop})
	if !bgerr.IsInvalidRequest(err) {
		t.Fatalf("arg without channel/topic: want InvalidRequest, got %v", err)
	}
	err = c.Subscribe(&Subscription{Arg: SubscriptionArg{InstType: "UTA", Topic: "order"}})
	if !bgerr.IsInvalidRequest(err) {
		t.Fatalf("nil handler: want InvalidRequest, got %v", err)
	}
	// Queued (never started) — both generations are accepted.
	if err = c.Subscribe(&Subscription{Arg: SubscriptionArg{InstType: "UTA", Topic: "order"}, Handler: noop}); err != nil {
		t.Fatalf("V3 arg rejected: %v", err)
	}
	if err = c.Subscribe(&Subscription{Arg: SubscriptionArg{InstType: "USDT-FUTURES", Channel: "ticker", InstID: "BTCUSDT"}, Handler: noop}); err != nil {
		t.Fatalf("V2 arg rejected: %v", err)
	}
}

// TestConnOnConnectHook: reconnect=false for the first successful
// connection, reconnect=true after the server force-closes the socket.
func TestConnOnConnectHook(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()

	var events chan bool = make(chan bool, 8)
	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            2 * time.Second,
		PingInterval:            300 * time.Millisecond,
		ReconnectInitialBackoff: 30 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
		OnConnect:               func(reconnect bool) { events <- reconnect },
	}, nil, nil, nil)
	defer c.Close()

	var sub *Subscription = &Subscription{
		Arg:     SubscriptionArg{InstType: "usdt-futures", Topic: "ticker", Symbol: "BTCUSDT"},
		Handler: func(SubscriptionArg, string, []byte, int64, int64) {},
	}
	if err := c.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	c.Start(context.Background())

	select {
	case reconnect := <-events:
		if reconnect {
			t.Fatal("first connection reported reconnect=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnConnect not invoked for the first connection")
	}
	select {
	case <-srv.subs:
	case <-time.After(2 * time.Second):
		t.Fatal("initial subscribe missing")
	}

	var oldConn *websocket.Conn
	select {
	case oldConn = <-srv.conns:
	case <-time.After(time.Second):
		t.Fatal("no captured conn")
	}
	_ = oldConn.Close()

	select {
	case reconnect := <-events:
		if !reconnect {
			t.Fatal("second connection reported reconnect=false")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnConnect not invoked after the forced close")
	}
	// The hook fires after the resubscribe op was written — the server
	// must see it.
	select {
	case got := <-srv.subs:
		if got.Topic != "ticker" {
			t.Fatalf("re-subscribe arg = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client did not resubscribe after reconnect")
	}
	select {
	case extra := <-events:
		t.Fatalf("unexpected extra OnConnect(%v)", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestConnOnConnectPrivateAfterLogin: on a private endpoint the hook runs
// only after the login round-trip completed.
func TestConnOnConnectPrivateAfterLogin(t *testing.T) {
	var srv *mockServer = newMockServer(t)
	defer srv.close()

	var loginsSeenAtHook chan int = make(chan int, 2)
	var signer *auth.Signer = auth.NewSigner("k", "s", "p")
	var c *Conn = NewConn(Config{
		URL:                     srv.wsURL(),
		IsPrivate:               true,
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             3 * time.Second,
		WriteTimeout:            2 * time.Second,
		LoginTimeout:            2 * time.Second,
		PingInterval:            500 * time.Millisecond,
		ReconnectInitialBackoff: 50 * time.Millisecond,
		ReconnectMaxBackoff:     200 * time.Millisecond,
		OnConnect: func(reconnect bool) {
			if reconnect {
				return
			}
			loginsSeenAtHook <- len(srv.logins)
		},
	}, signer, nil, nil)
	defer c.Close()
	c.Start(context.Background())

	select {
	case n := <-loginsSeenAtHook:
		if n != 1 {
			t.Fatalf("OnConnect fired with %d login ops processed by the server, want 1", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnConnect not invoked on the private endpoint")
	}
}
