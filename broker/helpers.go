/*
FILE: broker/helpers.go

DESCRIPTION:
Shared helpers for the broker sub-clients: typed error constructors, the
rate-limit request meta and the page-number pagination walker. Kept
profile-local because the error-message prefix encodes the sub-client path
and the meta category is fixed.
*/

package broker

import (
	"context"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/internal/rest"
)

// pageNoMaxPages is the hard ceiling on page-number pagination loops
// (broker/agent reporting endpoints). It bounds a runaway server that
// never returns a short page; at a page size of 100 this is 100k rows.
const pageNoMaxPages = 1000

// paginateByPageNo walks a 1-based pageNo / pageSize endpoint until a
// short (or empty) page or the hard ceiling. Mirrors the earn/copytrading
// helper; kept profile-local to avoid coupling the packages.
func paginateByPageNo[T any](
	ctx context.Context,
	pageSize int,
	fetch func(pageNo, pageSize int) ([]T, error),
) ([]T, error) {
	var out []T
	var page int
	for page = 1; page <= pageNoMaxPages; page++ {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}
		var rows []T
		var err error
		rows, err = fetch(page, pageSize)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		out = append(out, rows...)
		if len(rows) < pageSize {
			break
		}
	}
	return out, nil
}

// errInvalid builds a client-side validation error. `scope` is the
// sub-client + method (e.g. "SubAccounts.Create").
func errInvalid(scope, msg string) error {
	return bitget.NewError(bitget.ErrorKindInvalidRequest, "", "broker."+scope+": "+msg, nil)
}

// errParse wraps a response-decode failure.
func errParse(scope string, cause error) error {
	return bitget.NewError(bitget.ErrorKindUnknown, "", "broker."+scope+": parse", cause)
}

// queryMeta is the rate-limit meta for a broker call. Broker has no
// order-flow category of its own; every endpoint is tagged "query".
func queryMeta() rest.RequestMeta {
	return rest.RequestMeta{Category: string(bitget.RateLimitCategoryQuery)}
}
