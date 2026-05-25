/*
FILE: rate-limit-event.go

DESCRIPTION:
Public RateLimitEvent type that the SDK delivers to subscribers via
Config.RateLimitEventObserver. The observer pattern is identical to
go-bybit / go-okx: the SDK writes once per completed REST call, the desk
rate-limiter consumes events to update its model.

WHY HEADERS ARE FORWARDED 1:1:
Bitget returns rate-limit metadata headers on most signed REST responses
(X-RateLimit-Limit / X-RateLimit-Remaining / X-RateLimit-Used /
X-RateLimit-Reset / Retry-After). They are forwarded as-is so an external
rate-limiter at the desk level can reconcile its model with the live
remaining budget.

THE THREE METADATA AXES:

  1. OrderCount: 1 for single-order endpoints, len(orders) for batch
     endpoints (/api/v2/mix/order/batch-place-order,
     /api/v2/mix/order/batch-cancel-orders, etc.). Bitget accounts for
     batch budgets in orders, not requests.
  2. Symbols:    sorted unique list of symbols affected by the request.
     Bitget V2 trading limits on contracts are per (UID + Symbol);
     subscribers must debit usage to the symbols actually consumed,
     not aggregate by endpoint.
  3. Category:   "place" | "amend" | "cancel" | "query" | "market" | "".
     Used by the rate-limiter to model the sub-account-level
     NEW+AMEND budget separately from cancellations and queries.
*/

package bitget

// RateLimitCategory classifies a REST call from the rate-limit model
// perspective. Used by external rate-limiters to distribute usage across
// different limit planes.
type RateLimitCategory string

const (
	// RateLimitCategoryPlace — order creation. Endpoints:
	// /api/v2/mix/order/place-order, /api/v2/mix/order/batch-place-order, ...
	RateLimitCategoryPlace RateLimitCategory = "place"

	// RateLimitCategoryAmend — order modification. Endpoints:
	// /api/v2/mix/order/modify-order, /api/v2/mix/order/batch-modify-orders.
	RateLimitCategoryAmend RateLimitCategory = "amend"

	// RateLimitCategoryCancel — order cancellation. Endpoints:
	// /api/v2/mix/order/cancel-order, /api/v2/mix/order/batch-cancel-orders,
	// /api/v2/mix/order/cancel-all-orders.
	RateLimitCategoryCancel RateLimitCategory = "cancel"

	// RateLimitCategoryQuery — private GET / non-trading POST. Endpoints:
	// /api/v2/mix/order/orders-pending, /api/v2/mix/position/single-position,
	// /api/v2/mix/account/account, /api/v2/mix/account/set-leverage, etc.
	RateLimitCategoryQuery RateLimitCategory = "query"

	// RateLimitCategoryMarketData — public GET (per-IP limits). Endpoints:
	// /api/v2/mix/market/depth, /api/v2/mix/market/candles, ...
	RateLimitCategoryMarketData RateLimitCategory = "market"

	// RateLimitCategoryUnknown — fallback for requests not covered by any
	// explicit category. Treat as Query for safety in subscribers.
	RateLimitCategoryUnknown RateLimitCategory = ""
)

// String returns the string representation.
func (c RateLimitCategory) String() string { return string(c) }

// RateLimitEvent is the structured event delivered to
// Config.RateLimitEventObserver. The SDK emits exactly one event per
// completed REST call (whether successful or rejected at the application
// layer). Network-only failures (timeout before any HTTP response) do
// NOT trigger the observer.
type RateLimitEvent struct {
	// Endpoint — request path in canonical form (e.g.
	// "/api/v2/mix/order/place-order"). Never empty.
	Endpoint string

	// Method — HTTP method in upper-case ("GET", "POST", ...).
	Method string

	// Headers — selected rate-limit headers from the response. Populated
	// from the X-RateLimit-* family when present:
	//
	//   X-RateLimit-Limit     (max requests in current window)
	//   X-RateLimit-Remaining (remaining requests)
	//   X-RateLimit-Used      (requests used so far)
	//   X-RateLimit-Reset     (window reset timestamp)
	//   Retry-After           (set on 429 responses)
	//
	// May be empty for public endpoints that do not advertise per-UID
	// limits. Always non-nil.
	Headers map[string]string

	// OrderCount — number of orders this request creates / amends /
	// cancels:
	//   - 1 for single-order endpoints;
	//   - len(orders) for batch endpoints;
	//   - 0 for non-trading queries.
	OrderCount int

	// Symbols — sorted unique list of symbols affected. Empty for
	// account-level queries (wallet balance, fee rates, etc.).
	Symbols []string

	// Category — see RateLimitCategory.
	Category RateLimitCategory
}
