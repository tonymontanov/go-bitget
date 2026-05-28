/*
FILE: internal/bgcommon/clientoid.go

DESCRIPTION:
Profile-agnostic client-order-id helpers shared by mix/, spot/ and uta/
trading paths.

WHY HERE:

  - GenClientOid is the SDK-side fallback when the caller did not
    supply NewClientOrderID on a ModifyOrder request. Bitget V2 (both
    mix and spot) requires a NEW clientOid distinct from the existing
    one — internally modify is implemented as a cancel-replace at the
    matcher level, and reusing the existing clientOid yields code=40786
    "Duplicate clientOid" on every profile.

  - ChooseClientOid picks the venue-echoed clientOid in preference to
    the request's own value, falling back gracefully when Bitget left
    the field empty on a fixture / edge case. Same logic on every
    profile.

PERFORMANCE:

GenClientOid uses crypto/rand for a 16-byte token (32 hex chars) which
the standard library reads from /dev/urandom (Linux) or BCryptGenRandom
(Windows). Per-call cost <1µs on commodity hardware — well below the
order-modify hot-path budget. On the (extremely unlikely) RNG read
failure the helper degrades to a nanoseconds-since-epoch hex string
rather than returning an error, because the modify path must not
refuse to ship just because the OS RNG momentarily glitched.

PREFIX CONVENTION:

Each profile picks its own prefix so log greps and audit trails can
tell modify-side IDs apart from caller-supplied ones at a glance:

  - mix:  "m-"
  - spot: "s-"
  - uta:  "u-" (reserved for v2.5)

The prefix has no semantic meaning to Bitget — the venue accepts any
ASCII string up to its 50-char clientOid cap.
*/

package bgcommon

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

// timeNowNano returns the current monotonic time in nanoseconds.
// Indirected through a var so tests can stub determinism if needed.
var timeNowNano = func() int64 { return time.Now().UnixNano() }

// GenClientOid returns a freshly-generated clientOid of the form
// "<prefix><32-hex>". prefix should typically include the trailing "-"
// (e.g. "m-", "s-") so the resulting token reads as a single
// dash-delimited identifier. On RNG failure the helper falls back to
// a timestamp-derived hex string under the same prefix — never
// returns an error.
func GenClientOid(prefix string) string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return prefix + strconv.FormatInt(timeNowNano(), 16)
	}
	return prefix + hex.EncodeToString(buf[:])
}

// ChooseClientOid returns the venue-echoed clientOid when present,
// falling back to the request's value. Bitget always echoes back
// the value it accepted on success rows; this helper is a safety
// net for fixtures with empty strings and for fail-rows where the
// venue may omit the field.
func ChooseClientOid(fromVenue, fromRequest string) string {
	if fromVenue != "" {
		return fromVenue
	}
	return fromRequest
}
