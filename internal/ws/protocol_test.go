/*
FILE: internal/ws/protocol_test.go

DESCRIPTION:
Unit tests for the wire-level protocol types in protocol.go.

The most load-bearing assertion is the dual-form parsing of the
`code` field: Bitget V2 docs show "code" quoted ("0") but the live
server emits a JSON number on login/subscribe acks. Our envelope
must accept both — anything else makes the entire login envelope
unparseable, the dispatcher drops the frame, and the supervisor
times out waiting for an ack that already arrived (regression
seen in PARTIUSDT field session: 98-byte ack ~300ms after login,
then the 30s deadline expires).
*/

package ws

import (
	"testing"

	"github.com/tonymontanov/go-bitget/v2/internal/codec"
)

// TestEnvelopeCodeAcceptsString covers the documented form
// (`"code":"0"`).
func TestEnvelopeCodeAcceptsString(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{"event":"login","code":"0","msg":""}`)
	var env Envelope
	if err := codec.Unmarshal(raw, &env); err != nil {
		t.Fatalf("Unmarshal failed on quoted code: %v", err)
	}
	if env.Event != "login" {
		t.Fatalf("Event=%q, want %q", env.Event, "login")
	}
	if env.Code != "0" {
		t.Fatalf("Code=%q, want %q", env.Code, "0")
	}
}

// TestEnvelopeCodeAcceptsNumber covers the production-server form
// (`"code":0`). This is the wire shape the SDK actually receives;
// the docs example is misleading.
func TestEnvelopeCodeAcceptsNumber(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{"event":"login","code":0,"msg":""}`)
	var env Envelope
	if err := codec.Unmarshal(raw, &env); err != nil {
		t.Fatalf("Unmarshal failed on numeric code: %v", err)
	}
	if env.Event != "login" {
		t.Fatalf("Event=%q, want %q", env.Event, "login")
	}
	if env.Code != "0" {
		t.Fatalf("Code=%q, want %q (numeric form must canonicalise to decimal string)", env.Code, "0")
	}
}

// TestEnvelopeCodeNumericError covers the failure path that the
// production server uses for invalid logins: numeric error code
// like 30005.
func TestEnvelopeCodeNumericError(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{"event":"login","code":30005,"msg":"sign error"}`)
	var env Envelope
	if err := codec.Unmarshal(raw, &env); err != nil {
		t.Fatalf("Unmarshal failed on numeric error code: %v", err)
	}
	if env.Code != "30005" {
		t.Fatalf("Code=%q, want %q", env.Code, "30005")
	}
	if env.Msg != "sign error" {
		t.Fatalf("Msg=%q, want %q", env.Msg, "sign error")
	}
}

// TestEnvelopePushFrameStillParses guards against accidentally
// breaking the data-push path with the flexCode change. Push frames
// don't carry a code at all — we want a clean zero-value here, not
// a parse error.
func TestEnvelopePushFrameStillParses(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{"action":"snapshot","arg":{"instType":"USDT-FUTURES","channel":"books","instId":"BTCUSDT"},"data":[],"ts":1700000000000}`)
	var env Envelope
	if err := codec.Unmarshal(raw, &env); err != nil {
		t.Fatalf("Unmarshal failed on push frame: %v", err)
	}
	if env.Code != "" {
		t.Fatalf("expected empty Code on push frame, got %q", env.Code)
	}
	if !env.IsPush() {
		t.Fatal("IsPush=false; push frames must still classify as push after flexCode change")
	}
	if env.Arg.Channel != "books" {
		t.Fatalf("Arg.Channel=%q, want %q", env.Arg.Channel, "books")
	}
}

// TestEnvelopeCodeNullStaysEmpty covers the JSON `null` literal —
// some servers emit null for absent fields, and we don't want that
// to surface as a parse error either.
func TestEnvelopeCodeNullStaysEmpty(t *testing.T) {
	t.Parallel()
	var raw []byte = []byte(`{"event":"login","code":null,"msg":""}`)
	var env Envelope
	if err := codec.Unmarshal(raw, &env); err != nil {
		t.Fatalf("Unmarshal failed on null code: %v", err)
	}
	if env.Code != "" {
		t.Fatalf("Code=%q, want empty on null literal", env.Code)
	}
}
