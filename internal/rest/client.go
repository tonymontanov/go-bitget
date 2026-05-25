/*
FILE: internal/rest/client.go

DESCRIPTION:
Low-level Bitget REST client. Stays at the transport / envelope layer:

  - assembles the URL (BaseURL + path + canonical query);
  - signs requests via auth.Signer (ACCESS-* headers);
  - executes the HTTP call honouring ctx deadline / Config.RequestTimeout;
  - parses the Bitget envelope { code, msg, data, requestTime };
  - maps non-success code and 4xx/5xx HTTP statuses into *bgerr.Error with
    the right Kind via bgerr.MapBitgetCode / bgerr.MapHTTPStatus;
  - notifies the legacy and event-based rate-limit observers with whatever
    rate-limit headers Bitget returned.

WHY HEADERS ARE FORWARDED 1:1 (LIKE BYBIT, UNLIKE OKX):
Bitget returns a small set of rate-limit metadata headers on most signed
REST responses (X-RateLimit-Used / X-RateLimit-Limit / Retry-After). They
are forwarded as-is so an external rate-limiter at the desk level can
reconcile its model with the live remaining budget.

DESIGN:
  - The package does NOT import the public root (which imports rest), so
    everything we need lives in internal/* (auth, codec, bgerr, bglog).
  - Domain layers (mix/trading.go, etc.) call Do() with a populated
    Options.Meta describing OrderCount / Symbols / Category. The metadata
    is forwarded to the event-observer 1:1; the legacy observer receives
    only (endpoint, headers) for source-level back-compat with the OKX-
    style observer pattern.
*/

package rest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tonymontanov/go-bitget/v2/internal/auth"
	"github.com/tonymontanov/go-bitget/v2/internal/bgerr"
	"github.com/tonymontanov/go-bitget/v2/internal/bglog"
	"github.com/tonymontanov/go-bitget/v2/internal/codec"
)

// Config — REST transport parameters. Populated from public Config.REST in
// the root package via explicit field copy (avoids an import cycle: the
// root package imports internal/rest, not the other way around).
type Config struct {
	// RequestTimeout — global timeout for a single REST call. ctx with its
	// own deadline overrides this for a particular request.
	RequestTimeout time.Duration
	// MaxIdleConns — http.Transport pool size.
	MaxIdleConns int
	// MaxIdleConnsPerHost — http.Transport per-host pool size.
	MaxIdleConnsPerHost int
	// IdleConnTimeout — keep-alive idle timeout.
	IdleConnTimeout time.Duration
	// Locale — value of the "locale" header. Bitget honours "en-US",
	// "zh-CN", "ja-JP" and a handful of others — the SDK defaults to
	// "en-US" so error messages are English. Empty → header omitted.
	Locale string
	// RateLimitObserver — legacy callback. Receives only (endpoint, headers).
	// nil → no-op.
	RateLimitObserver func(endpoint string, headers map[string]string)
	// RateLimitEventObserver — primary callback. Receives every REST
	// response with the full RequestMeta (OrderCount/Symbols/Category) plus
	// the live rate-limit headers. nil → no-op.
	//
	// Speed contract: called synchronously from the goroutine that issued
	// the REST request. Implementations must be O(1) — typically a
	// non-blocking send to a buffered channel. Blocking the observer
	// stalls the REST pipeline.
	RateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// RequestMeta — domain-level information about the request that the
// external rate-limiter needs to model Bitget limits accurately. The
// meta is set by the calling domain method (mix/trading.go etc.) at the
// point where the symbol set, batch size and category are known.
//
// Fields:
//
//   - OrderCount: 1 for single-order endpoints, len(orders) for batch
//     endpoints, 0 for non-trading. Bitget accounts for batch
//     budgets in orders, not requests.
//   - Symbols:    sorted unique list of symbols affected by the request.
//     Bitget V2 trading limits are per (UID + Symbol) on
//     contracts; the rate-limiter uses this to debit only the
//     affected symbol(s) instead of blocking all of them.
//   - Category:   "place" | "amend" | "cancel" | "query" | "market" | "".
//     Used by the rate-limiter to model the sub-account-level
//     NEW+AMEND budget separately from cancellations and
//     queries.
//   - Endpoint:   the request path in canonical form
//     (e.g. "/api/v2/mix/order/place-order"). Set by the rest
//     client itself just before invoking the observer; callers
//     do not need to populate it.
type RequestMeta struct {
	OrderCount int
	Symbols    []string
	Category   string
	// Endpoint — populated by Do() before invoking the observer; ignored on
	// input.
	Endpoint string
}

// Options — parameters for a single REST request.
type Options struct {
	// Method — HTTP method, must be upper-case ("GET", "POST", ...). The
	// client also tolerates lower-case and upper-cases internally.
	Method string
	// Path — request path including the leading "/" (e.g.
	// "/api/v2/mix/order/place-order").
	Path string
	// Query — query parameters; serialized in URL-encoded form. For signed
	// GET requests the canonical query string is part of the signature
	// pre-hash (Bitget signs path?query for GET).
	Query url.Values
	// Body — JSON body. Marshalled by codec; the resulting bytes are used
	// both for the wire and for the signature pre-hash. Pass nil for GET.
	Body any
	// Signed — true for endpoints that require ACCESS-SIGN.
	Signed bool
	// Meta — request metadata for the rate-limit observer.
	Meta RequestMeta
}

// Response — Bitget response envelope. The data is kept as raw JSON so
// domain methods can decode into typed structs without re-marshalling.
type Response struct {
	Code        string        `json:"code"`
	Msg         string        `json:"msg"`
	Data        codec.RawJSON `json:"data"`
	RequestTime int64         `json:"requestTime"`
}

// UnmarshalData decodes the Data field into dest. No-op if Data is missing
// or "null".
func (r Response) UnmarshalData(dest any) error {
	if r.Data.IsNull() {
		return nil
	}
	return codec.Unmarshal(r.Data, dest)
}

// Client — low-level REST client.
type Client struct {
	httpClient             *http.Client
	signer                 *auth.Signer
	baseURL                string
	userAgent              string
	locale                 string
	logger                 bglog.Logger
	rateLimitObserver      func(endpoint string, headers map[string]string)
	rateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// NewClient creates a REST client. signer may have empty credentials —
// public endpoints will still work, signed calls will surface
// auth.ErrSignerDisabled at call time.
func NewClient(baseURL string, signer *auth.Signer, cfg Config, ua string, log bglog.Logger) *Client {
	if log == nil {
		log = bglog.Noop()
	}
	var transport *http.Transport = &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		ForceAttemptHTTP2:   true,
	}
	var httpClient *http.Client = &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
	}
	return &Client{
		httpClient:             httpClient,
		signer:                 signer,
		baseURL:                strings.TrimRight(baseURL, "/"),
		userAgent:              ua,
		locale:                 cfg.Locale,
		logger:                 log,
		rateLimitObserver:      cfg.RateLimitObserver,
		rateLimitEventObserver: cfg.RateLimitEventObserver,
	}
}

// Close releases idle transport connections.
func (c *Client) Close() {
	if c == nil || c.httpClient == nil {
		return
	}
	if t, ok := c.httpClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

/*
Do executes a single REST call. Returns the response envelope, the
collected rate-limit headers, and an error.

Error semantics:
  - context cancel/deadline / network failures → *bgerr.Error with
    Kind=Network, Cause=underlying error.
  - HTTP 4xx/5xx without a parseable envelope → *bgerr.Error with
    Kind = bgerr.MapHTTPStatus(status), HTTPStatus set, Message=truncated body.
  - HTTP 2xx with code != "00000"          → *bgerr.Error with
    Kind = bgerr.MapBitgetCode(code, msg), BitgetCode=code,
    Message=msg, HTTPStatus=2xx.
  - HTTP 4xx/5xx WITH a parseable envelope (Bitget sometimes wraps errors
    in the standard envelope on 5xx)        → same as above.

The rate-limit headers map is always non-nil but may be empty (e.g. for
public endpoints that do not advertise per-UID limits). It is allocated
fresh on every call so observers may safely retain references.
*/
func (c *Client) Do(ctx context.Context, opts Options) (Response, map[string]string, error) {
	var resp Response
	var rateHeaders map[string]string = map[string]string{}

	// Build URL and body BEFORE signing — the signature must cover the
	// exact bytes that go on the wire.
	var fullURL string
	var bodyStr string
	var signPath string
	var err error
	fullURL, bodyStr, signPath, err = c.buildRequest(opts)
	if err != nil {
		return resp, rateHeaders, err
	}

	var method string = strings.ToUpper(opts.Method)

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, method, fullURL, bytes.NewBufferString(bodyStr))
	if err != nil {
		return resp, rateHeaders, bgerr.New(bgerr.ErrorKindInvalidRequest, "", "rest: build request", err)
	}

	c.applyHeaders(req, opts, method, bodyStr, signPath)

	var httpResp *http.Response
	var started time.Time = time.Now()
	httpResp, err = c.httpClient.Do(req)
	if err != nil {
		return resp, rateHeaders, classifyTransportError(err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	// Collect the rate-limit headers BEFORE notifying observers and
	// BEFORE parsing the body. Even if the body turns out to be malformed,
	// the headers are still meaningful.
	rateHeaders = collectRateLimitHeaders(httpResp.Header)

	// Notify observers. The observer is called with the canonical endpoint
	// from opts.Path (we deliberately do NOT pass the full URL — observers
	// key off endpoints, not URLs).
	if c.rateLimitObserver != nil || c.rateLimitEventObserver != nil {
		var meta RequestMeta = opts.Meta
		meta.Endpoint = opts.Path
		if c.rateLimitObserver != nil {
			c.rateLimitObserver(opts.Path, rateHeaders)
		}
		if c.rateLimitEventObserver != nil {
			c.rateLimitEventObserver(opts.Path, method, rateHeaders, meta)
		}
	}

	var raw []byte
	raw, err = io.ReadAll(httpResp.Body)
	if err != nil {
		return resp, rateHeaders, bgerr.New(bgerr.ErrorKindNetwork, "", "rest: read body", err)
	}

	c.logger.Debug(
		"rest.Do",
		bglog.Str("method", method),
		bglog.Str("path", opts.Path),
		bglog.Int("status", int64(httpResp.StatusCode)),
		bglog.Int("durationMs", time.Since(started).Milliseconds()),
		bglog.Int("bytes", int64(len(raw))),
	)

	// Try to decode the envelope on every status. Bitget returns the same
	// {code, msg, data} wrapper on 4xx/5xx for application-level
	// validation errors and only falls back to plain-text on infra
	// failures (LB returning 502 with HTML, etc.).
	var parseErr error = codec.Unmarshal(raw, &resp)

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		if parseErr != nil {
			return resp, rateHeaders, bgerr.New(bgerr.ErrorKindUnknown, "", "rest: parse response", parseErr)
		}
		if resp.Code != bgerr.CodeOK && resp.Code != "" {
			return resp, rateHeaders, &bgerr.Error{
				Kind:       bgerr.MapBitgetCode(resp.Code, resp.Msg),
				HTTPStatus: httpResp.StatusCode,
				BitgetCode: resp.Code,
				Message:    resp.Msg,
			}
		}
		return resp, rateHeaders, nil
	}

	// Non-2xx path. Prefer the typed envelope when available.
	if parseErr == nil && resp.Code != "" && resp.Code != bgerr.CodeOK {
		return resp, rateHeaders, &bgerr.Error{
			Kind:       bgerr.MapBitgetCode(resp.Code, resp.Msg),
			HTTPStatus: httpResp.StatusCode,
			BitgetCode: resp.Code,
			Message:    resp.Msg,
		}
	}
	return resp, rateHeaders, &bgerr.Error{
		Kind:       bgerr.MapHTTPStatus(httpResp.StatusCode),
		HTTPStatus: httpResp.StatusCode,
		Message:    truncate(string(raw), 256),
	}
}

// buildRequest assembles the URL, the body string and the signature path
// (path + canonical query). Bitget signs the FULL request path including
// query string for GET requests, so signPath = "/api/v2/...?k1=v1&k2=v2".
// For POST the canonical query is empty and signPath is just opts.Path.
func (c *Client) buildRequest(opts Options) (string, string, string, error) {
	var u *url.URL
	var err error
	u, err = url.Parse(c.baseURL + opts.Path)
	if err != nil {
		return "", "", "", bgerr.New(bgerr.ErrorKindInvalidRequest, "", "rest: invalid url", err)
	}
	var canonicalQuery string
	if len(opts.Query) > 0 {
		canonicalQuery = opts.Query.Encode()
		u.RawQuery = canonicalQuery
	}

	var bodyStr string
	if opts.Body != nil {
		var raw []byte
		raw, err = codec.Marshal(opts.Body)
		if err != nil {
			return "", "", "", bgerr.New(bgerr.ErrorKindInvalidRequest, "", "rest: marshal body", err)
		}
		bodyStr = string(raw)
	}

	var signPath string = opts.Path
	if canonicalQuery != "" {
		signPath = signPath + "?" + canonicalQuery
	}
	return u.String(), bodyStr, signPath, nil
}

// applyHeaders sets common headers and, for signed calls, the Bitget
// ACCESS-* headers.
func (c *Client) applyHeaders(req *http.Request, opts Options, method, body, signPath string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.locale != "" {
		req.Header.Set("locale", c.locale)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}

	if !opts.Signed {
		return
	}
	if c.signer == nil || !c.signer.Enabled() {
		// Surface signing failure later via auth.ErrSignerDisabled at the
		// call site that builds the error explicitly. The transport layer
		// keeps going so that public-only embedders are not broken.
		return
	}

	// Bitget signs:
	//   - GET:  preHash = ts + "GET"  + path + "?" + query   (body == "")
	//   - POST: preHash = ts + "POST" + path                  (body == json)
	// The rest client passes signPath already containing "?query" for GET.
	var ts string = c.signer.MillisTimestamp(time.Now())
	var signature string
	var err error
	signature, err = c.signer.SignREST(ts, method, signPath, body)
	if err != nil {
		c.logger.Warn("rest: sign skipped", bglog.Err(err))
		return
	}
	req.Header.Set("ACCESS-KEY", c.signer.APIKey())
	req.Header.Set("ACCESS-TIMESTAMP", ts)
	req.Header.Set("ACCESS-PASSPHRASE", c.signer.Passphrase())
	req.Header.Set("ACCESS-SIGN", signature)
}

// rateLimitHeaderAllowList enumerates the headers Bitget ships with rate-
// limit metadata. We hard-code the list to avoid leaking unrelated headers
// (cookies, auth) into observer maps that may be logged downstream.
var rateLimitHeaderAllowList = map[string]struct{}{
	"x-ratelimit-limit":     {},
	"x-ratelimit-remaining": {},
	"x-ratelimit-reset":     {},
	"x-ratelimit-used":      {},
	"retry-after":           {},
}

// collectRateLimitHeaders extracts the rate-limit metadata that Bitget
// returns. The header keys are normalised to canonical Go form (http.Header
// already does that on receive). The returned map is fresh on every call.
func collectRateLimitHeaders(h http.Header) map[string]string {
	var out map[string]string = map[string]string{}
	var name string
	var values []string
	for name, values = range h {
		if len(values) == 0 {
			continue
		}
		var lower string = strings.ToLower(name)
		if _, ok := rateLimitHeaderAllowList[lower]; ok {
			out[name] = values[0]
		}
	}
	return out
}

// classifyTransportError converts a transport error into *bgerr.Error
// with Kind=Network. Distinguishes context cancel / deadline so callers
// can use errors.Is(err, context.Canceled) when needed.
func classifyTransportError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return bgerr.New(bgerr.ErrorKindNetwork, "", "rest: context canceled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return bgerr.New(bgerr.ErrorKindNetwork, "", "rest: deadline exceeded", err)
	}
	return bgerr.New(bgerr.ErrorKindNetwork, "", "rest: transport error", err)
}

// truncate returns at most n bytes of s, appending an ellipsis when cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
