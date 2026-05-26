/*
FILE: mix/stream-private.go

DESCRIPTION:
Private WebSocket sub-client for Bitget MIX. Wires the three account-side
channels the desk consumes off the private feed:

	WatchOrders     → "orders"     (full order lifecycle: place/fill/cancel)
	WatchPositions  → "positions"  (position size / margin / pnl)
	WatchAccount    → "account"    (per-margin-coin wallet snapshot)

DESIGN:

  - One LAZY *ws.Conn per StreamClient, separate from the public conn
    spun up in stream.go. The supervisor performs the V2 login op
    (ACCESS-KEY / passphrase / timestamp / sign over GET /user/verify
    in base64-HMAC, see internal/auth.SignWS) before issuing any
    subscribe op.

  - Channel wire shape: instType=ProductType (USDT-FUTURES / ...),
    channel=orders|positions, instId="default" — Bitget V2 REQUIRES
    "default" for the orders / positions / fill / plan-order private
    channels. Passing the actual symbol is rejected with code=30001
    "...doesn't exist" (regression seen in PARTIUSDT field log under
    v1.0.4: the new login fix surfaced this older subscribe bug).
    Confirmed via https://www.bitget.com/api-doc/classic/best-practices
    and tiagosiebler/bitget-api (`coin: string = 'default'`).

    The SDK preserves the per-symbol public API (callers pass the
    symbol they care about) by filtering rows in the dispatcher:
    the "default" subscription delivers EVERY symbol the account
    holds, and the dispatcher invokes the user handler only for
    rows whose row.InstID matches the requested symbol. Pass
    symbol="default" to receive every row unfiltered. The "account"
    channel keys off coin=marginCoin instead of instId — separate
    rule, untouched by this change.

  - Reconnect / relogin / resubscribe is fully handled by ws.Conn —
    the StreamClient never observes a transport reset.

  - JSON wire shapes overlap heavily with the REST counterparts (see
    mix/account.go orderRow / positionRow / accountsRow) but a few
    fields are renamed on the WS side: `instId` instead of `symbol`,
    `status` instead of `state`, `frozen` instead of `locked`, etc.
    A dedicated wire-row struct lives next to each handler so the
    REST helpers in mix/account.go stay focused on their endpoint.

CALLBACK CONTRACTS:

  - The user handler is invoked once per row in the data array.
    Bitget batches multiple events into a single push during fast
    state transitions; the SDK fans them out so desk-side handlers
    do not have to.

  - errHandler receives decode errors, validation failures and any
    SDK-level error. The supervisor keeps retrying on its own — a
    handler returning silently is a valid "log only" pattern.
*/

package mix

import (
	"context"
	"sync"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgmet"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// Channel name constants for private subscriptions. Kept here so a
// typo on the wire side surfaces at compile-time.
const (
	channelOrders    = "orders"
	channelPositions = "positions"
	channelAccount   = "account"
)

// instIDDefaultPrivate is the only accepted instId value on Bitget
// V2 private orders / positions / fill / plan-order channels.
// Passing any actual symbol yields code=30001 "...doesn't exist".
const instIDDefaultPrivate = "default"

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
			"mix.Stream: private channels require API key + secret + passphrase", nil)
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

// closePrivate shuts the private connection down. Idempotent;
// invoked from StreamClient.Close.
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

// detachPrivateOnContextDone — same shape as detachOnContextDone but
// keyed at the private socket. Kept separate so a future refactor can
// add per-arg cleanup specific to private channels (e.g. dropping
// position/orders maps) without touching the public path.
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
// string) to receive every row unfiltered — useful for desks that
// fan out by symbol on their own.
//
// IMPORTANT:
// On the wire the SDK always subscribes with instId="default":
// Bitget V2 has no per-symbol orders subscription (returns code=30001
// "instId:<sym> doesn't exist"). The per-symbol semantics callers
// expect are preserved client-side via the InstID filter inside
// handleOrdersFrame.
func (s *StreamClient) WatchOrders(
	ctx context.Context,
	symbol string,
	handler func(mixtypes.OrderInfo),
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

	// Capture the caller's symbol filter into the closure; "default"
	// (and the empty string, defensively) means "no filter".
	var filter string = symbol
	if filter == instIDDefaultPrivate {
		filter = ""
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: string(s.c.productType),
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
// WatchPositions.
// ---------------------------------------------------------------------

// WatchPositions subscribes to the "positions" private channel.
//
// The handler is invoked once per position row whose InstID matches
// `symbol`. Pass `symbol="default"` (or the empty string) to receive
// every position on the account.
//
// IMPORTANT:
// On the wire the SDK always subscribes with instId="default":
// Bitget V2's positions channel has no per-symbol mode (returns
// code=30001 "instId:<sym> doesn't exist"; per Bitget Best Practices
// guide the only accepted instId is "default"). The per-symbol
// semantics callers expect are preserved client-side via the InstID
// filter inside handlePositionsFrame.
func (s *StreamClient) WatchPositions(
	ctx context.Context,
	symbol string,
	handler func(mixtypes.PositionInfo),
	errHandler func(error),
) error {
	if symbol == "" {
		return errInvalidRequest("WatchPositions", "symbol is empty")
	}
	if handler == nil {
		return errInvalidRequest("WatchPositions", "handler is nil")
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
		InstType: string(s.c.productType),
		Channel:  channelPositions,
		InstID:   instIDDefaultPrivate,
	}
	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handlePositionsFrame(payload, filter, handler, errHandler)
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

// WatchAccount subscribes to the "account" private channel for the
// margin coin pinned to mix.Client (USDT for USDT-FUTURES, USDC for
// USDC-FUTURES, etc.). Each push delivers one Balance per row; for the
// USDT-FUTURES product type the row count is 1.
//
// The handler receives a fully-populated roottypes.Balance: the wire
// row maps to MarginCoin / TotalEquity / AvailableBalance /
// LockedBalance / UnrealizedPnL plus a single CoinBalance. Callers
// that want the per-coin breakdown should iterate balance.Coins.
func (s *StreamClient) WatchAccount(
	ctx context.Context,
	handler func(roottypes.Balance),
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

	// Bitget V2 keys the account channel by coin, not instId. We pass
	// the configured marginCoin (USDT for USDT-FUTURES) so a hedge-mode
	// account on multiple products still gets one focused stream per
	// mix.Client.
	var coin string = s.c.marginCoin
	if coin == "" {
		// COIN-FUTURES has no single margin coin; "default" makes the
		// stream wildcard. Documented behaviour, not a Bitget bug.
		coin = "default"
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: string(s.c.productType),
		Channel:  channelAccount,
		Coin:     coin,
	}
	var sub *ws.Subscription = &ws.Subscription{
		Arg: arg,
		Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
			s.handleAccountFrame(payload, handler, errHandler)
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
// for symbol="default" callers that want every order on the account).
// The filter runs BEFORE convertWSOrderRow so we don't pay the
// decimal-parse cost for irrelevant rows on multi-symbol accounts.
func (s *StreamClient) handleOrdersFrame(
	payload []byte,
	symbolFilter string,
	handler func(mixtypes.OrderInfo),
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
		var info mixtypes.OrderInfo
		var err error
		info, err = convertWSOrderRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchOrders", "parse orders row", err)
			continue
		}
		handler(info)
	}
}

// handlePositionsFrame parses one "positions" channel frame.
// See handleOrdersFrame for symbolFilter semantics.
func (s *StreamClient) handlePositionsFrame(
	payload []byte,
	symbolFilter string,
	handler func(mixtypes.PositionInfo),
	errHandler func(error),
) {
	if len(payload) == 0 {
		return
	}
	var rows []wsPositionRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		s.surfaceError(errHandler, "WatchPositions", "decode positions frame", err)
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		if symbolFilter != "" && rows[i].InstID != symbolFilter {
			continue
		}
		var info mixtypes.PositionInfo
		var err error
		info, err = convertWSPositionRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchPositions", "parse positions row", err)
			continue
		}
		handler(info)
	}
}

// handleAccountFrame parses one "account" channel frame.
func (s *StreamClient) handleAccountFrame(
	payload []byte,
	handler func(roottypes.Balance),
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
		var bal roottypes.Balance
		var err error
		bal, err = convertWSAccountRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchAccount", "parse account row", err)
			continue
		}
		handler(bal)
	}
}

// ---------------------------------------------------------------------
// Wire row structs.
// ---------------------------------------------------------------------

// wsOrderRow mirrors one element of the "orders" data array. Field
// names follow the Bitget V2 WS shape; differences vs. the REST
// orderRow:
//
//   - instId   instead of symbol;
//   - status   instead of state;
//   - accBaseVolume / fillPrice / fillSize / priceAvg
//     (REST collapses fills into baseVolume / priceAvg only).
type wsOrderRow struct {
	InstID         string `json:"instId"`
	OrderID        string `json:"orderId"`
	ClientOid      string `json:"clientOid"`
	Side           string `json:"side"`
	TradeSide      string `json:"tradeSide"`
	PosSide        string `json:"posSide"`
	OrderType      string `json:"orderType"`
	Force          string `json:"force"`
	Status         string `json:"status"`
	Size           string `json:"size"`
	Price          string `json:"price"`
	NotionalUSD    string `json:"notionalUsd"`
	AccBaseVolume  string `json:"accBaseVolume"`
	PriceAvg       string `json:"priceAvg"`
	Fee            string `json:"fee"`
	FeeDetailRaw   string `json:"feeDetail"`
	MarginCoin     string `json:"marginCoin"`
	MarginMode     string `json:"marginMode"`
	Leverage       string `json:"leverage"`
	ReduceOnly     string `json:"reduceOnly"`
	CTime          string `json:"cTime"`
	UTime          string `json:"uTime"`
}

// wsPositionRow mirrors one element of the "positions" data array.
// Differences vs. REST positionRow:
//
//   - instId  instead of symbol;
//   - frozen  instead of locked.
type wsPositionRow struct {
	InstID           string `json:"instId"`
	MarginCoin       string `json:"marginCoin"`
	HoldSide         string `json:"holdSide"`
	HoldMode         string `json:"holdMode"`
	OpenDelegateSize string `json:"openDelegateSize"`
	MarginSize       string `json:"marginSize"`
	Available        string `json:"available"`
	Frozen           string `json:"frozen"`
	Total            string `json:"total"`
	Leverage         string `json:"leverage"`
	AchievedProfits  string `json:"achievedProfits"`
	OpenPriceAvg     string `json:"openPriceAvg"`
	MarginMode       string `json:"marginMode"`
	UnrealizedPL     string `json:"unrealizedPL"`
	LiquidationPrice string `json:"liquidationPrice"`
	KeepMarginRate   string `json:"keepMarginRate"`
	MarkPrice        string `json:"markPrice"`
	MarginRatio      string `json:"marginRatio"`
	BreakEvenPrice   string `json:"breakEvenPrice"`
	CTime            string `json:"cTime"`
	UTime            string `json:"uTime"`
}

// wsAccountRow mirrors one element of the "account" data array. The
// "account" channel is per-coin: each row carries balance fields for
// one margin coin. For USDT-FUTURES the array is length 1.
type wsAccountRow struct {
	MarginCoin          string `json:"marginCoin"`
	Frozen              string `json:"frozen"`
	Available           string `json:"available"`
	MaxOpenPosAvail     string `json:"maxOpenPosAvailable"`
	MaxTransferOut      string `json:"maxTransferOut"`
	Equity              string `json:"equity"`
	UsdtEquity          string `json:"usdtEquity"`
	BtcEquity           string `json:"btcEquity"`
	UnrealizedPL        string `json:"unrealizedPL"`
	CrossedRiskRate     string `json:"crossedRiskRate"`
	CrossedMarginLever  string `json:"crossedMarginLeverage"`
	IsolatedLongLever   string `json:"isolatedLongLever"`
	IsolatedShortLever  string `json:"isolatedShortLever"`
	Locked              string `json:"locked"`
	Coupon              string `json:"coupon"`
}

// ---------------------------------------------------------------------
// Wire → SDK conversions.
// ---------------------------------------------------------------------

func convertWSOrderRow(row wsOrderRow) (mixtypes.OrderInfo, error) {
	var out mixtypes.OrderInfo = mixtypes.OrderInfo{
		OrderID:       row.OrderID,
		ClientOrderID: row.ClientOid,
		Symbol:        row.InstID,
		Side:          roottypes.SideType(row.Side),
		TradeSide:     roottypes.TradeSide(row.TradeSide),
		HoldSide:      mixtypes.HoldSide(row.PosSide),
		OrderType:     roottypes.OrderType(row.OrderType),
		TimeInForce:   roottypes.TimeInForceType(row.Force),
		Status:        roottypes.OrderStatus(row.Status),
	}

	var err error
	out.Quantity, err = parseDecimalOrZero(row.Size)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapWSOrderParseErr("size", err)
	}
	out.Price, err = parseDecimalOrZero(row.Price)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapWSOrderParseErr("price", err)
	}
	out.FilledQuantity, err = parseDecimalOrZero(row.AccBaseVolume)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapWSOrderParseErr("accBaseVolume", err)
	}
	out.AvgFilledPrice, err = parseDecimalOrZero(row.PriceAvg)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapWSOrderParseErr("priceAvg", err)
	}
	out.CumFee, err = parseDecimalOrZero(row.Fee)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapWSOrderParseErr("fee", err)
	}
	out.CreatedAtMs, err = parseInt64OrZero(row.CTime)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapWSOrderParseErr("cTime", err)
	}
	out.UpdatedAtMs, err = parseInt64OrZero(row.UTime)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapWSOrderParseErr("uTime", err)
	}
	return out, nil
}

func convertWSPositionRow(row wsPositionRow) (mixtypes.PositionInfo, error) {
	var out mixtypes.PositionInfo = mixtypes.PositionInfo{
		Symbol:     row.InstID,
		HoldSide:   mixtypes.HoldSide(row.HoldSide),
		MarginMode: roottypes.MarginMode(row.MarginMode),
		MarginCoin: row.MarginCoin,
	}

	var err error
	out.Quantity, err = parseDecimalOrZero(row.Total)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("total", err)
	}
	out.Available, err = parseDecimalOrZero(row.Available)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("available", err)
	}
	out.Locked, err = parseDecimalOrZero(row.Frozen)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("frozen", err)
	}
	out.AvgOpenPrice, err = parseDecimalOrZero(row.OpenPriceAvg)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("openPriceAvg", err)
	}
	out.MarkPrice, err = parseDecimalOrZero(row.MarkPrice)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("markPrice", err)
	}
	out.LiquidationPrice, err = parseDecimalOrZero(row.LiquidationPrice)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("liquidationPrice", err)
	}
	out.UnrealizedPnL, err = parseDecimalOrZero(row.UnrealizedPL)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("unrealizedPL", err)
	}
	out.RealizedPnL, err = parseDecimalOrZero(row.AchievedProfits)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("achievedProfits", err)
	}
	out.Leverage, err = parseIntOrZero(row.Leverage)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("leverage", err)
	}
	out.CreatedAtMs, err = parseInt64OrZero(row.CTime)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("cTime", err)
	}
	out.UpdatedAtMs, err = parseInt64OrZero(row.UTime)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapWSPositionParseErr("uTime", err)
	}
	return out, nil
}

func convertWSAccountRow(row wsAccountRow) (roottypes.Balance, error) {
	var out roottypes.Balance = roottypes.Balance{
		MarginCoin: row.MarginCoin,
	}

	var err error
	out.TotalEquity, err = parseDecimalOrZero(row.Equity)
	if err != nil {
		return roottypes.Balance{}, wrapWSAccountParseErr("equity", err)
	}
	out.AvailableBalance, err = parseDecimalOrZero(row.Available)
	if err != nil {
		return roottypes.Balance{}, wrapWSAccountParseErr("available", err)
	}
	// Bitget WS push uses "frozen" for funds reserved by orders/margin;
	// REST emits the same value under "locked". We surface both as
	// LockedBalance so downstream code does not need to know which
	// transport delivered the row.
	var locked decimal.Decimal
	locked, err = parseDecimalOrZero(row.Frozen)
	if err != nil {
		return roottypes.Balance{}, wrapWSAccountParseErr("frozen", err)
	}
	if locked.IsZero() {
		// Some Bitget endpoints emit both; prefer the non-zero one.
		locked, err = parseDecimalOrZero(row.Locked)
		if err != nil {
			return roottypes.Balance{}, wrapWSAccountParseErr("locked", err)
		}
	}
	out.LockedBalance = locked

	out.UnrealizedPnL, err = parseDecimalOrZero(row.UnrealizedPL)
	if err != nil {
		return roottypes.Balance{}, wrapWSAccountParseErr("unrealizedPL", err)
	}
	out.MaintenanceMargin = decimal.Zero

	var usdtEquity decimal.Decimal
	usdtEquity, err = parseDecimalOrZero(row.UsdtEquity)
	if err != nil {
		return roottypes.Balance{}, wrapWSAccountParseErr("usdtEquity", err)
	}
	var btcEquity decimal.Decimal
	btcEquity, err = parseDecimalOrZero(row.BtcEquity)
	if err != nil {
		return roottypes.Balance{}, wrapWSAccountParseErr("btcEquity", err)
	}
	var frozen decimal.Decimal
	frozen, err = parseDecimalOrZero(row.Frozen)
	if err != nil {
		return roottypes.Balance{}, wrapWSAccountParseErr("frozen2", err)
	}

	out.Coins = []roottypes.CoinBalance{{
		Coin:          row.MarginCoin,
		Equity:        out.TotalEquity,
		Available:     out.AvailableBalance,
		Frozen:        frozen,
		Locked:        out.LockedBalance,
		UnrealizedPnL: out.UnrealizedPnL,
		UsdtEquity:    usdtEquity,
		BtcEquity:     btcEquity,
	}}
	return out, nil
}

// ---------------------------------------------------------------------
// Error helpers.
// ---------------------------------------------------------------------

func wrapWSOrderParseErr(field string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "",
		"mix.Stream.WatchOrders: parse "+field, cause)
}

func wrapWSPositionParseErr(field string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "",
		"mix.Stream.WatchPositions: parse "+field, cause)
}

func wrapWSAccountParseErr(field string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "",
		"mix.Stream.WatchAccount: parse "+field, cause)
}
