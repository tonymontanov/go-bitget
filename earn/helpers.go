/*
FILE: earn/helpers.go

DESCRIPTION:
Shared helpers for the earn sub-clients: typed error constructors and the
rate-limit request meta. Kept profile-local because the error-message
prefix encodes the sub-client path and the meta category is fixed.
*/

package earn

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// errInvalid builds a client-side validation error. `scope` is the
// sub-client + method (e.g. "Savings.Subscribe").
func errInvalid(scope, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "earn."+scope+": "+msg, nil)
}

// errParse wraps a response-decode failure.
func errParse(scope string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "earn."+scope+": parse", cause)
}

// queryMeta is the rate-limit meta for an earn call. Earn has no
// order-flow category of its own; every endpoint is tagged "query".
func queryMeta() rest.RequestMeta {
	return rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)}
}
