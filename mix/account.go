/*
FILE: mix/account.go

DESCRIPTION:
Account / position sub-client for Bitget MIX (legacy V2). Wires the
seven authenticated query / config endpoints used by the desk and by
the M3 reconciliation loop:

	GET  /api/v2/mix/account/accounts           — GetAccount
	GET  /api/v2/mix/position/single-position   — GetPosition
	GET  /api/v2/mix/order/orders-pending       — GetOpenOrders (paginated)
	GET  /api/v2/mix/order/detail               — GetOrderDetail
	POST /api/v2/mix/order/close-positions      — ClosePosition (market close)
	POST /api/v2/mix/account/set-leverage       — SetLeverage
	POST /api/v2/mix/account/set-position-mode  — SetPositionMode

PINNED SETTINGS:
Every request carries productType / marginMode / marginCoin from the
parent mix.Client (see mix/client.go). The desk pins them once in
NewClientWithSettings and never thinks about them again.

PAGINATION:
GetOpenOrders follows Bitget's V2 cursor model: the response carries
`endId`, the next request uses `idLessThan = endId`. The SDK pages
internally and returns the full list in a single call. There is a hard
ceiling (`bgcommon.OrdersMaxPages * bgcommon.OrdersPageLimit`) so a runaway
result set cannot wedge the goroutine — the SDK surfaces a typed error
when the ceiling is hit so the desk can decide whether to retry with a
narrower symbol filter or to swap to a streaming reconciliation.

ID-MAPPING:
Bitget echoes both `orderId` and `clientOid` on every account-side
order event (orders-pending, order/detail, WS "orders" channel), so
the SDK does NOT need to maintain an in-memory cache to translate
between the two. Callers that want such a cache (e.g. the desk
connector) keep it themselves — the SDK's job is to surface whatever
Bitget gave us. This mirrors mix/trading.go: the trading sub-client
also operates without a cache, propagating both ids through.

CLOSE-POSITION SCOPE:
Bitget V2 close-positions market-closes a position leg. In one-way
mode it closes the only leg; in hedge mode the wire requires
`holdSide`. v1.0 of the SDK targets one-way (the desk's only mode),
so ClosePosition takes only `symbol`. A future ClosePositionInLeg
helper can take an explicit holdSide for hedge users — kept out of
v1.0 to avoid premature API surface.
*/

package mix

import (
	"context"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// AccountClient — account / position sub-client. Built once per
// mix.Client (see client.go) and safe for concurrent use.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// ---------------------------------------------------------------------
// Pagination knobs.
// ---------------------------------------------------------------------
//
// The 100-orders-per-page / 10-page ceiling lives in
// internal/bgcommon (OrdersPageLimit / OrdersMaxPages) so spot/ and
// future uta/ pagination loops use the same caps. The mix profile
// consumes the constants via the bgcommon import; no profile-local
// duplicate is kept.

// ---------------------------------------------------------------------
// GetAccount — /api/v2/mix/account/accounts.
// ---------------------------------------------------------------------

// accountsRow mirrors one row of /api/v2/mix/account/accounts. Bitget
// returns ALL margin coins under a productType in a single call; the
// SDK filters by the pinned `marginCoin`.
//
// Field naming follows the live Bitget V2 wire format. Numeric fields
// arrive as JSON strings — the SDK normalises them via
// bgcommon.ParseDecimalOrZero so empty strings resolve to decimal.Zero rather
// than failing the parser.
type accountsRow struct {
	MarginCoin       string `json:"marginCoin"`
	Locked           string `json:"locked"`
	Available        string `json:"available"`
	CrossedMaxAvail  string `json:"crossedMaxAvailable"`
	IsolatedMaxAvail string `json:"isolatedMaxAvailable"`
	MaxTransferOut   string `json:"maxTransferOut"`
	AccountEquity    string `json:"accountEquity"`
	UsdtEquity       string `json:"usdtEquity"`
	BtcEquity        string `json:"btcEquity"`
	UnrealizedPL     string `json:"unrealizedPL"`
	CrossedRiskRate  string `json:"crossedRiskRate"`
	CrossedLeverage  string `json:"crossedMarginLeverage"`
	IsoLongLeverage  string `json:"isolatedLongLever"`
	IsoShortLeverage string `json:"isolatedShortLever"`
	MarginMode       string `json:"marginMode"`
	PosMode          string `json:"posMode"`
}

// GetAccount returns the account-level wallet snapshot for the pinned
// productType + marginCoin. The shared roottypes.Balance struct is
// flat (one CoinBalance), since Bitget MIX exposes a single coin per
// productType.
func (a *AccountClient) GetAccount(ctx context.Context) (roottypes.Balance, error) {
	var out roottypes.Balance

	var query url.Values = url.Values{}
	query.Set("productType", string(a.c.productType))

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/mix/account/accounts",
		Query:  query,
		Signed: true,
		Meta: rest.RequestMeta{
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return out, err
	}

	var rows []accountsRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.GetAccount: parse", err)
	}

	// Find the row matching the pinned marginCoin. If marginCoin is
	// empty (COIN-FUTURES default) the SDK takes the first row, since
	// callers asked for "the account I'm pinned to" and Bitget orders
	// rows by marginCoin alphabetically.
	var picked *accountsRow
	var i int
	for i = 0; i < len(rows); i++ {
		if a.c.marginCoin == "" || rows[i].MarginCoin == a.c.marginCoin {
			picked = &rows[i]
			break
		}
	}
	if picked == nil {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "",
			"mix.Account.GetAccount: marginCoin "+a.c.marginCoin+" not present under productType "+string(a.c.productType), nil)
	}

	return convertAccountRow(*picked)
}

// convertAccountRow maps the Bitget account row into roottypes.Balance.
// Errors short-circuit on the first malformed numeric — a single bad
// row should not silently corrupt downstream consumers.
func convertAccountRow(row accountsRow) (roottypes.Balance, error) {
	var out roottypes.Balance = roottypes.Balance{
		MarginCoin: row.MarginCoin,
	}

	var err error
	out.TotalEquity, err = bgcommon.ParseDecimalOrZero(row.AccountEquity)
	if err != nil {
		return roottypes.Balance{}, wrapAccountParseErr("accountEquity", err)
	}
	out.AvailableBalance, err = bgcommon.ParseDecimalOrZero(row.Available)
	if err != nil {
		return roottypes.Balance{}, wrapAccountParseErr("available", err)
	}
	out.LockedBalance, err = bgcommon.ParseDecimalOrZero(row.Locked)
	if err != nil {
		return roottypes.Balance{}, wrapAccountParseErr("locked", err)
	}
	out.UnrealizedPnL, err = bgcommon.ParseDecimalOrZero(row.UnrealizedPL)
	if err != nil {
		return roottypes.Balance{}, wrapAccountParseErr("unrealizedPL", err)
	}
	// Bitget MIX exposes maintenance margin via a separate position
	// query (/single-position field `keepMarginRate`). We leave the
	// account-level field zero — callers needing it should sum it from
	// GetPosition results.
	out.MaintenanceMargin = decimal.Zero

	var usdtEquity decimal.Decimal
	usdtEquity, err = bgcommon.ParseDecimalOrZero(row.UsdtEquity)
	if err != nil {
		return roottypes.Balance{}, wrapAccountParseErr("usdtEquity", err)
	}
	var btcEquity decimal.Decimal
	btcEquity, err = bgcommon.ParseDecimalOrZero(row.BtcEquity)
	if err != nil {
		return roottypes.Balance{}, wrapAccountParseErr("btcEquity", err)
	}

	// Frozen / CumRealizedPnL / UsdValue are not exposed on this
	// endpoint; left zero. Spot endpoints fill them when the SDK adds
	// spot in v2.0.
	out.Coins = []roottypes.CoinBalance{{
		Coin:          row.MarginCoin,
		Equity:        out.TotalEquity,
		Available:     out.AvailableBalance,
		Locked:        out.LockedBalance,
		UnrealizedPnL: out.UnrealizedPnL,
		UsdtEquity:    usdtEquity,
		BtcEquity:     btcEquity,
	}}
	return out, nil
}

func wrapAccountParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.GetAccount: parse "+field, err)
}

// ---------------------------------------------------------------------
// GetPosition — /api/v2/mix/position/single-position.
// ---------------------------------------------------------------------

// positionRow mirrors one row of /api/v2/mix/position/single-position.
type positionRow struct {
	Symbol           string `json:"symbol"`
	MarginCoin       string `json:"marginCoin"`
	HoldSide         string `json:"holdSide"`
	OpenDelegateSize string `json:"openDelegateSize"`
	MarginSize       string `json:"marginSize"`
	Available        string `json:"available"`
	Locked           string `json:"locked"`
	Total            string `json:"total"`
	Leverage         string `json:"leverage"`
	AchievedProfits  string `json:"achievedProfits"`
	OpenPriceAvg     string `json:"openPriceAvg"`
	MarginMode       string `json:"marginMode"`
	PosMode          string `json:"posMode"`
	UnrealizedPL     string `json:"unrealizedPL"`
	LiquidationPrice string `json:"liquidationPrice"`
	KeepMarginRate   string `json:"keepMarginRate"`
	MarkPrice        string `json:"markPrice"`
	MarginRatio      string `json:"marginRatio"`
	BreakEvenPrice   string `json:"breakEvenPrice"`
	TotalFee         string `json:"totalFee"`
	DeductedFee      string `json:"deductedFee"`
	CTime            string `json:"cTime"`
	UTime            string `json:"uTime"`
}

// GetPosition returns the open position for `symbol` under the pinned
// productType + marginCoin. In one-way mode there is at most one
// position per symbol; in hedge mode there may be one long and one
// short — the SDK returns the FIRST non-empty row (Bitget orders the
// list "long, short" deterministically).
//
// An empty symbol or no open position resolves to the zero
// PositionInfo + nil error. Network / auth failures surface as typed
// SDK errors.
func (a *AccountClient) GetPosition(ctx context.Context, symbol string) (mixtypes.PositionInfo, error) {
	var out mixtypes.PositionInfo
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account.GetPosition: symbol is empty", nil)
	}

	var query url.Values = url.Values{}
	query.Set("productType", string(a.c.productType))
	query.Set("symbol", symbol)
	if a.c.marginCoin != "" {
		query.Set("marginCoin", a.c.marginCoin)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/mix/position/single-position",
		Query:  query,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return out, err
	}

	var rows []positionRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.GetPosition: parse", err)
	}

	// Pick the first non-empty row. "Empty" means total ≤ 0 — Bitget
	// keeps a row with all-zero numerics for symbols the account has
	// touched but has no current exposure on, and we filter those.
	var i int
	for i = 0; i < len(rows); i++ {
		var total decimal.Decimal
		total, err = bgcommon.ParseDecimalOrZero(rows[i].Total)
		if err != nil {
			return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.GetPosition: parse total", err)
		}
		if total.IsPositive() {
			return convertPositionRow(rows[i])
		}
	}
	// No active position — clean zero PositionInfo with the symbol
	// echoed back, so callers can distinguish "no exposure" from "I
	// never asked".
	out.Symbol = symbol
	return out, nil
}

// convertPositionRow normalises one /single-position row.
func convertPositionRow(row positionRow) (mixtypes.PositionInfo, error) {
	var out mixtypes.PositionInfo = mixtypes.PositionInfo{
		Symbol:     row.Symbol,
		HoldSide:   mixtypes.HoldSide(row.HoldSide),
		MarginMode: roottypes.MarginMode(row.MarginMode),
		MarginCoin: row.MarginCoin,
	}

	var err error
	out.Quantity, err = bgcommon.ParseDecimalOrZero(row.Total)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("total", err)
	}
	out.Available, err = bgcommon.ParseDecimalOrZero(row.Available)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("available", err)
	}
	out.Locked, err = bgcommon.ParseDecimalOrZero(row.Locked)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("locked", err)
	}
	out.AvgOpenPrice, err = bgcommon.ParseDecimalOrZero(row.OpenPriceAvg)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("openPriceAvg", err)
	}
	out.MarkPrice, err = bgcommon.ParseDecimalOrZero(row.MarkPrice)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("markPrice", err)
	}
	out.LiquidationPrice, err = bgcommon.ParseDecimalOrZero(row.LiquidationPrice)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("liquidationPrice", err)
	}
	out.UnrealizedPnL, err = bgcommon.ParseDecimalOrZero(row.UnrealizedPL)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("unrealizedPL", err)
	}
	out.RealizedPnL, err = bgcommon.ParseDecimalOrZero(row.AchievedProfits)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("achievedProfits", err)
	}
	out.Leverage, err = bgcommon.ParseIntOrZero(row.Leverage)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("leverage", err)
	}
	out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(row.CTime)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("cTime", err)
	}
	out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(row.UTime)
	if err != nil {
		return mixtypes.PositionInfo{}, wrapPositionParseErr("uTime", err)
	}
	return out, nil
}

func wrapPositionParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.GetPosition: parse "+field, err)
}

// ---------------------------------------------------------------------
// GetOpenOrders / GetOrderDetail — order lookup endpoints.
// ---------------------------------------------------------------------

// orderRow mirrors one row of /orders-pending and /order/detail.
// Bitget returns identical schemas on both endpoints (with /detail
// returning a single-element list for a specific orderId).
type orderRow struct {
	Symbol        string `json:"symbol"`
	Size          string `json:"size"`
	OrderID       string `json:"orderId"`
	ClientOid     string `json:"clientOid"`
	BaseVolume    string `json:"baseVolume"`
	Fee           string `json:"fee"`
	Price         string `json:"price"`
	PriceAvg      string `json:"priceAvg"`
	State         string `json:"state"`
	Side          string `json:"side"`
	Force         string `json:"force"`
	TotalProfits  string `json:"totalProfits"`
	PosSide       string `json:"posSide"`
	MarginCoin    string `json:"marginCoin"`
	MarginMode    string `json:"marginMode"`
	TradeSide     string `json:"tradeSide"`
	Leverage      string `json:"leverage"`
	OrderType     string `json:"orderType"`
	CTime         string `json:"cTime"`
	UTime         string `json:"uTime"`
	ReduceOnly    string `json:"reduceOnly"`
}

// openOrdersResp wraps the cursor-paginated /orders-pending payload.
type openOrdersResp struct {
	EndID         string     `json:"endId"`
	EntrustedList []orderRow `json:"entrustedList"`
}

// GetOpenOrders returns every live order for the pinned productType,
// optionally filtered by `symbol`. Pagination is handled internally
// via Bitget's `idLessThan = endId` cursor model (the shared
// bgcommon.PaginateByCursor helper). The hard ceiling
// (`bgcommon.OrdersMaxPages * bgcommon.OrdersPageLimit`) prevents a
// buggy or adversarial cursor echo from looping forever; hitting it
// surfaces ErrorKindUnknown so the desk can react.
//
// An empty symbol returns ALL open orders under the productType — the
// desk uses that path for full-account reconciliation on startup.
func (a *AccountClient) GetOpenOrders(ctx context.Context, symbol string) ([]mixtypes.OrderInfo, error) {
	var rows []orderRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "mix.Account.GetOpenOrders",
		func(idLessThan string, limit int) ([]orderRow, string, error) {
			var query url.Values = url.Values{}
			query.Set("productType", string(a.c.productType))
			if symbol != "" {
				query.Set("symbol", symbol)
			}
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}

			var meta rest.RequestMeta = rest.RequestMeta{
				Category: string(bitget.RateLimitCategoryQuery),
			}
			if symbol != "" {
				meta.Symbols = []string{symbol}
			}

			var resp rest.Response
			var ferr error
			resp, _, ferr = a.c.rest().Do(ctx, rest.Options{
				Method: "GET",
				Path:   "/api/v2/mix/order/orders-pending",
				Query:  query,
				Signed: true,
				Meta:   meta,
			})
			if ferr != nil {
				return nil, "", ferr
			}

			var data openOrdersResp
			if ferr = resp.UnmarshalData(&data); ferr != nil {
				return nil, "", bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.GetOpenOrders: parse", ferr)
			}
			return data.EntrustedList, data.EndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []mixtypes.OrderInfo = make([]mixtypes.OrderInfo, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var info mixtypes.OrderInfo
		info, err = convertOrderRow(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// GetOrderDetail returns the lifecycle snapshot for one order. Either
// orderID OR clientOrderID must be set; passing both is allowed (Bitget
// gives orderID priority). An empty symbol surfaces
// ErrorKindInvalidRequest — Bitget V2 makes the symbol mandatory on
// this endpoint.
func (a *AccountClient) GetOrderDetail(ctx context.Context, symbol, orderID, clientOrderID string) (mixtypes.OrderInfo, error) {
	var out mixtypes.OrderInfo
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account.GetOrderDetail: symbol is empty", nil)
	}
	if orderID == "" && clientOrderID == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account.GetOrderDetail: either orderID or clientOrderID is required", nil)
	}

	var query url.Values = url.Values{}
	query.Set("productType", string(a.c.productType))
	query.Set("symbol", symbol)
	if orderID != "" {
		query.Set("orderId", orderID)
	} else {
		query.Set("clientOid", clientOrderID)
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/mix/order/detail",
		Query:  query,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return out, err
	}

	// /order/detail returns a single object (not a list). We unmarshal
	// directly into orderRow.
	var row orderRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.GetOrderDetail: parse", err)
	}
	if row.OrderID == "" && row.ClientOid == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "",
			"mix.Account.GetOrderDetail: order not found (symbol="+symbol+", orderID="+orderID+", clientOid="+clientOrderID+")", nil)
	}
	return convertOrderRow(row)
}

// convertOrderRow normalises one /orders-pending or /order/detail row
// into mixtypes.OrderInfo. Errors short-circuit on the first malformed
// numeric.
func convertOrderRow(row orderRow) (mixtypes.OrderInfo, error) {
	var out mixtypes.OrderInfo = mixtypes.OrderInfo{
		OrderID:       row.OrderID,
		ClientOrderID: row.ClientOid,
		Symbol:        row.Symbol,
		Side:          roottypes.SideType(row.Side),
		TradeSide:     roottypes.TradeSide(row.TradeSide),
		HoldSide:      mixtypes.HoldSide(row.PosSide),
		OrderType:     roottypes.OrderType(row.OrderType),
		TimeInForce:   roottypes.TimeInForceType(row.Force),
		Status:        roottypes.OrderStatus(row.State),
	}

	var err error
	out.Quantity, err = bgcommon.ParseDecimalOrZero(row.Size)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapOrderParseErr("size", err)
	}
	out.Price, err = bgcommon.ParseDecimalOrZero(row.Price)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapOrderParseErr("price", err)
	}
	out.FilledQuantity, err = bgcommon.ParseDecimalOrZero(row.BaseVolume)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapOrderParseErr("baseVolume", err)
	}
	out.AvgFilledPrice, err = bgcommon.ParseDecimalOrZero(row.PriceAvg)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapOrderParseErr("priceAvg", err)
	}
	out.CumFee, err = bgcommon.ParseDecimalOrZero(row.Fee)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapOrderParseErr("fee", err)
	}
	out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(row.CTime)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapOrderParseErr("cTime", err)
	}
	out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(row.UTime)
	if err != nil {
		return mixtypes.OrderInfo{}, wrapOrderParseErr("uTime", err)
	}
	return out, nil
}

func wrapOrderParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account: parse "+field, err)
}

// ---------------------------------------------------------------------
// ClosePosition — POST /api/v2/mix/order/close-positions.
// ---------------------------------------------------------------------

// closePositionsBody mirrors the JSON body of close-positions.
// HoldSide is OMITTED for one-way mode (the SDK's v1.0 target) so
// Bitget closes whichever leg the symbol currently holds.
type closePositionsBody struct {
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
	HoldSide    string `json:"holdSide,omitempty"`
}

// ClosePosition market-closes the open position on `symbol` under the
// pinned productType. Designed for one-way mode — passes no holdSide,
// letting Bitget close whichever leg currently exists.
//
// Returns nil on accept, typed SDK error otherwise. Bitget responds
// with a per-row success/failure list internally, but the SDK collapses
// it: any per-row failure surfaces as ErrorKindExchange so the desk
// sees a single boolean outcome.
func (a *AccountClient) ClosePosition(ctx context.Context, symbol string) error {
	if symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account.ClosePosition: symbol is empty", nil)
	}

	var body closePositionsBody = closePositionsBody{
		Symbol:      symbol,
		ProductType: string(a.c.productType),
	}
	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/close-positions",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{symbol},
			OrderCount: 1,
			Category:   string(bitget.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return err
	}

	// close-positions returns a {successList, failureList} envelope —
	// reusing the batch shape from trading.go. Any failure row surfaces
	// as a typed exchange error.
	var data bgcommon.BatchEnvelope
	if err = resp.UnmarshalData(&data); err != nil {
		return bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Account.ClosePosition: parse", err)
	}
	if len(data.FailureList) > 0 {
		var f bgcommon.BatchFailureRow = data.FailureList[0]
		return bitget.NewError(bitget.ErrorKindExchange, f.ErrorCode,
			"mix.Account.ClosePosition: "+f.ErrorMsg, nil)
	}
	return nil
}

// ---------------------------------------------------------------------
// SetLeverage — POST /api/v2/mix/account/set-leverage.
// ---------------------------------------------------------------------

// setLeverageBody mirrors the JSON body of set-leverage. HoldSide is
// OMITTED in one-way mode (SDK v1.0 target).
type setLeverageBody struct {
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
	MarginCoin  string `json:"marginCoin"`
	Leverage    string `json:"leverage"`
	HoldSide    string `json:"holdSide,omitempty"`
}

// SetLeverage updates leverage for `symbol` under the pinned
// productType + marginCoin. v1.0 supports one-way mode only (no
// holdSide on the wire).
//
// Bitget rejects leverage <= 0 or > MaxLever from /contracts; the SDK
// only validates leverage > 0 client-side and lets Bitget enforce the
// per-symbol cap. Idempotent when the requested leverage equals the
// current setting (Bitget returns 22002 "leverage not modified" — the
// SDK treats this as a typed InvalidRequest, so callers wanting an
// idempotent code-path should suppress that code via bitget.IsInvalidRequest).
func (a *AccountClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	if symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account.SetLeverage: symbol is empty", nil)
	}
	if leverage <= 0 {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account.SetLeverage: leverage must be positive, got "+strconv.Itoa(leverage), nil)
	}

	var body setLeverageBody = setLeverageBody{
		Symbol:      symbol,
		ProductType: string(a.c.productType),
		MarginCoin:  a.c.marginCoin,
		Leverage:    strconv.Itoa(leverage),
	}
	var _, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/account/set-leverage",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	return err
}

// ---------------------------------------------------------------------
// SetPositionMode — POST /api/v2/mix/account/set-position-mode.
// ---------------------------------------------------------------------

// setPositionModeBody mirrors the JSON body of set-position-mode.
// posMode is account-global; productType selects which contract family
// is reconfigured.
type setPositionModeBody struct {
	ProductType string `json:"productType"`
	PosMode     string `json:"posMode"`
}

// SetPositionMode flips the account-global position mode (one-way vs.
// hedge) for the pinned productType. Bitget rejects the call if there
// are any open positions or open orders on the productType — surfaces
// as ErrorKindExchange with the venue's code.
func (a *AccountClient) SetPositionMode(ctx context.Context, mode roottypes.PositionMode) error {
	switch mode {
	case roottypes.PositionModeOneWay, roottypes.PositionModeHedge:
		// ok
	default:
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Account.SetPositionMode: unknown mode "+string(mode), nil)
	}

	var body setPositionModeBody = setPositionModeBody{
		ProductType: string(a.c.productType),
		PosMode:     string(mode),
	}
	var _, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/account/set-position-mode",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	return err
}
