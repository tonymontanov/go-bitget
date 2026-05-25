/*
FILE: metrics.go

DESCRIPTION:
Public re-export of the metrics interfaces defined in internal/bgmet.
The SDK emits only monotonic counters; histograms/gauges are out of
scope (embedders that want timing distributions should wrap user-facing
calls themselves).

STABLE COUNTER NAMES (the SDK contract — see README):

	bitget_ws_messages_received_total
	bitget_ws_messages_dropped_total
	bitget_ws_reconnects_total
	bitget_ws_subscriptions_total
	bitget_ws_ping_failed_total
	bitget_ws_auth_ok_total
	bitget_ws_auth_failed_total
*/

package bitget

import "github.com/tonymontanov/go-bitget/v2/internal/bgmet"

// Counter is a monotonically increasing metric.
type Counter = bgmet.Counter

// CounterFactory creates named counters. Implementations may attach common
// labels at construction time.
type CounterFactory = bgmet.CounterFactory

// NoopMetrics returns a CounterFactory that discards every increment.
func NoopMetrics() CounterFactory { return bgmet.Noop() }
