/*
FILE: internal/rest/client_test.go

DESCRIPTION:
httptest-based contract tests for the low-level REST client. We assert
that:

  - GET / POST endpoints sign correctly (ACCESS-KEY / ACCESS-SIGN /
    ACCESS-PASSPHRASE / ACCESS-TIMESTAMP headers present, signature
    matches the canonical recipe).
  - Bitget envelope { code, msg, data, requestTime } is decoded correctly.
  - Non-success codes are mapped to *bgerr.Error with the right Kind.
  - HTTP 4xx/5xx without a parseable envelope falls back to MapHTTPStatus.
  - Network failures (context cancel, server hangup) surface as
    ErrorKindNetwork.
  - Rate-limit headers are propagated to the observer.

The fixtures used here are minimal payloads that match the actual Bitget
shape; full per-endpoint contract tests live in mix/ packages.
*/

package rest

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/tonymontanov/go-bitget/v2/internal/auth"
	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
)

const (
	tApiKey     = "bg_test_apikey_AAAA"
	tSecret     = "test_secret_xx_yy"
	tPassphrase = "the-passphrase"
)

// newTestClient builds a *Client targeted at the given mock server URL.
func newTestClient(t *testing.T, srv *httptest.Server, observer func(string, string, map[string]string, RequestMeta)) *Client {
	t.Helper()
	var signer *auth.Signer = auth.NewSigner(tApiKey, tSecret, tPassphrase)
	return NewClient(srv.URL, signer, Config{
		RequestTimeout:         5 * time.Second,
		MaxIdleConns:           4,
		MaxIdleConnsPerHost:    4,
		IdleConnTimeout:        30 * time.Second,
		Locale:                 "en-US",
		RateLimitEventObserver: observer,
	}, "go-bitget-test/1", nil)
}

func TestDoSuccessGet(t *testing.T) {
	var captured *http.Request
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.Header().Set("X-RateLimit-Used", "5")
		w.Header().Set("X-RateLimit-Limit", "20")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"symbol":"BTCUSDT"},"requestTime":1700000001000}`))
	}))
	defer srv.Close()

	var observerCalls int
	var observedHeaders map[string]string
	var observedMeta RequestMeta
	var observer = func(_, _ string, h map[string]string, m RequestMeta) {
		observerCalls++
		observedHeaders = h
		observedMeta = m
	}
	var c *Client = newTestClient(t, srv, observer)

	var ctx context.Context = context.Background()
	var resp Response
	var hdrs map[string]string
	var err error
	resp, hdrs, err = c.Do(ctx, Options{
		Method: "GET",
		Path:   "/api/v2/mix/market/depth",
		Query:  map[string][]string{"symbol": {"BTCUSDT"}, "limit": {"20"}},
		Signed: true,
		Meta:   RequestMeta{OrderCount: 0, Symbols: []string{"BTCUSDT"}, Category: "market"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.Code != "00000" {
		t.Fatalf("resp.Code = %q, want 00000", resp.Code)
	}

	// Headers and observer.
	if hdrs["X-Ratelimit-Used"] != "5" {
		t.Fatalf("X-Ratelimit-Used header missing: %v", hdrs)
	}
	if observerCalls != 1 {
		t.Fatalf("observer calls = %d, want 1", observerCalls)
	}
	if observedHeaders["X-Ratelimit-Limit"] != "20" {
		t.Fatalf("observed headers wrong: %v", observedHeaders)
	}
	if observedMeta.Endpoint != "/api/v2/mix/market/depth" || observedMeta.Category != "market" {
		t.Fatalf("observed meta = %+v", observedMeta)
	}

	// Signed-request headers.
	if captured == nil {
		t.Fatal("server did not capture the request")
	}
	if got := captured.Header.Get("ACCESS-KEY"); got != tApiKey {
		t.Fatalf("ACCESS-KEY = %q", got)
	}
	if got := captured.Header.Get("ACCESS-PASSPHRASE"); got != tPassphrase {
		t.Fatalf("ACCESS-PASSPHRASE = %q", got)
	}
	var ts string = captured.Header.Get("ACCESS-TIMESTAMP")
	var sig string = captured.Header.Get("ACCESS-SIGN")
	if ts == "" || sig == "" {
		t.Fatalf("missing ACCESS-TIMESTAMP/SIGN: %q %q", ts, sig)
	}
	if _, parseErr := strconv.ParseInt(ts, 10, 64); parseErr != nil {
		t.Fatalf("ACCESS-TIMESTAMP not numeric: %v", parseErr)
	}
	if _, decodeErr := base64.StdEncoding.DecodeString(sig); decodeErr != nil {
		t.Fatalf("ACCESS-SIGN not base64: %v", decodeErr)
	}

	// Recompute and assert: canonical-query order matters; url.Values.Encode()
	// sorts keys alphabetically (limit < symbol).
	var signer *auth.Signer = auth.NewSigner(tApiKey, tSecret, tPassphrase)
	var expected, _ = signer.SignREST(ts, "GET", "/api/v2/mix/market/depth?limit=20&symbol=BTCUSDT", "")
	if expected != sig {
		t.Fatalf("signature mismatch:\nwant %q\ngot  %q", expected, sig)
	}
	if got := captured.Header.Get("locale"); got != "en-US" {
		t.Fatalf("locale = %q", got)
	}
}

func TestDoSuccessPost(t *testing.T) {
	var capturedBody string
	var capturedSig string
	var capturedTs string
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b []byte = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		capturedBody = string(b)
		capturedSig = r.Header.Get("ACCESS-SIGN")
		capturedTs = r.Header.Get("ACCESS-TIMESTAMP")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"00000","msg":"success","data":{"orderId":"42"},"requestTime":1}`))
	}))
	defer srv.Close()

	var c *Client = newTestClient(t, srv, nil)

	var resp Response
	var err error
	resp, _, err = c.Do(context.Background(), Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/place-order",
		Body:   map[string]any{"symbol": "BTCUSDT", "side": "buy"},
		Signed: true,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.Code != "00000" {
		t.Fatalf("resp.Code = %q", resp.Code)
	}

	// The signature must cover the EXACT byte sequence on the wire.
	var signer *auth.Signer = auth.NewSigner(tApiKey, tSecret, tPassphrase)
	var expected, _ = signer.SignREST(capturedTs, "POST", "/api/v2/mix/order/place-order", capturedBody)
	if expected != capturedSig {
		t.Fatalf("signature mismatch:\nwant %q\ngot  %q\nbody=%q", expected, capturedSig, capturedBody)
	}
}

func TestDoExchangeRejection(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"40037","msg":"order does not exist","data":null,"requestTime":1}`))
	}))
	defer srv.Close()

	var c *Client = newTestClient(t, srv, nil)
	var _, _, err = c.Do(context.Background(), Options{
		Method: "POST",
		Path:   "/api/v2/mix/order/cancel-order",
		Body:   map[string]any{"orderId": "42"},
		Signed: true,
	})
	if err == nil {
		t.Fatal("Do: want error, got nil")
	}
	if !bgerr.IsInvalidRequest(err) {
		t.Fatalf("want IsInvalidRequest, got %v", err)
	}
	var be *bgerr.Error
	if !asBgErr(err, &be) {
		t.Fatalf("not *bgerr.Error: %T", err)
	}
	if be.BitgetCode != "40037" {
		t.Fatalf("BitgetCode = %q", be.BitgetCode)
	}
}

func TestDoRateLimit(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"40029","msg":"rate limit exceeded","data":null,"requestTime":1}`))
	}))
	defer srv.Close()
	var c *Client = newTestClient(t, srv, nil)
	var _, hdrs, err = c.Do(context.Background(), Options{Method: "GET", Path: "/x"})
	if err == nil || !bgerr.IsRateLimit(err) {
		t.Fatalf("want IsRateLimit, got %v", err)
	}
	if hdrs["Retry-After"] != "1" {
		t.Fatalf("Retry-After missing: %v", hdrs)
	}
}

func TestDo5xxNonEnvelope(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<html>502 Bad Gateway</html>`))
	}))
	defer srv.Close()
	var c *Client = newTestClient(t, srv, nil)
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/x"})
	if err == nil || !bgerr.IsNetwork(err) {
		t.Fatalf("want IsNetwork (502), got %v", err)
	}
}

func TestDoCtxCancel(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	var c *Client = newTestClient(t, srv, nil)
	var ctx, cancel = context.WithCancel(context.Background())
	cancel()
	var _, _, err = c.Do(ctx, Options{Method: "GET", Path: "/x"})
	if err == nil || !bgerr.IsNetwork(err) {
		t.Fatalf("want IsNetwork on ctx cancel, got %v", err)
	}
}

func TestDoUnsignedPublic(t *testing.T) {
	var sawSign bool
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSign = r.Header.Get("ACCESS-SIGN") != ""
		_, _ = w.Write([]byte(`{"code":"00000","msg":"","data":[]}`))
	}))
	defer srv.Close()
	var c *Client = newTestClient(t, srv, nil)
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/public"})
	if err != nil {
		t.Fatal(err)
	}
	if sawSign {
		t.Fatal("public endpoint received ACCESS-SIGN")
	}
}

// asBgErr is a tiny errors.As shim that does not pull in the errors pkg
// in the prod path.
func asBgErr(err error, dest **bgerr.Error) bool {
	var be *bgerr.Error
	be, ok := err.(*bgerr.Error)
	if ok {
		*dest = be
		return true
	}
	return false
}
