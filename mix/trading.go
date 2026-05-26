/*
FILE: mix/trading.go

DESCRIPTION:
Trading sub-client for Bitget MIX (legacy V2). Implements the four
single-order endpoints, the three batch-order endpoints, and the
global cancel-all:

	POST /api/v2/mix/order/place-order         — CreateOrder
	POST /api/v2/mix/order/modify-order        — ModifyOrder
	POST /api/v2/mix/order/cancel-order        — CancelOrder
	POST /api/v2/mix/order/batch-place-order   — CreateBatchOrders
	POST /api/v2/mix/order/batch-modify-order  — ModifyBatchOrders
	POST /api/v2/mix/order/batch-cancel-orders — CancelBatchOrders
	POST /api/v2/mix/order/cancel-all-orders   — CancelAllOrders

PINNED SETTINGS:
Every request carries productType, marginMode and marginCoin from the
parent mix.Client (see mix/client.go). Callers configure them once via
NewClientWithSettings and then use the trading methods without thinking
about the venue-specific knobs.

CLIENT-SIDE VALIDATION (fail-fast before the wire):

  - Symbol non-empty.
  - Quantity > 0 on Create / Modify (Modify accepts only-price changes
    with NewQuantity left at zero).
  - Price > 0 for limit orders. Market orders ignore Price.
  - At least one of (NewQuantity, NewPrice) on Modify; at least one of
    (OrderID, ClientOrderID) on Cancel/Modify.
  - Batch size 1..50 on every batch endpoint (Bitget V2 cap, see
    https://www.bitget.com/api-doc/contract/trade/Batch-Place-Order).

RATE-LIMIT META:
Each call sets RequestMeta with the right RateLimitCategory
(place/amend/cancel) and OrderCount. Symbols carry the affected
symbols (single-symbol on Create/Modify/Cancel; collected from the
batch on Batch*). The rate-limiter strategy at the desk reads this via
Config.RateLimitEventObserver.

NEW CLIENT-ORDER-ID ON MODIFY:
Bitget MIX requires a NEW clientOid on every modify-order — the old
one cannot be reused (server returns code=40786 "Duplicate clientOid"
otherwise; observed in PARTIUSDT field session). ModifyOrderRequest
exposes a dedicated NewClientOrderID field; when the caller leaves
it blank the SDK auto-generates a `m-<16-hex>` token via crypto/rand
so the modify always succeeds. The existing ClientOrderID is sent
as the `clientOid` identifier of the order being amended (when
OrderID is empty).

NO BATCH-MODIFY ON BITGET V2:
Despite the symmetric naming, Bitget V2 has NEVER shipped a
`batch-modify-order` endpoint for MIX — the URL returns HTTP 404
with code=40404 "Request URL NOT FOUND" (verified in production,
confirmed against tiagosiebler/bitget-api and the official docs
which list batch-place / batch-cancel only). Batch modify exists
only on the V3 (UTA) trade API. ModifyBatchOrders therefore
fails fast with ErrorKindInvalidRequest and a clear remediation
hint — issue per-order ModifyOrder calls in a loop, or
cancel-then-place if the modify pattern is unsupported by the
caller's strategy. Saves a needless 404 round-trip.

CANCEL-ALL SCOPE:
POST /api/v2/mix/order/cancel-all-orders is GLOBAL by productType +
marginCoin — Bitget V2 does NOT accept a `symbol` filter on this
endpoint. To preserve the desk's CancelAllOrders(symbol) ergonomics
we treat:

  - symbol == ""    → cancel-all-orders (global within product type);
  - symbol != ""    → ErrorKindInvalidRequest with a clear message.
    The desk caller is expected to enumerate open orders for the
    symbol and call CancelBatchOrders instead.
*/

package mix

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
	mixtypes "github.com/tonymontanov/go-bitget/v2/mix/types"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// timeNowNano returns the current monotonic time in nanoseconds.
// Indirected through a var so tests can stub determinism if needed.
var timeNowNano = func() int64 { return time.Now().UnixNano() }

// genNewClientOid produces a venue-acceptable clientOid for the
// modified order when the caller did not provide one. Format
// `m-<32-hex>` (34 chars total, well under Bitget's 50-char cap).
//
// Why crypto/rand: this is the only generator already linked into
// the standard library that gives a collision-resistant token
// without pulling a UUID dep. The hex output is monotonically safe
// across goroutines (no shared state) and deterministic-shaped, so
// log greps and idempotency caches stay simple.
//
// On the (extremely unlikely) read failure from /dev/urandom we
// degrade to a timestamp-derived ID rather than returning an error
// — modify must not refuse to ship just because the OS RNG
// momentarily glitched.
func genNewClientOid() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback: nanoseconds-since-epoch hex; still unique enough
		// for clientOid purposes, and we never silently drop the
		// modify request.
		return "m-" + strconv.FormatInt(timeNowNano(), 16)
	}
	return "m-" + hex.EncodeToString(buf[:])
}

// maxBatchSize — Bitget V2 cap on batch-place-order /
// batch-modify-order / batch-cancel-orders. Enforced client-side so
// requests never round-trip to be rejected on the server.
const maxBatchSize = 50

// TradingClient — trading sub-client.
type TradingClient struct {
	c *Client
}

func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{c: c}
}

// ---------------------------------------------------------------------
// Single-order endpoints.
// ---------------------------------------------------------------------

// placeOrderBody mirrors the JSON body Bitget V2 expects on
// POST /api/v2/mix/order/place-order. Optional fields are emitted as
// empty strings; Bitget treats `force=""` as "use the default for
// this orderType".
type placeOrderBody struct {
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
	MarginMode  string `json:"marginMode"`
	MarginCoin  string `json:"marginCoin,omitempty"`
	Size        string `json:"size"`
	Price       string `json:"price,omitempty"`
	Side        string `json:"side"`
	TradeSide   string `json:"tradeSide,omitempty"`
	OrderType   string `json:"orderType"`
	Force       string `json:"force,omitempty"`
	ClientOid   string `json:"clientOid,omitempty"`
	ReduceOnly  string `json:"reduceOnly,omitempty"`
}

// placeOrderResp is the JSON `data` returned by place-order on success.
type placeOrderResp struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// CreateOrder submits one MIX order. The returned OrderInfo is built
// from the response payload (orderId + clientOid) plus the request
// itself — Bitget does not echo the full lifecycle here, M3's
// queries are the source of truth for fill / status.
func (t *TradingClient) CreateOrder(ctx context.Context, req mixtypes.CreateOrderRequest) (mixtypes.OrderInfo, error) {
	var out mixtypes.OrderInfo
	var err error
	if err = validateCreateOrderRequest(req); err != nil {
		return out, err
	}

	var body placeOrderBody
	body, err = t.buildPlaceBody(req)
	if err != nil {
		return out, err
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/place-order",
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
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Trading.CreateOrder: parse", err)
	}
	return mixtypes.OrderInfo{
		OrderID:       data.OrderID,
		ClientOrderID: chooseClientOid(data.ClientOid, req.ClientOrderID),
		Symbol:        req.Symbol,
		Side:          req.Side,
		TradeSide:     req.TradeSide,
		OrderType:     req.OrderType,
		TimeInForce:   req.TimeInForce,
		Status:        roottypes.OrderStatusLive,
		Quantity:      req.Quantity,
		Price:         req.Price,
	}, nil
}

// modifyOrderBody mirrors the JSON body of POST
// /api/v2/mix/order/modify-order. NewClientOid is REQUIRED by Bitget
// (the old clientOid cannot be reused), so the SDK fails fast if the
// caller did not supply one.
type modifyOrderBody struct {
	Symbol       string `json:"symbol"`
	ProductType  string `json:"productType"`
	OrderID      string `json:"orderId,omitempty"`
	ClientOid    string `json:"clientOid,omitempty"`
	NewClientOid string `json:"newClientOid"`
	NewSize      string `json:"newSize,omitempty"`
	NewPrice     string `json:"newPrice,omitempty"`
}

// modifyOrderResp is the JSON `data` returned by modify-order. Bitget
// echoes the new orderId / clientOid (the modify is implemented as a
// cancel-replace at the matcher level, hence a NEW orderId is
// returned).
type modifyOrderResp struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// ModifyOrder amends size and / or price on an open MIX order.
//
// Identification: either OrderID or ClientOrderID points at the
// existing order. If both are populated, OrderID wins (Bitget's
// documented precedence rule).
//
// New clientOid: req.NewClientOrderID flows through as the venue's
// `newClientOid`. If the caller leaves it empty the SDK auto-fills
// a `m-<32-hex>` token via crypto/rand — Bitget V2 mandates
// newClientOid to be non-empty AND distinct from the existing
// clientOid (else code=40786 "Duplicate clientOid"; observed in
// PARTIUSDT field session under v1.0.3 when the SDK reused
// req.ClientOrderID for both fields).
func (t *TradingClient) ModifyOrder(ctx context.Context, req mixtypes.ModifyOrderRequest) (mixtypes.OrderInfo, error) {
	var out mixtypes.OrderInfo
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
		// will reject this with 40786. We could regen silently but
		// surfacing the misuse early is clearer than a confusing
		// server-side rejection two RTTs from now.
		return out, bitget.NewError(
			bitget.ErrorKindInvalidRequest, "",
			"mix.Trading.ModifyOrder: NewClientOrderID must differ from ClientOrderID (Bitget rejects with code=40786 otherwise)",
			nil,
		)
	}

	var body modifyOrderBody = modifyOrderBody{
		Symbol:       req.Symbol,
		ProductType:  string(t.c.productType),
		OrderID:      req.OrderID,
		ClientOid:    req.ClientOrderID,
		NewClientOid: newClientOid,
	}
	if !req.NewQuantity.IsZero() {
		body.NewSize = req.NewQuantity.String()
	}
	if !req.NewPrice.IsZero() {
		body.NewPrice = req.NewPrice.String()
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/modify-order",
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
		return out, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Trading.ModifyOrder: parse", err)
	}
	return mixtypes.OrderInfo{
		OrderID:       data.OrderID,
		ClientOrderID: chooseClientOid(data.ClientOid, newClientOid),
		Symbol:        req.Symbol,
		Status:        roottypes.OrderStatusLive,
		Quantity:      req.NewQuantity,
		Price:         req.NewPrice,
	}, nil
}

// cancelOrderBody mirrors the JSON body of POST
// /api/v2/mix/order/cancel-order. Either orderId or clientOid is
// required; if both are present, Bitget gives orderId priority.
type cancelOrderBody struct {
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
	MarginCoin  string `json:"marginCoin,omitempty"`
	OrderID     string `json:"orderId,omitempty"`
	ClientOid   string `json:"clientOid,omitempty"`
}

// CancelOrder cancels one MIX order. Returns nil on success, error on
// any rejection (including "order not found", which Bitget reports as
// a typed code that maps to ErrorKindInvalidRequest /
// ErrorKindExchange depending on the exact code).
func (t *TradingClient) CancelOrder(ctx context.Context, req roottypes.CancelOrderRequest) error {
	var err error
	if err = validateCancelOrderRequest(req); err != nil {
		return err
	}

	var body cancelOrderBody = cancelOrderBody{
		Symbol:      req.Symbol,
		ProductType: string(t.c.productType),
		MarginCoin:  t.c.marginCoin,
		OrderID:     req.OrderID,
		ClientOid:   req.ClientOrderID,
	}
	_, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/cancel-order",
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
// Batch endpoints.
// ---------------------------------------------------------------------

// batchPlaceOrderBody is the wire payload for batch-place-order.
// Bitget requires productType + marginMode + marginCoin at the top
// level (not per-row); orderList holds the per-row variants.
type batchPlaceOrderBody struct {
	ProductType string                  `json:"productType"`
	MarginMode  string                  `json:"marginMode"`
	MarginCoin  string                  `json:"marginCoin,omitempty"`
	Symbol      string                  `json:"symbol"`
	OrderList   []batchPlaceOrderEntry  `json:"orderList"`
}

// batchPlaceOrderEntry is one row of orderList in batch-place-order.
type batchPlaceOrderEntry struct {
	Size       string `json:"size"`
	Price      string `json:"price,omitempty"`
	Side       string `json:"side"`
	TradeSide  string `json:"tradeSide,omitempty"`
	OrderType  string `json:"orderType"`
	Force      string `json:"force,omitempty"`
	ClientOid  string `json:"clientOid,omitempty"`
	ReduceOnly string `json:"reduceOnly,omitempty"`
}

// batchOrderResp mirrors the standard envelope used by every batch
// trading endpoint: parallel successList / failureList of per-row
// outcomes.
type batchOrderResp struct {
	SuccessList []batchOrderSuccess `json:"successList"`
	FailureList []batchOrderFailure `json:"failureList"`
}

type batchOrderSuccess struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

type batchOrderFailure struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
	ErrorMsg  string `json:"errorMsg"`
	ErrorCode string `json:"errorCode"`
}

/*
CreateBatchOrders submits a batch of MIX orders.

CONSTRAINTS:
  - 1 <= len(reqs) <= 50.
  - All rows MUST share the same Symbol — Bitget V2 batch-place-order
    is per-symbol (the symbol is at the top level, not per-row). The
    SDK validates this client-side.

The returned slice has the same length as reqs and is ordered to
match: every input row maps 1-to-1 to an output entry. Successful
rows have Order set (with OrderID + ClientOrderID populated); failed
rows have Err set with a typed *bitget.Error. ClientOrderID is
populated on every row regardless of success.
*/
func (t *TradingClient) CreateBatchOrders(ctx context.Context, reqs []mixtypes.CreateOrderRequest) ([]mixtypes.BatchOrderResult, error) {
	var err error
	if err = validateBatchSize("CreateBatchOrders", len(reqs)); err != nil {
		return nil, err
	}

	var symbol string = reqs[0].Symbol
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateCreateOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading.CreateBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		if reqs[i].Symbol != symbol {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading.CreateBatchOrders: all rows must share the same symbol (row 0="+symbol+", row "+strconv.Itoa(i)+"="+reqs[i].Symbol+")", nil)
		}
	}

	var body batchPlaceOrderBody = batchPlaceOrderBody{
		ProductType: string(t.c.productType),
		MarginMode:  string(t.c.marginMode),
		MarginCoin:  t.c.marginCoin,
		Symbol:      symbol,
		OrderList:   make([]batchPlaceOrderEntry, len(reqs)),
	}
	for i = 0; i < len(reqs); i++ {
		body.OrderList[i] = buildBatchPlaceEntry(reqs[i])
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/batch-place-order",
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

	var data batchOrderResp
	if err = resp.UnmarshalData(&data); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Trading.CreateBatchOrders: parse", err)
	}

	var clientOids []string = make([]string, len(reqs))
	for i = 0; i < len(reqs); i++ {
		clientOids[i] = reqs[i].ClientOrderID
	}
	return collateBatchResultsFromCreate(reqs, clientOids, data, symbol), nil
}

/*
ModifyBatchOrders is a no-op stub on Bitget V2: the venue has never
shipped a `/api/v2/mix/order/batch-modify-order` endpoint. The URL
returns HTTP 404 + code=40404 "Request URL NOT FOUND" (verified in
production logs and against the official spec
https://www.bitget.com/api-doc/contract/trade/Modify-Order, which
lists batch-place / batch-cancel only — not batch-modify). The
batch-modify capability lives on the V3 (UTA) trade API.

Rather than serialise a request that we KNOW will round-trip to a
404, this method returns an explicit ErrorKindInvalidRequest with
remediation hints so the desk-side connector can fall back cleanly
(loop of single ModifyOrder calls, or cancel-then-place). The
behavioural contract from the caller's perspective is "this batch
of modifications failed end-to-end" — same shape it would see if
the server actually did return 40404, but two RTTs faster.

When/if Bitget extends V2 with a real batch-modify endpoint we'll
restore the wire body + resp parsing. For now keeping this method
on the surface (rather than deleting it) means the desk-core
ConnectorWrapper interface stays stable across the V2/V3 cut-over.
*/
func (t *TradingClient) ModifyBatchOrders(_ context.Context, reqs []mixtypes.ModifyOrderRequest) ([]mixtypes.BatchOrderResult, error) {
	// Validate inputs anyway — gives callers consistent errors when
	// they pass garbage, and surfaces the unsupported-by-venue path
	// cleanly when inputs are well-formed.
	var err error
	if err = validateBatchSize("ModifyBatchOrders", len(reqs)); err != nil {
		return nil, err
	}
	var symbol string = reqs[0].Symbol
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateModifyOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading.ModifyBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		if reqs[i].Symbol != symbol {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading.ModifyBatchOrders: all rows must share the same symbol (row 0="+symbol+", row "+strconv.Itoa(i)+"="+reqs[i].Symbol+")", nil)
		}
	}
	return nil, bitget.NewError(
		bitget.ErrorKindInvalidRequest, "",
		"mix.Trading.ModifyBatchOrders: Bitget V2 does not support batch order modification "+
			"(POST /api/v2/mix/order/batch-modify-order returns HTTP 404 / code=40404 in production); "+
			"call ModifyOrder per row, or use CancelBatchOrders + CreateBatchOrders",
		nil,
	)
}

// batchCancelOrderBody is the wire payload for batch-cancel-orders.
type batchCancelOrderBody struct {
	ProductType string                  `json:"productType"`
	Symbol      string                  `json:"symbol"`
	MarginCoin  string                  `json:"marginCoin,omitempty"`
	OrderIDList []batchCancelOrderEntry `json:"orderIdList"`
}

type batchCancelOrderEntry struct {
	OrderID   string `json:"orderId,omitempty"`
	ClientOid string `json:"clientOid,omitempty"`
}

/*
CancelBatchOrders cancels a batch of MIX orders. Same shape contract
as the other batch methods (per-symbol, 1..50 rows). Each row needs
exactly one of (OrderID, ClientOrderID).

NOTE: Bitget V2 batch-cancel rejects mixing orderId-based and
clientOid-based rows in the same call (the spot endpoint enforces this
strictly; mix tolerates it for now but the docs warn against it). The
SDK does not enforce homogeneity client-side — callers can mix
identifiers if they know what they want.
*/
func (t *TradingClient) CancelBatchOrders(ctx context.Context, reqs []roottypes.CancelOrderRequest) ([]mixtypes.BatchOrderResult, error) {
	var err error
	if err = validateBatchSize("CancelBatchOrders", len(reqs)); err != nil {
		return nil, err
	}

	var symbol string = reqs[0].Symbol
	var i int
	for i = 0; i < len(reqs); i++ {
		if err = validateCancelOrderRequest(reqs[i]); err != nil {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading.CancelBatchOrders["+strconv.Itoa(i)+"]: "+err.Error(), nil)
		}
		if reqs[i].Symbol != symbol {
			return nil, bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading.CancelBatchOrders: all rows must share the same symbol (row 0="+symbol+", row "+strconv.Itoa(i)+"="+reqs[i].Symbol+")", nil)
		}
	}

	var body batchCancelOrderBody = batchCancelOrderBody{
		ProductType: string(t.c.productType),
		Symbol:      symbol,
		MarginCoin:  t.c.marginCoin,
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
		Path:   "/api/v2/mix/order/batch-cancel-orders",
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

	var data batchOrderResp
	if err = resp.UnmarshalData(&data); err != nil {
		return nil, bitget.NewError(bitget.ErrorKindUnknown, "", "mix.Trading.CancelBatchOrders: parse", err)
	}

	var clientOids []string = make([]string, len(reqs))
	for i = 0; i < len(reqs); i++ {
		clientOids[i] = reqs[i].ClientOrderID
	}
	return collateBatchResultsFromCancel(reqs, clientOids, data, symbol), nil
}

// cancelAllBody is the wire payload for cancel-all-orders.
// Bitget V2 takes only productType + marginCoin at top-level — no
// per-symbol filter is supported.
type cancelAllBody struct {
	ProductType string `json:"productType"`
	MarginCoin  string `json:"marginCoin,omitempty"`
}

/*
CancelAllOrders cancels every open MIX order under the configured
productType (and marginCoin, when pinned).

  - symbol == ""    → cancel-all-orders, the genuine venue-level
    "cancel everything" call.
  - symbol != ""    → ErrorKindInvalidRequest. Bitget V2 does NOT
    accept a symbol filter on this endpoint; callers that want a
    per-symbol cancel should query open orders for the symbol and
    use CancelBatchOrders.

The response carries successList / failureList; the SDK collapses
them into a slice the caller can ignore on success-only paths. If the
caller cares about per-row outcomes (e.g. for logging), they can
prefer CancelBatchOrders.
*/
func (t *TradingClient) CancelAllOrders(ctx context.Context, symbol string) error {
	if symbol != "" {
		return bitget.NewError(
			bitget.ErrorKindInvalidRequest, "",
			"mix.Trading.CancelAllOrders: per-symbol cancel is not supported by Bitget V2 cancel-all-orders endpoint; use CancelBatchOrders after enumerating open orders for "+symbol,
			nil,
		)
	}

	var body cancelAllBody = cancelAllBody{
		ProductType: string(t.c.productType),
		MarginCoin:  t.c.marginCoin,
	}
	var _, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/cancel-all-orders",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 0,
			Category:   string(bitget.RateLimitCategoryCancel),
		},
	})
	return err
}

// ---------------------------------------------------------------------
// Helpers — request building.
// ---------------------------------------------------------------------

// buildPlaceBody assembles the wire body for a single place-order
// call from the typed request and the parent client's pinned settings.
func (t *TradingClient) buildPlaceBody(req mixtypes.CreateOrderRequest) (placeOrderBody, error) {
	var body placeOrderBody = placeOrderBody{
		Symbol:      req.Symbol,
		ProductType: string(t.c.productType),
		MarginMode:  string(t.c.marginMode),
		MarginCoin:  t.c.marginCoin,
		Size:        req.Quantity.String(),
		Side:        string(req.Side),
		OrderType:   string(req.OrderType),
		Force:       string(req.TimeInForce),
		ClientOid:   req.ClientOrderID,
		TradeSide:   string(req.TradeSide),
	}
	if req.OrderType == roottypes.OrderTypeLimit {
		body.Price = req.Price.String()
	}
	if req.ReduceOnly {
		body.ReduceOnly = "YES"
	}
	return body, nil
}

// buildBatchPlaceEntry mirrors buildPlaceBody but produces a
// per-row entry for batch-place-order (no symbol / productType /
// marginMode / marginCoin — those are at the top level).
func buildBatchPlaceEntry(req mixtypes.CreateOrderRequest) batchPlaceOrderEntry {
	var entry batchPlaceOrderEntry = batchPlaceOrderEntry{
		Size:      req.Quantity.String(),
		Side:      string(req.Side),
		OrderType: string(req.OrderType),
		Force:     string(req.TimeInForce),
		ClientOid: req.ClientOrderID,
		TradeSide: string(req.TradeSide),
	}
	if req.OrderType == roottypes.OrderTypeLimit {
		entry.Price = req.Price.String()
	}
	if req.ReduceOnly {
		entry.ReduceOnly = "YES"
	}
	return entry
}

// chooseClientOid returns the venue-echoed clientOid when present,
// falling back to the request's. Bitget always echoes back the value
// it accepted, so this is mostly a safety net for fixtures with empty
// strings.
func chooseClientOid(fromVenue, fromRequest string) string {
	if fromVenue != "" {
		return fromVenue
	}
	return fromRequest
}

// ---------------------------------------------------------------------
// Helpers — validation.
// ---------------------------------------------------------------------

func validateCreateOrderRequest(req mixtypes.CreateOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: symbol is empty", nil)
	}
	if req.Side == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: side is empty", nil)
	}
	if req.OrderType == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: orderType is empty", nil)
	}
	if !decimalIsPositive(req.Quantity) {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: quantity must be > 0", nil)
	}
	if req.OrderType == roottypes.OrderTypeLimit && !decimalIsPositive(req.Price) {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: price must be > 0 for limit orders", nil)
	}
	return nil
}

func validateModifyOrderRequest(req mixtypes.ModifyOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: symbol is empty", nil)
	}
	if req.OrderID == "" && req.ClientOrderID == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: either orderId or clientOrderId is required", nil)
	}
	if req.NewQuantity.IsZero() && req.NewPrice.IsZero() {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: at least one of newQuantity or newPrice must be set", nil)
	}
	return nil
}

func validateCancelOrderRequest(req roottypes.CancelOrderRequest) error {
	if req.Symbol == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: symbol is empty", nil)
	}
	if req.OrderID == "" && req.ClientOrderID == "" {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading: either orderId or clientOrderId is required", nil)
	}
	return nil
}

func validateBatchSize(method string, n int) error {
	if n == 0 {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading."+method+": empty request slice", nil)
	}
	if n > maxBatchSize {
		return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "mix.Trading."+method+": batch size "+strconv.Itoa(n)+" exceeds Bitget V2 cap of "+strconv.Itoa(maxBatchSize), nil)
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
collateBatchResultsFromCreate maps Bitget's parallel
successList / failureList into the SDK's per-row BatchOrderResult
slice, ordered to match the original request slice.

PAIRING STRATEGY:
Bitget echoes back clientOid on every row of both lists (the SDK fills
in clientOid from the request when the venue returns ""). We index the
original request slice by clientOid and use that as the join key. If a
row had no clientOid (caller didn't supply one), we fall back to a
positional join for the remainder — the test suite covers both cases.

If Bitget reports MORE rows than we asked for (defensive) the extra
rows are appended as Err-bearing results so the caller is not silently
left with wrong indices.
*/
func collateBatchResultsFromCreate(
	reqs []mixtypes.CreateOrderRequest,
	clientOids []string,
	data batchOrderResp,
	symbol string,
) []mixtypes.BatchOrderResult {
	var results []mixtypes.BatchOrderResult = make([]mixtypes.BatchOrderResult, len(reqs))
	var byClientOid map[string]*mixtypes.BatchOrderResult = map[string]*mixtypes.BatchOrderResult{}
	var positional []*mixtypes.BatchOrderResult
	var i int
	for i = 0; i < len(reqs); i++ {
		results[i] = mixtypes.BatchOrderResult{ClientOrderID: clientOids[i]}
		if clientOids[i] != "" {
			byClientOid[clientOids[i]] = &results[i]
		} else {
			positional = append(positional, &results[i])
		}
	}

	var ok batchOrderSuccess
	var idx int = 0
	for _, ok = range data.SuccessList {
		var target *mixtypes.BatchOrderResult
		if ok.ClientOid != "" {
			target = byClientOid[ok.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		if target == nil {
			results = append(results, mixtypes.BatchOrderResult{
				ClientOrderID: ok.ClientOid,
				Order: &mixtypes.OrderInfo{
					OrderID:       ok.OrderID,
					ClientOrderID: ok.ClientOid,
					Symbol:        symbol,
					Status:        roottypes.OrderStatusLive,
				},
			})
			continue
		}
		var info *mixtypes.OrderInfo = &mixtypes.OrderInfo{
			OrderID:       ok.OrderID,
			ClientOrderID: chooseClientOid(ok.ClientOid, target.ClientOrderID),
			Symbol:        symbol,
			Status:        roottypes.OrderStatusLive,
		}
		// Echo the placed quantity / price / side from the request to
		// keep results self-describing without an extra GET.
		var reqIdx int
		reqIdx, _ = findRequestIndex(reqs, results, target)
		if reqIdx >= 0 {
			info.Side = reqs[reqIdx].Side
			info.OrderType = reqs[reqIdx].OrderType
			info.TimeInForce = reqs[reqIdx].TimeInForce
			info.TradeSide = reqs[reqIdx].TradeSide
			info.Quantity = reqs[reqIdx].Quantity
			info.Price = reqs[reqIdx].Price
		}
		target.Order = info
	}

	var fail batchOrderFailure
	idx = 0
	for _, fail = range data.FailureList {
		var target *mixtypes.BatchOrderResult
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
			results = append(results, mixtypes.BatchOrderResult{
				ClientOrderID: fail.ClientOid,
				Err:           perRowErr,
			})
			continue
		}
		target.Err = perRowErr
	}

	return results
}

// collateBatchResultsFromModify previously mapped batch-modify-order
// responses; removed alongside ModifyBatchOrders in v1.1.0 because
// the endpoint does not exist on Bitget V2. Restore from git history
// once the venue ships a real batch-modify endpoint (or when the
// SDK adds the V3/UTA trade client).

// collateBatchResultsFromCancel pairs cancel-batch outcomes with the
// originating CancelOrderRequest rows. Cancellation responses do not
// carry quantity / price; the success Order contains only OrderID +
// ClientOrderID + a "cancelled" status placeholder.
func collateBatchResultsFromCancel(
	reqs []roottypes.CancelOrderRequest,
	clientOids []string,
	data batchOrderResp,
	symbol string,
) []mixtypes.BatchOrderResult {
	var results []mixtypes.BatchOrderResult = make([]mixtypes.BatchOrderResult, len(reqs))
	var byClientOid map[string]*mixtypes.BatchOrderResult = map[string]*mixtypes.BatchOrderResult{}
	var positional []*mixtypes.BatchOrderResult
	var i int
	for i = 0; i < len(reqs); i++ {
		results[i] = mixtypes.BatchOrderResult{ClientOrderID: clientOids[i]}
		if clientOids[i] != "" {
			byClientOid[clientOids[i]] = &results[i]
		} else {
			positional = append(positional, &results[i])
		}
	}

	var ok batchOrderSuccess
	var idx int = 0
	for _, ok = range data.SuccessList {
		var target *mixtypes.BatchOrderResult
		if ok.ClientOid != "" {
			target = byClientOid[ok.ClientOid]
		}
		if target == nil && idx < len(positional) {
			target = positional[idx]
			idx++
		}
		var info *mixtypes.OrderInfo = &mixtypes.OrderInfo{
			OrderID:       ok.OrderID,
			ClientOrderID: chooseClientOid(ok.ClientOid, ""),
			Symbol:        symbol,
			Status:        roottypes.OrderStatusCancelled,
		}
		if target == nil {
			results = append(results, mixtypes.BatchOrderResult{
				ClientOrderID: ok.ClientOid,
				Order:         info,
			})
			continue
		}
		info.ClientOrderID = chooseClientOid(ok.ClientOid, target.ClientOrderID)
		target.Order = info
	}

	var fail batchOrderFailure
	idx = 0
	for _, fail = range data.FailureList {
		var target *mixtypes.BatchOrderResult
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
			results = append(results, mixtypes.BatchOrderResult{
				ClientOrderID: fail.ClientOid,
				Err:           perRowErr,
			})
			continue
		}
		target.Err = perRowErr
	}

	return results
}

// findRequestIndex maps a *BatchOrderResult to its index inside the
// results slice. Used internally to recover the originating request
// index when the venue echoes only orderId / clientOid (so we can
// copy quantity / price / side back into the OrderInfo).
//
// Returns (index, true) on success, (-1, false) if pointer is foreign
// to the slice (defensive).
//
// Note: kept in a tiny helper so the collate functions stay readable.
func findRequestIndex(_ any, results []mixtypes.BatchOrderResult, target *mixtypes.BatchOrderResult) (int, bool) {
	var i int
	for i = 0; i < len(results); i++ {
		if &results[i] == target {
			return i, true
		}
	}
	return -1, false
}

