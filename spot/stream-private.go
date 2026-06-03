/*
FILE: spot/stream-private.go

DESCRIPTION:
Private WebSocket sub-client for the Bitget V2 SPOT profile. After
M6 the file wires THREE channels:

	WatchOrders   → "orders"  full order lifecycle (M5)
	WatchAccount  → "account" per-asset balance pushes (M6)
	WatchFills    → "fill"    per-execution feed (M6)

NOT INCLUDED ON SPOT (intentionally):

  - WatchPositions: spot is cash-only, no positions. Mix has it as
    a separate channel; on spot the "positions" private channel
    simply does not exist.

DESIGN — IDENTICAL TO mix.StreamClient WHERE THE PROTOCOL IS IDENTICAL:

  - One LAZY *ws.Conn per StreamClient, separate from the public
    conn spun up in stream.go. The supervisor performs the V2 login
    op (ACCESS-KEY / passphrase / timestamp / sign over GET
    /user/verify in base64-HMAC, see internal/auth.SignWS) before
    issuing any subscribe op.

  - Channel wire shape: instType="SPOT", channel="orders",
    instId="default" — Bitget V2 REQUIRES "default" for the orders
    private channel. Per-symbol subscriptions are rejected with
    code=30001 "...doesn't exist", same as on mix (confirmed via
    https://www.bitget.com/api-doc/spot/websocket/private-channels
    and the regression history captured in mix/stream-private.go).

    The SDK preserves the per-symbol public API (callers pass the
    symbol they care about) by filtering rows in the dispatcher:
    the "default" subscription delivers EVERY order on the spot
    account, and the dispatcher invokes the user handler only for
    rows whose row.InstID matches the requested symbol. Pass
    symbol="default" (or empty) to receive every row unfiltered.

  - Reconnect / relogin / resubscribe is fully handled by ws.Conn —
    the StreamClient never observes a transport reset.

  - Wire row struct lives next to the handler so the REST helpers
    in spot/account.go stay focused on their endpoint. SPOT WS
    "orders" frame omits mix-only fields (TradeSide / PosSide /
    MarginCoin / MarginMode / Leverage / ReduceOnly).

CALLBACK CONTRACTS:

  - The user handler is invoked once per row in the data array.
    Bitget batches multiple events into a single push during fast
    state transitions; the SDK fans them out so desk-side handlers
    do not have to.

  - errHandler receives decode errors and validation failures. The
    supervisor keeps retrying on its own — a handler returning
    silently is a valid "log only" pattern.
*/

package spot

import (
	"context"
	"sync"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/bgmet"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// Channel name constants for private subscriptions. Kept here so a
// typo on the wire side surfaces at compile-time.
const (
	channelOrders  = "orders"
	channelAccount = "account"
	channelFill    = "fill"
)

// instIDDefaultPrivate is the only accepted instId value on Bitget
// V2 private orders / fill channels. Passing any actual symbol
// yields code=30001 "...doesn't exist".
const instIDDefaultPrivate = "default"

// coinDefaultPrivate is the only accepted `coin` value on Bitget V2
// spot's `account` channel ("Only default is supported now" — per
// the official docs). Per-coin filtering happens client-side.
const coinDefaultPrivate = "default"

// privateConnState bundles every field that needs locking around the
// private connection lifecycle. StreamClient embeds it under its own
// privateMu so the public-side fields stay decoupled.
type privateConnState struct {
	mu        sync.Mutex
	conn      *ws.Conn
	ctx       context.Context
	closeOnce sync.Once
}

// ensurePrivateConn returns the lazily-constructed private WS
// connection. First call dials, logs in (handled by ws.Conn), and
// starts the supervisor. Returns a typed ErrorKindAuth when the
// signer has no credentials configured — private channels make no
// sense without API keys.
func (s *StreamClient) ensurePrivateConn() (*ws.Conn, error) {
	if !s.c.signerEnabled() {
		return nil, bitget.NewError(bitget.ErrorKindAuth, "",
			"spot.Stream: private channels require API key + secret + passphrase", nil)
	}

	s.privateState.mu.Lock()
	defer s.privateState.mu.Unlock()
	if s.privateState.conn != nil {
		return s.privateState.conn, nil
	}

	var cfg bitget.Config = s.c.config()
	var wsCfg ws.Config = ws.Config{
		URL:                     cfg.WS.PrivateURL,
		IsPrivate:               true,
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

	s.privateState.conn = ws.NewConn(wsCfg, s.c.parent.Signer(), s.c.logger(), metricsFactory)
	s.privateState.ctx = context.Background()
	s.privateState.conn.Start(s.privateState.ctx)
	return s.privateState.conn, nil
}

// closePrivate shuts the private connection down. Idempotent; invoked
// from StreamClient.Close.
func (s *StreamClient) closePrivate() {
	s.privateState.closeOnce.Do(func() {
		s.privateState.mu.Lock()
		defer s.privateState.mu.Unlock()
		if s.privateState.conn != nil {
			_ = s.privateState.conn.Close()
			s.privateState.conn = nil
		}
	})
}

// detachPrivateOnContextDone — same shape as detachOnContextDone
// (public side) but keyed at the private socket. Kept separate so a
// future refactor can add per-arg cleanup specific to private
// channels (e.g. dropping order maps) without touching the public
// path.
func (s *StreamClient) detachPrivateOnContextDone(ctx context.Context, arg ws.SubscriptionArg) {
	if ctx == nil {
		return
	}
	go func() {
		<-ctx.Done()
		s.privateState.mu.Lock()
		var conn *ws.Conn = s.privateState.conn
		s.privateState.mu.Unlock()
		if conn != nil {
			_ = conn.Unsubscribe(arg)
		}
	}()
}

// ---------------------------------------------------------------------
// WatchOrders.
// ---------------------------------------------------------------------

// WatchOrders subscribes to the "orders" private channel.
//
// The handler is invoked once per order row whose InstID matches
// `symbol` (Bitget batches state transitions into a single push and
// the SDK fans them out). Pass `symbol="default"` (or the empty
// string is rejected by validation — see below) to subscribe; rows
// for OTHER symbols on the account are filtered out client-side.
//
// IMPORTANT — WIRE CONTRACT:
// On the wire the SDK always subscribes with instId="default":
// Bitget V2 has no per-symbol orders subscription on spot
// (returns code=30001 "instId:<sym> doesn't exist", same as mix).
// The per-symbol semantics callers expect are preserved client-side
// via the InstID filter inside handleOrdersFrame. To opt out of the
// filter (receive every order on the account) pass symbol="default"
// — this is the public escape hatch that mirrors mix.
func (s *StreamClient) WatchOrders(
	ctx context.Context,
	symbol string,
	handler func(spottypes.OrderInfo),
	errHandler func(error),
) error {
	if symbol == "" {
		return errInvalidRequest("WatchOrders", "symbol is empty")
	}
	if handler == nil {
		return errInvalidRequest("WatchOrders", "handler is nil")
	}

	var conn *ws.Conn
	var err error
	conn, err = s.ensurePrivateConn()
	if err != nil {
		return err
	}

	// Capture the caller's symbol filter into the closure;
	// "default" means "no filter" (caller wants every order on the
	// account).
	var filter string = symbol
	if filter == instIDDefaultPrivate {
		filter = ""
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: SpotInstType,
		Channel:  channelOrders,
		InstID:   instIDDefaultPrivate,
	}
	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handleOrdersFrame(payload, filter, handler, errHandler)
		},
	}
	if err = conn.Subscribe(sub); err != nil {
		return err
	}
	s.detachPrivateOnContextDone(ctx, arg)
	return nil
}

// ---------------------------------------------------------------------
// WatchAccount.
// ---------------------------------------------------------------------

// WatchAccount subscribes to the spot `account` private channel and
// surfaces per-asset balance updates. Bitget pushes one row per coin
// on the account; the SDK fans them out so the handler is invoked
// once per coin per push.
//
// `coin` filter — pass any concrete coin (e.g. "USDT") to receive
// only its rows; pass empty string or "default" to receive every
// asset on the account. The wire ALWAYS subscribes with
// coin="default" because Bitget V2's spot account channel rejects
// per-coin subscriptions ("Only default is supported now"). Per-
// coin semantics are preserved client-side via the dispatcher
// filter.
//
// Empty handler is rejected at validation time (ErrorKindInvalidRequest).
// Empty coin is allowed and means "no filter" — same opt-out shape
// as orders/symbol="default".
func (s *StreamClient) WatchAccount(
	ctx context.Context,
	coin string,
	handler func(spottypes.AccountUpdate),
	errHandler func(error),
) error {
	if handler == nil {
		return errInvalidRequest("WatchAccount", "handler is nil")
	}

	var conn *ws.Conn
	var err error
	conn, err = s.ensurePrivateConn()
	if err != nil {
		return err
	}

	// "default" / empty string both opt out of the client-side
	// filter (caller wants every asset on the account).
	var filter string = coin
	if filter == coinDefaultPrivate {
		filter = ""
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: SpotInstType,
		Channel:  channelAccount,
		Coin:     coinDefaultPrivate,
	}
	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handleAccountFrame(payload, filter, handler, errHandler)
		},
	}
	if err = conn.Subscribe(sub); err != nil {
		return err
	}
	s.detachPrivateOnContextDone(ctx, arg)
	return nil
}

// ---------------------------------------------------------------------
// WatchFills.
// ---------------------------------------------------------------------

// WatchFills subscribes to the spot `fill` private channel and
// surfaces per-execution rows. Each push delivers one or more fill
// rows; the SDK fans them out to the handler.
//
// `symbol` filter — pass any concrete symbol (e.g. "BTCUSDT") to
// receive only its fills; pass "default" (or the empty string is
// rejected by validation) to subscribe globally. Wire-level the SDK
// always subscribes with instId="default" (Bitget V2 has no per-
// symbol fill subscription on spot — same as orders); the per-symbol
// semantics are preserved client-side via the InstID filter inside
// handleFillsFrame.
//
// IMPORTANT — clientOid IS NOT SHIPPED ON SPOT FILLS:
// Bitget V2 spot's fill channel does not include the clientOid field
// (verified against the official docs). Callers that need to map
// fills back to their own idempotency keys MUST go through the
// orders channel (which DOES carry clientOid) and join on OrderID.
// This is a venue-level invariant; the SDK does not invent a
// stateful cross-channel join.
func (s *StreamClient) WatchFills(
	ctx context.Context,
	symbol string,
	handler func(spottypes.FillUpdate),
	errHandler func(error),
) error {
	if symbol == "" {
		return errInvalidRequest("WatchFills", "symbol is empty")
	}
	if handler == nil {
		return errInvalidRequest("WatchFills", "handler is nil")
	}

	var conn *ws.Conn
	var err error
	conn, err = s.ensurePrivateConn()
	if err != nil {
		return err
	}

	var filter string = symbol
	if filter == instIDDefaultPrivate {
		filter = ""
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: SpotInstType,
		Channel:  channelFill,
		InstID:   instIDDefaultPrivate,
	}
	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handleFillsFrame(payload, filter, handler, errHandler)
		},
	}
	if err = conn.Subscribe(sub); err != nil {
		return err
	}
	s.detachPrivateOnContextDone(ctx, arg)
	return nil
}

// ---------------------------------------------------------------------
// Frame handlers.
// ---------------------------------------------------------------------

// handleOrdersFrame parses one "orders" channel frame and fans the
// per-order rows out to the user handler.
//
// symbolFilter — when non-empty, only rows with row.InstID ==
// symbolFilter are surfaced. Empty string disables filtering (used
// for symbol="default" callers that want every order on the
// account). The filter runs BEFORE convertWSOrderRow so we don't pay
// the decimal-parse cost for irrelevant rows on multi-symbol
// accounts.
func (s *StreamClient) handleOrdersFrame(
	payload []byte,
	symbolFilter string,
	handler func(spottypes.OrderInfo),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}
	var rows []wsOrderRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchOrders", "decode orders frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		if symbolFilter != "" && rows[i].InstID != symbolFilter {
			continue
		}
		var info spottypes.OrderInfo
		var err error
		info, err = convertWSOrderRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchOrders", "parse orders row", err)
			continue
		}
		handler(info)
	}
}

// handleAccountFrame parses one "account" channel frame and fans the
// per-asset rows out to the user handler.
//
// coinFilter — when non-empty, only rows with row.Coin == coinFilter
// are surfaced. Empty string disables filtering (used for empty/
// "default" callers). The filter runs BEFORE convertWSAccountRow so
// we don't pay the decimal-parse cost for irrelevant rows on multi-
// asset accounts (typical: one push of 10–30 coins).
func (s *StreamClient) handleAccountFrame(
	payload []byte,
	coinFilter string,
	handler func(spottypes.AccountUpdate),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}
	var rows []wsAccountRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchAccount", "decode account frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		if coinFilter != "" && rows[i].Coin != coinFilter {
			continue
		}
		var info spottypes.AccountUpdate
		var err error
		info, err = convertWSAccountRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchAccount", "parse account row", err)
			continue
		}
		handler(info)
	}
}

// handleFillsFrame parses one "fill" channel frame and fans the
// per-execution rows out to the user handler.
//
// symbolFilter — same semantics as handleOrdersFrame.
func (s *StreamClient) handleFillsFrame(
	payload []byte,
	symbolFilter string,
	handler func(spottypes.FillUpdate),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}
	var rows []wsFillRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchFills", "decode fill frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		if symbolFilter != "" && rows[i].Symbol != symbolFilter {
			continue
		}
		var info spottypes.FillUpdate
		var err error
		info, err = convertWSFillRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchFills", "parse fill row", err)
			continue
		}
		handler(info)
	}
}

// ---------------------------------------------------------------------
// Wire row struct.
// ---------------------------------------------------------------------

// wsOrderRow mirrors one element of the spot "orders" data array.
// Field names follow the Bitget V2 WS shape; differences vs. the
// REST orderRow:
//
//   - instId   instead of symbol;
//   - status   instead of state;
//   - accBaseVolume / fillPrice / fillSize / priceAvg
//     (REST collapses fills into baseVolume / priceAvg only).
//
// Differences vs. mix.wsOrderRow:
//
//   - No tradeSide / posSide (spot is cash-only);
//   - no marginCoin / marginMode / leverage (no margin on spot);
//   - no reduceOnly (no positions to reduce).
//
// Numeric fields use bgcommon.FlexString to accept both quoted
// strings ("0.01") and JSON numbers (0.01) — Bitget has been seen
// to ship both shapes for the same field across endpoints (and
// across hot-fixes).
type wsOrderRow struct {
	InstID        string              `json:"instId"`
	OrderID       string              `json:"orderId"`
	ClientOid     string              `json:"clientOid"`
	Side          string              `json:"side"`
	OrderType     string              `json:"orderType"`
	Force         string              `json:"force"`
	Status        string              `json:"status"`
	Size          bgcommon.FlexString `json:"size"`
	Price         bgcommon.FlexString `json:"price"`
	NotionalUSD   bgcommon.FlexString `json:"notionalUsd"`
	AccBaseVolume bgcommon.FlexString `json:"accBaseVolume"`
	PriceAvg      bgcommon.FlexString `json:"priceAvg"`
	Fee           bgcommon.FlexString `json:"fee"`
	FeeDetailRaw  string              `json:"feeDetail"`
	CTime         bgcommon.FlexString `json:"cTime"`
	UTime         bgcommon.FlexString `json:"uTime"`
}

// ---------------------------------------------------------------------
// Wire → SDK conversion.
// ---------------------------------------------------------------------

func convertWSOrderRow(row wsOrderRow) (spottypes.OrderInfo, error) {
	var out spottypes.OrderInfo = spottypes.OrderInfo{
		OrderID:       row.OrderID,
		ClientOrderID: row.ClientOid,
		Symbol:        row.InstID,
		Side:          roottypes.SideType(row.Side),
		OrderType:     roottypes.OrderType(row.OrderType),
		TimeInForce:   roottypes.TimeInForceType(row.Force),
		Status:        roottypes.OrderStatus(row.Status),
	}

	var err error
	out.Quantity, err = bgcommon.ParseDecimalOrZero(string(row.Size))
	if err != nil {
		return spottypes.OrderInfo{}, wrapWSOrderParseErr("size", err)
	}
	out.Price, err = bgcommon.ParseDecimalOrZero(string(row.Price))
	if err != nil {
		return spottypes.OrderInfo{}, wrapWSOrderParseErr("price", err)
	}
	out.FilledQuantity, err = bgcommon.ParseDecimalOrZero(string(row.AccBaseVolume))
	if err != nil {
		return spottypes.OrderInfo{}, wrapWSOrderParseErr("accBaseVolume", err)
	}
	out.AvgFilledPrice, err = bgcommon.ParseDecimalOrZero(string(row.PriceAvg))
	if err != nil {
		return spottypes.OrderInfo{}, wrapWSOrderParseErr("priceAvg", err)
	}
	out.CumFee, err = bgcommon.ParseDecimalOrZero(string(row.Fee))
	if err != nil {
		return spottypes.OrderInfo{}, wrapWSOrderParseErr("fee", err)
	}
	out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.CTime))
	if err != nil {
		return spottypes.OrderInfo{}, wrapWSOrderParseErr("cTime", err)
	}
	out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.UTime))
	if err != nil {
		return spottypes.OrderInfo{}, wrapWSOrderParseErr("uTime", err)
	}
	return out, nil
}

// wsAccountRow mirrors one element of the spot "account" data array.
// Numeric fields use bgcommon.FlexString to absorb the JSON-number-
// vs-quoted-string regression class (PARTIUSDT, May 2026, v1.2.1 on
// mix positions; lock the same guardrail here pre-emptively).
type wsAccountRow struct {
	Coin           string              `json:"coin"`
	Available      bgcommon.FlexString `json:"available"`
	Frozen         bgcommon.FlexString `json:"frozen"`
	Locked         bgcommon.FlexString `json:"locked"`
	LimitAvailable bgcommon.FlexString `json:"limitAvailable"`
	UTime          bgcommon.FlexString `json:"uTime"`
}

// wsFillRow mirrors one element of the spot "fill" data array.
// Differences vs. mix.wsFillRow:
//
//   - No clientOid (spot's fill channel does not ship it);
//   - no posMode / tradeSide / profit (spot is cash-only);
//   - priceAvg / size / amount instead of price / baseVolume /
//     quoteVolume (Bitget renames the same concepts on spot).
//
// FeeDetail uses the shared bgcommon row + parser — its wire shape
// is byte-identical between spot and mix.
type wsFillRow struct {
	OrderID    string                    `json:"orderId"`
	TradeID    string                    `json:"tradeId"`
	Symbol     string                    `json:"symbol"`
	Side       string                    `json:"side"`
	OrderType  string                    `json:"orderType"`
	PriceAvg   bgcommon.FlexString       `json:"priceAvg"`
	Size       bgcommon.FlexString       `json:"size"`
	Amount     bgcommon.FlexString       `json:"amount"`
	TradeScope string                    `json:"tradeScope"`
	FeeDetail  []bgcommon.WSFeeDetailRow `json:"feeDetail"`
	CTime      bgcommon.FlexString       `json:"cTime"`
	UTime      bgcommon.FlexString       `json:"uTime"`
}

// ---------------------------------------------------------------------
// Wire → SDK conversions (account / fill).
// ---------------------------------------------------------------------

func convertWSAccountRow(row wsAccountRow) (spottypes.AccountUpdate, error) {
	var out spottypes.AccountUpdate = spottypes.AccountUpdate{
		Coin: row.Coin,
	}
	var err error
	out.Available, err = bgcommon.ParseDecimalOrZero(string(row.Available))
	if err != nil {
		return spottypes.AccountUpdate{}, wrapWSAccountParseErr("available", err)
	}
	out.Frozen, err = bgcommon.ParseDecimalOrZero(string(row.Frozen))
	if err != nil {
		return spottypes.AccountUpdate{}, wrapWSAccountParseErr("frozen", err)
	}
	out.Locked, err = bgcommon.ParseDecimalOrZero(string(row.Locked))
	if err != nil {
		return spottypes.AccountUpdate{}, wrapWSAccountParseErr("locked", err)
	}
	out.LimitAvailable, err = bgcommon.ParseDecimalOrZero(string(row.LimitAvailable))
	if err != nil {
		return spottypes.AccountUpdate{}, wrapWSAccountParseErr("limitAvailable", err)
	}
	out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.UTime))
	if err != nil {
		return spottypes.AccountUpdate{}, wrapWSAccountParseErr("uTime", err)
	}
	return out, nil
}

func convertWSFillRow(row wsFillRow) (spottypes.FillUpdate, error) {
	var out spottypes.FillUpdate = spottypes.FillUpdate{
		OrderID:    row.OrderID,
		TradeID:    row.TradeID,
		Symbol:     row.Symbol,
		Side:       roottypes.SideType(row.Side),
		OrderType:  roottypes.OrderType(row.OrderType),
		TradeScope: row.TradeScope,
	}

	var err error
	out.PriceAvg, err = bgcommon.ParseDecimalOrZero(string(row.PriceAvg))
	if err != nil {
		return spottypes.FillUpdate{}, wrapWSFillParseErr("priceAvg", err)
	}
	out.Size, err = bgcommon.ParseDecimalOrZero(string(row.Size))
	if err != nil {
		return spottypes.FillUpdate{}, wrapWSFillParseErr("size", err)
	}
	out.Amount, err = bgcommon.ParseDecimalOrZero(string(row.Amount))
	if err != nil {
		return spottypes.FillUpdate{}, wrapWSFillParseErr("amount", err)
	}
	out.FeeDetail, err = bgcommon.ParseFeeDetailList(row.FeeDetail)
	if err != nil {
		return spottypes.FillUpdate{}, wrapWSFillParseErr("feeDetail", err)
	}
	out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.CTime))
	if err != nil {
		return spottypes.FillUpdate{}, wrapWSFillParseErr("cTime", err)
	}
	out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.UTime))
	if err != nil {
		return spottypes.FillUpdate{}, wrapWSFillParseErr("uTime", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Error helpers.
// ---------------------------------------------------------------------

func wrapWSOrderParseErr(field string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "",
		"spot.Stream.WatchOrders: parse "+field, cause)
}

func wrapWSAccountParseErr(field string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "",
		"spot.Stream.WatchAccount: parse "+field, cause)
}

func wrapWSFillParseErr(field string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "",
		"spot.Stream.WatchFills: parse "+field, cause)
}
