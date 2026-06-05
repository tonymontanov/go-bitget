/*
FILE: broker/contract_test.go

DESCRIPTION:
Shared contract-test harness for the BROKER profile: a body-aware
httptest.Server (mockBitget) plus a per-request dynamic variant
(mockBitgetDynamic) for cursor / page-number pagination tests. Fixtures
are hand-derived from the Bitget V2 broker docs and the tiagosiebler
reference client; no network calls.
*/

package broker

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	bitget "github.com/tonymontanov/go-bitget/v2"
)

// dec is a test shorthand for decimal.RequireFromString.
func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// atoiOr parses s or returns def on failure.
func atoiOr(s string, def int) int {
	var n, err = strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
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

// mockBitgetDynamic stands up a server whose JSON body is computed per
// request by `body` (used for pagination tests where the response depends
// on the query / body). Returns a wired bitget.Client.
func mockBitgetDynamic(
	t *testing.T,
	body func(t *testing.T, r *http.Request) string,
) (*httptest.Server, *bitget.Client) {
	t.Helper()

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "20")
		w.Header().Set("X-RateLimit-Remaining", "19")
		_, _ = io.WriteString(w, body(t, r))
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

// brokerClient resolves the broker.Client off a wired parent.
func brokerClient(t *testing.T, client *bitget.Client) *Client {
	t.Helper()
	var v any = client.Broker()
	var bc, ok = v.(*Client)
	if !ok || bc == nil {
		t.Fatalf("Broker(): want *broker.Client, got %T", v)
	}
	return bc
}
