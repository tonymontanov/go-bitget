/*
FILE: spot/account.go

DESCRIPTION:
Account / history sub-client for Bitget V2 SPOT. Wires the six
authenticated query endpoints used by the desk and by the M3
reconciliation loop:

	GET  /api/v2/spot/account/info             — GetAccountInfo
	GET  /api/v2/spot/account/assets           — GetAccount
	GET  /api/v2/spot/trade/unfilled-orders    — GetOpenOrders (paginated)
	POST /api/v2/spot/trade/orderInfo          — GetOrderDetail
	GET  /api/v2/spot/trade/history-orders     — GetOrderHistory (paginated)
	GET  /api/v2/spot/trade/fills              — GetFills (paginated)

PROFILE NOTES:

  - No productType / marginMode / marginCoin / holdSide / tradeSide
    on the wire. Spot is a single product per request.
  - GetOrderDetail is a POST with a JSON body — that's the only
    Bitget V2 spot account endpoint that does not use GET. SDK
    callers see no asymmetry; the verb difference is hidden.
  - Pagination follows the standard idLessThan = endId cursor model.
    The shared bgcommon.PaginateByCursor helper drives every paged
    call; the per-profile constants live in bgcommon
    (OrdersPageLimit / OrdersMaxPages).

ID MAPPING:

Bitget echoes both `orderId` and `clientOid` on every spot account
response (unfilled-orders, history-orders, orderInfo, fills, plus
the WS "orders" channel). The SDK does NOT keep an in-memory cache
to translate between the two — callers that need such a cache
(e.g. the desk connector) keep it themselves. This mirrors mix.
*/

package spot

import (
	"context"
	"net/url"
	"strconv"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// AccountClient — account / balance / order-history sub-client.
// Built once per spot.Client (see client.go) and safe for concurrent
// use.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// ---------------------------------------------------------------------
// GetAccountInfo — /api/v2/spot/account/info.
// ---------------------------------------------------------------------

// accountInfoRow mirrors the JSON `data` of GET /spot/account/info.
// Bitget returns a flat object (NOT a list) here, in contrast to the
// account/assets endpoint.
type accountInfoRow struct {
	UserID      string   `json:"userId"`
	InviterID   string   `json:"inviterId"`
	IPs         string   `json:"ips"`
	Authorities []string `json:"authorities"`
	ParentID    string   `json:"parentId"`
	TraderType  string   `json:"traderType"`
	ChannelCode string   `json:"channelCode"`
	RegisTime   string   `json:"regisTime"`
}

// GetAccountInfo returns identity / configuration metadata for the
// API key's owning account. The desk uses this for boot-time
// health checks (whitelisted IPs, granted authorities).
func (a *AccountClient) GetAccountInfo(ctx context.Context) (spottypes.AccountInfo, error) {
	var out spottypes.AccountInfo

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/spot/account/info",
		Signed: true,
		Meta: rest.RequestMeta{
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return out, err
	}

	var row accountInfoRow
	if err = resp.UnmarshalData(&row); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account.GetAccountInfo: parse", err)
	}

	out.UserID = row.UserID
	out.InviterID = row.InviterID
	out.IPs = row.IPs
	out.Authorities = row.Authorities
	out.ParentID = row.ParentID
	out.TraderType = row.TraderType
	out.ChannelCode = row.ChannelCode
	out.RegisTimeMs, err = bgcommon.ParseInt64OrZero(row.RegisTime)
	if err != nil {
		return spottypes.AccountInfo{}, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account.GetAccountInfo: parse regisTime", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetAccount — /api/v2/spot/account/assets.
// ---------------------------------------------------------------------

// assetsRow mirrors one row of GET /spot/account/assets. Bitget
// returns one object per coin the account has touched.
type assetsRow struct {
	Coin           string `json:"coin"`
	Available      string `json:"available"`
	Frozen         string `json:"frozen"`
	Locked         string `json:"locked"`
	LimitAvailable string `json:"limitAvailable"`
	UTime          string `json:"uTime"`
}

// GetAccount returns the per-coin wallet snapshot for every coin the
// account holds. Aggregate fields (TotalEquity / AvailableBalance /
// LockedBalance / UnrealizedPnL / MaintenanceMargin) are zero — the
// spot endpoint does not expose them. Per-coin data lives in
// `Coins[]`.
//
// To restrict the query to a specific subset of coins, callers can
// post-filter the returned slice; the spot endpoint accepts an
// optional `coin` query parameter but the desk's reconciliation loop
// always pulls the full list, so the SDK exposes only the unfiltered
// form (callers that want the parameter can fork the helper later).
func (a *AccountClient) GetAccount(ctx context.Context) (roottypes.Balance, error) {
	var out roottypes.Balance

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v2/spot/account/assets",
		Signed: true,
		Meta: rest.RequestMeta{
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return out, err
	}

	var rows []assetsRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account.GetAccount: parse", err)
	}

	out.Coins = make([]roottypes.CoinBalance, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var coin roottypes.CoinBalance
		coin, err = convertAssetsRow(rows[i])
		if err != nil {
			return roottypes.Balance{}, err
		}
		out.Coins = append(out.Coins, coin)
	}
	return out, nil
}

// convertAssetsRow normalises one /assets row.
func convertAssetsRow(row assetsRow) (roottypes.CoinBalance, error) {
	var out roottypes.CoinBalance = roottypes.CoinBalance{
		Coin: row.Coin,
	}

	var err error
	out.Available, err = bgcommon.ParseDecimalOrZero(row.Available)
	if err != nil {
		return roottypes.CoinBalance{}, wrapAssetsParseErr(row.Coin, "available", err)
	}
	out.Frozen, err = bgcommon.ParseDecimalOrZero(row.Frozen)
	if err != nil {
		return roottypes.CoinBalance{}, wrapAssetsParseErr(row.Coin, "frozen", err)
	}
	out.Locked, err = bgcommon.ParseDecimalOrZero(row.Locked)
	if err != nil {
		return roottypes.CoinBalance{}, wrapAssetsParseErr(row.Coin, "locked", err)
	}
	// Equity = available + frozen + locked (Bitget V2 spot does not
	// expose a precomputed equity field on this endpoint; the sum
	// is the closest meaningful aggregate for callers that want
	// "total holdings of this coin").
	out.Equity = out.Available.Add(out.Frozen).Add(out.Locked)
	return out, nil
}

func wrapAssetsParseErr(coin, field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account.GetAccount: parse "+coin+"."+field, err)
}

// ---------------------------------------------------------------------
// Order rows — shared shape for unfilled / history / orderInfo.
// ---------------------------------------------------------------------

// spotOrderRow mirrors one row of /trade/unfilled-orders,
// /trade/history-orders, /trade/orderInfo. All three endpoints share
// the schema; only the cursor / window parameters differ on the
// request side.
type spotOrderRow struct {
	UserID           string `json:"userId"`
	Symbol           string `json:"symbol"`
	OrderID          string `json:"orderId"`
	ClientOid        string `json:"clientOid"`
	Price            string `json:"price"`
	Size             string `json:"size"`
	OrderType        string `json:"orderType"`
	Side             string `json:"side"`
	Status           string `json:"status"`
	PriceAvg         string `json:"priceAvg"`
	BaseVolume       string `json:"baseVolume"`
	QuoteVolume      string `json:"quoteVolume"`
	EnterPointSource string `json:"enterPointSource"`
	OrderSource      string `json:"orderSource"`
	Force            string `json:"force"`
	FeeDetail        string `json:"feeDetail"`
	CTime            string `json:"cTime"`
	UTime            string `json:"uTime"`
}

// convertSpotOrderRow normalises one row into spottypes.OrderInfo.
// Errors short-circuit on the first malformed numeric.
func convertSpotOrderRow(row spotOrderRow) (spottypes.OrderInfo, error) {
	var out spottypes.OrderInfo = spottypes.OrderInfo{
		OrderID:       row.OrderID,
		ClientOrderID: row.ClientOid,
		Symbol:        row.Symbol,
		Side:          roottypes.SideType(row.Side),
		OrderType:     roottypes.OrderType(row.OrderType),
		TimeInForce:   roottypes.TimeInForceType(row.Force),
		Status:        roottypes.OrderStatus(row.Status),
	}

	var err error
	out.Quantity, err = bgcommon.ParseDecimalOrZero(row.Size)
	if err != nil {
		return spottypes.OrderInfo{}, wrapSpotOrderParseErr("size", err)
	}
	out.Price, err = bgcommon.ParseDecimalOrZero(row.Price)
	if err != nil {
		return spottypes.OrderInfo{}, wrapSpotOrderParseErr("price", err)
	}
	out.FilledQuantity, err = bgcommon.ParseDecimalOrZero(row.BaseVolume)
	if err != nil {
		return spottypes.OrderInfo{}, wrapSpotOrderParseErr("baseVolume", err)
	}
	out.AvgFilledPrice, err = bgcommon.ParseDecimalOrZero(row.PriceAvg)
	if err != nil {
		return spottypes.OrderInfo{}, wrapSpotOrderParseErr("priceAvg", err)
	}
	// Spot ships fees as a JSON-encoded sub-object string ("feeDetail"),
	// not a flat number — parsing it requires a second JSON pass and
	// adds a Fill-shaped fanout the desk doesn't currently consume.
	// CumFee stays decimal.Zero on the OrderInfo path; callers that
	// need per-trade fees use GetFills (which exposes them as typed
	// fields).
	out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(row.CTime)
	if err != nil {
		return spottypes.OrderInfo{}, wrapSpotOrderParseErr("cTime", err)
	}
	out.UpdatedAtMs, err = bgcommon.ParseInt64OrZero(row.UTime)
	if err != nil {
		return spottypes.OrderInfo{}, wrapSpotOrderParseErr("uTime", err)
	}
	return out, nil
}

func wrapSpotOrderParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account: parse "+field, err)
}

// ---------------------------------------------------------------------
// GetOpenOrders — /api/v2/spot/trade/unfilled-orders.
// ---------------------------------------------------------------------

// GetOpenOrders returns every live order, optionally filtered by
// `symbol`. Pagination is handled internally via
// bgcommon.PaginateByCursor over Bitget's idLessThan = lastOrderId
// cursor model.
//
// An empty `symbol` returns ALL open orders for the API key — the
// desk uses that path for full-account reconciliation on startup.
func (a *AccountClient) GetOpenOrders(ctx context.Context, symbol string) ([]spottypes.OrderInfo, error) {
	var rows []spotOrderRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "spot.Account.GetOpenOrders",
		func(idLessThan string, limit int) ([]spotOrderRow, string, error) {
			return a.fetchOrderPage(ctx, "/api/v2/spot/trade/unfilled-orders", symbol, 0, 0, idLessThan, limit)
		})
	if err != nil {
		return nil, err
	}
	return convertOrderRows(rows)
}

// ---------------------------------------------------------------------
// GetOrderHistory — /api/v2/spot/trade/history-orders.
// ---------------------------------------------------------------------

// GetOrderHistory returns closed (filled / cancelled / rejected)
// orders for the API key, optionally filtered by symbol and time
// window. startTimeMs / endTimeMs == 0 leaves the bound off, letting
// Bitget apply its default look-back (90 days for spot at the time
// of writing).
//
// Pagination follows the same cursor protocol as GetOpenOrders.
func (a *AccountClient) GetOrderHistory(ctx context.Context, symbol string, startTimeMs, endTimeMs int64) ([]spottypes.OrderInfo, error) {
	var rows []spotOrderRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "spot.Account.GetOrderHistory",
		func(idLessThan string, limit int) ([]spotOrderRow, string, error) {
			return a.fetchOrderPage(ctx, "/api/v2/spot/trade/history-orders", symbol, startTimeMs, endTimeMs, idLessThan, limit)
		})
	if err != nil {
		return nil, err
	}
	return convertOrderRows(rows)
}

// fetchOrderPage runs one /unfilled-orders or /history-orders REST
// call. It is the per-page closure passed to bgcommon.PaginateByCursor.
//
// The wire envelope on these endpoints is `data: [<row>...]` — a
// flat list, with the cursor (`endId`) NOT shipped in a wrapper
// object (unlike mix's /orders-pending). The SDK derives the next
// cursor from the LAST orderId in the response.
func (a *AccountClient) fetchOrderPage(
	ctx context.Context,
	path string,
	symbol string,
	startTimeMs, endTimeMs int64,
	idLessThan string,
	limit int,
) ([]spotOrderRow, string, error) {
	var query url.Values = url.Values{}
	if symbol != "" {
		query.Set("symbol", symbol)
	}
	query.Set("limit", strconv.Itoa(limit))
	if idLessThan != "" {
		query.Set("idLessThan", idLessThan)
	}
	if startTimeMs > 0 {
		query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
	}
	if endTimeMs > 0 {
		query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
	}

	var meta rest.RequestMeta = rest.RequestMeta{
		Category: string(bitget.RateLimitCategoryQuery),
	}
	if symbol != "" {
		meta.Symbols = []string{symbol}
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: true,
		Meta:   meta,
	})
	if err != nil {
		return nil, "", err
	}

	var rows []spotOrderRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, "", bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account: parse "+path, err)
	}
	// Cursor: Bitget V2 spot expects the NEXT request to send
	// idLessThan = the last orderId in the just-returned page.
	var nextEndID string
	if len(rows) > 0 {
		nextEndID = rows[len(rows)-1].OrderID
	}
	return rows, nextEndID, nil
}

// convertOrderRows fans convertSpotOrderRow over a slice. Lifted
// because GetOpenOrders and GetOrderHistory both need it.
func convertOrderRows(rows []spotOrderRow) ([]spottypes.OrderInfo, error) {
	var out []spottypes.OrderInfo = make([]spottypes.OrderInfo, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var info spottypes.OrderInfo
		var err error
		info, err = convertSpotOrderRow(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// GetOrderDetail — POST /api/v2/spot/trade/orderInfo.
// ---------------------------------------------------------------------

// orderInfoBody is the JSON body of POST /trade/orderInfo. Bitget
// requires either orderId or clientOid; if both are present, orderId
// takes precedence.
type orderInfoBody struct {
	Symbol    string `json:"symbol"`
	OrderID   string `json:"orderId,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

// GetOrderDetail returns the lifecycle snapshot for one order.
// Either orderID OR clientOrderID must be set; passing both is
// allowed (Bitget gives orderID priority). An empty symbol surfaces
// ErrorKindInvalidRequest — kept symmetric with mix even though the
// venue tolerates an empty symbol when orderId is supplied (the
// rate-limit observer expects a typed Symbols list, and the desk
// always knows the symbol).
func (a *AccountClient) GetOrderDetail(ctx context.Context, symbol, orderID, clientOrderID string) (spottypes.OrderInfo, error) {
	var out spottypes.OrderInfo
	if symbol == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Account.GetOrderDetail: symbol is empty", nil)
	}
	if orderID == "" && clientOrderID == "" {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Account.GetOrderDetail: either orderID or clientOrderID is required", nil)
	}

	var body orderInfoBody = orderInfoBody{
		Symbol:    symbol,
		OrderID:   orderID,
		ClientOid: clientOrderID,
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/orderInfo",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return out, err
	}

	// Bitget /orderInfo returns a list (length 0 or 1 for a single-
	// order query). Decoding into []spotOrderRow lets us treat
	// "not found" uniformly across orderId and clientOid lookups.
	var rows []spotOrderRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account.GetOrderDetail: parse", err)
	}
	if len(rows) == 0 {
		return out, bitget.NewError(bitget.ErrorKindInvalidRequest, "",
			"spot.Account.GetOrderDetail: order not found (symbol="+symbol+", orderID="+orderID+", clientOid="+clientOrderID+")", nil)
	}
	return convertSpotOrderRow(rows[0])
}

// ---------------------------------------------------------------------
// GetFills — /api/v2/spot/trade/fills.
// ---------------------------------------------------------------------

// fillRow mirrors one row of GET /spot/trade/fills.
type fillRow struct {
	UserID     string        `json:"userId"`
	Symbol     string        `json:"symbol"`
	OrderID    string        `json:"orderId"`
	TradeID    string        `json:"tradeId"`
	OrderType  string        `json:"orderType"`
	Side       string        `json:"side"`
	PriceAvg   string        `json:"priceAvg"`
	Size       string        `json:"size"`
	Amount     string        `json:"amount"`
	FeeDetail  fillFeeDetail `json:"feeDetail"`
	TradeScope string        `json:"tradeScope"`
	CTime      string        `json:"cTime"`
	UTime      string        `json:"uTime"`
}

// fillFeeDetail mirrors the nested feeDetail object on /fills rows.
// Bitget ships fee data on /fills as a typed sub-object (unlike the
// JSON-encoded string blob that lives on /history-orders.feeDetail).
type fillFeeDetail struct {
	Deduction      string `json:"deduction"`
	FeeCoin        string `json:"feeCoin"`
	TotalDeduction string `json:"totalDeduction"`
	TotalFee       string `json:"totalFee"`
}

// GetFills returns every trade execution for the API key, optionally
// filtered by symbol, originating orderID, and time window.
//
// `orderID == ""` returns fills across all orders for the symbol;
// passing orderID restricts the query to a single order's fills
// (useful for post-trade analysis on a specific order). startTimeMs
// / endTimeMs == 0 leaves the bound off.
//
// Pagination follows the same cursor protocol as the order endpoints
// — the next request's idLessThan is the LAST tradeId on the page
// (NOT orderId, on this endpoint specifically).
func (a *AccountClient) GetFills(ctx context.Context, symbol, orderID string, startTimeMs, endTimeMs int64) ([]spottypes.Fill, error) {
	var rows []fillRow
	var err error
	rows, err = bgcommon.PaginateByCursor(ctx, "spot.Account.GetFills",
		func(idLessThan string, limit int) ([]fillRow, string, error) {
			var query url.Values = url.Values{}
			if symbol != "" {
				query.Set("symbol", symbol)
			}
			if orderID != "" {
				query.Set("orderId", orderID)
			}
			query.Set("limit", strconv.Itoa(limit))
			if idLessThan != "" {
				query.Set("idLessThan", idLessThan)
			}
			if startTimeMs > 0 {
				query.Set("startTime", strconv.FormatInt(startTimeMs, 10))
			}
			if endTimeMs > 0 {
				query.Set("endTime", strconv.FormatInt(endTimeMs, 10))
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
				Path:   "/api/v2/spot/trade/fills",
				Query:  query,
				Signed: true,
				Meta:   meta,
			})
			if ferr != nil {
				return nil, "", ferr
			}

			var pageRows []fillRow
			if ferr = resp.UnmarshalData(&pageRows); ferr != nil {
				return nil, "", bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account.GetFills: parse", ferr)
			}
			var nextEndID string
			if len(pageRows) > 0 {
				// /fills paginates by tradeId, not orderId.
				nextEndID = pageRows[len(pageRows)-1].TradeID
			}
			return pageRows, nextEndID, nil
		})
	if err != nil {
		return nil, err
	}

	var out []spottypes.Fill = make([]spottypes.Fill, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var f spottypes.Fill
		f, err = convertFillRow(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// convertFillRow normalises one /fills row into spottypes.Fill.
func convertFillRow(row fillRow) (spottypes.Fill, error) {
	var out spottypes.Fill = spottypes.Fill{
		OrderID:    row.OrderID,
		TradeID:    row.TradeID,
		Symbol:     row.Symbol,
		Side:       roottypes.SideType(row.Side),
		OrderType:  roottypes.OrderType(row.OrderType),
		FeeCoin:    row.FeeDetail.FeeCoin,
		TradeScope: row.TradeScope,
	}

	var err error
	out.FillPrice, err = bgcommon.ParseDecimalOrZero(row.PriceAvg)
	if err != nil {
		return spottypes.Fill{}, wrapFillParseErr("priceAvg", err)
	}
	out.Size, err = bgcommon.ParseDecimalOrZero(row.Size)
	if err != nil {
		return spottypes.Fill{}, wrapFillParseErr("size", err)
	}
	out.Amount, err = bgcommon.ParseDecimalOrZero(row.Amount)
	if err != nil {
		return spottypes.Fill{}, wrapFillParseErr("amount", err)
	}
	out.TotalFee, err = bgcommon.ParseDecimalOrZero(row.FeeDetail.TotalFee)
	if err != nil {
		return spottypes.Fill{}, wrapFillParseErr("feeDetail.totalFee", err)
	}
	out.CreatedAtMs, err = bgcommon.ParseInt64OrZero(row.CTime)
	if err != nil {
		return spottypes.Fill{}, wrapFillParseErr("cTime", err)
	}
	return out, nil
}

func wrapFillParseErr(field string, err error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Account.GetFills: parse "+field, err)
}
