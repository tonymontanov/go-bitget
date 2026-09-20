/*
FILE: uta/stream-private.go

DESCRIPTION:
Private WebSocket topics of the Bitget V3 Unified Trading Account:

	WatchOrders     → "order"     (order lifecycle: new / fills / cancel)
	WatchFills      → "fill"      (one row per execution)
	WatchPositions  → "position"  (futures positions)
	WatchAccount    → "account"   (unified account equity + per-coin assets)

WIRE SHAPE:

	{"op":"subscribe","args":[{"instType":"UTA","topic":"order"}]}

instType is the literal "UTA" and there is NO symbol: each topic is
ACCOUNT-WIDE and delivers every category / symbol of the account. The SDK
forwards every row; the consumer filters by Order.Category / Symbol. (V2
needed instId="default" plus a client-side filter; V3 has no per-symbol
private subscription at all.)

VENUE PUSH SEMANTICS (docs):

  - order, fill     — nothing on subscribe; one push per event.
  - position        — a full snapshot on subscribe, then incremental rows
                      for the positions that changed. A closed position
                      arrives with size 0 / positionStatus "ended".
  - account         — a full snapshot on subscribe, then a push on every
                      balance change.

A handler attached to an already-live topic (second WatchPositions call)
does NOT get the initial snapshot again — seed it over REST
(Position.GetCurrentPositions / Account.GetAssets). After a reconnect the
SDK resubscribes, which on the new connection is again a first-time
subscription, so the position / account snapshot is EXPECTED to be sent
again (unverified); order / fill events missed while the socket was down
are never replayed — re-seed over REST from OnPrivateReconnect.

VERIFICATION STATUS:
These decoders are written from the venue DOCS only (no UTA key was
available for a live capture; the public topics were verified live). They
are therefore deliberately tolerant:

  - every scalar is decoded through wsText, which accepts a quoted string,
    a bare number, a bool or null (the V2 private channels shipped numbers
    where the docs promised strings — see bgcommon.FlexString);
  - `feeDetail` is kept raw and accepted as an array of objects, a single
    object, a JSON string holding either, "" or null (the v2.5.1 incident:
    a string-typed feeDetail killed the whole V2 orders channel). An
    unreadable feeDetail never drops the row;
  - where the WS docs and the REST shape spell a field differently
    (`size`/`total`, `liqPrice`/`liquidationPrice`, `holdSide`/`posSide`,
    `totalEquity`/`accountEquity`, `debts`/`debt`, ...) BOTH spellings are
    read, the WS one first.

A row whose NUMERIC field is non-empty garbage is rejected (one error to
the errHandlers) rather than delivered with a silent zero.

LOGIN: identical to V2 (seconds timestamp) — see StreamClient.ensureConn.
*/

package uta

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
	"github.com/tonymontanov/go-bitget/v2/internal/ws"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"
)

// instTypeUTA is the only instType accepted by the V3 private topics.
const instTypeUTA = "UTA"

// Private topic names.
const (
	topicOrder    = "order"
	topicFill     = "fill"
	topicPosition = "position"
	topicAccount  = "account"
)

// Per-topic wire subscriptions. The private topics need no decode scratch
// (low rate, reflective decode), so they are bare fan-outs.
type orderSub struct {
	wireSub[utatypes.Order]
}

type fillSub struct {
	wireSub[utatypes.Fill]
}

type positionSub struct {
	wireSub[utatypes.CurrentPosition]
}

type accountSub struct {
	wireSub[utatypes.AccountAssets]
}

// ---------------------------------------------------------------------
// Tolerant scalar.
// ---------------------------------------------------------------------

// wsText is a JSON scalar decoded into its textual form whatever its wire
// type: "abc" → abc, 12.5 → 12.5, true → true, null → "". Objects and
// arrays are kept as raw JSON text. It never fails, so a type surprise on
// one field cannot take down a whole private frame.
type wsText string

// UnmarshalJSON implements json.Unmarshaler (honoured by jsoniter).
func (t *wsText) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*t = ""
		return nil
	}
	if data[0] == '"' {
		var unquoted string
		if err := codec.Unmarshal(data, &unquoted); err != nil {
			// Malformed escape — keep the raw text rather than fail.
			*t = wsText(strings.Trim(string(data), `"`))
			return nil
		}
		*t = wsText(unquoted)
		return nil
	}
	*t = wsText(string(data))
	return nil
}

// firstText returns the first non-empty value — used for fields the WS docs
// and the REST shape spell differently.
func firstText(values ...wsText) string {
	var i int
	for i = 0; i < len(values); i++ {
		if values[i] != "" {
			return string(values[i])
		}
	}
	return ""
}

// isYes interprets the venue's yes / no flags (and a bool, tolerated).
func isYes(v wsText) bool {
	return v == "yes" || v == "true"
}

// upperCategory normalises the lower-case WS category ("usdt-futures") to
// the canonical REST / enum form ("USDT-FUTURES").
func upperCategory(v wsText) string {
	return strings.ToUpper(string(v))
}

// ---------------------------------------------------------------------
// feeDetail.
// ---------------------------------------------------------------------

type wsFeeRow struct {
	FeeCoin wsText `json:"feeCoin"`
	Fee     wsText `json:"fee"`
}

// parseWSFeeDetail reads feeDetail in every shape the venue is known to
// use: an array of objects, a single object, a JSON string wrapping
// either, "", or null. It is BEST EFFORT by design — anything unreadable
// yields nil instead of an error, so a fee-format quirk can never drop
// an order or fill event.
func parseWSFeeDetail(raw codec.RawJSON) []utatypes.FeeDetail {
	var text []byte = []byte(raw)
	if raw.IsNull() {
		return nil
	}
	if text[0] == '"' {
		var inner string
		if err := codec.Unmarshal(text, &inner); err != nil {
			return nil
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return nil
		}
		text = []byte(inner)
	}
	var rows []wsFeeRow
	switch text[0] {
	case '[':
		if err := codec.Unmarshal(text, &rows); err != nil {
			return nil
		}
	case '{':
		var single wsFeeRow
		if err := codec.Unmarshal(text, &single); err != nil {
			return nil
		}
		rows = []wsFeeRow{single}
	default:
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	var out []utatypes.FeeDetail = make([]utatypes.FeeDetail, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var fee decimal.Decimal
		var err error
		fee, err = bgcommon.ParseDecimalOrZero(string(rows[i].Fee))
		if err != nil {
			continue
		}
		out = append(out, utatypes.FeeDetail{FeeCoin: string(rows[i].FeeCoin), Fee: fee})
	}
	return out
}

// textDecPair — one numeric wsText field and its destination.
type textDecPair struct {
	dst *decimal.Decimal
	raw string
}

// decTexts parses every pair; "" → zero, non-empty garbage → a scoped
// parse error (the row is rejected).
func decTexts(scope string, pairs []textDecPair) error {
	var i int
	for i = 0; i < len(pairs); i++ {
		var err error
		*pairs[i].dst, err = bgcommon.ParseDecimalOrZero(pairs[i].raw)
		if err != nil {
			return errParse(scope, err)
		}
	}
	return nil
}

// textMs parses an epoch-ms field; unreadable → 0 (timestamps are
// informational and must not reject a row).
func textMs(v wsText) int64 {
	return i64(string(v))
}

// ---------------------------------------------------------------------
// Shared attach for the four account-wide topics.
// ---------------------------------------------------------------------

// privateArg builds the wire arg of an account-wide private topic.
func privateArg(topic string) ws.SubscriptionArg {
	return ws.SubscriptionArg{InstType: instTypeUTA, Topic: topic}
}

// ---------------------------------------------------------------------
// WatchOrders.
// ---------------------------------------------------------------------

// WatchOrders subscribes to the account-wide `order` topic. handler is
// invoked once per order row — EVERY category and symbol of the account;
// filter by Order.Category (canonical upper-case) / Order.Symbol.
// OrderStatus is passed through verbatim (new | partially_filled | filled
// | cancelled). Nothing is pushed on subscribe.
//
// Any number of WatchOrders calls share one wire subscription; each
// handler detaches with its own ctx (nil ctx → until Close). Without API
// credentials the call fails with ErrorKindAuth before dialling.
func (s *StreamClient) WatchOrders(
	ctx context.Context,
	handler func(utatypes.Order),
	errHandler func(error),
) error {
	const scope string = "Stream.WatchOrders"
	if handler == nil {
		return errInvalid(scope, "handler is nil")
	}
	var conn *ws.Conn
	var err error
	conn, err = s.ensureConn(&s.private, true, scope)
	if err != nil {
		return err
	}
	var arg ws.SubscriptionArg = privateArg(topicOrder)
	return attachHandler(s, &s.private, conn, s.orders, arg, scope,
		func() *orderSub {
			var o *orderSub = &orderSub{}
			o.arg = arg
			o.scope = scope
			o.sub = &ws.Subscription{
				Arg: arg,
				Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
					s.handleOrdersFrame(o, payload)
				},
			}
			return o
		},
		ctx, handler, errHandler, nil, nil)
}

// wsOrderRow mirrors one element of the `order` data array (V3 docs).
// `posSide` and `execType` are the REST spellings, read as fallbacks.
// `requestId` (a number echoed for rejected modify requests) is not
// consumed.
type wsOrderRow struct {
	Category     wsText        `json:"category"`
	Symbol       wsText        `json:"symbol"`
	OrderID      wsText        `json:"orderId"`
	ClientOID    wsText        `json:"clientOid"`
	Price        wsText        `json:"price"`
	Qty          wsText        `json:"qty"`
	Amount       wsText        `json:"amount"`
	HoldMode     wsText        `json:"holdMode"`
	HoldSide     wsText        `json:"holdSide"`
	PosSide      wsText        `json:"posSide"`
	DelegateType wsText        `json:"delegateType"`
	TradeSide    wsText        `json:"tradeSide"`
	OrderType    wsText        `json:"orderType"`
	TimeInForce  wsText        `json:"timeInForce"`
	Side         wsText        `json:"side"`
	MarginMode   wsText        `json:"marginMode"`
	MarginCoin   wsText        `json:"marginCoin"`
	ReduceOnly   wsText        `json:"reduceOnly"`
	CumExecQty   wsText        `json:"cumExecQty"`
	CumExecValue wsText        `json:"cumExecValue"`
	AvgPrice     wsText        `json:"avgPrice"`
	TotalProfit  wsText        `json:"totalProfit"`
	OrderStatus  wsText        `json:"orderStatus"`
	CancelReason wsText        `json:"cancelReason"`
	Leverage     wsText        `json:"leverage"`
	FeeDetail    codec.RawJSON `json:"feeDetail"`
	ExecType     wsText        `json:"execType"`
	StpMode      wsText        `json:"stpMode"`
	CreatedTime  wsText        `json:"createdTime"`
	UpdatedTime  wsText        `json:"updatedTime"`
}

func (s *StreamClient) handleOrdersFrame(o *orderSub, payload []byte) {
	if len(payload) == 0 {
		return
	}
	var rows []wsOrderRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		surfaceError(s, &o.wireSub, "decode order frame", errParse(o.scope, err))
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		var order utatypes.Order
		var err error
		order, err = convertWSOrderRow(o.scope, &rows[i])
		if err != nil {
			surfaceError(s, &o.wireSub, "parse order row", err)
			continue
		}
		o.deliver(order)
	}
}

func convertWSOrderRow(scope string, r *wsOrderRow) (utatypes.Order, error) {
	var o utatypes.Order = utatypes.Order{
		OrderID:      string(r.OrderID),
		ClientOID:    string(r.ClientOID),
		Category:     upperCategory(r.Category),
		Symbol:       string(r.Symbol),
		OrderType:    string(r.OrderType),
		Side:         string(r.Side),
		TimeInForce:  string(r.TimeInForce),
		OrderStatus:  string(r.OrderStatus),
		PosSide:      firstText(r.HoldSide, r.PosSide),
		HoldMode:     string(r.HoldMode),
		DelegateType: string(r.DelegateType),
		ReduceOnly:   string(r.ReduceOnly),
		CancelReason: string(r.CancelReason),
		ExecType:     string(r.ExecType),
		StpMode:      string(r.StpMode),
		TradeSide:    string(r.TradeSide),
		MarginMode:   string(r.MarginMode),
		MarginCoin:   string(r.MarginCoin),
		CreatedTime:  textMs(r.CreatedTime),
		UpdatedTime:  textMs(r.UpdatedTime),
		FeeDetail:    parseWSFeeDetail(r.FeeDetail),
	}
	if err := decTexts(scope, []textDecPair{
		{&o.Price, string(r.Price)}, {&o.Qty, string(r.Qty)}, {&o.Amount, string(r.Amount)},
		{&o.CumExecQty, string(r.CumExecQty)}, {&o.CumExecValue, string(r.CumExecValue)},
		{&o.AvgPrice, string(r.AvgPrice)}, {&o.Leverage, string(r.Leverage)},
		{&o.TotalProfit, string(r.TotalProfit)},
	}); err != nil {
		return utatypes.Order{}, err
	}
	return o, nil
}

// ---------------------------------------------------------------------
// WatchFills.
// ---------------------------------------------------------------------

// WatchFills subscribes to the account-wide `fill` topic. handler is
// invoked once per execution row of ANY category / symbol; filter by
// Fill.Category (canonical upper-case) / Fill.Symbol. Nothing is pushed
// on subscribe. Same sharing / ctx / auth contract as WatchOrders.
func (s *StreamClient) WatchFills(
	ctx context.Context,
	handler func(utatypes.Fill),
	errHandler func(error),
) error {
	const scope string = "Stream.WatchFills"
	if handler == nil {
		return errInvalid(scope, "handler is nil")
	}
	var conn *ws.Conn
	var err error
	conn, err = s.ensureConn(&s.private, true, scope)
	if err != nil {
		return err
	}
	var arg ws.SubscriptionArg = privateArg(topicFill)
	return attachHandler(s, &s.private, conn, s.fills, arg, scope,
		func() *fillSub {
			var f *fillSub = &fillSub{}
			f.arg = arg
			f.scope = scope
			f.sub = &ws.Subscription{
				Arg: arg,
				Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
					s.handleFillsFrame(f, payload)
				},
			}
			return f
		},
		ctx, handler, errHandler, nil, nil)
}

// wsFillRow mirrors one element of the `fill` data array (V3 docs).
// `posSide` / `createdTime` are the REST spellings, read as fallbacks.
type wsFillRow struct {
	Category    wsText        `json:"category"`
	Symbol      wsText        `json:"symbol"`
	OrderID     wsText        `json:"orderId"`
	ClientOID   wsText        `json:"clientOid"`
	ExecID      wsText        `json:"execId"`
	ExecLinkID  wsText        `json:"execLinkId"`
	OrderType   wsText        `json:"orderType"`
	Side        wsText        `json:"side"`
	HoldSide    wsText        `json:"holdSide"`
	PosSide     wsText        `json:"posSide"`
	TradeSide   wsText        `json:"tradeSide"`
	ExecPrice   wsText        `json:"execPrice"`
	ExecQty     wsText        `json:"execQty"`
	ExecValue   wsText        `json:"execValue"`
	ExecPnl     wsText        `json:"execPnl"`
	TradeScope  wsText        `json:"tradeScope"`
	FeeDetail   codec.RawJSON `json:"feeDetail"`
	ExecTime    wsText        `json:"execTime"`
	CreatedTime wsText        `json:"createdTime"`
	UpdatedTime wsText        `json:"updatedTime"`
	IsRPI       wsText        `json:"isRPI"`
}

func (s *StreamClient) handleFillsFrame(f *fillSub, payload []byte) {
	if len(payload) == 0 {
		return
	}
	var rows []wsFillRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		surfaceError(s, &f.wireSub, "decode fill frame", errParse(f.scope, err))
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		var fill utatypes.Fill
		var err error
		fill, err = convertWSFillRow(f.scope, &rows[i])
		if err != nil {
			surfaceError(s, &f.wireSub, "parse fill row", err)
			continue
		}
		f.deliver(fill)
	}
}

func convertWSFillRow(scope string, r *wsFillRow) (utatypes.Fill, error) {
	var f utatypes.Fill = utatypes.Fill{
		ExecID:      string(r.ExecID),
		OrderID:     string(r.OrderID),
		Category:    upperCategory(r.Category),
		Symbol:      string(r.Symbol),
		OrderType:   string(r.OrderType),
		Side:        string(r.Side),
		TradeScope:  string(r.TradeScope),
		TradeSide:   string(r.TradeSide),
		UpdatedTime: textMs(r.UpdatedTime),
		ClientOID:   string(r.ClientOID),
		ExecTime:    textMs(r.ExecTime),
		PosSide:     firstText(r.HoldSide, r.PosSide),
		IsRPI:       isYes(r.IsRPI),
		ExecLinkID:  string(r.ExecLinkID),
		FeeDetail:   parseWSFeeDetail(r.FeeDetail),
	}
	// The WS row has no createdTime: keep the REST-shaped field usable.
	f.CreatedTime = textMs(r.CreatedTime)
	if f.CreatedTime == 0 {
		f.CreatedTime = f.ExecTime
	}
	if err := decTexts(scope, []textDecPair{
		{&f.ExecPrice, string(r.ExecPrice)}, {&f.ExecQty, string(r.ExecQty)},
		{&f.ExecValue, string(r.ExecValue)}, {&f.ExecPnl, string(r.ExecPnl)},
	}); err != nil {
		return utatypes.Fill{}, err
	}
	return f, nil
}

// ---------------------------------------------------------------------
// WatchPositions.
// ---------------------------------------------------------------------

// WatchPositions subscribes to the account-wide `position` topic. handler
// is invoked once per position row: the first push after the wire
// subscribe is a snapshot of every position, later pushes carry only the
// positions that changed (a closed one arrives with Total = 0 and
// PositionStatus "ended").
//
// The WS row carries NO category (venue docs) — CurrentPosition.Category
// is empty; match by Symbol (+ PosSide in hedge mode). Same sharing / ctx
// / auth contract as WatchOrders; a handler joining a live subscription
// misses the initial snapshot — seed it from Position.GetCurrentPositions.
func (s *StreamClient) WatchPositions(
	ctx context.Context,
	handler func(utatypes.CurrentPosition),
	errHandler func(error),
) error {
	const scope string = "Stream.WatchPositions"
	if handler == nil {
		return errInvalid(scope, "handler is nil")
	}
	var conn *ws.Conn
	var err error
	conn, err = s.ensureConn(&s.private, true, scope)
	if err != nil {
		return err
	}
	var arg ws.SubscriptionArg = privateArg(topicPosition)
	return attachHandler(s, &s.private, conn, s.positions, arg, scope,
		func() *positionSub {
			var p *positionSub = &positionSub{}
			p.arg = arg
			p.scope = scope
			p.sub = &ws.Subscription{
				Arg: arg,
				Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
					s.handlePositionsFrame(p, payload)
				},
			}
			return p
		},
		ctx, handler, errHandler, nil, nil)
}

// wsPositionRow mirrors one element of the `position` data array (V3
// docs). WS-vs-REST spelling pairs, WS first: size / total, liqPrice /
// liquidationPrice, totalFundingFee / totalFunding. `category` and
// `positionBalance` are REST-only and read in case the venue aligns the
// two shapes.
type wsPositionRow struct {
	Category         wsText `json:"category"`
	Symbol           wsText `json:"symbol"`
	MarginCoin       wsText `json:"marginCoin"`
	HoldMode         wsText `json:"holdMode"`
	PosSide          wsText `json:"posSide"`
	HoldSide         wsText `json:"holdSide"`
	MarginMode       wsText `json:"marginMode"`
	PositionStatus   wsText `json:"positionStatus"`
	MarginSize       wsText `json:"marginSize"`
	PositionBalance  wsText `json:"positionBalance"`
	Size             wsText `json:"size"`
	Total            wsText `json:"total"`
	Available        wsText `json:"available"`
	Frozen           wsText `json:"frozen"`
	Leverage         wsText `json:"leverage"`
	CurRealisedPnl   wsText `json:"curRealisedPnl"`
	AvgPrice         wsText `json:"avgPrice"`
	UnrealisedPnl    wsText `json:"unrealisedPnl"`
	LiqPrice         wsText `json:"liqPrice"`
	LiquidationPrice wsText `json:"liquidationPrice"`
	MMR              wsText `json:"mmr"`
	ProfitRate       wsText `json:"profitRate"`
	MarkPrice        wsText `json:"markPrice"`
	BreakEvenPrice   wsText `json:"breakEvenPrice"`
	TotalFundingFee  wsText `json:"totalFundingFee"`
	TotalFunding     wsText `json:"totalFunding"`
	OpenFeeTotal     wsText `json:"openFeeTotal"`
	CloseFeeTotal    wsText `json:"closeFeeTotal"`
	CreatedTime      wsText `json:"createdTime"`
	UpdatedTime      wsText `json:"updatedTime"`
}

func (s *StreamClient) handlePositionsFrame(p *positionSub, payload []byte) {
	if len(payload) == 0 {
		return
	}
	var rows []wsPositionRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		surfaceError(s, &p.wireSub, "decode position frame", errParse(p.scope, err))
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		var pos utatypes.CurrentPosition
		var err error
		pos, err = convertWSPositionRow(p.scope, &rows[i])
		if err != nil {
			surfaceError(s, &p.wireSub, "parse position row", err)
			continue
		}
		p.deliver(pos)
	}
}

func convertWSPositionRow(scope string, r *wsPositionRow) (utatypes.CurrentPosition, error) {
	var pos utatypes.CurrentPosition = utatypes.CurrentPosition{
		Category:       upperCategory(r.Category),
		Symbol:         string(r.Symbol),
		MarginCoin:     string(r.MarginCoin),
		HoldMode:       string(r.HoldMode),
		PosSide:        firstText(r.PosSide, r.HoldSide),
		MarginMode:     string(r.MarginMode),
		PositionStatus: string(r.PositionStatus),
		CreatedTime:    textMs(r.CreatedTime),
		UpdatedTime:    textMs(r.UpdatedTime),
	}
	if err := decTexts(scope, []textDecPair{
		{&pos.PositionBalance, string(r.PositionBalance)}, {&pos.MarginSize, string(r.MarginSize)},
		{&pos.Available, string(r.Available)}, {&pos.Frozen, string(r.Frozen)},
		{&pos.Total, firstText(r.Size, r.Total)}, {&pos.Leverage, string(r.Leverage)},
		{&pos.CurRealisedPnl, string(r.CurRealisedPnl)}, {&pos.AvgPrice, string(r.AvgPrice)},
		{&pos.UnrealisedPnl, string(r.UnrealisedPnl)},
		{&pos.LiquidationPrice, firstText(r.LiqPrice, r.LiquidationPrice)},
		{&pos.MMR, string(r.MMR)}, {&pos.ProfitRate, string(r.ProfitRate)},
		{&pos.MarkPrice, string(r.MarkPrice)}, {&pos.BreakEvenPrice, string(r.BreakEvenPrice)},
		{&pos.TotalFunding, firstText(r.TotalFundingFee, r.TotalFunding)},
		{&pos.OpenFeeTotal, string(r.OpenFeeTotal)}, {&pos.CloseFeeTotal, string(r.CloseFeeTotal)},
	}); err != nil {
		return utatypes.CurrentPosition{}, err
	}
	return pos, nil
}

// ---------------------------------------------------------------------
// WatchAccount.
// ---------------------------------------------------------------------

// WatchAccount subscribes to the account-wide `account` topic. handler is
// invoked once per data row (the venue sends one): the unified-account
// equity overview plus the per-coin list. The first push after the wire
// subscribe is a full snapshot; later pushes follow balance changes
// (fills, settlement, transfers).
//
// The venue marks pushes as snapshot (full) or update (incremental) and the
// docs do not say whether an update lists every coin or only the changed
// ones — MERGE Assets by Coin instead of treating one push as the complete
// coin list.
//
// Mapping: totalEquity → AccountEquity, coin[] → Assets (debts → Debt;
// borrow / bonus → AccountAsset.Borrow / Bonus). The USDT / BTC equity
// and per-currency PnL fields of AccountAssets are REST-only and stay
// zero. Same sharing / ctx / auth contract as WatchOrders; a handler
// joining a live subscription misses the initial snapshot — seed it from
// Account.GetAssets.
func (s *StreamClient) WatchAccount(
	ctx context.Context,
	handler func(utatypes.AccountAssets),
	errHandler func(error),
) error {
	const scope string = "Stream.WatchAccount"
	if handler == nil {
		return errInvalid(scope, "handler is nil")
	}
	var conn *ws.Conn
	var err error
	conn, err = s.ensureConn(&s.private, true, scope)
	if err != nil {
		return err
	}
	var arg ws.SubscriptionArg = privateArg(topicAccount)
	return attachHandler(s, &s.private, conn, s.accounts, arg, scope,
		func() *accountSub {
			var a *accountSub = &accountSub{}
			a.arg = arg
			a.scope = scope
			a.sub = &ws.Subscription{
				Arg: arg,
				Handler: func(_ ws.SubscriptionArg, _ string, payload []byte, _ int64, _ int64) {
					s.handleAccountFrame(a, payload)
				},
			}
			return a
		},
		ctx, handler, errHandler, nil, nil)
}

// wsAccountCoinRow mirrors one element of the account `coin` list.
// `debt` is the REST spelling of the WS `debts`.
type wsAccountCoinRow struct {
	Coin      wsText `json:"coin"`
	Equity    wsText `json:"equity"`
	USDValue  wsText `json:"usdValue"`
	Balance   wsText `json:"balance"`
	Available wsText `json:"available"`
	Debts     wsText `json:"debts"`
	Debt      wsText `json:"debt"`
	Locked    wsText `json:"locked"`
	Borrow    wsText `json:"borrow"`
	Bonus     wsText `json:"bonus"`
}

// wsAccountRow mirrors one element of the `account` data array (V3 docs).
// `accountEquity` / `assets` are the REST spellings of `totalEquity` /
// `coin`, read as fallbacks. The WS spells the PnL field `unrealisedPnL`;
// the decoder matches keys case-insensitively, so the REST `unrealisedPnl`
// lands in the same field.
type wsAccountRow struct {
	TotalEquity      wsText             `json:"totalEquity"`
	AccountEquity    wsText             `json:"accountEquity"`
	UnrealisedPnl    wsText             `json:"unrealisedPnL"`
	EffEquity        wsText             `json:"effEquity"`
	MMR              wsText             `json:"mmr"`
	IMR              wsText             `json:"imr"`
	MgnRatio         wsText             `json:"mgnRatio"`
	PositionMgnRatio wsText             `json:"positionMgnRatio"`
	Coins            []wsAccountCoinRow `json:"coin"`
	Assets           []wsAccountCoinRow `json:"assets"`
}

func (s *StreamClient) handleAccountFrame(a *accountSub, payload []byte) {
	if len(payload) == 0 {
		return
	}
	var rows []wsAccountRow
	if err := codec.Unmarshal(payload, &rows); err != nil {
		surfaceError(s, &a.wireSub, "decode account frame", errParse(a.scope, err))
		return
	}
	var i int
	for i = 0; i < len(rows); i++ {
		var assets utatypes.AccountAssets
		var err error
		assets, err = convertWSAccountRow(a.scope, &rows[i])
		if err != nil {
			surfaceError(s, &a.wireSub, "parse account row", err)
			continue
		}
		a.deliver(assets)
	}
}

func convertWSAccountRow(scope string, r *wsAccountRow) (utatypes.AccountAssets, error) {
	var out utatypes.AccountAssets
	if err := decTexts(scope, []textDecPair{
		{&out.AccountEquity, firstText(r.TotalEquity, r.AccountEquity)},
		{&out.UnrealisedPnl, string(r.UnrealisedPnl)}, {&out.EffEquity, string(r.EffEquity)},
		{&out.MMR, string(r.MMR)}, {&out.IMR, string(r.IMR)},
		{&out.MgnRatio, string(r.MgnRatio)}, {&out.PositionMgnRatio, string(r.PositionMgnRatio)},
	}); err != nil {
		return utatypes.AccountAssets{}, err
	}
	var coins []wsAccountCoinRow = r.Coins
	if len(coins) == 0 {
		coins = r.Assets
	}
	out.Assets = make([]utatypes.AccountAsset, 0, len(coins))
	var i int
	for i = 0; i < len(coins); i++ {
		var asset utatypes.AccountAsset = utatypes.AccountAsset{Coin: string(coins[i].Coin)}
		if err := decTexts(scope, []textDecPair{
			{&asset.Equity, string(coins[i].Equity)}, {&asset.USDValue, string(coins[i].USDValue)},
			{&asset.Balance, string(coins[i].Balance)}, {&asset.Available, string(coins[i].Available)},
			{&asset.Debt, firstText(coins[i].Debts, coins[i].Debt)}, {&asset.Locked, string(coins[i].Locked)},
			{&asset.Borrow, string(coins[i].Borrow)}, {&asset.Bonus, string(coins[i].Bonus)},
		}); err != nil {
			return utatypes.AccountAssets{}, err
		}
		out.Assets = append(out.Assets, asset)
	}
	return out, nil
}
