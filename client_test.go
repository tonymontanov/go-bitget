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

// TestUTAWsURLDefaults: the V3 (UTA) WS endpoints resolve to production
// by default and to the wspap demo host when Demo is set — also when Demo
// is flipped AFTER DefaultConfig(), which is how consumers build configs.
func TestUTAWsURLDefaults(t *testing.T) {
	var def Config = DefaultConfig()
	if def.WS.UTAPublicURL != "" || def.WS.UTAPrivateURL != "" {
		t.Fatalf("DefaultConfig must leave the UTA WS URLs empty (Demo-dependent), got %q / %q",
			def.WS.UTAPublicURL, def.WS.UTAPrivateURL)
	}

	var prod Config = DefaultConfig().withDefaults()
	if prod.WS.UTAPublicURL != DefaultWsUTAPublicURL {
		t.Fatalf("prod UTA public = %q", prod.WS.UTAPublicURL)
	}
	if prod.WS.UTAPrivateURL != DefaultWsUTAPrivateURL {
		t.Fatalf("prod UTA private = %q", prod.WS.UTAPrivateURL)
	}
	if DefaultWsUTAPublicURL != "wss://ws.bitget.com/v3/ws/public" || DefaultWsUTAPrivateURL != "wss://ws.bitget.com/v3/ws/private" {
		t.Fatalf("UTA prod constants changed: %q / %q", DefaultWsUTAPublicURL, DefaultWsUTAPrivateURL)
	}

	var demo Config = DefaultConfig()
	demo.Demo = true
	demo = demo.withDefaults()
	if demo.WS.UTAPublicURL != DefaultWsPublicURLDemo {
		t.Fatalf("demo UTA public = %q", demo.WS.UTAPublicURL)
	}
	if demo.WS.UTAPrivateURL != DefaultWsPrivateURLDemo {
		t.Fatalf("demo UTA private = %q", demo.WS.UTAPrivateURL)
	}
	// Demo never touches the V2 endpoints.
	if demo.WS.PublicURL != DefaultWsPublicURL || demo.WS.PrivateURL != DefaultWsPrivateURL {
		t.Fatalf("Demo leaked into the V2 WS URLs: %q / %q", demo.WS.PublicURL, demo.WS.PrivateURL)
	}

	// Zero-value Config (no DefaultConfig at all) resolves the same way.
	var bare Config = Config{}.withDefaults()
	if bare.WS.UTAPublicURL != DefaultWsUTAPublicURL || bare.WS.UTAPrivateURL != DefaultWsUTAPrivateURL {
		t.Fatalf("bare config UTA URLs = %q / %q", bare.WS.UTAPublicURL, bare.WS.UTAPrivateURL)
	}
}

// TestUTAWsURLExplicitPreserved: an explicit UTA URL wins over both the
// production and the demo default.
func TestUTAWsURLExplicitPreserved(t *testing.T) {
	var cfg Config = DefaultConfig()
	cfg.Demo = true
	cfg.WS.UTAPublicURL = "ws://127.0.0.1:9/public"
	cfg.WS.UTAPrivateURL = "ws://127.0.0.1:9/private"
	var c, err = NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()
	var got Config = c.Config()
	if got.WS.UTAPublicURL != "ws://127.0.0.1:9/public" {
		t.Fatalf("explicit UTA public URL overwritten: %q", got.WS.UTAPublicURL)
	}
	if got.WS.UTAPrivateURL != "ws://127.0.0.1:9/private" {
		t.Fatalf("explicit UTA private URL overwritten: %q", got.WS.UTAPrivateURL)
	}
}
