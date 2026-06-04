/*
FILE: copytrading/helpers.go

DESCRIPTION:
Shared helpers for the copy-trading sub-clients: typed error
constructors and the rate-limit request meta. Kept profile-local
(not in internal/bgcommon) because the error-message prefix encodes the
sub-client path and the meta category is fixed per profile.
*/

package copytrading

import (
	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// errInvalid builds a client-side validation error. `scope` is the
// sub-client + method (e.g. "FuturesFollower.Unfollow").
func errInvalid(scope, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "copytrading."+scope+": "+msg, nil)
}

// errParse wraps a response-decode failure.
func errParse(scope string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "copytrading."+scope+": parse", cause)
}

// queryMeta is the rate-limit meta for a copy-trading query / config
// call. Copy trading has no order-flow category of its own; every
// endpoint is tagged "query" so the RateLimitEvent observer can bucket
// it distinctly from hot-path trading.
func queryMeta() rest.RequestMeta {
	return rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)}
}
