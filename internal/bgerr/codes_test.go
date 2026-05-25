/*
FILE: internal/bgerr/codes_test.go

DESCRIPTION:
Table-driven tests for MapHTTPStatus / MapBitgetCode. Bitget extends the
code list over time; whenever a new code is added to codes.go, append
a row here so a future renumbering of Kinds is loud-failure rather than
silent.
*/

package bgerr

import "testing"

func TestMapHTTPStatus(t *testing.T) {
	var cases = []struct {
		name   string
		status int
		want   ErrorKind
	}{
		{"400 bad request", 400, ErrorKindInvalidRequest},
		{"401 unauthorized", 401, ErrorKindAuth},
		{"403 forbidden", 403, ErrorKindAuth},
		{"418 teapot", 418, ErrorKindInvalidRequest},
		{"429 too many", 429, ErrorKindRateLimit},
		{"500 server error", 500, ErrorKindNetwork},
		{"503 unavailable", 503, ErrorKindNetwork},
		{"600 unknown", 600, ErrorKindUnknown},
		{"200 success", 200, ErrorKindUnknown},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var c = cases[i]
		t.Run(c.name, func(t *testing.T) {
			var got ErrorKind = MapHTTPStatus(c.status)
			if got != c.want {
				t.Fatalf("MapHTTPStatus(%d) = %v, want %v", c.status, got, c.want)
			}
		})
	}
}

func TestMapBitgetCode(t *testing.T) {
	var cases = []struct {
		code string
		want ErrorKind
	}{
		// auth family
		{"40001", ErrorKindAuth},
		{"40004", ErrorKindAuth},
		{"40009", ErrorKindAuth},
		{"40011", ErrorKindAuth},
		{"40012", ErrorKindAuth},
		{"40018", ErrorKindAuth},
		// invalid request
		{"40007", ErrorKindInvalidRequest},
		{"40017", ErrorKindInvalidRequest},
		{"40021", ErrorKindInvalidRequest},
		{"40034", ErrorKindInvalidRequest},
		{"40037", ErrorKindInvalidRequest},
		{"40109", ErrorKindInvalidRequest},
		{"43011", ErrorKindInvalidRequest},
		{"45110", ErrorKindInvalidRequest},
		// rate limit
		{"40029", ErrorKindRateLimit},
		{"47001", ErrorKindRateLimit},
		// network / transient
		{"40010", ErrorKindNetwork},
		{"40015", ErrorKindNetwork},
		{"40725", ErrorKindNetwork},
		// exchange business
		{"40754", ErrorKindExchange},
		{"50067", ErrorKindExchange},
		// fallback
		{"99999", ErrorKindExchange},
		{"", ErrorKindExchange},
	}
	var i int
	for i = 0; i < len(cases); i++ {
		var c = cases[i]
		t.Run(c.code, func(t *testing.T) {
			var got ErrorKind = MapBitgetCode(c.code, "")
			if got != c.want {
				t.Fatalf("MapBitgetCode(%q) = %v, want %v", c.code, got, c.want)
			}
		})
	}
}

func TestErrorKindString(t *testing.T) {
	var cases = map[ErrorKind]string{
		ErrorKindUnknown:        "unknown",
		ErrorKindNetwork:        "network",
		ErrorKindRateLimit:      "rate_limit",
		ErrorKindAuth:           "auth",
		ErrorKindInvalidRequest: "invalid_request",
		ErrorKindExchange:       "exchange",
	}
	var k ErrorKind
	var v string
	for k, v = range cases {
		if k.String() != v {
			t.Fatalf("ErrorKind(%d).String() = %q, want %q", k, k.String(), v)
		}
	}
}
