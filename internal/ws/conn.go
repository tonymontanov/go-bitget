/*
FILE: internal/ws/conn.go

DESCRIPTION:
Managing wrapper over a single Bitget WebSocket connection. At most two
such objects are created per domain client (mix.Client, spot.Client, ...):
one for the public endpoint (no auth, market-data channels) and one for
the private endpoint (login required, account/position/order channels).

RESPONSIBILITIES:
  - connect / reconnect with exponential backoff + jitter;
  - private-only login (op=login, see internal/auth.SignWS);
  - heartbeat (plain-text "ping" frame every PingInterval);
  - subscribe / unsubscribe with a registry keyed by (instType, channel,
    instId, coin) that survives reconnects;
  - resubscribe after every successful (re)connect, transparently to caller;
  - dispatch incoming push frames to the per-arg handler;
  - graceful shutdown via Close() or ctx cancellation.

DESIGN NOTES (DIFFERENCES VS. BYBIT WS):
  - Bitget identifies a subscription by THREE wire fields
    (instType, channel, instId/coin) instead of a single dotted topic
    string. The Subscription struct here mirrors that.
  - Bitget's keep-alive is a plain-text "ping" body in a TEXT frame
    (NOT a JSON object). The server replies with plain-text "pong".
    Sending JSON {"op":"ping"} is rejected with a "param error" event,
    which we found out the hard way during scaffolding.
  - Login response shape: {"event":"login","code":"0"} (success) or
    {"event":"error","code":"30005","msg":"..."}. Subscribe responses
    use {"event":"subscribe","arg":{...},"code":"0"}.

CONCURRENCY:
  - mu guards subs/socket/closed/cancel.
  - writeMu guards the underlying gorilla/websocket conn writes (gorilla
    requires exclusive writes).
  - Background goroutines (read-loop + ping-loop) are started afresh on
    every connect and torn down on every disconnect; the supervisor runs
    in its own goroutine for the entire lifetime of Conn.

ERROR STRATEGY:
  - Read errors → readLoop exits with that error → supervise reconnects
    after backoff.
  - Application-level "wrong subscription" or "auth failed" replies are
    logged and counted in metrics, but the supervisor keeps trying. This
    avoids the "transient backend hiccup → permanent connector death"
    failure mode.
  - On Conn.Close() the supervisor exits cleanly without further
    reconnect attempts.
*/

package ws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/tonymontanov/go-bitget/v2/internal/auth"
	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
	"github.com/tonymontanov/go-bitget/v2/internal/bglog"
	"github.com/tonymontanov/go-bitget/v2/internal/bgmet"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
)

// ErrConnClosed is returned by operations performed on a closed Conn.
var ErrConnClosed = errors.New("ws: connection closed")

// Subscription describes a single Bitget channel subscription. The caller
// (domain stream package) constructs it once and passes it to Subscribe;
// the same Subscription is reused on every reconnect via its Reset hook.
type Subscription struct {
	// Arg — instType / channel / instId / coin tuple. Required;
	// Channel must be non-empty.
	Arg SubscriptionArg
	// Handler is invoked for every push frame whose arg matches. Args:
	//
	//   - arg     : the wire arg (so a single handler can serve multiple
	//               symbols if the caller wants);
	//   - action  : "snapshot" | "update";
	//   - payload : the raw bytes of the "data" field;
	//   - tsMs    : push timestamp in ms;
	//   - checksum: sequence/checksum (Bitget books channel ships a CRC32
	//               in this field; 0 means absent).
	//
	// Push frames whose data field is missing or null still call the
	// handler with payload=nil — handlers must be defensive.
	Handler func(arg SubscriptionArg, action string, payload []byte, tsMs int64, checksum int64)
	// Reset is called once before every (re)subscribe. Used by the
	// orderbook engine to drop any local state so the next snapshot
	// pushed by the server is treated as the new authoritative state.
	// May be nil.
	Reset func()
}

// Config — parameters for a single Bitget WS connection. Populated from
// the public root config via field-by-field copy.
type Config struct {
	// URL — wss://ws.bitget.com/v2/ws/public,
	//       wss://ws.bitget.com/v2/ws/private (UTA: /v3/...).
	URL string
	// IsPrivate — true for the private endpoint (login required).
	IsPrivate bool
	// HandshakeTimeout — TLS+HTTP upgrade timeout.
	HandshakeTimeout time.Duration
	// ReadTimeout — read deadline used to detect a silent server. Should be
	// >= 1.5 * PingInterval so a single dropped pong does not trigger a
	// reconnect.
	ReadTimeout time.Duration
	// WriteTimeout — write deadline.
	WriteTimeout time.Duration
	// PingInterval — interval between application-level "ping" frames.
	// Bitget's server-side timeout is 30s; recommended 20-25s.
	PingInterval time.Duration
	// LoginTimeout — how long to wait for the login ack. Default
	// 30s (see root Config.WS.LoginTimeout for rationale).
	LoginTimeout time.Duration
	// ReconnectInitialBackoff — first sleep after a connection failure.
	ReconnectInitialBackoff time.Duration
	// ReconnectMaxBackoff — cap for the exponential backoff.
	ReconnectMaxBackoff time.Duration
	// ReconnectJitter — random multiplier [1-j, 1+j] applied to backoff.
	// 0 disables jitter.
	ReconnectJitter float64
	// ReadBufferSize / WriteBufferSize — gorilla/websocket buffer sizes.
	ReadBufferSize  int
	WriteBufferSize int
}

// Conn — managing wrapper over a single Bitget WS connection.
type Conn struct {
	cfg     Config
	signer  *auth.Signer
	logger  bglog.Logger
	metrics bgmet.CounterFactory

	mu     sync.RWMutex
	subs   map[string]*Subscription
	socket *websocket.Conn
	closed bool
	cancel context.CancelFunc

	writeMu sync.Mutex

	startOnce sync.Once

	cReceived bgmet.Counter
	cDropped  bgmet.Counter
	cReconn   bgmet.Counter
	cSub      bgmet.Counter
	cPingErr  bgmet.Counter
	cAuthOK   bgmet.Counter
	cAuthFail bgmet.Counter
}

// pingPayload is the plain-text body Bitget expects on ping frames.
var pingPayload = []byte(`ping`)

// pongPayload is the plain-text body Bitget sends back.
var pongPayload = []byte(`pong`)

// NewConn creates a Conn. No network activity occurs until Start (or the
// first Subscribe) is called. log/mf may be nil.
func NewConn(cfg Config, signer *auth.Signer, log bglog.Logger, mf bgmet.CounterFactory) *Conn {
	if log == nil {
		log = bglog.Noop()
	}
	if mf == nil {
		mf = bgmet.Noop()
	}
	if cfg.LoginTimeout <= 0 {
		cfg.LoginTimeout = 5 * time.Second
	}
	return &Conn{
		cfg:       cfg,
		signer:    signer,
		logger:    log,
		metrics:   mf,
		subs:      make(map[string]*Subscription, 16),
		cReceived: mf.Counter("bitget_ws_messages_received_total"),
		cDropped:  mf.Counter("bitget_ws_messages_dropped_total"),
		cReconn:   mf.Counter("bitget_ws_reconnects_total"),
		cSub:      mf.Counter("bitget_ws_subscriptions_total"),
		cPingErr:  mf.Counter("bitget_ws_ping_failed_total"),
		cAuthOK:   mf.Counter("bitget_ws_auth_ok_total"),
		cAuthFail: mf.Counter("bitget_ws_auth_failed_total"),
	}
}

// Start launches the background supervisor (idempotent). It returns
// immediately; the supervisor exits when ctx is cancelled or Close is
// called.
func (c *Conn) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		var supCtx context.Context
		supCtx, c.cancel = context.WithCancel(ctx)
		go c.supervise(supCtx)
	})
}

// Subscribe registers a subscription and, if the socket is up, sends the
// subscribe op immediately. Otherwise the subscription waits in the
// registry and is sent automatically on the next successful (re)connect.
func (c *Conn) Subscribe(sub *Subscription) error {
	if sub == nil || sub.Arg.Channel == "" || sub.Handler == nil {
		return bgerr.New(bgerr.ErrorKindInvalidRequest, "", "ws: invalid subscription", nil)
	}
	var key string = sub.Arg.Key()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrConnClosed
	}
	c.subs[key] = sub
	var socket *websocket.Conn = c.socket
	c.mu.Unlock()
	c.cSub.Inc()

	if socket == nil {
		return nil
	}
	return c.sendOp(socket, "subscribe", []SubscriptionArg{sub.Arg})
}

// Unsubscribe removes the arg from the registry. If the socket is up,
// an unsubscribe op is sent. No error is returned for already-unknown
// args — Unsubscribe is idempotent.
func (c *Conn) Unsubscribe(arg SubscriptionArg) error {
	var key string = arg.Key()
	c.mu.Lock()
	delete(c.subs, key)
	var socket *websocket.Conn = c.socket
	c.mu.Unlock()
	if socket == nil {
		return nil
	}
	return c.sendOp(socket, "unsubscribe", []SubscriptionArg{arg})
}

// Close stops the supervisor and the underlying socket. Idempotent.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	var s *websocket.Conn = c.socket
	c.socket = nil
	c.mu.Unlock()

	if s != nil {
		_ = s.Close()
	}
	return nil
}

// supervise is the connect → run → backoff loop. Exits on ctx.Done.
func (c *Conn) supervise(ctx context.Context) {
	var backoff time.Duration = c.cfg.ReconnectInitialBackoff
	var attempt int
	for {
		if ctx.Err() != nil {
			return
		}
		var err error = c.connectAndRun(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.logger.Warn("ws: connection error, will reconnect",
				bglog.Str("url", c.cfg.URL),
				bglog.Int("attempt", int64(attempt)),
				bglog.Err(err),
			)
		}
		c.cReconn.Inc()
		attempt++

		var sleep time.Duration = applyJitter(backoff, c.cfg.ReconnectJitter)
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
		backoff = nextBackoff(backoff, c.cfg.ReconnectMaxBackoff)
	}
}

// connectAndRun owns one full connection lifecycle: dial → login (if
// private) → resubscribe → read-loop + ping-loop.
func (c *Conn) connectAndRun(ctx context.Context) error {
	var dialer *websocket.Dialer = &websocket.Dialer{
		HandshakeTimeout: c.cfg.HandshakeTimeout,
		ReadBufferSize:   c.cfg.ReadBufferSize,
		WriteBufferSize:  c.cfg.WriteBufferSize,
	}
	var socket *websocket.Conn
	var err error
	socket, _, err = dialer.DialContext(ctx, c.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	c.logger.Info("ws: connected", bglog.Str("url", c.cfg.URL))

	// Snapshot the current subscriptions and call Reset BEFORE we publish
	// the new socket — that way a stale push that arrived on the previous
	// socket cannot race with the engine reset.
	c.mu.Lock()
	c.socket = socket
	var subsCopy []*Subscription = make([]*Subscription, 0, len(c.subs))
	var s *Subscription
	for _, s = range c.subs {
		if s.Reset != nil {
			s.Reset()
		}
		subsCopy = append(subsCopy, s)
	}
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.socket == socket {
			c.socket = nil
		}
		c.mu.Unlock()
		_ = socket.Close()
	}()

	if c.cfg.IsPrivate {
		if err = c.performLogin(socket); err != nil {
			c.cAuthFail.Inc()
			return fmt.Errorf("login: %w", err)
		}
		c.cAuthOK.Inc()
	}

	// Resubscribe everything that survived the previous disconnect.
	if len(subsCopy) > 0 {
		var argsBatch []SubscriptionArg = make([]SubscriptionArg, len(subsCopy))
		var i int
		for i = 0; i < len(subsCopy); i++ {
			argsBatch[i] = subsCopy[i].Arg
		}
		if err = c.sendOp(socket, "subscribe", argsBatch); err != nil {
			c.logger.Warn("ws: resubscribe failed", bglog.Err(err))
		}
	}

	var loopCtx context.Context
	var loopCancel context.CancelFunc
	loopCtx, loopCancel = context.WithCancel(ctx)
	defer loopCancel()

	var wg sync.WaitGroup
	wg.Add(2)
	var readErr error
	go func() {
		defer wg.Done()
		defer loopCancel()
		readErr = c.readLoop(loopCtx, socket)
	}()
	go func() {
		defer wg.Done()
		c.pingLoop(loopCtx, socket)
	}()
	wg.Wait()

	if readErr != nil {
		return readErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// performLogin sends {"op":"login","args":[{apiKey, passphrase, ts, sign}]}
// and waits for the matching {"event":"login","code":"0"} reply.
func (c *Conn) performLogin(socket *websocket.Conn) error {
	if c.signer == nil || !c.signer.Enabled() {
		return errors.New("ws: private endpoint requires signer with credentials")
	}
	// Bitget V2 WS login takes the timestamp in SECONDS (not ms — REST
	// uses ms, WS doesn't). Sending ms made the server silently drop the
	// login frame and the client timed out on its login deadline. See
	// internal/auth/sign.go for the docs trail.
	var ts string = c.signer.SecondsTimestamp(time.Time{})
	var signature string
	var err error
	signature, err = c.signer.SignWS(ts)
	if err != nil {
		return err
	}
	var op outboundOp = outboundOp{
		Op: "login",
		Args: []loginArgs{{
			APIKey:     c.signer.APIKey(),
			Passphrase: c.signer.Passphrase(),
			Timestamp:  ts,
			Sign:       signature,
		}},
	}
	var raw []byte
	raw, err = codec.Marshal(op)
	if err != nil {
		return err
	}
	// Diagnostic log: surface the exact timestamp length (10 = seconds,
	// 13 = milliseconds) and signature length so operators can verify
	// from the application log that this binary actually contains the
	// v1.0.2+ fix without having to inspect the wire. Credentials and
	// the sign value itself are NOT logged.
	c.logger.Info("ws: sending login",
		bglog.Int("ts_len", int64(len(ts))),
		bglog.Int("sig_len", int64(len(signature))),
		bglog.Int("expected_ts_len", 10),
	)
	if err = c.writeFrame(socket, websocket.TextMessage, raw); err != nil {
		return err
	}

	var deadline time.Time = time.Now().Add(c.cfg.LoginTimeout)
	_ = socket.SetReadDeadline(deadline)
	defer func() { _ = socket.SetReadDeadline(time.Time{}) }()

	// Read up to 10 frames: pongs / push frames may arrive before the
	// login ack on a busy connection. We do NOT read forever — the
	// surrounding deadline above guarantees progress.
	var i int
	var sawAnyFrame bool
	for i = 0; i < 10; i++ {
		var msgType int
		var body []byte
		msgType, body, err = socket.ReadMessage()
		if err != nil {
			// Wrap with timeout context so operators can distinguish
			// "login was rejected" from "ack never arrived in time".
			// The latter typically points at overlay-network RTT
			// (Cloudflare WARP / VPN), not a credentials problem —
			// see config.WsConfig.LoginTimeout for the knob to raise.
			//
			// If we never saw a single frame from the server between
			// "ws: connected" and the deadline, this is the smoking
			// gun for an overlay-network drop: TLS handshake completes
			// but post-upgrade text frames never arrive. Surface that
			// explicitly so operators don't waste time on credentials.
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				if !sawAnyFrame {
					return fmt.Errorf("login ack not received within %s and no frames seen since connect (overlay-network likely dropping post-upgrade frames; raise WS.LoginTimeout or bypass VPN/WARP): %w",
						c.cfg.LoginTimeout, err)
				}
				return fmt.Errorf("login ack not received within %s (raise WS.LoginTimeout or check network/VPN routing): %w",
					c.cfg.LoginTimeout, err)
			}
			return err
		}
		sawAnyFrame = true
		if msgType != websocket.TextMessage {
			continue
		}
		// Plain-text "pong" before login ack — skip. Worth logging once
		// so operators see SOMETHING came back during the wait: an
		// unsolicited pong proves the post-upgrade direction is alive
		// and narrows the hunt to the login frame round-trip itself.
		if bytes.Equal(body, pongPayload) {
			c.logger.Debug("ws: pong received during login wait")
			continue
		}
		var env Envelope
		if err = codec.Unmarshal(body, &env); err != nil {
			// Surface a truncated body sample so operators can see WHY
			// the parse failed without dumping potentially-large pushes
			// to the log. 200 bytes is enough to capture login acks /
			// errors (98-byte frames have been observed in the field).
			var sample string = string(body)
			if len(sample) > 200 {
				sample = sample[:200] + "..."
			}
			c.logger.Debug("ws: unparseable frame during login wait",
				bglog.Int("body_len", int64(len(body))),
				bglog.Str("body_sample", sample),
				bglog.Err(err))
			continue
		}
		if env.Event != "login" && env.Event != "error" {
			// Push or subscribe-ack from a prior connection state —
			// surface in debug so the wait isn't a black box.
			c.logger.Debug("ws: non-login frame during login wait",
				bglog.Str("event", env.Event),
				bglog.Str("code", env.Code.String()))
			continue
		}
		if env.Event == "login" && env.Code == "0" {
			c.logger.Info("ws: login ok")
			return nil
		}
		return fmt.Errorf("login rejected: code=%s msg=%s", env.Code.String(), env.Msg)
	}
	return errors.New("ws: login ack not received")
}

// readLoop reads frames and dispatches push frames to subscription handlers.
// Returns the read error so supervise can decide whether to reconnect.
func (c *Conn) readLoop(ctx context.Context, socket *websocket.Conn) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = socket.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
		var msgType int
		var raw []byte
		var err error
		msgType, raw, err = socket.ReadMessage()
		if err != nil {
			return err
		}
		if msgType != websocket.TextMessage {
			continue
		}
		c.cReceived.Inc()

		// Plain-text "pong" — a heartbeat reply, not a push frame.
		if bytes.Equal(raw, pongPayload) {
			continue
		}

		var env Envelope
		if err = codec.Unmarshal(raw, &env); err != nil {
			c.cDropped.Inc()
			c.logger.Warn("ws: failed to parse envelope", bglog.Err(err))
			continue
		}

		if env.IsControl() {
			c.handleControl(&env)
			continue
		}
		if !env.IsPush() {
			c.cDropped.Inc()
			continue
		}

		var key string = env.Arg.Key()
		c.mu.RLock()
		var sub *Subscription = c.subs[key]
		c.mu.RUnlock()
		if sub == nil {
			c.cDropped.Inc()
			c.logger.Debug("ws: push for unknown arg",
				bglog.Str("key", key),
			)
			continue
		}
		sub.Handler(env.Arg, env.Action, env.Data, env.TsMs, env.Checksum)
	}
}

// handleControl logs ack frames and counts authentication outcomes that
// were not consumed by the synchronous performLogin path (e.g. login that
// arrives unexpectedly mid-stream).
func (c *Conn) handleControl(env *Envelope) {
	switch env.Event {
	case "subscribe":
		c.logger.Debug("ws: subscribed",
			bglog.Str("channel", env.Arg.Channel),
			bglog.Str("instId", env.Arg.InstID),
		)
	case "unsubscribe":
		c.logger.Debug("ws: unsubscribed",
			bglog.Str("channel", env.Arg.Channel),
			bglog.Str("instId", env.Arg.InstID),
		)
	case "login":
		c.logger.Debug("ws: login ack",
			bglog.Str("code", env.Code.String()),
		)
	case "error":
		c.logger.Warn("ws: server error",
			bglog.Str("code", env.Code.String()),
			bglog.Str("msg", env.Msg),
		)
	default:
		c.logger.Debug("ws: control",
			bglog.Str("event", env.Event),
			bglog.Str("msg", env.Msg),
		)
	}
}

// pingLoop sends a plain-text "ping" frame on a ticker. Exits on the first
// write error; the read-loop will fail too and supervise will reconnect.
func (c *Conn) pingLoop(ctx context.Context, socket *websocket.Conn) {
	if c.cfg.PingInterval <= 0 {
		return
	}
	var ticker *time.Ticker = time.NewTicker(c.cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.writeFrame(socket, websocket.TextMessage, pingPayload); err != nil {
				c.cPingErr.Inc()
				c.logger.Debug("ws: ping write failed", bglog.Err(err))
				return
			}
		}
	}
}

// sendOp marshals an op JSON and writes it. args must be a slice (Bitget
// expects a JSON array under "args" — even for a single item).
func (c *Conn) sendOp(socket *websocket.Conn, op string, args []SubscriptionArg) error {
	var msg outboundOp = outboundOp{Op: op, Args: args}
	var raw []byte
	var err error
	raw, err = codec.Marshal(msg)
	if err != nil {
		return err
	}
	return c.writeFrame(socket, websocket.TextMessage, raw)
}

// writeFrame is a thread-safe text-frame write. gorilla/websocket requires
// exclusive writes — the dedicated mutex keeps ping/sub/login from
// stepping on each other.
func (c *Conn) writeFrame(socket *websocket.Conn, msgType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = socket.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
	return socket.WriteMessage(msgType, data)
}

// nextBackoff doubles cur, capping at max.
func nextBackoff(cur, max time.Duration) time.Duration {
	cur *= 2
	if cur > max {
		cur = max
	}
	return cur
}

// applyJitter multiplies d by a random factor in [1-j, 1+j].
func applyJitter(d time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return d
	}
	var f float64 = 1.0 + (rand.Float64()*2.0-1.0)*jitter
	return time.Duration(float64(d) * f)
}
