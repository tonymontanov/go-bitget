/*
FILE: spot/client_test.go

DESCRIPTION:
M1 smoke tests for the spot profile scaffolding. The goal is to pin
two contracts that future milestones MUST not break:

 1. NewClient(nil) returns nil — caller code can compose without
    pre-checking the parent (every public method on the returned
    Client is nil-safe; that part is verified per-method as endpoints
    land).
 2. NewClient(parent) constructs all four sub-clients eagerly; the
    Trading / Account / MarketData / Stream getters return non-nil.
 3. The factory wired via init() makes bitget.Client.Spot() return a
    *spot.Client (typed as any) once this package is imported.
*/

package spot

import (
	"testing"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

// TestNewClient_Nil pins that NewClient gracefully degrades when the
// caller composes without bitget.NewClient succeeding.
func TestNewClient_Nil(t *testing.T) {
	if NewClient(nil) != nil {
		t.Fatalf("NewClient(nil) must return nil")
	}
}

// TestNewClient_BuildsSubClients pins that every sub-client is
// constructed eagerly so callers do not race on first access.
func TestNewClient_BuildsSubClients(t *testing.T) {
	var cfg bitget.Config = bitget.DefaultConfig()
	// Credentials are intentionally empty: M1 has no signed REST calls.
	// Once M2/M3 land tests will use the same DefaultConfig + a fake
	// signer to reach the contract layer without a real server.
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	defer func() { _ = parent.Close() }()

	var c *Client = NewClient(parent)
	if c == nil {
		t.Fatal("NewClient(parent) returned nil")
	}
	if c.Parent() != parent {
		t.Fatal("Parent() != parent")
	}
	if c.Trading() == nil {
		t.Fatal("Trading() must be non-nil after NewClient")
	}
	if c.Account() == nil {
		t.Fatal("Account() must be non-nil after NewClient")
	}
	if c.MarketData() == nil {
		t.Fatal("MarketData() must be non-nil after NewClient")
	}
	if c.Stream() == nil {
		t.Fatal("Stream() must be non-nil after NewClient")
	}
}

// TestSpotFactory_Registered pins the init() wiring: once spot is
// imported, bitget.Client.Spot() returns a *spot.Client (typed as
// any). This is the canonical entry point for desk-side adapters
// that hold only a *bitget.Client.
func TestSpotFactory_Registered(t *testing.T) {
	var cfg bitget.Config = bitget.DefaultConfig()
	var parent *bitget.Client
	var err error
	parent, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	defer func() { _ = parent.Close() }()

	var v any = parent.Spot()
	if v == nil {
		t.Fatal("bitget.Client.Spot() returned nil — factory not registered")
	}
	var c *Client
	var ok bool
	c, ok = v.(*Client)
	if !ok {
		t.Fatalf("bitget.Client.Spot() returned %T, want *spot.Client", v)
	}
	if c.Parent() != parent {
		t.Fatal("factory-built Client.Parent() != parent")
	}
}
