/*
FILE: internal/bgerr/codes.go

DESCRIPTION:
Mapping from raw transport-level signals (HTTP status, Bitget code) to
ErrorKind. Centralised here so the rest of the SDK never sprinkles
"if code == ..." chains.

SOURCES:
  - HTTP status mapping is the standard 4xx/5xx convention, with 401/403 as
    Auth and 429 as RateLimit.
  - Bitget code tables are derived from the public Bitget docs:
        https://www.bitget.com/api-doc/common/codes
        https://www.bitget.com/api-doc/contract/error-codes
    The list is NOT exhaustive — only codes the SDK can usefully classify.
    Anything not explicitly listed falls back to ErrorKindExchange (so the
    caller still gets an Exchange-class error, just without the SDK
    pre-classifying it).

UPDATE PROCEDURE:
When Bitget publishes a new error code that the SDK should react to:
  1. Add a `case` to MapBitgetCode below.
  2. Add a covering test in codes_test.go.
  3. If it changes Kind for an already-listed code, bump the SDK minor
     version — this is a behaviour change for callers of IsRateLimit etc.
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
// as a string in JSON: "00000" / "40037" / etc.).
//
// Codes are grouped by family:
//
//   - 400xx — generic API / signature / auth / rate-limit errors.
//   - 401xx / 430xx — order/position validation (derivatives + spot).
//   - 450xx / 470xx — risk / position-mode / leverage validation.
//   - 500xx — wallet / balance errors.
//
// Anything outside the explicitly listed codes maps to ErrorKindExchange:
// the SDK saw a non-success code, but does not pre-classify it.
func MapBitgetCode(code, _ string) ErrorKind {
	switch code {
	// ----- success -----
	case CodeOK:
		return ErrorKindUnknown // success — must not be passed here, but be safe

	// ----- 400xx generic auth / signature / params / rate limit -----
	case "40001":
		// ACCESS_KEY cannot be empty
		return ErrorKindAuth
	case "40002":
		// SECRET_KEY cannot be empty
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
		// Incorrect permissions
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
		// Operation has been blocked
		return ErrorKindAuth
	case "40020":
		// User does not exist
		return ErrorKindAuth
	case "40021":
		// Invalid Symbol
		return ErrorKindInvalidRequest
	case "40029":
		// Exceeded rate limit
		return ErrorKindRateLimit
	case "40034":
		// Parameter does not exist / generic parameter error
		return ErrorKindInvalidRequest
	case "40037":
		// Order does not exist
		return ErrorKindInvalidRequest
	case "40109":
		// Order data not found
		return ErrorKindInvalidRequest
	case "40725":
		// Service unavailable (transient)
		return ErrorKindNetwork
	case "40754":
		// Position quantity insufficient
		return ErrorKindExchange
	case "40762":
		// The order size is greater than the available position
		return ErrorKindInvalidRequest
	case "40774":
		// reduceOnly cannot increase position size
		return ErrorKindInvalidRequest

	// ----- 430xx order lifecycle -----
	case "43011":
		// The order does not exist or has been completed
		return ErrorKindInvalidRequest
	case "43025":
		// Plan order does not exist
		return ErrorKindInvalidRequest

	// ----- 451xx risk / quantity / leverage validation -----
	case "45110":
		// Less than the minimum order quantity
		return ErrorKindInvalidRequest
	case "45117":
		// Order price out of permissible range
		return ErrorKindInvalidRequest

	// ----- 470xx rate / margin -----
	case "47001":
		// Operation is too frequent
		return ErrorKindRateLimit

	// ----- 500xx wallet / balance -----
	case "50067":
		// Insufficient balance
		return ErrorKindExchange

	default:
		return ErrorKindExchange
	}
}
