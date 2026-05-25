/*
FILE: client_test.go

DESCRIPTION:
Basic public-surface tests: defaults, NewClient construction, public
error helpers, RateLimitEvent forwarding adapter.
*/

package bitget

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	var cfg Config = DefaultConfig()
	if cfg.REST.BaseURL != DefaultRestBaseURL {
		t.Fatalf("REST.BaseURL = %q", cfg.REST.BaseURL)
	}
	if cfg.WS.PublicURL != DefaultWsPublicURL {
		t.Fatalf("WS.PublicURL = %q", cfg.WS.PublicURL)
	}
	if cfg.WS.PrivateURL != DefaultWsPrivateURL {
		t.Fatalf("WS.PrivateURL = %q", cfg.WS.PrivateURL)
	}
	if cfg.REST.RequestTimeout != 10*time.Second {
		t.Fatalf("RequestTimeout = %v", cfg.REST.RequestTimeout)
	}
	if cfg.UserAgent != "go-bitget/1" {
		t.Fatalf("UserAgent = %q", cfg.UserAgent)
	}
	if cfg.Logger == nil || cfg.Metrics == nil {
		t.Fatal("Logger/Metrics defaults missing")
	}
}

func TestNewClientPublicOnly(t *testing.T) {
	var cfg Config = DefaultConfig()
	var c, err = NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()
	if c.Signer().Enabled() {
		t.Fatal("public-only client must have a disabled signer")
	}
	if c.Mix() != nil {
		t.Fatal("Mix() must be nil before importing the mix package")
	}
}

func TestNewClientWithCreds(t *testing.T) {
	var cfg Config = DefaultConfig()
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	var c, err = NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()
	if !c.Signer().Enabled() {
		t.Fatal("signer must be enabled with full creds")
	}
}

func TestErrorHelpers(t *testing.T) {
	var e *Error = NewError(ErrorKindRateLimit, "40029", "rate", nil)
	if !IsRateLimit(e) {
		t.Fatal("IsRateLimit failed")
	}
	if IsNetwork(e) {
		t.Fatal("IsNetwork misclassified rate limit")
	}
	if MapHTTPStatus(429) != ErrorKindRateLimit {
		t.Fatal("MapHTTPStatus(429)")
	}
	if MapBitgetCode("40029", "") != ErrorKindRateLimit {
		t.Fatal("MapBitgetCode(40029)")
	}
}

func TestWithDefaultsPreservesExplicit(t *testing.T) {
	var cfg Config = Config{
		REST: RestConfig{BaseURL: "https://example.com"},
		WS:   WsConfig{PublicURL: "wss://example.com/p"},
	}
	cfg = cfg.withDefaults()
	if cfg.REST.BaseURL != "https://example.com" {
		t.Fatalf("explicit REST URL overwritten: %q", cfg.REST.BaseURL)
	}
	if cfg.WS.PublicURL != "wss://example.com/p" {
		t.Fatalf("explicit public WS URL overwritten: %q", cfg.WS.PublicURL)
	}
	if cfg.WS.PrivateURL != DefaultWsPrivateURL {
		t.Fatalf("private WS default missing: %q", cfg.WS.PrivateURL)
	}
}
