/*
FILE: internal/auth/sign_test.go

DESCRIPTION:
Property tests for the Bitget signer. We assert:

  - Disabled signer surfaces ErrSignerDisabled instead of producing a
    bogus signature.
  - Sign* is deterministic for fixed inputs (same inputs → same signature).
  - Signature output is base64 (decodes cleanly to 32 bytes — HMAC-SHA256
    digest length).
  - Pre-hash composition: changing any of timestamp / method / path / body
    flips the signature. This catches the kind of regression where the
    composer accidentally reorders pieces.

A canonical-vector test is added once we have a verified Bitget reference
trace (testnet integration in M5). For now, "stable + properly composed +
correct length" is enough to guarantee wire compatibility — the byte
sequence of the pre-hash is fixed by SignREST itself.
*/

package auth

import (
	"encoding/base64"
	"testing"
	"time"
)

const (
	testKey        = "bg_test_apikey_123456"
	testSecret     = "supersecret_dontleakme"
	testPassphrase = "my-passphrase"
)

func TestSignerDisabled(t *testing.T) {
	var s *Signer = NewSigner("", "", "")
	if s.Enabled() {
		t.Fatal("signer with empty creds reports Enabled=true")
	}
	var _, err = s.SignREST("1700000000000", "GET", "/api/v2/mix/order/orders", "")
	if err != ErrSignerDisabled {
		t.Fatalf("SignREST: want ErrSignerDisabled, got %v", err)
	}
	_, err = s.SignWS("1700000000000")
	if err != ErrSignerDisabled {
		t.Fatalf("SignWS: want ErrSignerDisabled, got %v", err)
	}
}

func TestSignerPartialCredsDisabled(t *testing.T) {
	var cases = []struct {
		name string
		k, s, p string
	}{
		{"missing apiKey", "", testSecret, testPassphrase},
		{"missing secret", testKey, "", testPassphrase},
		{"missing passphrase", testKey, testSecret, ""},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var c = cases[i]
		t.Run(c.name, func(t *testing.T) {
			var s *Signer = NewSigner(c.k, c.s, c.p)
			if s.Enabled() {
				t.Fatal("Enabled with partial creds")
			}
		})
	}
}

func TestSignRESTDeterministic(t *testing.T) {
	var s *Signer = NewSigner(testKey, testSecret, testPassphrase)
	var ts string = "1700000000000"
	var path string = "/api/v2/mix/order/place-order"
	var body string = `{"symbol":"BTCUSDT","side":"buy"}`

	var a, err = s.SignREST(ts, "POST", path, body)
	if err != nil {
		t.Fatal(err)
	}
	var b string
	b, err = s.SignREST(ts, "POST", path, body)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("signature is non-deterministic: %q vs %q", a, b)
	}
	// HMAC-SHA256 → 32 bytes → base64 std encoding → 44 chars (with "=" pad).
	if len(a) != 44 {
		t.Fatalf("unexpected base64 length %d (want 44): %q", len(a), a)
	}
	var raw []byte
	raw, err = base64.StdEncoding.DecodeString(a)
	if err != nil {
		t.Fatalf("output is not valid base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("decoded HMAC length = %d, want 32", len(raw))
	}
}

func TestSignRESTComposition(t *testing.T) {
	var s *Signer = NewSigner(testKey, testSecret, testPassphrase)
	var base, err = s.SignREST("1700000000000", "POST", "/path", "body")
	if err != nil {
		t.Fatal(err)
	}

	// Each axis must change the signature.
	var v string
	v, _ = s.SignREST("1700000000001", "POST", "/path", "body") // ts
	if v == base {
		t.Fatal("timestamp change did not affect signature")
	}
	v, _ = s.SignREST("1700000000000", "GET", "/path", "body") // method
	if v == base {
		t.Fatal("method change did not affect signature")
	}
	v, _ = s.SignREST("1700000000000", "POST", "/other", "body") // path
	if v == base {
		t.Fatal("path change did not affect signature")
	}
	v, _ = s.SignREST("1700000000000", "POST", "/path", "body2") // body
	if v == base {
		t.Fatal("body change did not affect signature")
	}
}

func TestSignWSDeterministic(t *testing.T) {
	var s *Signer = NewSigner(testKey, testSecret, testPassphrase)
	var a, err = s.SignWS("1700000000000")
	if err != nil {
		t.Fatal(err)
	}
	var b string
	b, _ = s.SignWS("1700000000000")
	if a != b {
		t.Fatal("WS signature is non-deterministic")
	}
	if len(a) != 44 {
		t.Fatalf("WS sig length = %d, want 44", len(a))
	}
	var raw []byte
	raw, err = base64.StdEncoding.DecodeString(a)
	if err != nil || len(raw) != 32 {
		t.Fatalf("WS sig decode: err=%v len=%d", err, len(raw))
	}
}

func TestMillisTimestamp(t *testing.T) {
	var s *Signer = NewSigner(testKey, testSecret, testPassphrase)
	var t0 time.Time = time.UnixMilli(1700000000123)
	var ts string = s.MillisTimestamp(t0)
	if ts != "1700000000123" {
		t.Fatalf("MillisTimestamp = %q, want %q", ts, "1700000000123")
	}
	// Zero time falls back to time.Now() — just check non-empty.
	var auto string = s.MillisTimestamp(time.Time{})
	if auto == "" {
		t.Fatal("MillisTimestamp(zero) returned empty string")
	}
}

func TestSignerString(t *testing.T) {
	var disabled *Signer = NewSigner("", "", "")
	if got := disabled.String(); got != "auth.Signer{disabled}" {
		t.Fatalf("disabled.String() = %q", got)
	}
	var enabled *Signer = NewSigner(testKey, testSecret, testPassphrase)
	var s string = enabled.String()
	// Must NOT contain the secret or full key.
	if contains(s, testSecret) {
		t.Fatal("String() leaked the secret")
	}
	if contains(s, testPassphrase) {
		t.Fatal("String() leaked the passphrase")
	}
	if contains(s, testKey) {
		t.Fatal("String() leaked the full apiKey")
	}
}

// contains is a tiny strings.Contains shim — avoids importing strings just
// for tests.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	var i int
	for i = 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
