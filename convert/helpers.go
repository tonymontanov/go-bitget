/*
FILE: convert/helpers.go

DESCRIPTION:
Shared helpers for the convert profile: typed error constructors and the
rate-limit request meta. Kept profile-local (not in internal/bgcommon)
because the error-message prefix encodes the method and the meta category
is fixed per profile.
*/

package convert

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// errInvalid builds a client-side validation error. `scope` is the
// method name (e.g. "Trade").
func errInvalid(scope, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "convert."+scope+": "+msg, nil)
}

// errParse wraps a response-decode failure.
func errParse(scope string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "convert."+scope+": parse", cause)
}

// queryMeta is the rate-limit meta for a convert call. Convert has no
// order-flow category of its own; every endpoint is tagged "query".
func queryMeta() rest.RequestMeta {
	return rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)}
}
