/*
FILE: internal/ws/write-gate.go

DESCRIPTION:
Outbound message budget of ONE Bitget WebSocket connection.

Bitget caps a connection at 10 client messages per second (subscribe /
unsubscribe / login / the plain-text "ping" all count; RFC 6455 control
frames do not). A burst above the cap is answered by a silent drop of
the socket — no close frame — which the supervisor then treats as an
ordinary disconnect and re-subscribes everything, hitting the cap again
if the registry is large. The gate sits under writeMu in Conn.writeFrame
and delays a write until it fits the sliding window, so N Watch* calls
in a row cost at most N/limit seconds instead of a reconnect loop.

The gate is a pure sliding-window counter: it keeps the send instants of
the last `limit` writes in a ring and, before a write, reports how long
the caller must wait for the oldest of them to fall out of the window.
Time is passed in so the arithmetic is unit-testable without sleeping.
*/

package ws

import "time"

// writeGate — sliding-window budget of `limit` writes per `window`.
// Not goroutine-safe: Conn calls it under writeMu.
type writeGate struct {
	limit  int
	window time.Duration
	// stamps — ring of the last `limit` send instants; zero until filled.
	stamps []time.Time
	// next — ring index the next send instant is written to (== the slot
	// holding the OLDEST instant once the ring is full).
	next int
	// filled — number of valid instants in the ring (≤ limit).
	filled int
}

// newWriteGate returns a gate for `limit` writes per `window`. limit ≤ 0
// disables the gate (reserve always returns 0).
func newWriteGate(limit int, window time.Duration) *writeGate {
	if limit <= 0 || window <= 0 {
		return &writeGate{}
	}
	return &writeGate{limit: limit, window: window, stamps: make([]time.Time, limit)}
}

// reserve books one write at `now` and returns how long the caller has to
// wait BEFORE performing it so the window is respected (0 = write at
// once). The booked instant is now+wait, so back-to-back calls keep
// spacing correctly even though the caller has not slept yet.
func (g *writeGate) reserve(now time.Time) time.Duration {
	if g.limit <= 0 {
		return 0
	}
	var wait time.Duration
	if g.filled == g.limit {
		var oldest time.Time = g.stamps[g.next]
		var elapsed time.Duration = now.Sub(oldest)
		if elapsed < g.window {
			wait = g.window - elapsed
		}
	}
	g.stamps[g.next] = now.Add(wait)
	g.next = (g.next + 1) % g.limit
	if g.filled < g.limit {
		g.filled++
	}
	return wait
}
