/*
FILE: margin/stream.go

DESCRIPTION:
Private WebSocket sub-client for the Bitget V2 MARGIN profile.

Margin has NO public WS — books / tickers / trades / candles for the
underlying spot instruments come from the spot Stream. The margin
profile ships ONLY private channels, on instType="MARGIN":

	account-<mode>   per-coin borrow / available / interest snapshot
	orders-<mode>    margin order lifecycle pushes

Both channel names carry the crossed/isolated suffix matching the
client's pinned mode (account-crossed / orders-isolated / ...), built
from the SAME modeSegment() the REST paths use so the spelling has a
single source of truth.

WIRE CONTRACT — instId / coin = "default":

Like spot and mix, the V2 private channels are subscribed GLOBALLY:

  - orders-<mode>   subscribes with instId="default" (NOT a symbol —
    Bitget rejects per-symbol order subscriptions with code=30001).
  - account-<mode>  subscribes with coin="default" ("Only default is
    supported now" per the docs).

Per-symbol / per-coin semantics callers expect are preserved CLIENT-
SIDE: the dispatcher filters rows by the requested symbol / coin and
invokes the user handler only for matches. Pass "default" (or empty,
where allowed) to opt out of the filter and receive every row.

DESIGN — MIRRORS spot.StreamClient's private side:

  - One LAZY *ws.Conn per StreamClient. The supervisor performs the
    V2 login op before any subscribe; reconnect / relogin / resubscribe
    is fully handled by ws.Conn.
  - ensurePrivateConn returns a typed ErrorKindAuth when no API
    credentials are configured — private channels make no sense
    without keys.
*/

package margin

import (
	"context"
	"sync"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/bgmet"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	margintypes "github.com/tonymontanov/go-bitget/v2/margin/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// Channel name PREFIXES for the private margin subscriptions. The
// crossed/isolated suffix is appended at subscribe time from the
// client's pinned mode.
const (
	channelOrdersPrefix  = "orders-"
	channelAccountPrefix = "account-"
)

// instIDDefaultPrivate / coinDefaultPrivate — the only accepted values
// on the V2 margin private channels. Per-symbol / per-coin filtering
// happens client-side.
const (
	instIDDefaultPrivate = "default"
	coinDefaultPrivate   = "default"
)

// StreamClient — private WebSocket subscription sub-client. Built once
// per margin.Client (see client.go) and safe for concurrent use.
type StreamClient struct {
	c *Client

	mu        sync.Mutex
	conn      *ws.Conn
	ctx       context.Context
	closeOnce sync.Once
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// ordersChannel / accountChannel build the mode-suffixed channel names
// (orders-crossed / account-isolated / ...).
func (s *StreamClient) ordersChannel() string  { return channelOrdersPrefix + s.c.modeSegment() }
func (s *StreamClient) accountChannel() string { return channelAccountPrefix + s.c.modeSegment() }

// errInvalid is the canonical client-side validation error.
func (s *StreamClient) errInvalid(method, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "margin.Stream."+method+": "+msg, nil)
}

// Close shuts the private WS connection down. Idempotent.
func (s *StreamClient) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.conn != nil {
			_ = s.conn.Close()
			s.conn = nil
		}
	})
	return nil
}

// ---------------------------------------------------------------------
// Connection lifecycle.
// ---------------------------------------------------------------------

// ensureConn returns the lazily-constructed private WS connection.
// First call dials, logs in (handled by ws.Conn) and starts the
// supervisor. Returns a typed ErrorKindAuth when the signer has no
// credentials configured.
func (s *StreamClient) ensureConn() (*ws.Conn, error) {
	if !s.c.signerEnabled() {
		return nil, bitget.NewError(bitget.ErrorKindAuth, "",
			"margin.Stream: private channels require API key + secret + passphrase", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return s.conn, nil
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

	s.conn = ws.NewConn(wsCfg, s.c.parent.Signer(), s.c.logger(), metricsFactory)
	s.ctx = context.Background()
	s.conn.Start(s.ctx)
	return s.conn, nil
}

// detachOnContextDone unsubscribes the arg once the caller cancels its
// context. Runs in its own goroutine.
func (s *StreamClient) detachOnContextDone(ctx context.Context, arg ws.SubscriptionArg) {
	if ctx == nil {
		return
	}
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		var conn *ws.Conn = s.conn
		s.mu.Unlock()
		if conn != nil {
			_ = conn.Unsubscribe(arg)
		}
	}()
}

// ---------------------------------------------------------------------
// WatchOrders.
// ---------------------------------------------------------------------

// WatchOrders subscribes to the "orders-<mode>" private channel and
// surfaces margin order lifecycle pushes.
//
// The handler is invoked once per order row whose Symbol matches
// `symbol`. On the wire the SDK always subscribes with instId="default"
// (Bitget V2 has no per-symbol order subscription); the per-symbol
// semantics are preserved client-side. Pass symbol="default" to opt out
// of the filter and receive every order on the account.
func (s *StreamClient) WatchOrders(
	ctx context.Context,
	symbol string,
	handler func(margintypes.OrderInfo),
	errHandler func(error),
) error {
	if symbol == "" {
		return s.errInvalid("WatchOrders", "symbol is empty")
	}
	if handler == nil {
		return s.errInvalid("WatchOrders", "handler is nil")
	}

	var conn *ws.Conn
	var err error
	conn, err = s.ensureConn()
	if err != nil {
		return err
	}

	var filter string = symbol
	if filter == instIDDefaultPrivate {
		filter = ""
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: MarginInstType,
		Channel:  s.ordersChannel(),
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
	s.detachOnContextDone(ctx, arg)
	return nil
}

// ---------------------------------------------------------------------
// WatchAccount.
// ---------------------------------------------------------------------

// WatchAccount subscribes to the "account-<mode>" private channel and
// surfaces per-coin (crossed) / per-symbol-coin (isolated) balance
// pushes.
//
// `coin` filter — pass any concrete coin (e.g. "USDT") to receive only
// its rows; pass empty string or "default" to receive every asset. The
// wire ALWAYS subscribes with coin="default" (the only value Bitget V2
// accepts); per-coin semantics are preserved client-side.
func (s *StreamClient) WatchAccount(
	ctx context.Context,
	coin string,
	handler func(margintypes.AccountUpdate),
	errHandler func(error),
) error {
	if handler == nil {
		return s.errInvalid("WatchAccount", "handler is nil")
	}

	var conn *ws.Conn
	var err error
	conn, err = s.ensureConn()
	if err != nil {
		return err
	}

	var filter string = coin
	if filter == coinDefaultPrivate {
		filter = ""
	}

	var arg ws.SubscriptionArg = ws.SubscriptionArg{
		InstType: MarginInstType,
		Channel:  s.accountChannel(),
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
	s.detachOnContextDone(ctx, arg)
	return nil
}

// ---------------------------------------------------------------------
// Frame handlers.
// ---------------------------------------------------------------------

func (s *StreamClient) handleOrdersFrame(
	payload []byte,
	symbolFilter string,
	handler func(margintypes.OrderInfo),
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
		var sym string = rows[i].symbol()
		if symbolFilter != "" && sym != symbolFilter {
			continue
		}
		var info margintypes.OrderInfo
		var err error
		info, err = convertWSOrderRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchOrders", "parse orders row", err)
			continue
		}
		handler(info)
	}
}

func (s *StreamClient) handleAccountFrame(
	payload []byte,
	coinFilter string,
	handler func(margintypes.AccountUpdate),
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
		var info margintypes.AccountUpdate
		var err error
		info, err = convertWSAccountRow(rows[i])
		if err != nil {
			s.surfaceError(errHandler, "WatchAccount", "parse account row", err)
			continue
		}
		handler(info)
	}
}

// ---------------------------------------------------------------------
// Wire rows.
// ---------------------------------------------------------------------

// wsOrderRow mirrors one element of the margin "orders-<mode>" data
// array. Margin trades spot instruments, so the shape follows spot's
// orders row plus the margin-only loanType. Numeric fields use
// FlexString to absorb the JSON-number-vs-quoted-string regression
// class (see spot/stream-private.go).
type wsOrderRow struct {
	InstID          string                    `json:"instId"`
	Symbol          string                    `json:"symbol"`
	OrderID         string                    `json:"orderId"`
	ClientOid       string                    `json:"clientOid"`
	Side            string                    `json:"side"`
	OrderType       string                    `json:"orderType"`
	Force           string                    `json:"force"`
	Status          string                    `json:"status"`
	LoanType        string                    `json:"loanType"`
	STPMode         string                    `json:"stpMode"`
	EnterPointSrc   string                    `json:"enterPointSource"`
	Price           bgcommon.FlexString       `json:"price"`
	BaseSize        bgcommon.FlexString       `json:"baseSize"`
	QuoteSize       bgcommon.FlexString       `json:"quoteSize"`
	BaseVolume      bgcommon.FlexString       `json:"baseVolume"`
	FillPrice       bgcommon.FlexString       `json:"fillPrice"`
	FillTotalAmount bgcommon.FlexString       `json:"fillTotalAmount"`
	PriceAvg        bgcommon.FlexString       `json:"priceAvg"`
	FeeDetail       []bgcommon.WSFeeDetailRow `json:"feeDetail"`
	CTime           bgcommon.FlexString       `json:"cTime"`
	UTime           bgcommon.FlexString       `json:"uTime"`
}

// symbol resolves the trading pair from either the dedicated `symbol`
// field or the `instId` arg field (Bitget has shipped both spellings
// across the margin order channels).
func (r wsOrderRow) symbol() string {
	if r.Symbol != "" {
		return r.Symbol
	}
	return r.InstID
}

// wsAccountRow mirrors one element of the margin "account-<mode>" data
// array.
type wsAccountRow struct {
	Coin      string              `json:"coin"`
	Symbol    string              `json:"symbol"`
	Available bgcommon.FlexString `json:"available"`
	Frozen    bgcommon.FlexString `json:"frozen"`
	Borrow    bgcommon.FlexString `json:"borrow"`
	Interest  bgcommon.FlexString `json:"interest"`
	Coupon    bgcommon.FlexString `json:"coupon"`
	UTime     bgcommon.FlexString `json:"uTime"`
}

// ---------------------------------------------------------------------
// Wire → SDK conversions.
// ---------------------------------------------------------------------

func convertWSOrderRow(row wsOrderRow) (margintypes.OrderInfo, error) {
	var out margintypes.OrderInfo = margintypes.OrderInfo{
		OrderID:       row.OrderID,
		ClientOrderID: row.ClientOid,
		Symbol:        row.symbol(),
		Side:          roottypes.SideType(row.Side),
		OrderType:     roottypes.OrderType(row.OrderType),
		TimeInForce:   roottypes.TimeInForceType(row.Force),
		Status:        roottypes.OrderStatus(row.Status),
		LoanType:      margintypes.LoanType(row.LoanType),
	}

	var err error
	if out.Quantity, err = bgcommon.ParseDecimalOrZero(string(row.BaseSize)); err != nil {
		return margintypes.OrderInfo{}, wrapWSParseErr("WatchOrders", "baseSize", err)
	}
	if out.Price, err = bgcommon.ParseDecimalOrZero(string(row.Price)); err != nil {
		return margintypes.OrderInfo{}, wrapWSParseErr("WatchOrders", "price", err)
	}
	if out.FilledQuantity, err = bgcommon.ParseDecimalOrZero(string(row.BaseVolume)); err != nil {
		return margintypes.OrderInfo{}, wrapWSParseErr("WatchOrders", "baseVolume", err)
	}

	// AvgFilledPrice prefers priceAvg; fall back to fillPrice (the
	// per-fill price) when the venue omits the running average.
	var avgRaw string = string(row.PriceAvg)
	if avgRaw == "" {
		avgRaw = string(row.FillPrice)
	}
	if out.AvgFilledPrice, err = bgcommon.ParseDecimalOrZero(avgRaw); err != nil {
		return margintypes.OrderInfo{}, wrapWSParseErr("WatchOrders", "priceAvg", err)
	}

	// CumFee aggregates the feeDetail array (Bitget ships fees per
	// coin; margin orders pay a single quote-coin fee in practice but
	// the wire is an array).
	var fees []bgcommon.WSFeeDetail
	fees, err = bgcommon.ParseFeeDetailList(row.FeeDetail)
	if err != nil {
		return margintypes.OrderInfo{}, wrapWSParseErr("WatchOrders", "feeDetail", err)
	}
	var j int
	for j = 0; j < len(fees); j++ {
		out.CumFee = out.CumFee.Add(fees[j].TotalFee)
	}

	if out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.CTime)); err != nil {
		return margintypes.OrderInfo{}, wrapWSParseErr("WatchOrders", "cTime", err)
	}
	if out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.UTime)); err != nil {
		return margintypes.OrderInfo{}, wrapWSParseErr("WatchOrders", "uTime", err)
	}
	return out, nil
}

func convertWSAccountRow(row wsAccountRow) (margintypes.AccountUpdate, error) {
	var out margintypes.AccountUpdate = margintypes.AccountUpdate{
		Coin:   row.Coin,
		Symbol: row.Symbol,
	}
	var err error
	if out.Available, err = bgcommon.ParseDecimalOrZero(string(row.Available)); err != nil {
		return margintypes.AccountUpdate{}, wrapWSParseErr("WatchAccount", "available", err)
	}
	if out.Frozen, err = bgcommon.ParseDecimalOrZero(string(row.Frozen)); err != nil {
		return margintypes.AccountUpdate{}, wrapWSParseErr("WatchAccount", "frozen", err)
	}
	if out.Borrow, err = bgcommon.ParseDecimalOrZero(string(row.Borrow)); err != nil {
		return margintypes.AccountUpdate{}, wrapWSParseErr("WatchAccount", "borrow", err)
	}
	if out.Interest, err = bgcommon.ParseDecimalOrZero(string(row.Interest)); err != nil {
		return margintypes.AccountUpdate{}, wrapWSParseErr("WatchAccount", "interest", err)
	}
	if out.Coupon, err = bgcommon.ParseDecimalOrZero(string(row.Coupon)); err != nil {
		return margintypes.AccountUpdate{}, wrapWSParseErr("WatchAccount", "coupon", err)
	}
	if out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(string(row.UTime)); err != nil {
		return margintypes.AccountUpdate{}, wrapWSParseErr("WatchAccount", "uTime", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------

func wrapWSParseErr(method, field string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "",
		"margin.Stream."+method+": parse "+field, cause)
}

// surfaceError logs to the SDK logger and forwards to errHandler when
// non-nil.
func (s *StreamClient) surfaceError(errHandler func(error), method, ctx string, err error) {
	s.c.logger().Debug("margin.Stream: "+method+" "+ctx, bitget.Err(err))
	if errHandler != nil {
		errHandler(err)
	}
}
