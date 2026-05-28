/*
FILE: spot/stream.go

DESCRIPTION:
Public WebSocket sub-client for the Bitget V2 SPOT profile. Wires
the four public Watch* primitives the desk needs from a market-
making feed:

	WatchOrderbook   → "books"        (full-depth + checksum + delta)
	WatchTicker      → "ticker"       (last + best bid/ask + 24h roll-ups)
	WatchTrades      → "trade"        (public tape, fan-out per fill)
	WatchKline       → "candle{tf}"   (per-bar OHLCV updates)

DESIGN — IDENTICAL TO mix.StreamClient WHERE THE PROTOCOL IS IDENTICAL:

  - One shared *ws.Conn per StreamClient, lazy-initialised on the
    first Watch* call. Bitget's public endpoint multiplexes every
    instType ("USDT-FUTURES", "SPOT", ...) on the same socket, so
    spot reuses the cfg.WS.PublicURL field.
  - Per-symbol orderbook engine map keyed by ws.SubscriptionArg.Key().
    Reconnects are transparent: the supervisor reissues every
    Subscription on a fresh socket and the engine state is cleared
    via Subscription.Reset before the snapshot lands again.
  - The orderbook engine validates the Bitget CRC32 on every applied
    delta; on mismatch the engine flips dirty and the StreamClient
    schedules a tight Unsubscribe→Subscribe round-trip in the
    background so Bitget pushes a fresh snapshot.
  - Per-Watch ctx scopes the SUBSCRIPTION lifetime, NOT the
    connection lifetime. The ws.Conn itself is supervised by an
    internal Background ctx so a cancelled Watch* on one symbol
    does not tear down the feed for every other symbol.

REUSE FROM internal/bgcommon (NO COPY-PASTE):

  - bgcommon.OrderbookFrame / TradeFrame   — wire shapes
  - bgcommon.ParseTradeFrame                — TradeFrame → TradeUpdate
  - bgcommon.ParseCandleRow                 — []string  → KlineUpdate
  - bgcommon/orderbook.Engine               — CRC32 + delta engine
  - bgcommon/orderbook.ParseLevels          — [price, size] decoder
  - bgcommon.ParseDecimalOrZero / ParseInt64OrZero — numeric parsers

PROFILE-LOCAL (intentional):

  - tickerFrame + convertTickerFrame — spot ships 24h roll-ups while
    mix ships markPrice / indexPrice / fundingRate. A shared shape
    would force always-zero fields on one side or the other.
  - StreamClient orchestration — mix has private-channel state
    bolted on alongside the public surface; spot will grow the same
    in M5 but the private state is profile-specific (different
    instType, different login args), so the orchestration glue
    stays per-profile.

ERROR PROPAGATION:

  - Subscribe ack/error events are logged inside the conn wrapper —
    the StreamClient sees them only via metrics.
  - Decode errors and CRC mismatches travel to errHandler (if non-
    nil). A nil errHandler means "I do not care, just log and
    recover" — StreamClient still recovers via the resubscribe path.
*/

package spot

import (
	"context"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon/orderbook"
	"github.com/tonymontanov/go-bitget/v2/internal/bgmet"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// Channel name constants — kept here to avoid string typos sprinkled
// across the file.
const (
	channelBooks  = "books"
	channelTicker = "ticker"
	channelTrade  = "trade"
)

// StreamClient — WebSocket subscription sub-client. Built once per
// spot.Client (see client.go) and safe for concurrent use.
type StreamClient struct {
	c *Client

	mu         sync.Mutex
	publicConn *ws.Conn
	publicCtx  context.Context
	closeOnce  sync.Once

	// engines holds one orderbook engine per subscribed symbol.
	// Indexed by ws.SubscriptionArg.Key() so the same key the registry
	// uses also reaches the engine.
	engines map[string]*orderbook.Engine

	// orderbookSubs retains the books-channel Subscription per arg so
	// scheduleResync can re-Subscribe the SAME object (with its
	// handler and Reset hook intact) after Unsubscribe wipes it from
	// the ws.Conn registry.
	orderbookSubs map[string]*ws.Subscription

	// resyncing is the per-arg flag preventing back-to-back resync
	// goroutines from racing each other.
	resyncing map[string]struct{}
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{
		c:             c,
		engines:       make(map[string]*orderbook.Engine, 16),
		orderbookSubs: make(map[string]*ws.Subscription, 16),
		resyncing:     make(map[string]struct{}, 8),
	}
}

// Close shuts the underlying public WS connection down. Idempotent;
// callers without explicit shutdown can rely on the connection being
// torn down with the rest of the desk on process exit.
func (s *StreamClient) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if s.publicConn != nil {
			_ = s.publicConn.Close()
			s.publicConn = nil
		}
		s.mu.Unlock()
	})
	return nil
}

// errInvalidRequest is the canonical client-side validation error.
func errInvalidRequest(method, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Stream."+method+": "+msg, nil)
}

// ---------------------------------------------------------------------
// Public WS connection lifecycle.
// ---------------------------------------------------------------------

// ensurePublicConn returns the lazily-constructed public WS connection.
// First call dials and starts the supervisor; subsequent calls return
// the same instance.
func (s *StreamClient) ensurePublicConn() *ws.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publicConn != nil {
		return s.publicConn
	}

	var cfg bitget.Config = s.c.config()
	var wsCfg ws.Config = ws.Config{
		URL:                     cfg.WS.PublicURL,
		IsPrivate:               false,
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
	}

	var metricsFactory bgmet.CounterFactory = cfg.Metrics
	if metricsFactory == nil {
		metricsFactory = bgmet.Noop()
	}

	// Public stream needs no signer, but ws.NewConn accepts nil.
	s.publicConn = ws.NewConn(wsCfg, nil, s.c.logger(), metricsFactory)
	s.publicCtx = context.Background()
	s.publicConn.Start(s.publicCtx)
	return s.publicConn
}

// detachOnContextDone watches ctx and unsubscribes the arg once the
// caller cancels its context. Runs in its own goroutine; exits when
// ctx fires or when the StreamClient is closed.
func (s *StreamClient) detachOnContextDone(ctx context.Context, arg ws.SubscriptionArg) {
	if ctx == nil {
		return
	}
	go func() {
		<-ctx.Done()
		var key string = arg.Key()
		s.mu.Lock()
		var conn *ws.Conn = s.publicConn
		delete(s.engines, key)
		delete(s.orderbookSubs, key)
		s.mu.Unlock()
		if conn != nil {
			_ = conn.Unsubscribe(arg)
		}
	}()
}

// ---------------------------------------------------------------------
// Public channels.
// ---------------------------------------------------------------------

// WatchOrderbook subscribes to the full-depth "books" channel for the
// given symbol. The engine validates the Bitget CRC32 on every applied
// delta and triggers a resubscribe on mismatch.
//
// The handler receives a fresh OrderBookSnapshot after every applied
// frame (snapshot or delta). The slice fields are copies — handlers
// may retain them across calls.
//
// errHandler is invoked for decode errors and CRC mismatches. nil is
// allowed (errors are logged regardless).
func (s *StreamClient) WatchOrderbook(
	ctx context.Context,
	symbol string,
	handler func(roottypes.OrderBookSnapshot),
	errHandler func(error),
) error {
	if symbol == "" {
		return errInvalidRequest("WatchOrderbook", "symbol is empty")
	}
	if handler == nil {
		return errInvalidRequest("WatchOrderbook", "handler is nil")
	}

	var conn *ws.Conn = s.ensurePublicConn()

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: SpotInstType,
		Channel:  channelBooks,
		InstID:   symbol,
	}
	var key string = arg.Key()
	var maxDepth int = s.c.config().Orderbook.MaxDepth

	s.mu.Lock()
	var engine *orderbook.Engine = s.engines[key]
	if engine == nil {
		engine = orderbook.NewEngine(symbol, maxDepth)
		s.engines[key] = engine
	}
	s.mu.Unlock()

	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, action string, payload []byte, tsMs int64, _ int64) {
			s.handleBooksFrame(engine, arg, action, payload, tsMs, handler, errHandler)
		},
		Reset: engine.Reset,
	}

	s.mu.Lock()
	s.orderbookSubs[key] = sub
	s.mu.Unlock()

	if err := conn.Subscribe(sub); err != nil {
		s.mu.Lock()
		delete(s.orderbookSubs, key)
		s.mu.Unlock()
		return err
	}
	s.detachOnContextDone(ctx, arg)
	return nil
}

// WatchTicker subscribes to the "ticker" channel.
func (s *StreamClient) WatchTicker(
	ctx context.Context,
	symbol string,
	handler func(spottypes.MarketTicker),
	errHandler func(error),
) error {
	if symbol == "" {
		return errInvalidRequest("WatchTicker", "symbol is empty")
	}
	if handler == nil {
		return errInvalidRequest("WatchTicker", "handler is nil")
	}

	var conn *ws.Conn = s.ensurePublicConn()
	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: SpotInstType,
		Channel:  channelTicker,
		InstID:   symbol,
	}

	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handleTickerFrame(symbol, payload, handler, errHandler)
		},
	}
	if err := conn.Subscribe(sub); err != nil {
		return err
	}
	s.detachOnContextDone(ctx, arg)
	return nil
}

// WatchTrades subscribes to the public "trade" channel. Bitget ships
// trades in batches; the SDK fans them out so the handler receives
// one TradeUpdate per call.
func (s *StreamClient) WatchTrades(
	ctx context.Context,
	symbol string,
	handler func(roottypes.TradeUpdate),
	errHandler func(error),
) error {
	if symbol == "" {
		return errInvalidRequest("WatchTrades", "symbol is empty")
	}
	if handler == nil {
		return errInvalidRequest("WatchTrades", "handler is nil")
	}

	var conn *ws.Conn = s.ensurePublicConn()
	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: SpotInstType,
		Channel:  channelTrade,
		InstID:   symbol,
	}

	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handleTradesFrame(symbol, payload, handler, errHandler)
		},
	}
	if err := conn.Subscribe(sub); err != nil {
		return err
	}
	s.detachOnContextDone(ctx, arg)
	return nil
}

// WatchKline subscribes to the "candle{tf}" channel. The handler
// receives one KlineUpdate per pushed bar. Bitget does not flag
// closed bars on the wire; the SDK uniformly reports
// Confirmed=false (consumers detect closure by comparing StartMs).
func (s *StreamClient) WatchKline(
	ctx context.Context,
	symbol string,
	timeframe roottypes.Timeframe,
	handler func(roottypes.KlineUpdate),
	errHandler func(error),
) error {
	if symbol == "" {
		return errInvalidRequest("WatchKline", "symbol is empty")
	}
	if handler == nil {
		return errInvalidRequest("WatchKline", "handler is nil")
	}
	if timeframe.Wire() == "" {
		return errInvalidRequest("WatchKline", "timeframe is empty")
	}

	var conn *ws.Conn = s.ensurePublicConn()
	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: SpotInstType,
		Channel:  "candle" + timeframe.Wire(),
		InstID:   symbol,
	}

	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handleKlineFrame(symbol, timeframe, payload, handler, errHandler)
		},
	}
	if err := conn.Subscribe(sub); err != nil {
		return err
	}
	s.detachOnContextDone(ctx, arg)
	return nil
}

// ---------------------------------------------------------------------
// Frame handlers.
// ---------------------------------------------------------------------

// handleBooksFrame parses one "books" channel frame, applies it to
// the engine, surfaces snapshots to the user handler and reacts to
// CRC mismatches by triggering a resubscribe.
func (s *StreamClient) handleBooksFrame(
	engine *orderbook.Engine,
	arg ws.SubscriptionArg,
	action string,
	payload []byte,
	tsMs int64,
	handler func(roottypes.OrderBookSnapshot),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}

	var rows []bgcommon.OrderbookFrame
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchOrderbook", "decode books frame", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	var row bgcommon.OrderbookFrame = rows[0]
	var rowTsMs int64 = tsMs
	if v, _ := bgcommon.ParseInt64OrZero(row.Ts); v > 0 {
		rowTsMs = v
	}

	var asks []orderbook.Level
	var err error
	asks, err = orderbook.ParseLevels(row.Asks)
	if err != nil {
		s.surfaceError(errHandler, "WatchOrderbook", "parse asks", err)
		return
	}
	var bids []orderbook.Level
	bids, err = orderbook.ParseLevels(row.Bids)
	if err != nil {
		s.surfaceError(errHandler, "WatchOrderbook", "parse bids", err)
		return
	}

	switch action {
	case "snapshot":
		err = engine.ApplySnapshot(asks, bids, rowTsMs, row.Checksum)
	case "update":
		err = engine.ApplyUpdate(asks, bids, rowTsMs, row.Checksum)
	default:
		// Unknown action — ignore silently; future Bitget protocol
		// extensions should not break the stream.
		return
	}

	if err == orderbook.ErrDirty {
		// Engine is awaiting a snapshot; nothing to surface.
		return
	}
	if err == orderbook.ErrChecksum {
		s.surfaceError(errHandler, "WatchOrderbook", "checksum mismatch (resyncing)", err)
		s.scheduleResync(arg)
		return
	}
	if err != nil {
		s.surfaceError(errHandler, "WatchOrderbook", "apply frame", err)
		return
	}

	handler(engine.Snapshot())
}

// handleTickerFrame parses one "ticker" channel frame and invokes
// the caller handler with the converted spottypes.MarketTicker.
func (s *StreamClient) handleTickerFrame(
	symbol string,
	payload []byte,
	handler func(spottypes.MarketTicker),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}
	var rows []tickerFrame
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchTicker", "decode ticker frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		handler(convertTickerFrame(symbol, rows[i]))
	}
}

// handleTradesFrame parses one "trade" channel frame and fans the
// individual ticks out to the caller handler.
func (s *StreamClient) handleTradesFrame(
	symbol string,
	payload []byte,
	handler func(roottypes.TradeUpdate),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}
	var rows []bgcommon.TradeFrame
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchTrades", "decode trade frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		var u roottypes.TradeUpdate
		var err error
		u, err = bgcommon.ParseTradeFrame(symbol, rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchTrades", "parse trade row", err)
			continue
		}
		handler(u)
	}
}

// handleKlineFrame parses one "candle{tf}" channel frame and fans
// bars out to the caller handler.
func (s *StreamClient) handleKlineFrame(
	symbol string,
	tf roottypes.Timeframe,
	payload []byte,
	handler func(roottypes.KlineUpdate),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}
	var rows [][]string
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchKline", "decode candle frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		var u roottypes.KlineUpdate
		var err error
		u, err = bgcommon.ParseCandleRow(symbol, tf, rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchKline", "parse candle row", err)
			continue
		}
		handler(u)
	}
}

// ---------------------------------------------------------------------
// Resync.
// ---------------------------------------------------------------------

// scheduleResync kicks off an Unsubscribe→Subscribe round-trip in
// the background. The dedup map prevents overlapping resyncs for
// the same arg if multiple consecutive frames trip the CRC.
func (s *StreamClient) scheduleResync(arg ws.SubscriptionArg) {
	var key string = arg.Key()
	s.mu.Lock()
	if _, busy := s.resyncing[key]; busy {
		s.mu.Unlock()
		return
	}
	s.resyncing[key] = struct{}{}
	var conn *ws.Conn = s.publicConn
	var sub *ws.Subscription = s.orderbookSubs[key]
	var engine *orderbook.Engine = s.engines[key]
	s.mu.Unlock()

	if conn == nil || sub == nil {
		s.mu.Lock()
		delete(s.resyncing, key)
		s.mu.Unlock()
		return
	}

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.resyncing, key)
			s.mu.Unlock()
		}()
		_ = conn.Unsubscribe(arg)
		// Tiny pause — Bitget rejects back-to-back sub/unsub on the
		// same arg with a "frequency too high" event. 50ms is
		// generous.
		time.Sleep(50 * time.Millisecond)
		if engine != nil {
			engine.Reset()
		}
		// Re-Subscribe with the SAME Subscription object: its
		// Handler closes over the original engine and user handler,
		// so push frames keep flowing through the original delivery
		// path.
		_ = conn.Subscribe(sub)
	}()
}

// ---------------------------------------------------------------------
// Ticker — profile-specific wire shape and converter.
// ---------------------------------------------------------------------
//
// Spot tickers carry 24h roll-up metrics (open24h / high24h / low24h
// / openUtc / change24h / changeUtc24h / baseVolume / quoteVolume /
// usdtVolume) instead of mix's mark/index/funding fields. The
// streaming envelope is a strict superset of the REST /tickers
// response — every field that arrives on REST also arrives on WS,
// plus the venue-side timestamp `ts`.

// tickerFrame mirrors one element of the spot "ticker" channel data
// array.
type tickerFrame struct {
	InstID       string `json:"instId"`
	Last         string `json:"lastPr"`
	Open24h      string `json:"open24h"`
	High24h      string `json:"high24h"`
	Low24h       string `json:"low24h"`
	OpenUtc      string `json:"openUtc"`
	Change24h    string `json:"change24h"`
	ChangeUtc24h string `json:"changeUtc24h"`
	BidPr        string `json:"bidPr"`
	BidSz        string `json:"bidSz"`
	AskPr        string `json:"askPr"`
	AskSz        string `json:"askSz"`
	BaseVolume   string `json:"baseVolume"`
	QuoteVolume  string `json:"quoteVolume"`
	UsdtVolume   string `json:"usdtVolume"`
	Ts           string `json:"ts"`
}

// convertTickerFrame normalises one spot tickerFrame into the typed
// snapshot the desk consumes.
func convertTickerFrame(symbol string, t tickerFrame) spottypes.MarketTicker {
	var resolvedSymbol string = symbol
	if t.InstID != "" {
		resolvedSymbol = t.InstID
	}

	var last, open24h, high24h, low24h, openUtc decimal.Decimal
	last, _ = bgcommon.ParseDecimalOrZero(t.Last)
	open24h, _ = bgcommon.ParseDecimalOrZero(t.Open24h)
	high24h, _ = bgcommon.ParseDecimalOrZero(t.High24h)
	low24h, _ = bgcommon.ParseDecimalOrZero(t.Low24h)
	openUtc, _ = bgcommon.ParseDecimalOrZero(t.OpenUtc)

	var change24h, changeUtc24h decimal.Decimal
	change24h, _ = bgcommon.ParseDecimalOrZero(t.Change24h)
	changeUtc24h, _ = bgcommon.ParseDecimalOrZero(t.ChangeUtc24h)

	var bid, bidSz, ask, askSz decimal.Decimal
	bid, _ = bgcommon.ParseDecimalOrZero(t.BidPr)
	bidSz, _ = bgcommon.ParseDecimalOrZero(t.BidSz)
	ask, _ = bgcommon.ParseDecimalOrZero(t.AskPr)
	askSz, _ = bgcommon.ParseDecimalOrZero(t.AskSz)

	var baseVol, quoteVol, usdtVol decimal.Decimal
	baseVol, _ = bgcommon.ParseDecimalOrZero(t.BaseVolume)
	quoteVol, _ = bgcommon.ParseDecimalOrZero(t.QuoteVolume)
	usdtVol, _ = bgcommon.ParseDecimalOrZero(t.UsdtVolume)

	var tsMs int64
	tsMs, _ = bgcommon.ParseInt64OrZero(t.Ts)

	return spottypes.MarketTicker{
		Symbol:       resolvedSymbol,
		LastPrice:    last,
		AskPrice:     ask,
		AskSize:      askSz,
		BidPrice:     bid,
		BidSize:      bidSz,
		High24h:      high24h,
		Low24h:       low24h,
		Open:         open24h,
		OpenUtc:      openUtc,
		BaseVolume:   baseVol,
		QuoteVolume:  quoteVol,
		UsdtVolume:   usdtVol,
		Change24h:    change24h,
		ChangeUtc24h: changeUtc24h,
		TsMs:         tsMs,
	}
}

// ---------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------

// surfaceError logs to the SDK logger and forwards to errHandler when
// non-nil. The logger field set is intentionally compact — these
// errors are extremely chatty in production (one per botched frame).
func (s *StreamClient) surfaceError(errHandler func(error), method, ctx string, err error) {
	s.c.logger().Debug("spot.Stream: "+method+" "+ctx,
		bitget.Err(err),
	)
	if errHandler != nil {
		errHandler(err)
	}
}
