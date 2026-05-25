/*
FILE: mix/rest-doer.go

DESCRIPTION:
Internal interface mirror of *rest.Client.Do used to break the
production dependency for unit and contract tests. Test code provides
an inline stub that records calls and returns canned envelopes.

The real implementation comes from internal/rest.Client which already
satisfies this contract. The interface lives here (not in internal/rest)
because Go forbids declaring an interface in an internal package and
implementing it via concrete consumption from the same internal API
without a thin shim.

NOTE on returned headers:
The transport returns the rate-limit header map as a fresh allocation
on every call (see internal/rest.Client.Do). Callers MUST treat the map
as read-only — the transport keeps no reference after Do returns.
*/

package mix

import (
	"context"

	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// restDoer is the contract domain methods rely on. The concrete
// *rest.Client satisfies it as-is.
type restDoer interface {
	Do(ctx context.Context, opts rest.Options) (rest.Response, map[string]string, error)
}
