/*
FILE: internal/bgcommon/restdoer.go

DESCRIPTION:
Profile-agnostic interface mirror of *rest.Client.Do. Domain packages
(mix/, spot/, uta/) declare their REST helpers against this interface so
contract tests can swap in an inline stub that records calls and returns
canned envelopes. The concrete *rest.Client satisfies it as-is.

WHY HERE (not in internal/rest):
Go forbids declaring an interface in an internal package and
implementing it via concrete consumption from the same internal API
without a thin shim. Hosting the contract in internal/bgcommon lets
every domain package depend on a single declaration instead of
duplicating one per profile.

NOTE on returned headers:
The transport returns the rate-limit header map as a fresh allocation
on every call (see internal/rest.Client.Do). Callers MUST treat the map
as read-only — the transport keeps no reference after Do returns.
*/

package bgcommon

import (
	"context"

	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// RestDoer is the contract domain methods rely on. The concrete
// *rest.Client satisfies it as-is.
type RestDoer interface {
	Do(ctx context.Context, opts rest.Options) (rest.Response, map[string]string, error)
}
