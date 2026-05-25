/*
FILE: internal/bgerr/codes_test.go

DESCRIPTION:
Table-driven tests for MapHTTPStatus / MapBitgetCode. Bitget extends the
code list over time; whenever a new code is added to codes.go, append
a row here so a future renumbering of Kinds is loud-failure rather than
silent.

The table is grouped by Kind so that a contributor who flips the Kind
of an existing code immediately sees the row land in the wrong group
during code review.
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
		// ---- Auth (40xxx + select 22xxx/40700) ----
		{"40001", ErrorKindAuth},
		{"40002", ErrorKindAuth},
		{"40003", ErrorKindAuth},
		{"40004", ErrorKindAuth},
		{"40005", ErrorKindAuth},
		{"40006", ErrorKindAuth},
		{"40008", ErrorKindAuth},
		{"40009", ErrorKindAuth},
		{"40011", ErrorKindAuth},
		{"40012", ErrorKindAuth},
		{"40013", ErrorKindAuth},
		{"40014", ErrorKindAuth},
		{"40016", ErrorKindAuth},
		{"40018", ErrorKindAuth},
		{"40022", ErrorKindAuth},
		{"40023", ErrorKindAuth},
		{"40024", ErrorKindAuth},
		{"40025", ErrorKindAuth},
		{"40026", ErrorKindAuth},
		{"40036", ErrorKindAuth},
		{"40037", ErrorKindAuth}, // FIX: was InvalidRequest by mistake (40768 is "order not exist").
		{"40038", ErrorKindAuth},
		{"40040", ErrorKindAuth},
		{"40041", ErrorKindAuth},
		{"40710", ErrorKindAuth},
		{"40760", ErrorKindAuth},
		{"22010", ErrorKindAuth},

		// ---- InvalidRequest (params / lifecycle / risk / leverage) ----
		{"40007", ErrorKindInvalidRequest},
		{"40017", ErrorKindInvalidRequest},
		{"40019", ErrorKindInvalidRequest},
		{"40020", ErrorKindInvalidRequest},
		{"40021", ErrorKindInvalidRequest},
		{"40034", ErrorKindInvalidRequest},
		{"40072", ErrorKindInvalidRequest},
		{"40109", ErrorKindInvalidRequest},
		{"40303", ErrorKindInvalidRequest},
		{"40304", ErrorKindInvalidRequest},
		{"40305", ErrorKindInvalidRequest},
		{"40306", ErrorKindInvalidRequest},
		{"40402", ErrorKindInvalidRequest},
		{"40404", ErrorKindInvalidRequest},
		{"40409", ErrorKindInvalidRequest},
		{"40761", ErrorKindInvalidRequest},
		{"40762", ErrorKindInvalidRequest},
		{"40763", ErrorKindInvalidRequest},
		{"40764", ErrorKindInvalidRequest},
		{"40765", ErrorKindInvalidRequest},
		{"40768", ErrorKindInvalidRequest}, // canonical "Order does not exist".
		{"40769", ErrorKindInvalidRequest},
		{"40774", ErrorKindInvalidRequest},
		{"40814", ErrorKindInvalidRequest},
		{"40920", ErrorKindInvalidRequest},
		{"40923", ErrorKindInvalidRequest},
		{"40924", ErrorKindInvalidRequest},
		{"40925", ErrorKindInvalidRequest},
		{"40939", ErrorKindInvalidRequest},
		{"22001", ErrorKindInvalidRequest},
		{"22002", ErrorKindInvalidRequest}, // also "no-change-leverage" path used by desk-core.
		{"22003", ErrorKindInvalidRequest},
		{"22004", ErrorKindInvalidRequest},
		{"22005", ErrorKindInvalidRequest},
		{"22034", ErrorKindInvalidRequest},
		{"22038", ErrorKindInvalidRequest},
		{"43001", ErrorKindInvalidRequest},
		{"43004", ErrorKindInvalidRequest},
		{"43005", ErrorKindInvalidRequest},
		{"43006", ErrorKindInvalidRequest},
		{"43007", ErrorKindInvalidRequest},
		{"43008", ErrorKindInvalidRequest},
		{"43009", ErrorKindInvalidRequest},
		{"43011", ErrorKindInvalidRequest},
		{"43025", ErrorKindInvalidRequest},
		{"43030", ErrorKindInvalidRequest},
		{"43031", ErrorKindInvalidRequest},
		{"43048", ErrorKindInvalidRequest},
		{"43050", ErrorKindInvalidRequest},
		{"43118", ErrorKindInvalidRequest},
		{"45034", ErrorKindInvalidRequest},
		{"45035", ErrorKindInvalidRequest},
		{"45044", ErrorKindInvalidRequest},
		{"45045", ErrorKindInvalidRequest},
		{"45054", ErrorKindInvalidRequest},
		{"45055", ErrorKindInvalidRequest},
		{"45056", ErrorKindInvalidRequest},
		{"45057", ErrorKindInvalidRequest},
		{"45110", ErrorKindInvalidRequest},
		{"45111", ErrorKindInvalidRequest},
		{"45112", ErrorKindInvalidRequest},
		{"45113", ErrorKindInvalidRequest},
		{"45114", ErrorKindInvalidRequest},
		{"45115", ErrorKindInvalidRequest},
		{"45116", ErrorKindInvalidRequest},
		{"45117", ErrorKindInvalidRequest},
		{"45118", ErrorKindInvalidRequest},
		{"45119", ErrorKindInvalidRequest},
		{"45120", ErrorKindInvalidRequest},
		{"50001", ErrorKindInvalidRequest},
		{"50002", ErrorKindInvalidRequest},
		{"50003", ErrorKindInvalidRequest},
		{"50004", ErrorKindInvalidRequest},
		{"50060", ErrorKindInvalidRequest},

		// ---- RateLimit ----
		{"40029", ErrorKindRateLimit},
		{"45129", ErrorKindRateLimit},
		{"47001", ErrorKindRateLimit},
		{"59044", ErrorKindRateLimit},

		// ---- Network / transient ----
		{"40010", ErrorKindNetwork},
		{"40015", ErrorKindNetwork},
		{"40200", ErrorKindNetwork},
		{"40725", ErrorKindNetwork},
		{"40908", ErrorKindNetwork},
		{"40909", ErrorKindNetwork},
		{"40910", ErrorKindNetwork},
		{"45043", ErrorKindNetwork},
		{"50031", ErrorKindNetwork},
		{"50066", ErrorKindNetwork},

		// ---- Exchange (business rejections) ----
		{"22045", ErrorKindExchange},
		{"40309", ErrorKindExchange},
		{"40754", ErrorKindExchange},
		{"40755", ErrorKindExchange},
		{"40756", ErrorKindExchange},
		{"40757", ErrorKindExchange},
		{"40758", ErrorKindExchange},
		{"40798", ErrorKindExchange},
		{"40800", ErrorKindExchange},
		{"43012", ErrorKindExchange},
		{"45002", ErrorKindExchange},
		{"45003", ErrorKindExchange},
		{"45009", ErrorKindExchange},
		{"50020", ErrorKindExchange},
		{"50067", ErrorKindExchange},

		// ---- Fallback ----
		{"99999", ErrorKindExchange},
		{"", ErrorKindExchange},

		// ---- Success (defensive) ----
		{CodeOK, ErrorKindUnknown},
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

// TestMapBitgetCode_DefaultIsExchange ensures that any code outside the
// explicit mapping falls back to Exchange (NOT Unknown), so callers
// always get a typed Kind they can branch on.
func TestMapBitgetCode_DefaultIsExchange(t *testing.T) {
	var randomCodes = []string{"12345", "70000", "abcde", "  "}
	var i int
	for i = 0; i < len(randomCodes); i++ {
		var got ErrorKind = MapBitgetCode(randomCodes[i], "")
		if got != ErrorKindExchange {
			t.Fatalf("MapBitgetCode(%q) = %v, want ErrorKindExchange",
				randomCodes[i], got)
		}
	}
}
