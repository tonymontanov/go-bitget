/*
FILE: internal/auth/sign.go

DESCRIPTION:
Request signing for Bitget. Two flavours:

  1. REST signing (SignREST):
     preHash   = timestamp + method + requestPath + body
     signature = base64( HMAC_SHA256(secretKey, preHash) )

     - timestamp:   current Unix time in MILLISECONDS, as decimal string.
     - method:      HTTP method in UPPER case ("GET" / "POST" / ...).
     - requestPath: for GET — "/api/v2/mix/order/orders?symbol=BTCUSDT&...".
                    The exact byte sequence that goes onto the wire (with
                    leading "/api/..." and the canonical query string).
     - body:        for POST — the exact JSON body string that goes on the
                    wire; empty string for GET.
                    IMPORTANT: signing MUST happen on the same byte sequence
                    that is sent — re-marshalling can reorder map keys and
                    break the signature. SignREST takes the already-rendered
                    body string for that reason.

     Output sent as headers:
       ACCESS-KEY        — apiKey
       ACCESS-SIGN       — base64(signature)
       ACCESS-PASSPHRASE — passphrase
       ACCESS-TIMESTAMP  — ms timestamp string
       locale            — "en-US" (default; see config.go)
       Content-Type      — application/json (POST only)

  2. WebSocket auth (SignWS):
     preHash   = timestamp + "GET" + "/user/verify"
     signature = base64( HMAC_SHA256(secretKey, preHash) )

     - timestamp: Unix time in MILLISECONDS as decimal string. Bitget's
                  WS server checks server-side that |server_now - ts| < 30s
                  and rejects otherwise.

     The signature is sent as part of the JSON message:
       {"op":"login","args":[{"apiKey":...,"passphrase":...,"timestamp":...,"sign":...}]}

The base64 output uses the standard padded encoding (RFC 4648 §4) — that
is what Bitget compares against verbatim.

SECURITY:
  - Secret material is stored as []byte and never serialized. String()
    redacts the API key and passphrase.
  - Pre-hash and body strings MUST NOT be logged.

DEPENDENCIES:
  - crypto/hmac, crypto/sha256: signing.
  - encoding/base64:            output encoding.
*/

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrSignerDisabled is returned when Sign* is called on a signer that has
// no credentials. The same Signer can serve public REST/WS endpoints
// without keys; private endpoints surface this error at call time.
var ErrSignerDisabled = errors.New("auth: signer is disabled (api key/secret/passphrase not configured)")

// Signer holds Bitget credentials and produces conformant signatures.
// Safe for concurrent use: all fields are read-only after construction.
type Signer struct {
	apiKey     string
	secretKey  []byte
	passphrase string
	enabled    bool
}

// NewSigner creates a Signer. If any of apiKey / secretKey / passphrase is
// empty the signer is disabled and Sign* will return ErrSignerDisabled.
// This lets a single Client hit public endpoints without credentials.
func NewSigner(apiKey, secretKey, passphrase string) *Signer {
	var enabled bool = apiKey != "" && secretKey != "" && passphrase != ""
	return &Signer{
		apiKey:     apiKey,
		secretKey:  []byte(secretKey),
		passphrase: passphrase,
		enabled:    enabled,
	}
}

// Enabled reports whether the signer has credentials.
func (s *Signer) Enabled() bool { return s != nil && s.enabled }

// APIKey returns the bound API key, used to populate ACCESS-KEY.
func (s *Signer) APIKey() string {
	if s == nil {
		return ""
	}
	return s.apiKey
}

// Passphrase returns the bound passphrase, used to populate
// ACCESS-PASSPHRASE on REST requests and the "passphrase" field of the
// WS login payload.
func (s *Signer) Passphrase() string {
	if s == nil {
		return ""
	}
	return s.passphrase
}

// MillisTimestamp returns now in milliseconds as a decimal string. Used for
// ACCESS-TIMESTAMP and as the timestamp component of the pre-hash. If now
// is zero, time.Now() is used.
func (s *Signer) MillisTimestamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return strconv.FormatInt(now.UnixMilli(), 10)
}

/*
SignREST returns the base64 HMAC-SHA256 signature for a Bitget REST
request, per the Bitget specification:

	preHash = timestamp + method + requestPath + body

Parameters:
  - timestamp:   ms timestamp string (use MillisTimestamp).
  - method:      HTTP method in UPPER case ("GET" / "POST" / ...).
  - requestPath: full URL path including the leading "/" and the
    canonical query string (e.g. "/api/v2/mix/market/depth?symbol=BTCUSDT").
  - body:        for POST — the exact JSON body string; empty for GET.

Returns ErrSignerDisabled if the signer has no credentials.
*/
func (s *Signer) SignREST(timestamp, method, requestPath, body string) (string, error) {
	if !s.Enabled() {
		return "", ErrSignerDisabled
	}
	var sb strings.Builder
	sb.Grow(len(timestamp) + len(method) + len(requestPath) + len(body))
	sb.WriteString(timestamp)
	sb.WriteString(method)
	sb.WriteString(requestPath)
	sb.WriteString(body)
	return s.hmacBase64(sb.String()), nil
}

/*
SignWS returns the base64 HMAC-SHA256 signature for the Bitget
WebSocket auth message:

	preHash = timestamp + "GET" + "/user/verify"

Parameters:
  - timestamp: ms timestamp string. Bitget's WS server allows ±30s skew.

Returns ErrSignerDisabled if the signer has no credentials.
*/
func (s *Signer) SignWS(timestamp string) (string, error) {
	if !s.Enabled() {
		return "", ErrSignerDisabled
	}
	var preHash string = timestamp + "GET" + "/user/verify"
	return s.hmacBase64(preHash), nil
}

// hmacBase64 computes base64(HMAC_SHA256(secret, msg)).
func (s *Signer) hmacBase64(msg string) string {
	var mac = hmac.New(sha256.New, s.secretKey)
	mac.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// String returns a log-safe representation that NEVER includes the secret
// or passphrase.
func (s *Signer) String() string {
	if s == nil || !s.enabled {
		return "auth.Signer{disabled}"
	}
	return "auth.Signer{enabled, apiKey=" + redact(s.apiKey) + "}"
}

// redact turns a string into "abcd…wxyz" — first/last 4 chars. For logs only.
func redact(v string) string {
	if len(v) <= 8 {
		return "***"
	}
	return v[:4] + "…" + v[len(v)-4:]
}
