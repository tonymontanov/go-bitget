/*
FILE: common/contract_test.go

DESCRIPTION:
Shared contract-test harness for the COMMON profile: a body-aware
httptest.Server (mockBitget) returning a wired bitget.Client, plus small
shared helpers. Fixtures are hand-derived from the Bitget V2 docs and the
tiagosiebler reference client; no network calls.
*/

package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

// dec is a test shorthand for decimal.RequireFromString.
func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func mockBitget(
	t *testing.T,
	routes map[string]string,
	inspect func(t *testing.T, r *http.Request, body []byte),
) (*httptest.Server, *bitget.Client) {
	t.Helper()

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		if r.Body != nil {
			raw, _ = io.ReadAll(r.Body)
		}
		if inspect != nil {
			inspect(t, r, raw)
		}
		var body string
		var ok bool
		body, ok = routes[r.URL.Path]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":"40404","msg":"no fixture for `+r.URL.Path+`","data":null,"requestTime":0}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "20")
		w.Header().Set("X-RateLimit-Remaining", "19")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	cfg.REST.RequestTimeout = 3 * time.Second

	var client *bitget.Client
	var err error
	client, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return srv, client
}

// mockBitgetDynamic is like mockBitget but hands the raw handler to the
// caller — used for stateful tests (e.g. cursor pagination across pages).
func mockBitgetDynamic(
	t *testing.T,
	handler func(w http.ResponseWriter, r *http.Request, body []byte),
) (*httptest.Server, *bitget.Client) {
	t.Helper()

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		if r.Body != nil {
			raw, _ = io.ReadAll(r.Body)
		}
		handler(w, r, raw)
	}))
	t.Cleanup(srv.Close)

	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	cfg.REST.RequestTimeout = 3 * time.Second

	var client *bitget.Client
	var err error
	client, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return srv, client
}

// commonClient resolves the common.Client off a wired parent.
func commonClient(t *testing.T, client *bitget.Client) *Client {
	t.Helper()
	var v any = client.Common()
	var cc, ok = v.(*Client)
	if !ok || cc == nil {
		t.Fatalf("Common(): want *common.Client, got %T", v)
	}
	return cc
}
