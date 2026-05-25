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
    subscription is sent again automatically.
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
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"login","code":"0"}`))
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
				_ = conn.WriteMessage(websocket.TextMessage, ack)
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
	_ = conn.WriteMessage(websocket.TextMessage, push)

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
