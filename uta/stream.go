/*
FILE: uta/stream.go

DESCRIPTION:
WebSocket sub-client for the Bitget V3 Unified Trading Account: connection
lifecycle, the multi-handler fan-out shared by every topic, and the three
PUBLIC Watch* primitives

	WatchTicker       → "ticker"       (last / BBO / mark / index / funding)
	WatchPublicTrades → "publicTrade"  (public tape, one call per trade)
	WatchOrderBook    → "books1|5|50"  (stateless venue snapshots)
	                    "books"        (local incremental book, seq-validated;
	                                    see stream-book.go)

The private topics (order / fill / position / account) live in
stream-private.go and ride the same fan-out.

WIRE COORDINATES (V3 differs from V2):

	{"op":"subscribe","args":[{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"}]}

instType is the LOWER-CASE category ("spot", "usdt-futures", "coin-futures",
"usdc-futures"); `topic` / `symbol` replace the V2 `channel` / `instId`.
Verified against live frames from wss://ws.bitget.com/v3/ws/public
(2026-09-21). MARGIN has no public market-data stream (it trades the spot
book) and is rejected client-side. Topics that do NOT exist on V3: books15,
books200 (the venue answers event=error code=30001).

DESIGN:

  - Two LAZY *ws.Conn per StreamClient — public on the first public Watch*,
    private on the first private Watch* — built from the root Config.WS
    timeouts and the UTA endpoints (WS.UTAPublicURL / WS.UTAPrivateURL,
    which resolve to the wspap demo host when Config.Demo is set).
    Reconnect / relogin / resubscribe is handled by ws.Conn.

  - MULTI-HANDLER FAN-OUT. Several Watch* calls for the same wire arg
    (four consumers of WatchTicker(BTCUSDT), three consumers of
    WatchOrders) share ONE wire subscription. Every caller's handler is
    detached when ITS ctx is cancelled; the wire unsubscribe is sent only
    when the last handler of that arg detaches. (In the V2 profiles a
    second Watch* for the same arg overwrites the first handler and one
    ctx cancel kills everybody — consumers had to build their own
    fan-out.) Handlers of one arg run sequentially, in registration order,
    on the connection's read goroutine. The handler list is a
    copy-on-write slice behind an atomic pointer: the read path takes no
    lock and allocates nothing for the dispatch.

  - A handler attached to an ALREADY LIVE wire subscription does not
    receive what the venue pushed before it joined (notably the initial
    position / account snapshot) — seed such consumers from REST.

  - Per-Watch ctx scopes the HANDLER lifetime, not the connection's.
    ctx == nil is allowed: the handler lives until Close.

  - Hot decoders (ticker, books, publicTrade) use the allocation-light
    codec.Wire* field types and per-subscription scratch rows; what is
    left per frame is the big.Int inside every decimal plus one slice
    allocation for a delivered book (see stream_bench_test.go).

ERROR PROPAGATION:

  - Subscribe acks / venue error events are logged by ws.Conn (the venue
    does not echo which subscription an error belongs to).
  - Decode errors and order-book resyncs go to the errHandler of EVERY
    handler attached to the affected wire subscription (nil allowed —
    the error is still logged at debug level and the stream recovers on
    its own).

CALLBACK CONTRACT:
Handlers and reconnect callbacks run on the connection's goroutines —
they must be fast and must not block; hand slow work off to a channel.
*/

package uta

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgmet"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// Public topic names — kept as constants so a typo surfaces at compile
// time rather than as a venue 30001.
const (
	topicTicker      = "ticker"
	topicPublicTrade = "publicTrade"
	topicBooks       = "books"
	topicBooks1      = "books1"
	topicBooks5      = "books5"
	topicBooks50     = "books50"
)

// Push actions.
const (
	actionSnapshot = "snapshot"
	actionUpdate   = "update"
)

// ---------------------------------------------------------------------
// Fan-out core.
// ---------------------------------------------------------------------

// handlerEntry — one consumer attached to a wire subscription.
type handlerEntry[T any] struct {
	id         uint64
	handler    func(T)
	errHandler func(error)
}

// wireSub — ONE wire subscription shared by N consumer handlers. Embedded
// by the per-topic subscription structs, which add their decode scratch.
type wireSub[T any] struct {
	// arg — the wire coordinates; arg.Key() is the registry key.
	arg ws.SubscriptionArg
	// sub — the object registered with ws.Conn. Retained so an order-book
	// resync can re-Subscribe the SAME object after Unsubscribe removed
	// it from the Conn registry.
	sub *ws.Subscription
	// scope — "Stream.WatchTicker" etc., used in error / log messages.
	scope string

	// entries — copy-on-write handler list. Readers (the read goroutine)
	// Load it lock-free; writers replace it under the owning side's mutex.
	entries atomic.Pointer[[]handlerEntry[T]]
	// nextID — handler id generator; guarded by the owning side's mutex.
	nextID uint64
}

// subscriber is implemented by every per-topic subscription struct through
// the embedded wireSub; it lets attachHandler / detachHandler stay generic.
type subscriber[T any] interface {
	base() *wireSub[T]
}

func (w *wireSub[T]) base() *wireSub[T] { return w }

// add appends a handler (registration order = invocation order). The
// caller holds the owning side's mutex.
func (w *wireSub[T]) add(handler func(T), errHandler func(error)) uint64 {
	w.nextID++
	var id uint64 = w.nextID
	var old *[]handlerEntry[T] = w.entries.Load()
	var oldLen int = 0
	if old != nil {
		oldLen = len(*old)
	}
	var next []handlerEntry[T] = make([]handlerEntry[T], 0, oldLen+1)
	if old != nil {
		next = append(next, *old...)
	}
	next = append(next, handlerEntry[T]{id: id, handler: handler, errHandler: errHandler})
	w.entries.Store(&next)
	return id
}

// remove detaches the handler with the given id and returns how many
// handlers remain. The caller holds the owning side's mutex.
func (w *wireSub[T]) remove(id uint64) int {
	var old *[]handlerEntry[T] = w.entries.Load()
	if old == nil {
		return 0
	}
	var next []handlerEntry[T] = make([]handlerEntry[T], 0, len(*old))
	var i int
	for i = 0; i < len(*old); i++ {
		if (*old)[i].id != id {
			next = append(next, (*old)[i])
		}
	}
	w.entries.Store(&next)
	return len(next)
}

// deliver invokes every attached handler, in registration order. Lock-free
// and allocation-free: a concurrent attach / detach swaps the slice
// pointer and becomes visible from the next frame on.
func (w *wireSub[T]) deliver(v T) {
	var list *[]handlerEntry[T] = w.entries.Load()
	if list == nil {
		return
	}
	var i int
	for i = 0; i < len(*list); i++ {
		(*list)[i].handler(v)
	}
}

// fail forwards err to every attached errHandler (nil ones are skipped).
func (w *wireSub[T]) fail(err error) {
	var list *[]handlerEntry[T] = w.entries.Load()
	if list == nil {
		return
	}
	var i int
	for i = 0; i < len(*list); i++ {
		if (*list)[i].errHandler != nil {
			(*list)[i].errHandler(err)
		}
	}
}

// ---------------------------------------------------------------------
// Connection sides.
// ---------------------------------------------------------------------

// reconnectHook — one OnPublicReconnect / OnPrivateReconnect callback.
type reconnectHook struct {
	id uint64
	fn func()
}

// connSide bundles one lazily-built connection (public or private) with
// the lock that serialises everything touching its subscription registry.
//
// LOCK ORDER: side.mu → ws.Conn internals. Nothing reachable from a
// ws.Conn callback (frame handlers, Reset hooks, OnConnect) takes side.mu,
// so holding it across Conn.Subscribe / Unsubscribe cannot deadlock.
type connSide struct {
	mu     sync.Mutex
	conn   *ws.Conn
	closed bool

	// hooks — copy-on-write reconnect callbacks, read lock-free from the
	// ws.Conn supervisor goroutine.
	hooks      atomic.Pointer[[]reconnectHook]
	nextHookID uint64
}

// addHook registers fn and returns an idempotent remove func.
func (side *connSide) addHook(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	side.mu.Lock()
	side.nextHookID++
	var id uint64 = side.nextHookID
	var old *[]reconnectHook = side.hooks.Load()
	var next []reconnectHook
	if old != nil {
		next = append(next, *old...)
	}
	next = append(next, reconnectHook{id: id, fn: fn})
	side.hooks.Store(&next)
	side.mu.Unlock()

	return func() {
		side.mu.Lock()
		defer side.mu.Unlock()
		var cur *[]reconnectHook = side.hooks.Load()
		if cur == nil {
			return
		}
		var kept []reconnectHook = make([]reconnectHook, 0, len(*cur))
		var i int
		for i = 0; i < len(*cur); i++ {
			if (*cur)[i].id != id {
				kept = append(kept, (*cur)[i])
			}
		}
		side.hooks.Store(&kept)
	}
}

// onConnect is wired into ws.Config.OnConnect. Only RE-connects are
// reported: the first connection carries no state to re-seed.
func (side *connSide) onConnect(reconnect bool) {
	if !reconnect {
		return
	}
	var list *[]reconnectHook = side.hooks.Load()
	if list == nil {
		return
	}
	var i int
	for i = 0; i < len(*list); i++ {
		(*list)[i].fn()
	}
}

// ---------------------------------------------------------------------
// StreamClient.
// ---------------------------------------------------------------------

// tickerSub — wire subscription of one (category, symbol) ticker.
type tickerSub struct {
	wireSub[utatypes.TickerUpdate]
	category utatypes.Category
	symbol   string
	// rows — decode scratch; touched only by the read goroutine.
	rows []wsTickerRow
}

// tradeSub — wire subscription of one (category, symbol) public tape.
type tradeSub struct {
	wireSub[utatypes.PublicTradeUpdate]
	category utatypes.Category
	symbol   string
	// rows — decode scratch; touched only by the read goroutine.
	rows []wsTradeRow
}

// StreamClient — V3 UTA WebSocket subscription sub-client. Safe for
// concurrent use.
type StreamClient struct {
	c *Client

	closeOnce sync.Once
	// closed is closed by Close; it releases the per-handler ctx watchers.
	closed chan struct{}

	public  connSide
	private connSide

	// Wire-subscription registries, keyed by ws.SubscriptionArg.Key().
	// Guarded by public.mu.
	tickers map[string]*tickerSub
	trades  map[string]*tradeSub
	books   map[string]*bookSub

	// Guarded by private.mu. One entry per topic (the private topics are
	// account-wide), kept as maps so they share the generic fan-out.
	orders    map[string]*orderSub
	fills     map[string]*fillSub
	positions map[string]*positionSub
	accounts  map[string]*accountSub

	// bookResyncTimeout — see the const of the same name; a field so
	// tests can shorten it.
	bookResyncTimeout time.Duration
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{
		c:                 c,
		closed:            make(chan struct{}),
		bookResyncTimeout: bookResyncTimeout,
		tickers:           make(map[string]*tickerSub, 8),
		trades:            make(map[string]*tradeSub, 8),
		books:             make(map[string]*bookSub, 8),
		orders:            make(map[string]*orderSub, 1),
		fills:             make(map[string]*fillSub, 1),
		positions:         make(map[string]*positionSub, 1),
		accounts:          make(map[string]*accountSub, 1),
	}
}

// Close shuts the public and the private connection down and releases
// every ctx watcher. Idempotent. After Close every Watch* returns an
// ErrorKindInvalidRequest error.
func (s *StreamClient) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.closeSide(&s.public)
		s.closeSide(&s.private)
	})
	return nil
}

func (s *StreamClient) closeSide(side *connSide) {
	side.mu.Lock()
	defer side.mu.Unlock()
	side.closed = true
	if side.conn != nil {
		_ = side.conn.Close()
	}
}

// OnPublicReconnect registers fn to be called after every successful
// RE-connect of the public connection (resubscribe already sent) — never
// for the first connect. Any number of callbacks may be registered, also
// before the connection exists; they run in registration order on the
// connection's supervisor goroutine BEFORE reading resumes, so fn must
// not block. The returned func removes the callback (idempotent).
//
// A local `books` order book needs no action here: the SDK drops it on
// reconnect and rebuilds it from the venue's fresh snapshot.
func (s *StreamClient) OnPublicReconnect(fn func()) (remove func()) {
	return s.public.addHook(fn)
}

// OnPrivateReconnect registers fn to be called after every successful
// RE-connect of the private connection (login ok + resubscribe sent) —
// never for the first connect. Order / fill events that happened while
// the socket was down are NOT replayed by the venue: use this callback to
// re-seed open orders / positions / balances over REST. Same threading
// contract as OnPublicReconnect: fn must not block.
func (s *StreamClient) OnPrivateReconnect(fn func()) (remove func()) {
	return s.private.addHook(fn)
}

// ReconnectPrivate drops the private socket so it is dialled, logged in
// and resubscribed afresh; OnPrivateReconnect callbacks fire when the
// new socket is up. It is the hook for a watchdog that sees the private
// channel silent while the socket is alive (own fills arriving over
// REST but no order / position pushes). Returns at once; nil when no
// private socket exists yet; an ErrorKindInvalidRequest error after
// Close. reason is logged only.
func (s *StreamClient) ReconnectPrivate(reason string) error {
	return s.reconnectSide(&s.private, "Stream.ReconnectPrivate", reason)
}

// ReconnectPublic — same as ReconnectPrivate for the public socket
// (local `books` order books are rebuilt from the fresh snapshots).
func (s *StreamClient) ReconnectPublic(reason string) error {
	return s.reconnectSide(&s.public, "Stream.ReconnectPublic", reason)
}

func (s *StreamClient) reconnectSide(side *connSide, scope, reason string) error {
	side.mu.Lock()
	defer side.mu.Unlock()
	if side.closed {
		return errInvalid(scope, "stream client is closed")
	}
	if side.conn == nil {
		return nil
	}
	return side.conn.Reconnect(reason)
}

// ensureConn returns the lazily-constructed connection of the given side.
// The first call builds the ws.Conn and starts its supervisor (the dial
// itself is asynchronous); later calls return the same instance.
func (s *StreamClient) ensureConn(side *connSide, private bool, scope string) (*ws.Conn, error) {
	if private && !s.c.signerEnabled() {
		return nil, bitget.NewError(bitget.ErrorKindAuth, "",
			"uta.Stream: private topics require API key + secret + passphrase", nil)
	}

	side.mu.Lock()
	defer side.mu.Unlock()
	if side.closed {
		return nil, errInvalid(scope, "stream client is closed")
	}
	if side.conn != nil {
		return side.conn, nil
	}

	var cfg bitget.Config = s.c.config()
	var url string = cfg.WS.UTAPublicURL
	if private {
		url = cfg.WS.UTAPrivateURL
	}
	var wsCfg ws.Config = ws.Config{
		URL:                     url,
		IsPrivate:               private,
		HandshakeTimeout:        cfg.WS.HandshakeTimeout,
		ReadTimeout:             cfg.WS.ReadTimeout,
		WriteTimeout:            cfg.WS.WriteTimeout,
		PingInterval:            cfg.WS.PingInterval,
		LoginTimeout:            cfg.WS.LoginTimeout,
		ReconnectInitialBackoff: cfg.WS.ReconnectInitialBackoff,
		ReconnectMaxBackoff:     cfg.WS.ReconnectMaxBackoff,
		ReconnectJitter:         cfg.WS.ReconnectJitter,
		ReadBufferSize:          cfg.WS.ReadBufferSize,
		WriteBufferSize:         cfg.WS.WriteBufferSize,
		WriteRateLimit:          cfg.WS.WriteRateLimit,
		OnConnect:               side.onConnect,
	}

	var metricsFactory bgmet.CounterFactory = cfg.Metrics
	if metricsFactory == nil {
		metricsFactory = bgmet.Noop()
	}

	// LOGIN (private side): V3 reuses the V2 login verbatim — op=login,
	// args[{apiKey, passphrase, timestamp, sign}], prehash
	// timestamp+"GET"+"/user/verify", timestamp in SECONDS. The V3
	// quick-start text says "milliseconds", but its own Java sample
	// divides currentTimeMillis by 1000, its JSON sample carries a
	// 10-digit value and the reference client (tiagosiebler/bitget-api)
	// signs V3 with seconds; V2 shipped the same doc bug and milliseconds
	// made the server silently drop the login. ws.Conn.performLogin is
	// therefore used as is.
	if private {
		side.conn = ws.NewConn(wsCfg, s.c.parent.Signer(), s.c.logger(), metricsFactory)
	} else {
		// The public stream needs no signer; ws.NewConn accepts nil.
		side.conn = ws.NewConn(wsCfg, nil, s.c.logger(), metricsFactory)
	}
	// The connection outlives any single Watch* ctx — it is supervised by
	// a background ctx and torn down by Close.
	side.conn.Start(context.Background())
	return side.conn, nil
}

// attachHandler registers handler on the wire subscription of arg,
// creating (and subscribing) it when this is the first handler. The new
// handler is added BEFORE the subscribe op goes out, so the venue's first
// push (the position / account snapshot) cannot be missed. onAttach, when
// non-nil, runs under the side lock right after the handler was added.
func attachHandler[T any, S subscriber[T]](
	s *StreamClient,
	side *connSide,
	conn *ws.Conn,
	registry map[string]S,
	arg ws.SubscriptionArg,
	scope string,
	create func() S,
	ctx context.Context,
	handler func(T),
	errHandler func(error),
	onAttach func(sub S, id uint64),
	onDetach func(sub S, id uint64),
) error {
	var key string = arg.Key()

	side.mu.Lock()
	if side.closed {
		side.mu.Unlock()
		return errInvalid(scope, "stream client is closed")
	}
	var sub S
	var exists bool
	sub, exists = registry[key]
	if !exists {
		sub = create()
	}
	var id uint64 = sub.base().add(handler, errHandler)
	if onAttach != nil {
		onAttach(sub, id)
	}
	if !exists {
		registry[key] = sub
		if err := conn.Subscribe(sub.base().sub); err != nil {
			// ws.Conn registers the subscription BEFORE it writes the op,
			// so a failed write would leave it in the Conn registry and
			// the next reconnect would silently revive a subscription
			// whose Watch* call reported an error. Drop both sides.
			delete(registry, key)
			_ = conn.Unsubscribe(arg)
			side.mu.Unlock()
			return err
		}
	}
	side.mu.Unlock()

	// A nil ctx, or one that can never be cancelled (context.Background),
	// needs no watcher goroutine: the handler lives until Close.
	if ctx == nil || ctx.Done() == nil {
		return nil
	}
	go func() {
		select {
		case <-ctx.Done():
			detachHandler(side, registry, key, sub, id, onDetach)
		case <-s.closed:
		}
	}()
	return nil
}

// detachHandler removes one handler; the wire unsubscribe is sent only
// when it was the last handler of the arg. `sub` guards against removing
// a NEWER subscription that reused the key after this one was torn down.
func detachHandler[T any, S subscriber[T]](
	side *connSide,
	registry map[string]S,
	key string,
	sub S,
	id uint64,
	onDetach func(sub S, id uint64),
) {
	side.mu.Lock()
	defer side.mu.Unlock()
	var remaining int = sub.base().remove(id)
	if onDetach != nil {
		onDetach(sub, id)
	}
	if remaining > 0 {
		return
	}
	var current S
	var exists bool
	current, exists = registry[key]
	if !exists || current.base() != sub.base() {
		return
	}
	delete(registry, key)
	if side.conn != nil {
		_ = side.conn.Unsubscribe(sub.base().arg)
	}
}

// resolveCategory validates a public-stream category and returns its
// canonical constant plus the wire instType (the lower-case category).
func resolveCategory(scope string, category utatypes.Category) (utatypes.Category, string, error) {
	var canonical utatypes.Category = utatypes.Category(strings.ToUpper(string(category)))
	switch canonical {
	case utatypes.CategorySpot:
		return canonical, "spot", nil
	case utatypes.CategoryUSDTFutures:
		return canonical, "usdt-futures", nil
	case utatypes.CategoryCOINFutures:
		return canonical, "coin-futures", nil
	case utatypes.CategoryUSDCFutures:
		return canonical, "usdc-futures", nil
	case "":
		return "", "", errInvalid(scope, "category is required")
	case utatypes.CategoryMargin:
		return "", "", errInvalid(scope, "category MARGIN has no public stream (margin trades the SPOT book)")
	default:
		return "", "", errInvalid(scope, "unsupported category "+string(category))
	}
}

// surfaceError logs at debug level (these errors are chatty: one per
// botched frame) and fans err out to the errHandlers of the subscription.
func surfaceError[T any](s *StreamClient, w *wireSub[T], what string, err error) {
	s.c.logger().Debug("uta."+w.scope+": "+what, bitget.Err(err))
	w.fail(err)
}

// ---------------------------------------------------------------------
// WatchTicker.
// ---------------------------------------------------------------------

// WatchTicker subscribes to the `ticker` topic of (category, symbol). The
// venue pushes the full ticker on every change (≤ every ~200 ms); spot
// tickers carry no mark / index / funding (left zero).
//
// category: CategorySpot / CategoryUSDTFutures / CategoryCOINFutures /
// CategoryUSDCFutures (CategoryMargin and "" → ErrorKindInvalidRequest).
// Several calls for the same (category, symbol) share one wire
// subscription; see the package fan-out notes. ctx == nil keeps the
// handler attached until Close.
func (s *StreamClient) WatchTicker(
	ctx context.Context,
	category utatypes.Category,
	symbol string,
	handler func(utatypes.TickerUpdate),
	errHandler func(error),
) error {
	const scope string = "Stream.WatchTicker"
	var canonical utatypes.Category
	var instType string
	var err error
	canonical, instType, err = resolveCategory(scope, category)
	if err != nil {
		return err
	}
	if symbol == "" {
		return errInvalid(scope, "symbol is empty")
	}
	if handler == nil {
		return errInvalid(scope, "handler is nil")
	}

	var conn *ws.Conn
	conn, err = s.ensureConn(&s.public, false, scope)
	if err != nil {
		return err
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{InstType: instType, Topic: topicTicker, Symbol: symbol}
	return attachHandler(s, &s.public, conn, s.tickers, arg, scope,
		func() *tickerSub { return s.newTickerSub(arg, scope, canonical, symbol) },
		ctx, handler, errHandler, nil, nil)
}

func (s *StreamClient) newTickerSub(arg ws.SubscriptionArg, scope string, category utatypes.Category, symbol string) *tickerSub {
	var t *tickerSub = &tickerSub{category: category, symbol: symbol, rows: make([]wsTickerRow, 0, 1)}
	t.arg = arg
	t.scope = scope
	t.sub = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, tsMs int64, _ int64) {
			s.handleTickerFrame(t, payload, tsMs)
		},
	}
	return t
}

// wsTickerRow mirrors the consumed part of one `ticker` data element. The
// 24h roll-ups / open interest / delivery fields are not declared — the
// decoder skips them without allocating.
type wsTickerRow struct {
	LastPrice       codec.WireDecimal `json:"lastPrice"`
	Bid1Price       codec.WireDecimal `json:"bid1Price"`
	Bid1Size        codec.WireDecimal `json:"bid1Size"`
	Ask1Price       codec.WireDecimal `json:"ask1Price"`
	Ask1Size        codec.WireDecimal `json:"ask1Size"`
	MarkPrice       codec.WireDecimal `json:"markPrice"`
	IndexPrice      codec.WireDecimal `json:"indexPrice"`
	FundingRate     codec.WireDecimal `json:"fundingRate"`
	NextFundingTime codec.WireInt64   `json:"nextFundingTime"`
}

// decodeTickerRows decodes payload into the scratch rows. The scratch is
// zeroed first: jsoniter reuses the slice storage and only assigns fields
// present in the JSON, so a field missing from this frame would otherwise
// keep the previous frame's value. It is passed by pointer to the
// long-lived subscription field — a local slice header would escape to
// the heap on every frame.
func decodeTickerRows(scratch *[]wsTickerRow, payload []byte) error {
	clear((*scratch)[:cap(*scratch)])
	*scratch = (*scratch)[:0]
	return codec.UnmarshalWire(payload, scratch)
}

// handleTickerFrame parses one `ticker` frame and delivers one
// TickerUpdate per data row (the venue sends exactly one).
func (s *StreamClient) handleTickerFrame(t *tickerSub, payload []byte, tsMs int64) {
	if len(payload) == 0 {
		return
	}
	var err error = decodeTickerRows(&t.rows, payload)
	if err != nil {
		surfaceError(s, &t.wireSub, "decode ticker frame", errParse(t.scope, err))
		return
	}
	var i int
	for i = 0; i < len(t.rows); i++ {
		t.deliver(convertWSTickerRow(t.category, t.symbol, &t.rows[i], tsMs))
	}
}

func convertWSTickerRow(category utatypes.Category, symbol string, row *wsTickerRow, tsMs int64) utatypes.TickerUpdate {
	return utatypes.TickerUpdate{
		Category:          category,
		Symbol:            symbol,
		LastPrice:         row.LastPrice.Decimal(),
		Bid1Price:         row.Bid1Price.Decimal(),
		Bid1Size:          row.Bid1Size.Decimal(),
		Ask1Price:         row.Ask1Price.Decimal(),
		Ask1Size:          row.Ask1Size.Decimal(),
		MarkPrice:         row.MarkPrice.Decimal(),
		IndexPrice:        row.IndexPrice.Decimal(),
		FundingRate:       row.FundingRate.Decimal(),
		NextFundingTimeMs: row.NextFundingTime.Int64(),
		TsMs:              tsMs,
	}
}

// ---------------------------------------------------------------------
// WatchPublicTrades.
// ---------------------------------------------------------------------

// WatchPublicTrades subscribes to the `publicTrade` topic of (category,
// symbol) and invokes handler once per trade, oldest first.
//
// The venue answers a subscribe with an action=snapshot frame holding the
// ~50 most recent trades — HISTORY, which is NOT delivered (a consumer
// would double-count it after every reconnect). Only action=update frames
// reach the handler. Side is the TAKER side.
func (s *StreamClient) WatchPublicTrades(
	ctx context.Context,
	category utatypes.Category,
	symbol string,
	handler func(utatypes.PublicTradeUpdate),
	errHandler func(error),
) error {
	const scope string = "Stream.WatchPublicTrades"
	var canonical utatypes.Category
	var instType string
	var err error
	canonical, instType, err = resolveCategory(scope, category)
	if err != nil {
		return err
	}
	if symbol == "" {
		return errInvalid(scope, "symbol is empty")
	}
	if handler == nil {
		return errInvalid(scope, "handler is nil")
	}

	var conn *ws.Conn
	conn, err = s.ensureConn(&s.public, false, scope)
	if err != nil {
		return err
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{InstType: instType, Topic: topicPublicTrade, Symbol: symbol}
	return attachHandler(s, &s.public, conn, s.trades, arg, scope,
		func() *tradeSub { return s.newTradeSub(arg, scope, canonical, symbol) },
		ctx, handler, errHandler, nil, nil)
}

func (s *StreamClient) newTradeSub(arg ws.SubscriptionArg, scope string, category utatypes.Category, symbol string) *tradeSub {
	var t *tradeSub = &tradeSub{category: category, symbol: symbol, rows: make([]wsTradeRow, 0, 8)}
	t.arg = arg
	t.scope = scope
	t.sub = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, action string, payload []byte, _ int64, _ int64) {
			s.handleTradesFrame(t, action, payload)
		},
	}
	return t
}

// wsTradeRow mirrors one `publicTrade` data element: i = trade id, p =
// price, v = size, S = taker side, T = trade time (ms, quoted), isRPI.
// The correlation id `L` is not consumed.
type wsTradeRow struct {
	ID    string            `json:"i"`
	Price codec.WireDecimal `json:"p"`
	Size  codec.WireDecimal `json:"v"`
	Side  codec.WireToken   `json:"S"`
	Time  codec.WireInt64   `json:"T"`
	IsRPI codec.WireToken   `json:"isRPI"`
}

// decodeTradeRows — see decodeTickerRows for the zeroing and the pointer.
func decodeTradeRows(scratch *[]wsTradeRow, payload []byte) error {
	clear((*scratch)[:cap(*scratch)])
	*scratch = (*scratch)[:0]
	return codec.UnmarshalWire(payload, scratch)
}

// tradeRowsNewestFirst reports whether the frame lists trades newest
// first. The venue does (verified live: within one frame the ids
// descend, the timestamps are often equal), but the order is detected
// rather than assumed: compare trade time, then the numeric trade id.
func tradeRowsNewestFirst(rows []wsTradeRow) bool {
	if len(rows) < 2 {
		return false
	}
	var first *wsTradeRow = &rows[0]
	var last *wsTradeRow = &rows[len(rows)-1]
	if first.Time != last.Time {
		return first.Time > last.Time
	}
	// Decimal-string ids: the longer one is larger; equal lengths compare
	// lexicographically. Indistinguishable → the venue default.
	if len(first.ID) != len(last.ID) {
		return len(first.ID) > len(last.ID)
	}
	if first.ID == last.ID {
		return true
	}
	return first.ID > last.ID
}

// handleTradesFrame parses one `publicTrade` frame and fans the trades
// out oldest first. Snapshot frames (history) are skipped entirely.
func (s *StreamClient) handleTradesFrame(t *tradeSub, action string, payload []byte) {
	if action == actionSnapshot || len(payload) == 0 {
		return
	}
	var err error = decodeTradeRows(&t.rows, payload)
	if err != nil {
		surfaceError(s, &t.wireSub, "decode publicTrade frame", errParse(t.scope, err))
		return
	}
	var n int = len(t.rows)
	var i int
	if tradeRowsNewestFirst(t.rows) {
		for i = n - 1; i >= 0; i-- {
			t.deliver(convertWSTradeRow(t.category, t.symbol, &t.rows[i]))
		}
		return
	}
	for i = 0; i < n; i++ {
		t.deliver(convertWSTradeRow(t.category, t.symbol, &t.rows[i]))
	}
}

func convertWSTradeRow(category utatypes.Category, symbol string, row *wsTradeRow) utatypes.PublicTradeUpdate {
	return utatypes.PublicTradeUpdate{
		Category: category,
		Symbol:   symbol,
		TradeID:  row.ID,
		Price:    row.Price.Decimal(),
		Size:     row.Size.Decimal(),
		Side:     row.Side.String(),
		IsRPI:    row.IsRPI == "yes",
		TsMs:     row.Time.Int64(),
	}
}
