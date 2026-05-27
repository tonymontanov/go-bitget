/*
FILE: internal/bgcommon/flexstring.go

DESCRIPTION:
FlexString is a JSON wire-format helper that accepts both quoted-string
and JSON-number / JSON-null inputs and stores the canonical decimal
representation as a Go string. It exists because the Bitget V2 server
is inconsistent on private push channels: the public API contract says
every numeric scalar is a quoted decimal string ("leverage":"5") but
the live wire occasionally ships the same field as a bare JSON number
("leverage":5). A plain `string`-typed struct field then makes jsoniter
abort the whole row decoding with `ReadString: expects " or n, but
found 5` and the entire push gets dropped (production regression
observed on PARTIUSDT positions, fixed in v1.2.1).

CONTRACT:
  - JSON string  ("5")    → "5"           (verbatim)
  - JSON number  (5)      → "5"           (raw bytes preserved, so
                                           "0.084501465025" round-trips
                                           bit-for-bit)
  - JSON null    (null)   → ""            (downstream parse helpers
                                           treat empty as zero)
  - JSON bool / object    → unmarshal error

WHERE IT'S APPLIED:
Every numeric / timestamp field on private WS rows across all profiles
(mix wsOrderRow / wsPositionRow / wsAccountRow, future spot/uta
analogues). Identifier fields (instId, orderId, clientOid, side,
marginMode, status, ...) stay strict `string` because Bitget has no
incentive to emit them numerically and the looser parse would mask
real wire bugs.

PERFORMANCE:
UnmarshalJSON allocates only when the input is a quoted string (one
strconv-style unescape pass via codec.Unmarshal). For numeric and null
inputs the cost is bounded by the slice copy of the raw bytes —
typically <20 bytes. The type is therefore safe to use on every
field of every WS row in the hot path.
*/

package bgcommon

import (
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
)

// FlexString accepts either a JSON string, a JSON number, or JSON null
// and stores the canonical decimal representation as a string.
type FlexString string

// UnmarshalJSON makes FlexString a json.Unmarshaler. Both encoding/json
// and jsoniter honour this hook on reflection-based decoding.
func (s *FlexString) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}
	if data[0] == '"' && data[len(data)-1] == '"' {
		var unquoted string
		if err := codec.Unmarshal(data, &unquoted); err != nil {
			return err
		}
		*s = FlexString(unquoted)
		return nil
	}
	// Numeric (int / float / scientific notation): keep raw bytes
	// verbatim — they are already the canonical decimal string and
	// match what the ParseDecimalOrZero / ParseInt*OrZero helpers
	// expect.
	*s = FlexString(string(data))
	return nil
}

// String returns the canonical decimal form for downstream parsing.
func (s FlexString) String() string { return string(s) }
