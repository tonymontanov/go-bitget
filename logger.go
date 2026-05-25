/*
FILE: logger.go

DESCRIPTION:
Public re-export of the Logger interface and field constructors. The
underlying types live in internal/bglog so the rest/ws/orderbook
sub-packages can import them without taking a dependency on the root
(which itself depends on these packages — circular import otherwise).

The SDK ships only a NoopLogger by default. Embedders are expected to
adapt their own logger (zerolog, zap, slog, log/slog) to bitget.Logger
once and pass it via Config.Logger.
*/

package bitget

import "github.com/tonymontanov/go-bitget/v2/internal/bglog"

// Logger is the SDK logging facade. See internal/bglog for the full contract.
type Logger = bglog.Logger

// Field is a typed key/value pair used in log entries.
type Field = bglog.Field

// FieldKind enumerates supported field value types.
type FieldKind = bglog.FieldKind

// Field-kind aliases.
const (
	// FieldKindString — string value.
	FieldKindString = bglog.FieldKindString
	// FieldKindInt — int64 value.
	FieldKindInt = bglog.FieldKindInt
	// FieldKindFloat — float64 value.
	FieldKindFloat = bglog.FieldKindFloat
	// FieldKindBool — bool value.
	FieldKindBool = bglog.FieldKindBool
	// FieldKindError — error value.
	FieldKindError = bglog.FieldKindError
)

// Str is a shortcut for a string field.
func Str(key, value string) Field { return bglog.Str(key, value) }

// Int is a shortcut for an int64 field.
func Int(key string, value int64) Field { return bglog.Int(key, value) }

// Float is a shortcut for a float64 field.
func Float(key string, value float64) Field { return bglog.Float(key, value) }

// Bool is a shortcut for a bool field.
func Bool(key string, value bool) Field { return bglog.Bool(key, value) }

// Err is a shortcut for an error field with key "error".
func Err(err error) Field { return bglog.Err(err) }

// NoopLogger returns a Logger that discards every record.
func NoopLogger() Logger { return bglog.Noop() }
