/*
FILE: internal/bgcommon/pagination.go

DESCRIPTION:
Profile-agnostic cursor-pagination helper for Bitget V2 GET endpoints
that follow the `idLessThan = endId` model:

  - mix:  /api/v2/mix/order/orders-pending
  - spot: /api/v2/spot/trade/unfilled-orders
  - spot: /api/v2/spot/trade/history-orders
  - spot: /api/v2/spot/trade/fills
  - (uta will use the same pattern when v2.5 lands)

The helper accumulates rows across pages with a hard ceiling
(`OrdersMaxPages * OrdersPageLimit`) so a buggy or adversarial cursor
echo cannot loop forever — hitting the ceiling surfaces a typed error
the caller can react to (narrow filter, or swap to streaming
reconciliation).

CONCURRENCY:

The helper is single-goroutine on purpose: cursor pagination is
inherently serial (each page depends on the previous page's `endId`).
Callers that want to fan-out across symbols / time-windows compose
multiple PaginateByCursor calls, one per partition.

ERROR DISCRIMINATION:

The fetch closure returns errors verbatim — the helper does NOT wrap
them, so the caller's typed `*bgerr.Error` propagates unchanged
(important for the desk's retry decision tree on 429 / 401 / 5xx).
The ceiling-hit error IS wrapped here, and uses the caller-supplied
`label` for the diagnostic message — typically the qualified method
path ("mix.Account.GetOpenOrders", "spot.Account.GetOrderHistory").
The wording mirrors the v1.0 message so existing tests / log greps
keep working byte-stable.
*/

package bgcommon

import (
	"context"
	"strconv"

	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
)

// OrdersPageLimit is the per-call cap on Bitget V2 paged order /
// fill endpoints. Pinned to 100 (the venue maximum) to minimise
// round-trips. Larger values are silently clamped server-side.
const OrdersPageLimit = 100

// OrdersMaxPages is the hard ceiling on internal pagination. 1000
// rows covers every realistic market-making book; if the desk hits
// it we want a typed error rather than an infinite loop on a buggy
// `endId` echo.
const OrdersMaxPages = 10

// PaginateByCursor walks the standard Bitget V2 cursor protocol:
//
//   - call `fetch("", OrdersPageLimit)` for page 0;
//   - for each subsequent page, call `fetch(endID, OrdersPageLimit)`
//     where `endID` is the value the previous page returned.
//
// Stops as soon as the server says "no more" (empty result) or the
// last page has fewer rows than the requested limit. Returns
// ErrorKindUnknown wrapped in bgerr.Error if OrdersMaxPages is hit
// (defensive — runaway cursor protection).
//
// `label` is the caller-qualified method path used in diagnostic
// messages so the same helper can serve every profile without losing
// context (e.g. "spot.Account.GetOpenOrders").
func PaginateByCursor[T any](
	ctx context.Context,
	label string,
	fetch func(idLessThan string, limit int) (rows []T, nextEndID string, err error),
) ([]T, error) {
	var out []T
	var idLessThan string
	var page int

	for page = 0; page < OrdersMaxPages; page++ {
		// Honour ctx cancellation between pages — the per-page fetch
		// closure already plumbs ctx into the HTTP call, but checking
		// here lets us abort cleanly even if the closure decides to
		// retry internally.
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}

		var rows []T
		var nextEndID string
		var err error
		rows, nextEndID, err = fetch(idLessThan, OrdersPageLimit)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		out = append(out, rows...)

		// Stop conditions:
		//   - server says "no more" via empty endId;
		//   - last page returned fewer than the requested limit (no
		//     point asking again).
		if nextEndID == "" || len(rows) < OrdersPageLimit {
			break
		}
		idLessThan = nextEndID
	}

	if page == OrdersMaxPages {
		return nil, bgerr.New(bgerr.ErrorKindUnknown, "",
			label+": pagination ceiling hit ("+strconv.Itoa(OrdersMaxPages)+
				" pages of "+strconv.Itoa(OrdersPageLimit)+
				" orders); narrow by symbol or use streaming reconciliation", nil)
	}
	return out, nil
}
