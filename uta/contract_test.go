/*
FILE: uta/contract_test.go

DESCRIPTION:
Shared contract-test harness for the V3 UTA profile: a route-table
httptest.Server (mockBitget) plus a dynamic variant, returning a wired
bitget.Client. Fixtures are hand-derived from the Bitget V3 docs and the
tiagosiebler reference client; no network calls.
*/

package uta

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

// decv is a test shorthand for decimal.RequireFromString.
func decv(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func newClient(t *testing.T, baseURL string, demo bool) *bitget.Client {
	t.Helper()
	var cfg bitget.Config = bitget.DefaultConfig()
	cfg.REST.BaseURL = baseURL
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	cfg.Demo = demo
	cfg.REST.RequestTimeout = 3 * time.Second

	var client *bitget.Client
	var err error
	client, err = bitget.NewClient(cfg)
	if err != nil {
		t.Fatalf("bitget.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

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

	return srv, newClient(t, srv.URL, false)
}

// mockBitgetDynamic hands the raw handler to the caller — used for
// stateful tests (e.g. cursor pagination) and header assertions.
func mockBitgetDynamic(
	t *testing.T,
	demo bool,
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

	return srv, newClient(t, srv.URL, demo)
}

// utaClient resolves the uta.Client off a wired parent.
func utaClient(t *testing.T, client *bitget.Client) *Client {
	t.Helper()
	var v any = client.UTA()
	var uc, ok = v.(*Client)
	if !ok || uc == nil {
		t.Fatalf("UTA(): want *uta.Client, got %T", v)
	}
	return uc
}
