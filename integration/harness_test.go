//go:build integration

/*
FILE: integration/harness_test.go

DESCRIPTION:
Shared harness for the LIVE / DEMO integration suite. These tests hit the
real Bitget host (api.bitget.com) and are excluded from the normal build —
run them explicitly:

	go test -tags integration ./integration/... -v

SAFETY:
  - Demo (paper) trading is FORCED on by default (cfg.Demo = true) so every
    request carries `paptrading: 1`. Use a DEMO API Key. Set BITGET_DEMO=0
    only if you deliberately want to hit live (not recommended).
  - Read tests run whenever credentials are present; unsigned public tests
    run even without credentials.
  - WRITE tests (place / cancel real paper orders) are gated behind
    BITGET_ITEST_WRITE=1 AND demo mode, and place far-from-market limit
    orders that are cancelled immediately.

ENV:

	BITGET_API_KEY / BITGET_SECRET_KEY / BITGET_PASSPHRASE  — DEMO key triple
	BITGET_DEMO=1            (default) paper trading; 0 to disable
	BITGET_ITEST_WRITE=1     opt-in to paper write tests
	BITGET_ITEST_SYMBOL      futures symbol, default BTCUSDT
*/

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	bitget "github.com/tonymontanov/go-bitget/v2"
	"github.com/tonymontanov/go-bitget/v2/uta"
	utatypes "github.com/tonymontanov/go-bitget/v2/uta/types"

	// Blank imports register the lazy profile factories.
	_ "github.com/tonymontanov/go-bitget/v2/common"
)

const defaultSymbol = "BTCUSDT"

var futures = utatypes.CategoryUSDTFutures

func symbol() string {
	if s := os.Getenv("BITGET_ITEST_SYMBOL"); s != "" {
		return s
	}
	return defaultSymbol
}

func demoEnabled() bool {
	// Demo is ON unless explicitly disabled.
	return os.Getenv("BITGET_DEMO") != "0"
}

func writeEnabled() bool {
	return os.Getenv("BITGET_ITEST_WRITE") == "1"
}

func hasCreds() bool {
	return os.Getenv("BITGET_API_KEY") != "" &&
		os.Getenv("BITGET_SECRET_KEY") != "" &&
		os.Getenv("BITGET_PASSPHRASE") != ""
}

// newClient builds a client from env. demo defaults ON.
func newClient(t *testing.T) *bitget.Client {
	t.Helper()
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.APIKey = os.Getenv("BITGET_API_KEY")
	cfg.SecretKey = os.Getenv("BITGET_SECRET_KEY")
	cfg.Passphrase = os.Getenv("BITGET_PASSPHRASE")
	cfg.Demo = demoEnabled()
	cfg.REST.RequestTimeout = 15 * time.Second

	var c *bitget.Client
	var err error
	c, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// utaClient resolves the *uta.Client off a fresh parent.
func utaClient(t *testing.T) *uta.Client {
	t.Helper()
	var v any = newClient(t).UTA()
	var u, ok = v.(*uta.Client)
	if !ok || u == nil {
		t.Fatalf("UTA(): want *uta.Client, got %T", v)
	}
	return u
}

// requireCreds skips the test when no API credentials are configured.
func requireCreds(t *testing.T) {
	t.Helper()
	if !hasCreds() {
		t.Skip("skipping signed test: set BITGET_API_KEY / BITGET_SECRET_KEY / BITGET_PASSPHRASE")
	}
}

// requireWrite skips unless paper writes are explicitly opted in (and demo).
func requireWrite(t *testing.T) {
	t.Helper()
	requireCreds(t)
	if !demoEnabled() {
		t.Skip("refusing to run write tests outside demo mode (set BITGET_DEMO=1)")
	}
	if !writeEnabled() {
		t.Skip("skipping paper write test: set BITGET_ITEST_WRITE=1 to enable")
	}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
