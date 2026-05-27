/*
FILE: mix/stream.go

DESCRIPTION:
Public WebSocket sub-client for Bitget MIX. Implements the four
public Watch* primitives the desk needs from a market-making feed:

	WatchOrderbook   → "books"  (full-depth + checksum + delta engine)
	WatchTicker      → "ticker" (last/mark/index price + funding)
	WatchTrades      → "trade"  (public tape, fan-out per fill)
	WatchKline       → "candle{tf}" (per-bar OHLCV updates)

DESIGN:

  - One shared *ws.Conn per StreamClient, lazy-initialised on the first
    Watch* call. The same connection multiplexes every channel — Bitget's
    public endpoint accepts up to 240 subscriptions per socket which is
    plenty for a desk that watches a few dozen symbols.

  - Every Watch* call registers a ws.Subscription in the underlying
    Conn's registry. Reconnects are transparent: the supervisor reissues
    the subscribe op on every fresh socket and the engine state is
    cleared via Subscription.Reset before the snapshot lands again.

  - The orderbook engine validates the Bitget CRC32 on every applied
    delta. On mismatch the engine flips dirty and the StreamClient
    schedules a tight Unsubscribe→Subscribe round-trip in the
    background so Bitget pushes a fresh snapshot.

  - Per-Watch ctx is used to scope the SUBSCRIPTION lifetime, NOT the
    connection lifetime. The ws.Conn itself is supervised by an
    internal Background ctx so that a cancelled Watch* on one symbol
    does not tear down the feed for every other symbol.

ERROR PROPAGATION:

  - Subscribe ack/error events are logged inside the conn wrapper —
    the StreamClient sees them only via metrics.
  - Decode errors and CRC mismatches travel to errHandler (if non-nil).
    A nil errHandler means "I do not care, just log and recover" —
    StreamClient still recovers via the resubscribe path.
*/

package mix

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon/orderbook"
	"github.com/tonymontanov/go-bitget/v2/internal/bgmet"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// Channel name constants — kept here to avoid string typos sprinkled
// across the file.
const (
	channelBooks  = "books"
	channelTicker = "ticker"
	channelTrade  = "trade"
)

// StreamClient — WebSocket subscription sub-client.
type StreamClient struct {
	c *Client

	mu         sync.Mutex
	publicConn *ws.Conn
	publicCtx  context.Context
	closeOnce  sync.Once

	// engines holds one orderbook engine per subscribed symbol. Indexed
	// by ws.SubscriptionArg.Key() so the same key the registry uses
	// also reaches the engine.
	engines map[string]*orderbook.Engine

	// orderbookSubs retains the books-channel Subscription per arg so
	// scheduleResync can re-Subscribe the SAME object (with its handler
	// and Reset hook intact) after Unsubscribe wipes it from the
	// ws.Conn registry.
	orderbookSubs map[string]*ws.Subscription

	// resyncing is the per-arg flag preventing back-to-back resync
	// goroutines from racing each other.
	resyncing map[string]struct{}

	// privateState bundles every field that locks around the lazily
	// constructed private *ws.Conn. Defined in stream-private.go and
	// kept under its own mutex so the public-side fields above stay
	// decoupled.
	privateState privateConnState
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
	s.closePrivate()
	return nil
}

// errInvalidRequest is the canonical client-side validation error.
func errInvalidRequest(method, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Stream."+method+": "+msg, nil)
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
// caller cancels its context. Runs in its own goroutine; exits when ctx
// fires or when the StreamClient is closed.
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
// Public channels (M4).
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
		InstType: string(s.c.productType),
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
	handler func(mixtypes.MarketTicker),
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
		InstType: string(s.c.productType),
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
// trades in batches; the SDK fans them out so the handler receives one
// TradeUpdate per call.
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
		InstType: string(s.c.productType),
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
// receives one KlineUpdate per pushed bar. Bitget ships an "unconfirmed"
// flag on the wire that the SDK forwards verbatim (true = bar still
// open, false = bar closed).
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
		InstType: string(s.c.productType),
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

// handleBooksFrame parses one "books" channel frame, applies it to the
// engine, surfaces snapshots to the user handler and reacts to CRC
// mismatches by triggering a resubscribe.
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

	// Bitget ships data as a single-element array even on push frames.
	var rows []orderbookFrame
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchOrderbook", "decode books frame", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	// Use the per-row timestamp when present (older Bitget builds populate
	// the per-row ts string, newer ones rely on the envelope ts).
	var row orderbookFrame = rows[0]
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

// handleTickerFrame parses one "ticker" channel frame and invokes the
// caller handler with the converted MarketTicker.
func (s *StreamClient) handleTickerFrame(
	symbol string,
	payload []byte,
	handler func(mixtypes.MarketTicker),
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
		var t mixtypes.MarketTicker = convertTickerFrame(symbol, rows[i])
		handler(t)
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
	var rows []tradeFrame
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchTrades", "decode trade frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		var u roottypes.TradeUpdate
		var err error
		u, err = convertTradeFrame(symbol, rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchTrades", "parse trade row", err)
			continue
		}
		handler(u)
	}
}

// handleKlineFrame parses one "candle{tf}" channel frame and fans bars
// out to the caller handler.
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
		u, err = convertKlineRow(symbol, tf, rows[i])
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

// scheduleResync kicks off an Unsubscribe→Subscribe round-trip in the
// background. The dedup map prevents overlapping resyncs for the same
// arg if multiple consecutive frames trip the CRC.
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
		// same arg with a "frequency too high" event. 50ms is generous.
		time.Sleep(50 * time.Millisecond)
		if engine != nil {
			engine.Reset()
		}
		// Re-Subscribe with the SAME Subscription object: its Handler
		// closes over the original engine and user handler, so push
		// frames keep flowing through the original delivery path.
		_ = conn.Subscribe(sub)
	}()
}

// ---------------------------------------------------------------------
// Wire structs (decoded with codec.Unmarshal).
// ---------------------------------------------------------------------

// orderbookFrame mirrors one element of the "books" channel data array.
type orderbookFrame struct {
	Asks     [][]string `json:"asks"`
	Bids     [][]string `json:"bids"`
	Checksum int64      `json:"checksum"`
	Ts       string     `json:"ts"`
}

// tickerFrame mirrors one element of the "ticker" channel data array.
// Field names follow the Bitget V2 wire (camelCase). The WS shape is
// strictly a superset of the REST /ticker response: the streaming
// envelope ALSO carries bidSz / askSz next to bidPr / askPr — REST
// does not.
type tickerFrame struct {
	InstID            string `json:"instId"`
	Last              string `json:"lastPr"`
	MarkPrice         string `json:"markPrice"`
	IndexPrice        string `json:"indexPrice"`
	AskPr             string `json:"askPr"`
	AskSz             string `json:"askSz"`
	BidPr             string `json:"bidPr"`
	BidSz             string `json:"bidSz"`
	FundingRate       string `json:"fundingRate"`
	NextFundingTimeMs string `json:"nextFundingTime"`
	Ts                string `json:"ts"`
}

// tradeFrame mirrors one element of the "trade" channel data array.
type tradeFrame struct {
	Ts      string `json:"ts"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Side    string `json:"side"`
	TradeID string `json:"tradeId"`
}

// ---------------------------------------------------------------------
// Wire → SDK conversions.
// ---------------------------------------------------------------------

func convertTickerFrame(symbol string, t tickerFrame) mixtypes.MarketTicker {
	var resolvedSymbol string = symbol
	if t.InstID != "" {
		resolvedSymbol = t.InstID
	}
	var last decimal.Decimal
	last, _ = bgcommon.ParseDecimalOrZero(t.Last)
	var mark decimal.Decimal
	mark, _ = bgcommon.ParseDecimalOrZero(t.MarkPrice)
	var index decimal.Decimal
	index, _ = bgcommon.ParseDecimalOrZero(t.IndexPrice)
	var ask decimal.Decimal
	ask, _ = bgcommon.ParseDecimalOrZero(t.AskPr)
	var askSz decimal.Decimal
	askSz, _ = bgcommon.ParseDecimalOrZero(t.AskSz)
	var bid decimal.Decimal
	bid, _ = bgcommon.ParseDecimalOrZero(t.BidPr)
	var bidSz decimal.Decimal
	bidSz, _ = bgcommon.ParseDecimalOrZero(t.BidSz)
	var funding decimal.Decimal
	funding, _ = bgcommon.ParseDecimalOrZero(t.FundingRate)
	var nextFundingMs int64
	nextFundingMs, _ = bgcommon.ParseInt64OrZero(t.NextFundingTimeMs)
	var tsMs int64
	tsMs, _ = bgcommon.ParseInt64OrZero(t.Ts)
	return mixtypes.MarketTicker{
		Symbol:            resolvedSymbol,
		LastPrice:         last,
		MarkPrice:         mark,
		IndexPrice:        index,
		AskPrice:          ask,
		AskSize:           askSz,
		BidPrice:          bid,
		BidSize:           bidSz,
		FundingRate:       funding,
		NextFundingTimeMs: nextFundingMs,
		TsMs:              tsMs,
	}
}

func convertTradeFrame(symbol string, t tradeFrame) (roottypes.TradeUpdate, error) {
	var price decimal.Decimal
	var size decimal.Decimal
	var err error
	price, err = bgcommon.ParseDecimalOrZero(t.Price)
	if err != nil {
		return roottypes.TradeUpdate{}, err
	}
	size, err = bgcommon.ParseDecimalOrZero(t.Size)
	if err != nil {
		return roottypes.TradeUpdate{}, err
	}
	var side roottypes.SideType
	switch t.Side {
	case "buy":
		side = roottypes.SideTypeBuy
	case "sell":
		side = roottypes.SideTypeSell
	default:
		side = roottypes.SideType(t.Side)
	}
	var tsMs int64
	tsMs, _ = bgcommon.ParseInt64OrZero(t.Ts)
	return roottypes.TradeUpdate{
		Symbol:  symbol,
		Price:   price,
		Size:    size,
		Side:    side,
		TradeID: t.TradeID,
		TsMs:    tsMs,
	}, nil
}

// convertKlineRow turns one candle row from the wire into a KlineUpdate.
// Bitget V2 candle wire format is the SAME 7-element array everywhere:
// [openTime, open, high, low, close, baseVolume, quoteVolume].
//
// The WS push does not flag closed bars explicitly; Bitget ships the
// in-progress bar repeatedly with the same openTime, then a frame with
// the next openTime starts the new bar. The SDK reports Confirmed=false
// uniformly — closure detection is the consumer's responsibility (it
// already needs the closed-bar logic for backfill mismatches).
func convertKlineRow(symbol string, tf roottypes.Timeframe, row []string) (roottypes.KlineUpdate, error) {
	if len(row) < 7 {
		return roottypes.KlineUpdate{}, bitget.NewError(bitget.ErrorKindUnknown, "",
			"mix.Stream.WatchKline: row arity "+strconv.Itoa(len(row))+" < 7", nil)
	}
	var openMs int64
	var err error
	openMs, err = bgcommon.ParseInt64OrZero(row[0])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	var open, high, low, close, volume, turnover decimal.Decimal
	open, err = bgcommon.ParseDecimalOrZero(row[1])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	high, err = bgcommon.ParseDecimalOrZero(row[2])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	low, err = bgcommon.ParseDecimalOrZero(row[3])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	close, err = bgcommon.ParseDecimalOrZero(row[4])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	volume, err = bgcommon.ParseDecimalOrZero(row[5])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	turnover, err = bgcommon.ParseDecimalOrZero(row[6])
	if err != nil {
		return roottypes.KlineUpdate{}, err
	}
	return roottypes.KlineUpdate{
		Symbol:    symbol,
		Interval:  tf,
		StartMs:   openMs,
		EndMs:     0,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		Turnover:  turnover,
		Confirmed: false,
	}, nil
}

// ---------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------

// surfaceError logs to the SDK logger and forwards to errHandler when
// non-nil. The logger field set is intentionally compact — these errors
// are extremely chatty in production (one per botched frame).
func (s *StreamClient) surfaceError(errHandler func(error), method, ctx string, err error) {
	s.c.logger().Debug("mix.Stream: "+method+" "+ctx,
		bitget.Err(err),
	)
	if errHandler != nil {
		errHandler(err)
	}
}
