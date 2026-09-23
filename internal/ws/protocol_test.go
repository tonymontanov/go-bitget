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

// ---------------------------------------------------------------------
// SubscriptionArg — V2 byte-compat and V3 coordinates.
// ---------------------------------------------------------------------

// TestSubscriptionArgV2MarshalUnchanged pins the exact V2 wire bytes. The
// V3 fields (topic / symbol) are `omitempty`; if one of them ever leaks
// into a V2 frame the venue answers 30001 "doesn't exist" and the whole
// V2 stream dies — so this asserts the exact JSON, not just a round-trip.
func TestSubscriptionArgV2MarshalUnchanged(t *testing.T) {
	t.Parallel()
	type tc struct {
		name string
		arg  SubscriptionArg
		want string
	}
	var cases []tc = []tc{
		{
			name: "public books",
			arg:  SubscriptionArg{InstType: "USDT-FUTURES", Channel: "books", InstID: "BTCUSDT"},
			want: `{"instType":"USDT-FUTURES","channel":"books","instId":"BTCUSDT"}`,
		},
		{
			name: "private orders default",
			arg:  SubscriptionArg{InstType: "USDT-FUTURES", Channel: "orders", InstID: "default"},
			want: `{"instType":"USDT-FUTURES","channel":"orders","instId":"default"}`,
		},
		{
			name: "private account coin",
			arg:  SubscriptionArg{InstType: "USDT-FUTURES", Channel: "account", Coin: "USDT"},
			want: `{"instType":"USDT-FUTURES","channel":"account","coin":"USDT"}`,
		},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var c tc = cases[i]
		var raw []byte
		var err error
		raw, err = codec.Marshal(c.arg)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", c.name, err)
		}
		if string(raw) != c.want {
			t.Fatalf("%s: V2 arg bytes changed:\n got  %s\n want %s", c.name, raw, c.want)
		}
	}

	// The whole subscribe op, as sendOp builds it.
	var op outboundOp = outboundOp{Op: "subscribe", Args: []SubscriptionArg{cases[0].arg}}
	var raw []byte
	var err error
	raw, err = codec.Marshal(op)
	if err != nil {
		t.Fatalf("Marshal op: %v", err)
	}
	var want string = `{"op":"subscribe","args":[{"instType":"USDT-FUTURES","channel":"books","instId":"BTCUSDT"}]}`
	if string(raw) != want {
		t.Fatalf("V2 subscribe op bytes changed:\n got  %s\n want %s", raw, want)
	}
}

// TestSubscriptionArgV3Marshal pins the V3 wire bytes: topic / symbol,
// no channel / instId / coin keys at all.
func TestSubscriptionArgV3Marshal(t *testing.T) {
	t.Parallel()
	var public SubscriptionArg = SubscriptionArg{InstType: "usdt-futures", Topic: "books5", Symbol: "BTCUSDT"}
	var raw []byte
	var err error
	raw, err = codec.Marshal(public)
	if err != nil {
		t.Fatalf("Marshal public: %v", err)
	}
	if string(raw) != `{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"}` {
		t.Fatalf("V3 public arg = %s", raw)
	}

	var private SubscriptionArg = SubscriptionArg{InstType: "UTA", Topic: "order"}
	raw, err = codec.Marshal(private)
	if err != nil {
		t.Fatalf("Marshal private: %v", err)
	}
	if string(raw) != `{"instType":"UTA","topic":"order"}` {
		t.Fatalf("V3 private arg = %s", raw)
	}
}

// TestSubscriptionArgKeyStability pins both key formats: the V2 key is
// the historical one (registry keys of live V2 subscriptions must not
// move), the V3 key is namespaced and cannot collide with a V2 key.
func TestSubscriptionArgKeyStability(t *testing.T) {
	t.Parallel()
	var v2 SubscriptionArg = SubscriptionArg{InstType: "USDT-FUTURES", Channel: "books", InstID: "BTCUSDT"}
	if v2.Key() != "USDT-FUTURES:books:BTCUSDT:" {
		t.Fatalf("V2 key = %q", v2.Key())
	}
	var v2coin SubscriptionArg = SubscriptionArg{InstType: "USDT-FUTURES", Channel: "account", Coin: "USDT"}
	if v2coin.Key() != "USDT-FUTURES:account::USDT" {
		t.Fatalf("V2 coin key = %q", v2coin.Key())
	}
	var v3 SubscriptionArg = SubscriptionArg{InstType: "usdt-futures", Topic: "books", Symbol: "BTCUSDT"}
	if v3.Key() != "v3:usdt-futures:books:BTCUSDT" {
		t.Fatalf("V3 key = %q", v3.Key())
	}
	var v3private SubscriptionArg = SubscriptionArg{InstType: "UTA", Topic: "order"}
	if v3private.Key() != "v3:UTA:order:" {
		t.Fatalf("V3 private key = %q", v3private.Key())
	}
	if v2.IsV3() || !v3.IsV3() {
		t.Fatalf("IsV3: v2=%v v3=%v", v2.IsV3(), v3.IsV3())
	}
	if v2.Name() != "books" || v3.Name() != "books" {
		t.Fatalf("Name: v2=%q v3=%q", v2.Name(), v3.Name())
	}
	// Same logical name, different protocol generation → different keys.
	if v2.Key() == v3.Key() {
		t.Fatal("V2 and V3 keys collide")
	}
}

// TestEnvelopeV3Frames decodes the live V3 frames captured 2026-09-21
// from wss://ws.bitget.com/v3/ws/public: a subscribe ack without `code`,
// an error with a NUMERIC code and a push keyed by topic / symbol.
func TestEnvelopeV3Frames(t *testing.T) {
	t.Parallel()

	var ack Envelope
	if err := codec.Unmarshal([]byte(`{"event":"subscribe","arg":{"instType":"usdt-futures","topic":"ticker","symbol":"BTCUSDT"},"connId":"0649f8ff"}`), &ack); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if !ack.IsControl() || ack.IsPush() {
		t.Fatalf("ack classification: control=%v push=%v", ack.IsControl(), ack.IsPush())
	}
	if ack.Arg.Topic != "ticker" || ack.Arg.Symbol != "BTCUSDT" || ack.Arg.InstType != "usdt-futures" {
		t.Fatalf("ack arg = %+v", ack.Arg)
	}

	var errFrame Envelope
	if err := codec.Unmarshal([]byte(`{"event":"error","code":30001,"msg":"{\"instType\":\"usdt-futures\",\"symbol\":\"BTCUSDT\",\"topic\":\"books15\"} doesn't exist","connId":"06aa"}`), &errFrame); err != nil {
		t.Fatalf("error frame: %v", err)
	}
	if !errFrame.IsControl() || errFrame.Code != "30001" {
		t.Fatalf("error frame: control=%v code=%q", errFrame.IsControl(), errFrame.Code)
	}

	var push Envelope
	if err := codec.Unmarshal([]byte(`{"action":"snapshot","arg":{"instType":"usdt-futures","topic":"books5","symbol":"BTCUSDT"},"data":[{"a":[],"b":[],"seq":1,"pseq":0,"ts":"1789940582101"}],"ts":1789940582102}`), &push); err != nil {
		t.Fatalf("push: %v", err)
	}
	if !push.IsPush() || push.IsControl() {
		t.Fatalf("push classification: push=%v control=%v", push.IsPush(), push.IsControl())
	}
	if push.Arg.Key() != "v3:usdt-futures:books5:BTCUSDT" {
		t.Fatalf("push key = %q", push.Arg.Key())
	}
	if push.TsMs != 1789940582102 {
		t.Fatalf("push ts = %d", push.TsMs)
	}
}
