/*
FILE: errors.go

DESCRIPTION:
Public re-export of the SDK error type and category predicates from
internal/bgerr. Importers work through the root package only:

	import bitget "github.com/tonymontanov/go-bitget/v2"

	if bitget.IsRateLimit(err) { ... }

The aliases below preserve the typed identity (`type X = internal.X`),
so users can also do `errors.As(err, &bitget.Error{})`.
*/

package bitget

import "github.com/tonymontanov/go-bitget/v2/internal/bgerr"

// Error is the SDK error type (alias). All SDK methods return *Error
// (sometimes wrapped). errors.As / errors.Is work normally.
type Error = bgerr.Error

// ErrorKind is the error category enum (alias).
type ErrorKind = bgerr.ErrorKind

// Error categories. See internal/bgerr for full semantics of each kind.
const (
	// ErrorKindUnknown — the SDK could not classify the failure.
	ErrorKindUnknown = bgerr.ErrorKindUnknown
	// ErrorKindNetwork — transport-level failure (timeout, conn reset, ...).
	ErrorKindNetwork = bgerr.ErrorKindNetwork
	// ErrorKindRateLimit — Bitget told us we hit a rate limit.
	ErrorKindRateLimit = bgerr.ErrorKindRateLimit
	// ErrorKindAuth — credentials missing/invalid or signature rejected.
	ErrorKindAuth = bgerr.ErrorKindAuth
	// ErrorKindInvalidRequest — malformed request, validation rejection.
	ErrorKindInvalidRequest = bgerr.ErrorKindInvalidRequest
	// ErrorKindExchange — exchange rejected the request for business reasons.
	ErrorKindExchange = bgerr.ErrorKindExchange
)

// NewError constructs an *Error. Mostly used by SDK internals; user code
// rarely needs this.
func NewError(kind ErrorKind, code, msg string, cause error) *Error {
	return bgerr.New(kind, code, msg, cause)
}

// IsNetwork reports whether err is a network-class error.
func IsNetwork(err error) bool { return bgerr.IsNetwork(err) }

// IsRateLimit reports whether err is a rate-limit error.
func IsRateLimit(err error) bool { return bgerr.IsRateLimit(err) }

// IsAuth reports whether err is an auth/permission error.
func IsAuth(err error) bool { return bgerr.IsAuth(err) }

// IsInvalidRequest reports whether err is a validation/build-time error.
func IsInvalidRequest(err error) bool { return bgerr.IsInvalidRequest(err) }

// IsExchange reports whether err is an exchange-level rejection.
func IsExchange(err error) bool { return bgerr.IsExchange(err) }

// MapBitgetCode returns the SDK ErrorKind for a Bitget code (string).
func MapBitgetCode(code, msg string) ErrorKind { return bgerr.MapBitgetCode(code, msg) }

// MapHTTPStatus returns the SDK ErrorKind for an HTTP status code.
func MapHTTPStatus(status int) ErrorKind { return bgerr.MapHTTPStatus(status) }
