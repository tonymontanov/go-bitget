/*
FILE: spot/trading.go

DESCRIPTION:
Trading sub-client for the Bitget V2 SPOT profile. Wires every
single + batch place / amend / cancel endpoint:

  - POST /api/v2/spot/trade/place-order               — CreateOrder
  - POST /api/v2/spot/trade/batch-orders              — CreateBatchOrders
  - POST /api/v2/spot/trade/cancel-replace-order      — ModifyOrder
  - POST /api/v2/spot/trade/batch-cancel-replace-order — ModifyBatchOrders
  - POST /api/v2/spot/trade/cancel-order              — CancelOrder
  - POST /api/v2/spot/trade/batch-cancel-order         — CancelBatchOrders
  - POST /api/v2/spot/trade/cancel-symbol-order       — CancelAllOrders

DIFFERENCES FROM mix.TradingClient:

  - No marginMode / marginCoin / tradeSide / reduceOnly on the wire.
  - batch-orders is per-symbol (symbol at top level + orderList of
    same-symbol rows). Same homogeneity validation as mix.
  - batch-cancel-replace-order is a NATIVE endpoint — mix has none
    and falls back to client-side fan-out. spot.ModifyBatchOrders
    therefore issues a SINGLE REST RPC and decodes the standard
    successList / failureList envelope.
  - cancel-symbol-order takes only `symbol` — no productType filter,
    no fan-out. Matches the desk's per-symbol cleanup contract.

NUMERIC HANDLING:

The `size` field is side-dependent on spot — for limit orders and
market sells it is base-side; for market buys it is QUOTE-side. The
SDK ships req.Quantity verbatim and documents the convention on
spot.types.CreateOrderRequest. Conversions (if needed) live one
layer up in the desk-side adapter.

CLIENT-OID GENERATION:

ModifyOrder auto-fills NewClientOrderID with bgcommon.GenClientOid("s-")
when the caller leaves it empty — same pattern as mix's "m-" prefix,
shared helper in internal/bgcommon/clientoid.go. The "s-" prefix lets
log greps tell spot- vs mix-side modify IDs apart.
*/

package spot

import (
	"context"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	spottypes "github.com/tonymontanov/go-bitget/v2/spot/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// TradingClient — REST trading sub-client. Built once per spot.Client
// (see client.go) and safe for concurrent use.
type TradingClient struct {
	c *Client
}

func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{c: c}
}

// genNewClientOid produces a venue-acceptable clientOid for the
// modified order when the caller did not provide one. Format
// `s-<32-hex>` (34 chars, well below Bitget's 50-char cap). Backed
// by bgcommon.GenClientOid; the prefix discriminates spot-side
// modify IDs from mix-side ones in audit logs.
func genNewClientOid() string { return bgcommon.GenClientOid("s-") }

// ---------------------------------------------------------------------
// Single-order: CreateOrder.
// ---------------------------------------------------------------------

// placeOrderBody mirrors the JSON body Bitget V2 expects on
// POST /api/v2/spot/trade/place-order. Optional fields are emitted
// with omitempty; Bitget treats `force=""` as "use the default for
// this orderType".
type placeOrderBody struct {
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	OrderType string `json:"orderType"`
	Force     string `json:"force,omitempty"`
	Price     string `json:"price,omitempty"`
	Size      string `json:"size"`
	ClientOid string `json:"clientOid,omitempty"`
}

// placeOrderResp is the JSON `data` returned by place-order on success.
type placeOrderResp struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// CreateOrder submits one spot order. The returned OrderInfo is built
// from the response payload (orderId + clientOid) plus the request
// itself — Bitget does not echo full lifecycle here; M3 query
// endpoints are the source of truth for fill / status.
func (t *TradingClient) CreateOrder(ctx context.Context, req spottypes.CreateOrderRequest) (spottypes.OrderInfo, error) {
	var out spottypes.OrderInfo
	var err error
	if err = validateCreateOrderRequest(req); err != nil {
		return out, err
	}

	var body placeOrderBody = buildPlaceBody(req)

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/place-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{req.Symbol},
			OrderCount: 1,
			Category:   string(bitget.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return out, err
	}

	var data placeOrderResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Trading.CreateOrder: parse", err)
	}
	return spottypes.OrderInfo{
		OrderID:       data.OrderID,
		ClientOrderID: bgcommon.ChooseClientOid(data.ClientOid, req.ClientOrderID),
		Symbol:        req.Symbol,
		Side:          req.Side,
		OrderType:     req.OrderType,
		TimeInForce:   req.TimeInForce,
		Status:        roottypes.OrderStatusLive,
		Quantity:      req.Quantity,
		Price:         req.Price,
	}, nil
}

// ---------------------------------------------------------------------
// Single-order: ModifyOrder.
// ---------------------------------------------------------------------

// modifyOrderBody mirrors the JSON body of POST
// /api/v2/spot/trade/cancel-replace-order. NewClientOid is REQUIRED
// (the old clientOid cannot be reused) so the SDK fails fast if the
// resolved newClientOid collides with the existing one.
//
// WIRE NAMES: the venue expects the NEW order's amount/price under the
// bare `size` / `price` keys (NOT `newSize` / `newPrice`) — the request
// is a full re-placement. Both are mandatory; Bitget rejects a body
// that omits either with code=400172 ("size...empty" / "price...empty").
type modifyOrderBody struct {
	Symbol       string `json:"symbol"`
	OrderID      string `json:"orderId,omitempty"`
	ClientOid    string `json:"clientOid,omitempty"`
	NewClientOid string `json:"newClientOid"`
	Size         string `json:"size"`
	Price        string `json:"price"`
}

// modifyOrderResp is the JSON `data` returned by cancel-replace-order.
// Bitget echoes the new orderId / clientOid (modify is implemented
// as a cancel-replace at the matcher level — hence a NEW orderId) plus
// a per-order `success` ("success" | "failure") flag and a `msg` reason:
// the HTTP envelope can be code=00000 while the individual amend still
// failed (e.g. the order vanished mid-flight), so the SDK must inspect
// `success` rather than trust the 200.
type modifyOrderResp struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
	Success   string `json:"success"`
	Msg       string `json:"msg"`
}

// ModifyOrder re-prices and / or re-sizes an open spot order.
//
// WIRE CONTRACT — both fields required: the spot endpoint is a native
// cancel-replace-order, i.e. a full re-placement of the order, and the
// venue carries the NEW values under the bare `size` / `price` keys
// (NOT `newSize` / `newPrice`). Bitget rejects a request that omits
// either side with code=400172 ("size...spot.order.size.empty" /
// "price...spot.order.price.empty"). Callers that only want to change
// one dimension (e.g. a pure re-price) MUST still echo the unchanged
// value of the other. The SDK does not fetch the live order to backfill
// the missing side — that is an application-layer policy (a REST
// round-trip) and the desk connector owns it. validateModifyOrderRequest
// enforces that BOTH NewQuantity and NewPrice are positive, so a partial
// modify fails fast locally instead of round-tripping a 400172.
//
// Identification: either OrderID or ClientOrderID points at the
// existing order. If both are populated, OrderID wins (Bitget's
// documented precedence rule).
//
// New clientOid: req.NewClientOrderID flows through as `newClientOid`.
// If empty the SDK auto-fills `s-<32-hex>` via bgcommon.GenClientOid
// — Bitget V2 mandates newClientOid to be non-empty AND distinct
// from the existing clientOid (else code=40786 on every profile).
func (t *TradingClient) ModifyOrder(ctx context.Context, req spottypes.ModifyOrderRequest) (spottypes.OrderInfo, error) {
	var out spottypes.OrderInfo
	var err error
	if err = validateModifyOrderRequest(req); err != nil {
		return out, err
	}

	// Resolve the new clientOid up-front so the value is observable
	// by the caller in OrderInfo.ClientOrderID even if the venue
	// echoes an empty string (defensive).
	var newClientOid string = req.NewClientOrderID
	if newClientOid == "" {
		newClientOid = genNewClientOid()
	}
	if newClientOid == req.ClientOrderID && req.ClientOrderID != "" {
		// Caller explicitly supplied the same ID for both — Bitget
		// would reject this with 40786. Surface the misuse early
		// instead of waiting for the venue to round-trip-reject.
		return out, bitget.NewError(
			bitget.ErrorKindInvalidRequest, "",
			"spot.Trading.ModifyOrder: NewClientOrderID must differ from ClientOrderID (Bitget rejects with code=40786 otherwise)",
			nil,
		)
	}

	var body modifyOrderBody = modifyOrderBody{
		Symbol:       req.Symbol,
		OrderID:      req.OrderID,
		ClientOid:    req.ClientOrderID,
		NewClientOid: newClientOid,
	}
	// Both fields are validated > 0 in validateModifyOrderRequest and
	// the venue mandates both, so emit them unconditionally.
	body.Size = req.NewQuantity.String()
	body.Price = req.NewPrice.String()

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/cancel-replace-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{req.Symbol},
			OrderCount: 1,
			Category:   string(bitget.RateLimitCategoryAmend),
		},
	})
	if err != nil {
		return out, err
	}

	var data modifyOrderResp
	if err = resp.UnmarshalData(&data); err != nil {
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Trading.ModifyOrder: parse", err)
	}
	if isSpotModifyFailure(data.Success) {
		return out, bitget.NewError(
			bitget.MapBitgetCode("", data.Msg), "",
			"spot.Trading.ModifyOrder: venue rejected amend: "+data.Msg, nil,
		)
	}
	return spottypes.OrderInfo{
		OrderID:       data.OrderID,
		ClientOrderID: bgcommon.ChooseClientOid(data.ClientOid, newClientOid),
		Symbol:        req.Symbol,
		Status:        roottypes.OrderStatusLive,
		Quantity:      req.NewQuantity,
		Price:         req.NewPrice,
	}, nil
}

// ---------------------------------------------------------------------
// Single-order: CancelOrder.
// ---------------------------------------------------------------------

// cancelOrderBody mirrors the JSON body of POST
// /api/v2/spot/trade/cancel-order. Either orderId or clientOid is
// required; if both are present Bitget gives orderId priority.
type cancelOrderBody struct {
	Symbol    string `json:"symbol"`
	OrderID   string `json:"orderId,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

// CancelOrder cancels one spot order. Returns nil on success, error
// on any rejection.
func (t *TradingClient) CancelOrder(ctx context.Context, req roottypes.CancelOrderRequest) error {
	var err error
	if err = validateCancelOrderRequest(req); err != nil {
		return err
	}

	var body cancelOrderBody = cancelOrderBody{
		Symbol:    req.Symbol,
		OrderID:   req.OrderID,
		ClientOid: req.ClientOrderID,
	}
	_, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/cancel-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{req.Symbol},
			OrderCount: 1,
			Category:   string(bitget.RateLimitCategoryCancel),
		},
	})
	return err
}

// ---------------------------------------------------------------------
// Batch: CreateBatchOrders.
// ---------------------------------------------------------------------

// batchPlaceOrderBody is the wire payload for batch-orders. Bitget
// requires `symbol` at the top level (every orderList row inherits
// it) — the SDK validates that all reqs share the same symbol.
type batchPlaceOrderBody struct {
	Symbol    string                 `json:"symbol"`
	BatchMode string                 `json:"batchMode,omitempty"`
	OrderList []batchPlaceOrderEntry `json:"orderList"`
}

// batchPlaceOrderEntry is one row of orderList.
type batchPlaceOrderEntry struct {
	Side      string `json:"side"`
	OrderType string `json:"orderType"`
	Force     string `json:"force,omitempty"`
	Price     string `json:"price,omitempty"`
	Size      string `json:"size"`
	ClientOid string `json:"clientOid,omitempty"`
}

/*
CreateBatchOrders submits a batch of spot orders.

CONSTRAINTS:
  - 1 <= len(reqs) <= bgcommon.MaxBatchSize (50).
  - All rows MUST share the same Symbol — Bitget V2 batch-orders
    is per-symbol (the symbol is at the top level, not per-row). The
    SDK validates this client-side.

The returned slice has the same length as reqs and is ordered to
match: every input row maps 1-to-1 to an output entry. Successful
rows have Order set (with OrderID + ClientOrderID populated); failed
rows have Err set with a typed *bitget.Error. ClientOrderID is
populated on every row regardless of success.
*/
func (t *TradingClient) CreateBatchOrders(ctx context.Context, reqs []spottypes.CreateOrderRequest) ([]spottypes.BatchOrderResult, error) {
	var err error
	if err = bgcommon.ValidateBatchSize("spot.Trading.CreateBatchOrders", len(reqs)); err != nil {
		return nil, err
	}

	var symbol string = reqs[0].Symbol
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateCreateOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading.CreateBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		if reqs[i].Symbol != symbol {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading.CreateBatchOrders: all rows must share the same symbol (row 0="+symbol+", row "+strconv.Itoa(i)+"="+reqs[i].Symbol+")", nil)
		}
	}

	var body batchPlaceOrderBody = batchPlaceOrderBody{
		Symbol:    symbol,
		OrderList: make([]batchPlaceOrderEntry, len(reqs)),
	}
	for i = 0; i < len(reqs); i++ {
		body.OrderList[i] = buildBatchPlaceEntry(reqs[i])
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/batch-orders",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{symbol},
			OrderCount: len(reqs),
			Category:   string(bitget.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return nil, err
	}

	var data bgcommon.BatchEnvelope
	if err = resp.UnmarshalData(&data); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Trading.CreateBatchOrders: parse", err)
	}

	var clientOids []string = make([]string, len(reqs))
	for i = 0; i < len(reqs); i++ {
		clientOids[i] = reqs[i].ClientOrderID
	}
	return collateCreateResults(reqs, clientOids, data, symbol), nil
}

// ---------------------------------------------------------------------
// Batch: ModifyBatchOrders (NATIVE on spot).
// ---------------------------------------------------------------------

// batchModifyOrderBody is the wire payload for
// /api/v2/spot/trade/batch-cancel-replace-order. Each order row carries
// its own symbol (unlike batch-orders), so the SDK does NOT enforce
// homogeneous symbol — the venue accepts mixed-symbol modifies.
type batchModifyOrderBody struct {
	OrderList []batchModifyOrderEntry `json:"orderList"`
}

type batchModifyOrderEntry struct {
	Symbol       string `json:"symbol"`
	OrderID      string `json:"orderId,omitempty"`
	ClientOid    string `json:"clientOid,omitempty"`
	NewClientOid string `json:"newClientOid"`
	// Bare `size` / `price` (NOT `newSize` / `newPrice`) — same wire
	// contract as the single cancel-replace-order; both mandatory.
	Size  string `json:"size"`
	Price string `json:"price"`
}

/*
ModifyBatchOrders amends a batch of spot orders.

NATIVE ENDPOINT (vs mix CLIENT-SIDE FAN-OUT):

Unlike mix (where Bitget V2 ships no batch-modify endpoint and the
SDK fans out to N single ModifyOrder RPCs), spot has a NATIVE
/api/v2/spot/trade/batch-cancel-replace-order. spot.ModifyBatchOrders
issues a SINGLE REST call and decodes the standard successList /
failureList envelope. No client-side concurrency cap is needed
(the venue handles bursts on its side, just like batch-place).

NewClientOrderID auto-fill: as on the single ModifyOrder, when a
row leaves NewClientOrderID empty the SDK fills it with
bgcommon.GenClientOid("s-"). Same collision check (newClientOid !=
existing clientOid) is enforced per-row.

Like CreateBatchOrders, the returned slice mirrors the request order
and every row has either Order or Err populated.
*/
func (t *TradingClient) ModifyBatchOrders(ctx context.Context, reqs []spottypes.ModifyOrderRequest) ([]spottypes.BatchOrderResult, error) {
	var err error
	if err = bgcommon.ValidateBatchSize("spot.Trading.ModifyBatchOrders", len(reqs)); err != nil {
		return nil, err
	}

	// Validate every row + auto-fill missing NewClientOrderIDs so
	// the body and the result echo carry the same values.
	var resolvedNewOid []string = make([]string, len(reqs))
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateModifyOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading.ModifyBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		var n string = reqs[i].NewClientOrderID
		if n == "" {
			n = genNewClientOid()
		}
		if n == reqs[i].ClientOrderID && reqs[i].ClientOrderID != "" {
			return nil, bitget.NewError(
				bitget.ErrorKindInvalidRequest, "",
				"spot.Trading.ModifyBatchOrders["+strconv.Itoa(i)+"]: NewClientOrderID must differ from ClientOrderID",
				nil,
			)
		}
		resolvedNewOid[i] = n
	}

	var body batchModifyOrderBody = batchModifyOrderBody{
		OrderList: make([]batchModifyOrderEntry, len(reqs)),
	}
	var seenSymbols map[string]struct{} = map[string]struct{}{}
	for i = 0; i < len(reqs); i++ {
		var entry batchModifyOrderEntry = batchModifyOrderEntry{
			Symbol:       reqs[i].Symbol,
			OrderID:      reqs[i].OrderID,
			ClientOid:    reqs[i].ClientOrderID,
			NewClientOid: resolvedNewOid[i],
		}
		// Both validated > 0 (validateModifyOrderRequest); venue
		// mandates both, so emit unconditionally.
		entry.Size = reqs[i].NewQuantity.String()
		entry.Price = reqs[i].NewPrice.String()
		body.OrderList[i] = entry
		seenSymbols[reqs[i].Symbol] = struct{}{}
	}

	// Collect the set of symbols for the rate-limit accounting;
	// batch-cancel-replace-order is multi-symbol so we report all.
	var symbols []string = make([]string, 0, len(seenSymbols))
	var s string
	for s = range seenSymbols {
		symbols = append(symbols, s)
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/batch-cancel-replace-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    symbols,
			OrderCount: len(reqs),
			Category:   string(bitget.RateLimitCategoryAmend),
		},
	})
	if err != nil {
		return nil, err
	}

	// IMPORTANT: unlike batch-orders / batch-cancel-order (which return a
	// {successList, failureList} envelope), batch-cancel-replace-order
	// returns a FLAT array of per-row outcomes in request order, each
	// tagged with `success` / `msg`. Decoding it as a BatchEnvelope fails
	// with "expect { but found [".
	var rows []batchModifyResultRow
	if err = resp.UnmarshalData(&rows); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Trading.ModifyBatchOrders: parse", err)
	}

	return collateModifyResults(reqs, resolvedNewOid, rows), nil
}

// batchModifyResultRow is one element of the batch-cancel-replace-order
// response `data` array. clientOid may arrive as JSON null (→ "").
type batchModifyResultRow struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
	Success   string `json:"success"`
	Msg       string `json:"msg"`
}

// isSpotModifyFailure reports whether a per-order `success` flag signals
// a venue-side rejection. Only an explicit "failure" counts; an empty or
// "success" value is treated as success (lenient — avoids inventing
// errors if the venue omits the flag).
func isSpotModifyFailure(success string) bool {
	return strings.EqualFold(strings.TrimSpace(success), "failure")
}

// ---------------------------------------------------------------------
// Batch: CancelBatchOrders.
// ---------------------------------------------------------------------

// batchCancelOrderBody is the wire payload for
// /api/v2/spot/trade/batch-cancel-order. Like batch-orders, it pins
// `symbol` at the top level and the SDK enforces homogeneity.
type batchCancelOrderBody struct {
	Symbol      string                  `json:"symbol"`
	BatchMode   string                  `json:"batchMode,omitempty"`
	OrderIDList []batchCancelOrderEntry `json:"orderList"`
}

type batchCancelOrderEntry struct {
	OrderID   string `json:"orderId,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

/*
CancelBatchOrders cancels a batch of spot orders. Same shape contract
as the other batch methods (per-symbol, 1..50 rows). Each row needs
exactly one of (OrderID, ClientOrderID).
*/
func (t *TradingClient) CancelBatchOrders(ctx context.Context, reqs []roottypes.CancelOrderRequest) ([]spottypes.BatchOrderResult, error) {
	var err error
	if err = bgcommon.ValidateBatchSize("spot.Trading.CancelBatchOrders", len(reqs)); err != nil {
		return nil, err
	}

	var symbol string = reqs[0].Symbol
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateCancelOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading.CancelBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		if reqs[i].Symbol != symbol {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading.CancelBatchOrders: all rows must share the same symbol (row 0="+symbol+", row "+strconv.Itoa(i)+"="+reqs[i].Symbol+")", nil)
		}
	}

	var body batchCancelOrderBody = batchCancelOrderBody{
		Symbol:      symbol,
		OrderIDList: make([]batchCancelOrderEntry, len(reqs)),
	}
	for i = 0; i < len(reqs); i++ {
		body.OrderIDList[i] = batchCancelOrderEntry{
			OrderID:   reqs[i].OrderID,
			ClientOid: reqs[i].ClientOrderID,
		}
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/batch-cancel-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:    []string{symbol},
			OrderCount: len(reqs),
			Category:   string(bitget.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return nil, err
	}

	var data bgcommon.BatchEnvelope
	if err = resp.UnmarshalData(&data); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "spot.Trading.CancelBatchOrders: parse", err)
	}

	var clientOids []string = make([]string, len(reqs))
	for i = 0; i < len(reqs); i++ {
		clientOids[i] = reqs[i].ClientOrderID
	}
	return collateCancelResults(reqs, clientOids, data, symbol), nil
}

// ---------------------------------------------------------------------
// CancelAllOrders (per-symbol on spot).
// ---------------------------------------------------------------------

// cancelSymbolOrderBody is the wire payload for
// /api/v2/spot/trade/cancel-symbol-order.
type cancelSymbolOrderBody struct {
	Symbol string `json:"symbol"`
}

/*
CancelAllOrders cancels every open spot order for `symbol`.

  - symbol == "" → ErrorKindInvalidRequest. Bitget V2 spot does NOT
    expose a "cancel everything across all symbols" endpoint
    (mix has /cancel-all-orders + productType, spot does not).

The endpoint returns a {successList, failureList} envelope; the SDK
discards the per-row outcomes and returns a single error if Bitget
populated failureList. Callers that need per-row visibility should
enumerate open orders first and use CancelBatchOrders.
*/
func (t *TradingClient) CancelAllOrders(ctx context.Context, symbol string) error {
	if symbol == "" {
		return bitget.NewError(
			bitget.ErrorKindInvalidRequest, "",
			"spot.Trading.CancelAllOrders: symbol is required (Bitget V2 spot has no cross-symbol cancel-all endpoint)",
			nil,
		)
	}

	var body cancelSymbolOrderBody = cancelSymbolOrderBody{Symbol: symbol}

	var resp rest.Response
	var _, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/spot/trade/cancel-symbol-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(bitget.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return err
	}

	// cancel-symbol-order returns the standard envelope; surface the
	// first failure as a typed exchange error, ignore success rows.
	var data bgcommon.BatchEnvelope
	if err = resp.UnmarshalData(&data); err != nil {
		// Some Bitget builds return an empty data object — treat
		// "no envelope" as success rather than an error so this
		// helper stays robust across minor API revisions.
		return nil
	}
	if len(data.FailureList) > 0 {
		var f bgcommon.BatchFailureRow = data.FailureList[0]
		return bitget.NewError(bitget.ErrorKindExchange, f.ErrorCode, "spot.Trading.CancelAllOrders: "+f.ErrorMsg, nil)
	}
	return nil
}

// ---------------------------------------------------------------------
// Helpers — request building.
// ---------------------------------------------------------------------

// buildPlaceBody assembles the wire body for a single place-order
// call from the typed request.
func buildPlaceBody(req spottypes.CreateOrderRequest) placeOrderBody {
	var body placeOrderBody = placeOrderBody{
		Symbol:    req.Symbol,
		Side:      string(req.Side),
		OrderType: string(req.OrderType),
		Force:     string(req.TimeInForce),
		Size:      req.Quantity.String(),
		ClientOid: req.ClientOrderID,
	}
	if req.OrderType == roottypes.OrderTypeLimit {
		body.Price = req.Price.String()
	}
	return body
}

// buildBatchPlaceEntry mirrors buildPlaceBody but produces a per-row
// entry for batch-orders (no symbol — that lives at the top level).
func buildBatchPlaceEntry(req spottypes.CreateOrderRequest) batchPlaceOrderEntry {
	var entry batchPlaceOrderEntry = batchPlaceOrderEntry{
		Side:      string(req.Side),
		OrderType: string(req.OrderType),
		Force:     string(req.TimeInForce),
		Size:      req.Quantity.String(),
		ClientOid: req.ClientOrderID,
	}
	if req.OrderType == roottypes.OrderTypeLimit {
		entry.Price = req.Price.String()
	}
	return entry
}

// ---------------------------------------------------------------------
// Helpers — validation.
// ---------------------------------------------------------------------

func validateCreateOrderRequest(req spottypes.CreateOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: symbol is empty", nil)
	}
	if req.Side == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: side is empty", nil)
	}
	if req.OrderType == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: orderType is empty", nil)
	}
	if !decimalIsPositive(req.Quantity) {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: quantity must be > 0", nil)
	}
	if req.OrderType == roottypes.OrderTypeLimit && !decimalIsPositive(req.Price) {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: price must be > 0 for limit orders", nil)
	}
	return nil
}

func validateModifyOrderRequest(req spottypes.ModifyOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: symbol is empty", nil)
	}
	if req.OrderID == "" && req.ClientOrderID == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: either orderId or clientOrderId is required", nil)
	}
	if req.NewQuantity.Sign() <= 0 || req.NewPrice.Sign() <= 0 {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: both newQuantity and newPrice are required and must be positive (cancel-replace is a full re-placement)", nil)
	}
	return nil
}

func validateCancelOrderRequest(req roottypes.CancelOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: symbol is empty", nil)
	}
	if req.OrderID == "" && req.ClientOrderID == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "spot.Trading: either orderId or clientOrderId is required", nil)
	}
	return nil
}

// decimalIsPositive returns true iff d > 0. Used by validators to
// reject zero / negative quantities and prices early.
func decimalIsPositive(d decimal.Decimal) bool {
	return d.Sign() > 0
}

// ---------------------------------------------------------------------
// Helpers — response collation.
// ---------------------------------------------------------------------

/*
collateCreateResults maps Bitget's parallel successList / failureList
into a per-row BatchOrderResult slice ordered to match the original
request slice.

PAIRING STRATEGY (same as the mix counterpart):
Bitget echoes back clientOid on every row of both lists. We index the
original request slice by clientOid and use that as the join key. If
a row had no clientOid we fall back to a positional join. Extra rows
(should never happen but guarded against) are appended at the end so
the caller is not silently left with wrong indices.
*/
func collateCreateResults(
	reqs []spottypes.CreateOrderRequest,
	clientOids []string,
	data bgcommon.BatchEnvelope,
	symbol string,
) []spottypes.BatchOrderResult {
	var results []spottypes.BatchOrderResult = make([]spottypes.BatchOrderResult, len(reqs))
	var byClientOid map[string]*spottypes.BatchOrderResult = map[string]*spottypes.BatchOrderResult{}
	var positional []*spottypes.BatchOrderResult
	var i int
	for i = 0; i < len(reqs); i++ {
		results[i] = spottypes.BatchOrderResult{ClientOrderID: clientOids[i]}
		if clientOids[i] != "" {
			byClientOid[clientOids[i]] = &results[i]
		} else {
			positional = append(positional, &results[i])
		}
	}

	var ok bgcommon.BatchSuccessRow
	var idx int = 0
	for _, ok = range data.SuccessList {
		var target *spottypes.BatchOrderResult
		if ok.ClientOid != "" {
			target = byClientOid[ok.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		if target == nil {
			results = append(results, spottypes.BatchOrderResult{
				ClientOrderID: ok.ClientOid,
				Order: &spottypes.OrderInfo{
					OrderID:       ok.OrderID,
					ClientOrderID: ok.ClientOid,
					Symbol:        symbol,
					Status:        roottypes.OrderStatusLive,
				},
			})
			continue
		}
		var info *spottypes.OrderInfo = &spottypes.OrderInfo{
			OrderID:       ok.OrderID,
			ClientOrderID: bgcommon.ChooseClientOid(ok.ClientOid, target.ClientOrderID),
			Symbol:        symbol,
			Status:        roottypes.OrderStatusLive,
		}
		// Echo the placed quantity / price / side from the request to
		// keep results self-describing without an extra GET.
		var reqIdx int
		reqIdx, _ = findResultIndex(results, target)
		if reqIdx >= 0 {
			info.Side = reqs[reqIdx].Side
			info.OrderType = reqs[reqIdx].OrderType
			info.TimeInForce = reqs[reqIdx].TimeInForce
			info.Quantity = reqs[reqIdx].Quantity
			info.Price = reqs[reqIdx].Price
		}
		target.Order = info
	}

	var fail bgcommon.BatchFailureRow
	idx = 0
	for _, fail = range data.FailureList {
		var target *spottypes.BatchOrderResult
		if fail.ClientOid != "" {
			target = byClientOid[fail.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		var perRowErr error = bitget.NewError(
			bitget.MapBitgetCode(fail.ErrorCode, fail.ErrorMsg),
			fail.ErrorCode,
			fail.ErrorMsg,
			nil,
		)
		if target == nil {
			results = append(results, spottypes.BatchOrderResult{
				ClientOrderID: fail.ClientOid,
				Err:           perRowErr,
			})
			continue
		}
		target.Err = perRowErr
	}

	return results
}

/*
collateModifyResults pairs batch-modify outcomes with the originating
ModifyOrderRequest rows.

WIRE SHAPE: batch-cancel-replace-order returns a FLAT array of per-row
results in REQUEST order (not a success/failure envelope), each carrying
the NEW orderId, an echoed clientOid (often null), a `success` flag and a
`msg` reason. Pairing is therefore POSITIONAL — the only reliable join,
since the venue routinely nulls clientOid on modify. clientOid is used
only as a display value, never as the join key.
*/
func collateModifyResults(
	reqs []spottypes.ModifyOrderRequest,
	resolvedNewOid []string,
	rows []batchModifyResultRow,
) []spottypes.BatchOrderResult {
	var results []spottypes.BatchOrderResult = make([]spottypes.BatchOrderResult, len(reqs))
	var i int
	for i = 0; i < len(reqs); i++ {
		results[i] = spottypes.BatchOrderResult{ClientOrderID: resolvedNewOid[i]}
	}

	var paired int = len(rows)
	if paired > len(reqs) {
		paired = len(reqs)
	}
	for i = 0; i < paired; i++ {
		var row batchModifyResultRow = rows[i]
		if isSpotModifyFailure(row.Success) {
			results[i].Err = bitget.NewError(
				bitget.MapBitgetCode("", row.Msg), "",
				"spot.Trading.ModifyBatchOrders: venue rejected amend: "+row.Msg, nil,
			)
			continue
		}
		results[i].Order = &spottypes.OrderInfo{
			OrderID:       row.OrderID,
			ClientOrderID: bgcommon.ChooseClientOid(row.ClientOid, resolvedNewOid[i]),
			Symbol:        reqs[i].Symbol,
			Status:        roottypes.OrderStatusLive,
			Quantity:      reqs[i].NewQuantity,
			Price:         reqs[i].NewPrice,
		}
	}

	// Defensive: the venue returned more rows than we sent. Should never
	// happen, but surface the extras instead of dropping them silently.
	for i = paired; i < len(rows); i++ {
		var row batchModifyResultRow = rows[i]
		var extra spottypes.BatchOrderResult = spottypes.BatchOrderResult{ClientOrderID: row.ClientOid}
		if isSpotModifyFailure(row.Success) {
			extra.Err = bitget.NewError(
				bitget.MapBitgetCode("", row.Msg), "",
				"spot.Trading.ModifyBatchOrders: venue rejected amend: "+row.Msg, nil,
			)
		} else {
			extra.Order = &spottypes.OrderInfo{
				OrderID:       row.OrderID,
				ClientOrderID: row.ClientOid,
				Status:        roottypes.OrderStatusLive,
			}
		}
		results = append(results, extra)
	}

	return results
}

// collateCancelResults pairs cancel-batch outcomes with the
// originating CancelOrderRequest rows. Cancellation responses do not
// carry quantity / price; the success Order contains only OrderID +
// ClientOrderID + a "cancelled" status placeholder.
func collateCancelResults(
	reqs []roottypes.CancelOrderRequest,
	clientOids []string,
	data bgcommon.BatchEnvelope,
	symbol string,
) []spottypes.BatchOrderResult {
	_ = reqs // kept in the signature for future side / orderType echo
	var results []spottypes.BatchOrderResult = make([]spottypes.BatchOrderResult, len(clientOids))
	var byClientOid map[string]*spottypes.BatchOrderResult = map[string]*spottypes.BatchOrderResult{}
	var positional []*spottypes.BatchOrderResult
	var i int
	for i = 0; i < len(clientOids); i++ {
		results[i] = spottypes.BatchOrderResult{ClientOrderID: clientOids[i]}
		if clientOids[i] != "" {
			byClientOid[clientOids[i]] = &results[i]
		} else {
			positional = append(positional, &results[i])
		}
	}

	var ok bgcommon.BatchSuccessRow
	var idx int = 0
	for _, ok = range data.SuccessList {
		var target *spottypes.BatchOrderResult
		if ok.ClientOid != "" {
			target = byClientOid[ok.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		var info *spottypes.OrderInfo = &spottypes.OrderInfo{
			OrderID:       ok.OrderID,
			ClientOrderID: bgcommon.ChooseClientOid(ok.ClientOid, ""),
			Symbol:        symbol,
			Status:        roottypes.OrderStatusCancelled,
		}
		if target == nil {
			results = append(results, spottypes.BatchOrderResult{
				ClientOrderID: ok.ClientOid,
				Order:         info,
			})
			continue
		}
		info.ClientOrderID = bgcommon.ChooseClientOid(ok.ClientOid, target.ClientOrderID)
		target.Order = info
	}

	var fail bgcommon.BatchFailureRow
	idx = 0
	for _, fail = range data.FailureList {
		var target *spottypes.BatchOrderResult
		if fail.ClientOid != "" {
			target = byClientOid[fail.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		var perRowErr error = bitget.NewError(
			bitget.MapBitgetCode(fail.ErrorCode, fail.ErrorMsg),
			fail.ErrorCode,
			fail.ErrorMsg,
			nil,
		)
		if target == nil {
			results = append(results, spottypes.BatchOrderResult{
				ClientOrderID: fail.ClientOid,
				Err:           perRowErr,
			})
			continue
		}
		target.Err = perRowErr
	}

	return results
}

// findResultIndex maps a *BatchOrderResult to its index inside the
// results slice. Used internally to recover the originating request
// index when the venue echoes only orderId / clientOid.
//
// Returns (index, true) on success, (-1, false) if the pointer is
// foreign to the slice (defensive).
func findResultIndex(results []spottypes.BatchOrderResult, target *spottypes.BatchOrderResult) (int, bool) {
	var i int
	for i = 0; i < len(results); i++ {
		if &results[i] == target {
			return i, true
		}
	}
	return -1, false
}
