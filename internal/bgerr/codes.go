/*
FILE: internal/bgerr/codes.go

DESCRIPTION:
Mapping from raw transport-level signals (HTTP status, Bitget code) to
ErrorKind. Centralised here so the rest of the SDK never sprinkles
"if code == ..." chains.

SOURCES:
  - HTTP status mapping is the standard 4xx/5xx convention, with 401/403 as
    Auth and 429 as RateLimit.
  - Bitget V2 code tables are derived from the public docs:
        https://www.bitget.com/api-doc/spot/error-code/restapi
        https://www.bitget.com/api-doc/uta/error-code/websocket
        https://bitgetlimited.github.io/apidoc/en/broker/  (legacy reference)
    The list below is intentionally NON-exhaustive — only codes the SDK
    can usefully classify into a Kind. Anything not explicitly listed
    falls back to ErrorKindExchange (so the caller still gets an Exchange-
    class error, just without the SDK pre-classifying it).

DESIGN PRINCIPLES:
  - Keep mappings stable: this is a public contract via IsAuth /
    IsRateLimit / IsInvalidRequest / IsNetwork / IsExchange.
  - When a code is genuinely ambiguous between SDK Kind buckets (e.g.
    "balance not enough" — Exchange business rejection, NOT a build-time
    validation error), prefer ErrorKindExchange. Callers can still branch
    on BitgetCode for fine-grained handling without giving up the SDK's
    coarse classification.
  - Idempotent-success codes (e.g. 22002 "no position to close" returned
    on /set-leverage when the value already matches; 45054 "no change in
    leverage") are mapped to ErrorKindInvalidRequest. Callers that want
    to suppress them MUST inspect BitgetCode explicitly — the SDK does
    not silently turn errors into successes.

UPDATE PROCEDURE:
When Bitget publishes a new error code that the SDK should react to:
  1. Add a `case` to MapBitgetCode below, with the doc-string as a
     comment so future readers can verify the mapping.
  2. Add a covering test in codes_test.go (one row per bucket).
  3. If a code's Kind changes for an already-listed code, bump the SDK
     minor version — IsRateLimit/IsAuth callers may rely on it.
*/

package bgerr

// CodeOK is the success code Bitget returns on every 2xx response.
// Anything else is treated as an error by the REST client.
const CodeOK = "00000"

// MapHTTPStatus maps an HTTP status code to an ErrorKind.
//
// 2xx is a success and is not expected to be passed here.
// 401/403 → Auth (key/IP/permission).
// 429     → RateLimit.
// 4xx     → InvalidRequest (the SDK or caller built a bad request).
// 5xx     → Network (transient at the network/exchange edge — retryable).
// other   → Unknown.
func MapHTTPStatus(status int) ErrorKind {
	switch {
	case status == 401, status == 403:
		return ErrorKindAuth
	case status == 429:
		return ErrorKindRateLimit
	case status >= 400 && status < 500:
		return ErrorKindInvalidRequest
	case status >= 500 && status < 600:
		return ErrorKindNetwork
	default:
		return ErrorKindUnknown
	}
}

// MapBitgetCode maps a Bitget error code to an ErrorKind. msg is currently
// unused but kept in the signature so future heuristics (e.g. parsing
// "Too many requests" out of msg) do not break callers.
//
// The function operates on the string form of the code (Bitget returns it
// as a string in JSON: "00000" / "40768" / etc.).
//
// Codes are grouped by family:
//
//   - 22xxx  — futures lifecycle (cancel/close on empty state, modify
//     ergonomics, derivative-specific symbol/cross-mode rejects).
//   - 40000-40099 — auth, signature, IP, passphrase, top-level account
//     state.
//   - 40300-40499 — generic param formatting (clientOid length, batch
//     size, orderId format).
//   - 40700-40799 — derivatives runtime (order-book depth, balance,
//     position size, reduce-only conflicts).
//   - 40900-40999 — derivatives operational (position mode switch,
//     amend ergonomics, ID validation).
//   - 43xxx  — order lifecycle (REST order CRUD on derivatives + spot).
//   - 45xxx  — risk/quantity/price/leverage validation (largest family).
//   - 47xxx  — withdrawal/wallet rate-limit-adjacent.
//   - 50xxx  — wallet/balance + cross/isolated mode hints.
//   - 59xxx  — Bitget Earn / financial products (rare in HFT but common
//     in onboarding dry-runs).
//
// Anything outside the explicitly listed codes maps to ErrorKindExchange:
// the SDK saw a non-success code, but does not pre-classify it.
//
//nolint:gocyclo,funlen // table-driven mapping with one case per code; a
// switch is the right shape and matches the structure of Bitget's docs.
func MapBitgetCode(code, _ string) ErrorKind {
	switch code {
	// ----- success -----
	case CodeOK:
		return ErrorKindUnknown // success — must not be passed here, but be safe

	// ----- 22xxx derivatives lifecycle -----
	case "22001":
		// No order to cancel — caller asked us to cancel an order that
		// either never existed or has already been removed.
		return ErrorKindInvalidRequest
	case "22002":
		// No position to close — also returned on /set-leverage when the
		// new leverage equals the current value, i.e. the operation is
		// a no-op. desk-core treats this as idempotent success.
		return ErrorKindInvalidRequest
	case "22003":
		// modify price and size, please pass in newClientOid
		return ErrorKindInvalidRequest
	case "22004":
		// This symbol does not support API trade
		return ErrorKindInvalidRequest
	case "22005":
		// This symbol does not support cross mode
		return ErrorKindInvalidRequest
	case "22010":
		// Please bind ip whitelist address
		return ErrorKindAuth
	case "22034":
		// Less than the minimum order amount
		return ErrorKindInvalidRequest
	case "22038":
		// Please enter the quantity as an integral multiple of {0}
		return ErrorKindInvalidRequest
	case "22045":
		// Insufficient liquidity in the market — exchange-side rejection.
		return ErrorKindExchange

	// ----- 40000-40099 auth / signature / params / rate limit -----
	case "40001":
		// ACCESS_KEY cannot be empty
		return ErrorKindAuth
	case "40002":
		// SECRET_KEY / ACCESS_SIGN cannot be empty
		return ErrorKindAuth
	case "40003":
		// Signature cannot be empty
		return ErrorKindAuth
	case "40004":
		// Request timestamp expired
		return ErrorKindAuth
	case "40005":
		// Invalid ACCESS_TIMESTAMP
		return ErrorKindAuth
	case "40006":
		// Invalid ACCESS_KEY
		return ErrorKindAuth
	case "40007":
		// Invalid Content_Type — caller built a bad request
		return ErrorKindInvalidRequest
	case "40008":
		// Request timestamp expired (recv-window equivalent)
		return ErrorKindAuth
	case "40009":
		// sign signature error
		return ErrorKindAuth
	case "40010":
		// Request timed out
		return ErrorKindNetwork
	case "40011":
		// ACCESS_PASSPHRASE cannot be empty
		return ErrorKindAuth
	case "40012":
		// apikey/password is incorrect
		return ErrorKindAuth
	case "40013":
		// User status is abnormal
		return ErrorKindAuth
	case "40014":
		// Incorrect permissions, need {0} permissions
		return ErrorKindAuth
	case "40015":
		// System is abnormal — typically transient, retryable
		return ErrorKindNetwork
	case "40016":
		// User must bind phone / Google authenticator
		return ErrorKindAuth
	case "40017":
		// Parameter verification failed
		return ErrorKindInvalidRequest
	case "40018":
		// Invalid IP / IP not in whitelist
		return ErrorKindAuth
	case "40019":
		// Parameter {0} cannot be empty
		return ErrorKindInvalidRequest
	case "40020":
		// Parameter {0} error
		return ErrorKindInvalidRequest
	case "40021":
		// User disable withdraw / Invalid Symbol (legacy alias)
		return ErrorKindInvalidRequest
	case "40022":
		// The business of this account has been restricted
		return ErrorKindAuth
	case "40023":
		// API service has been restricted
		return ErrorKindAuth
	case "40024":
		// Account has been frozen
		return ErrorKindAuth
	case "40025":
		// The user does not have this permission
		return ErrorKindAuth
	case "40026":
		// User is disabled
		return ErrorKindAuth
	case "40029":
		// Exceeded rate limit (legacy generic rate-limit code)
		return ErrorKindRateLimit
	case "40034":
		// Parameter {0} does not exist / generic parameter error
		return ErrorKindInvalidRequest
	case "40036":
		// passphrase is error
		return ErrorKindAuth
	case "40037":
		// Apikey does not exist (Auth — NOT "Order does not exist";
		// "Order does not exist" is 40768 in V2 contract docs).
		return ErrorKindAuth
	case "40038":
		// The current ip is not in the apikey ip whitelist
		return ErrorKindAuth
	case "40040":
		// user api key permission setting error
		return ErrorKindAuth
	case "40041":
		// User's ApiKey does not exist
		return ErrorKindAuth
	case "40072":
		// symbol {0} is Invalid or not supported mix contract trade
		return ErrorKindInvalidRequest

	// ----- 40100-40199 KYC / region / order metadata -----
	case "40109":
		// Order data not found / cannot be confirmed
		return ErrorKindInvalidRequest

	// ----- 40200-40499 server lifecycle / params / batch shape -----
	case "40200":
		// Server upgrade, please try again later — transient.
		return ErrorKindNetwork
	case "40303":
		// Can only query up to 20,000 data
		return ErrorKindInvalidRequest
	case "40304":
		// clientOid or clientOrderId length cannot greater than 50
		return ErrorKindInvalidRequest
	case "40305":
		// clientOid or clientOrderId length cannot greater than 64
		return ErrorKindInvalidRequest
	case "40306":
		// Batch processing orders can only process up to 20
		return ErrorKindInvalidRequest
	case "40309":
		// The contract has been removed
		return ErrorKindExchange
	case "40402":
		// orderId or clientOId format error
		return ErrorKindInvalidRequest
	case "40404":
		// Request URL NOT FOUND
		return ErrorKindInvalidRequest
	case "40409":
		// wrong format
		return ErrorKindInvalidRequest

	// ----- 40700-40899 derivatives runtime / margin / depth -----
	case "40710":
		// The account has been frozen
		return ErrorKindAuth
	case "40725":
		// Service unavailable / generic transient
		return ErrorKindNetwork
	case "40754":
		// balance not enough — exchange business rejection.
		return ErrorKindExchange
	case "40755":
		// Not enough open positions are available
		return ErrorKindExchange
	case "40756":
		// The balance lock is insufficient
		return ErrorKindExchange
	case "40757":
		// Not enough position is available
		return ErrorKindExchange
	case "40758":
		// The position lock is insufficient
		return ErrorKindExchange
	case "40760":
		// Account abnormal status
		return ErrorKindAuth
	case "40761":
		// The total number of unfilled orders is too high
		return ErrorKindInvalidRequest
	case "40762":
		// The order size is greater than the max open size
		return ErrorKindInvalidRequest
	case "40763":
		// Exceeds position tier limit
		return ErrorKindInvalidRequest
	case "40764":
		// Remaining order amount < current transaction volume
		return ErrorKindInvalidRequest
	case "40765":
		// Remaining position volume < current transaction volume
		return ErrorKindInvalidRequest
	case "40768":
		// Order does not exist (the canonical "order not found" code).
		return ErrorKindInvalidRequest
	case "40769":
		// Reject order has been completed
		return ErrorKindInvalidRequest
	case "40774":
		// reduceOnly cannot increase position size
		return ErrorKindInvalidRequest
	case "40798":
		// Insufficient contract account balance
		return ErrorKindExchange
	case "40800":
		// Insufficient amount of margin
		return ErrorKindExchange
	case "40814":
		// No change in leverage (alternate of 22002 / 45054).
		return ErrorKindInvalidRequest

	// ----- 40900-40999 derivatives operational -----
	case "40908":
		// Concurrent operation failed — transient.
		return ErrorKindNetwork
	case "40909":
		// Transfer processing — transient.
		return ErrorKindNetwork
	case "40910":
		// Operation timed out — transient.
		return ErrorKindNetwork
	case "40920":
		// Position or order exists, the position mode cannot be switched
		return ErrorKindInvalidRequest
	case "40923":
		// Order size and price have not changed (amend with same params)
		return ErrorKindInvalidRequest
	case "40924":
		// orderId and clientOid must have one
		return ErrorKindInvalidRequest
	case "40925":
		// price or size must be passed in together
		return ErrorKindInvalidRequest
	case "40939":
		// reduceOnly will decrease position; cancel/modify the original
		// order before placing a new one.
		return ErrorKindInvalidRequest

	// ----- 43xxx order lifecycle (REST + spot) -----
	case "43001":
		// The order does not exist
		return ErrorKindInvalidRequest
	case "43004":
		// There is no order to cancel
		return ErrorKindInvalidRequest
	case "43005":
		// Exceed the maximum number of orders
		return ErrorKindInvalidRequest
	case "43006":
		// Order quantity is less than the minimum
		return ErrorKindInvalidRequest
	case "43007":
		// Order quantity is greater than the maximum
		return ErrorKindInvalidRequest
	case "43008":
		// Order price below the minimum
		return ErrorKindInvalidRequest
	case "43009":
		// Order price above the maximum
		return ErrorKindInvalidRequest
	case "43011":
		// Parameter does not meet the specification
		return ErrorKindInvalidRequest
	case "43012":
		// Insufficient balance — exchange business rejection.
		return ErrorKindExchange
	case "43025":
		// Plan order does not exist
		return ErrorKindInvalidRequest
	case "43030":
		// Take profit order already existed
		return ErrorKindInvalidRequest
	case "43031":
		// Stop loss order already existed
		return ErrorKindInvalidRequest
	case "43048":
		// The symbol is null
		return ErrorKindInvalidRequest
	case "43050":
		// Leverage exceeds the effective range
		return ErrorKindInvalidRequest
	case "43118":
		// clientOrderId duplicate
		return ErrorKindInvalidRequest

	// ----- 45xxx risk / quantity / leverage / order-status validation -----
	case "45002":
		// Insufficient asset
		return ErrorKindExchange
	case "45003":
		// Insufficient position
		return ErrorKindExchange
	case "45009":
		// The account is at risk and cannot perform trades temporarily
		return ErrorKindExchange
	case "45034":
		// clientOid duplicate
		return ErrorKindInvalidRequest
	case "45035":
		// Price step mismatch
		return ErrorKindInvalidRequest
	case "45043":
		// Trade suspended for settlement / maintenance — transient.
		return ErrorKindNetwork
	case "45044":
		// Leverage is not within the suitable range after adjustment
		return ErrorKindInvalidRequest
	case "45045":
		// Exceeds the maximum possible leverage
		return ErrorKindInvalidRequest
	case "45054":
		// No change in leverage (alternate of 22002 / 40814).
		return ErrorKindInvalidRequest
	case "45055":
		// The current order status cannot be cancelled
		return ErrorKindInvalidRequest
	case "45056":
		// The current order type cannot be cancelled
		return ErrorKindInvalidRequest
	case "45057":
		// The order does not exist (45-family alternate of 40768)
		return ErrorKindInvalidRequest
	case "45110":
		// Less than the minimum order amount {0} USDT
		return ErrorKindInvalidRequest
	case "45111":
		// Less than the minimum order quantity
		return ErrorKindInvalidRequest
	case "45112":
		// More than the maximum order quantity
		return ErrorKindInvalidRequest
	case "45113":
		// Maximum order value limit triggered
		return ErrorKindInvalidRequest
	case "45114":
		// The minimum order requirement is not met
		return ErrorKindInvalidRequest
	case "45115":
		// Price must be a multiple of {0}
		return ErrorKindInvalidRequest
	case "45116":
		// The count of positions held exceeds the maximum
		return ErrorKindInvalidRequest
	case "45117":
		// Currently holding positions or orders, margin mode cannot be
		// adjusted. (Earlier SDK comment said "price out of range" — that
		// is 45115/45122; this is the official V2 docs definition.)
		return ErrorKindInvalidRequest
	case "45118":
		// Reached the upper limit of the order of transactions
		return ErrorKindInvalidRequest
	case "45119":
		// This symbol does not support position opening operation
		return ErrorKindInvalidRequest
	case "45120":
		// Size > max can open order size
		return ErrorKindInvalidRequest
	case "45129":
		// Cancel order is too frequent — same orderId only once per second.
		return ErrorKindRateLimit

	// ----- 47xxx wallet rate-limit-adjacent -----
	case "47001":
		// Operation is too frequent (legacy alias) — RateLimit.
		// Note: 47001 also maps to "currency recharge not enabled" in the
		// withdraw/recharge V2 docs; HFT callers will only ever hit the
		// rate-limit semantic, so we keep RateLimit. Callers that
		// distinguish should branch on BitgetCode + endpoint.
		return ErrorKindRateLimit

	// ----- 50xxx wallet / balance / cross-isolated -----
	case "50001":
		// coin {0} does not support cross
		return ErrorKindInvalidRequest
	case "50002":
		// symbol {0} does not support isolated
		return ErrorKindInvalidRequest
	case "50003":
		// coin {0} does not support isolated
		return ErrorKindInvalidRequest
	case "50004":
		// symbol {0} does not support cross
		return ErrorKindInvalidRequest
	case "50020":
		// Insufficient balance (spot)
		return ErrorKindExchange
	case "50031":
		// System error — transient.
		return ErrorKindNetwork
	case "50060":
		// Duplicated clientOid (spot)
		return ErrorKindInvalidRequest
	case "50066":
		// Position closing, please try again later — transient.
		return ErrorKindNetwork
	case "50067":
		// Insufficient balance (legacy)
		return ErrorKindExchange

	// ----- 59xxx Earn / financial products (HFT-adjacent) -----
	case "59044":
		// Operations are frequent, please try again later.
		return ErrorKindRateLimit

	default:
		return ErrorKindExchange
	}
}
