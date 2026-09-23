package ws

import (
	"testing"
	"time"
)

func TestWriteGateSlidingWindow(t *testing.T) {
	t.Parallel()
	var g *writeGate = newWriteGate(3, time.Second)
	var t0 time.Time = time.Unix(1_700_000_000, 0)

	// The first `limit` writes go out at once.
	var i int
	for i = 0; i < 3; i++ {
		if w := g.reserve(t0); w != 0 {
			t.Fatalf("write %d: wait = %v, want 0", i, w)
		}
	}
	// The 4th write at t0 must wait for the 1st to leave the window.
	if w := g.reserve(t0); w != time.Second {
		t.Fatalf("4th write: wait = %v, want 1s", w)
	}
	// The 5th write at t0 is booked after the 2nd leaves — still 1s (the
	// 2nd was also sent at t0).
	if w := g.reserve(t0); w != time.Second {
		t.Fatalf("5th write: wait = %v, want 1s", w)
	}
	// Half a second later: the oldest booked instant is now the 3rd
	// write (t0) → wait 500ms.
	if w := g.reserve(t0.Add(500 * time.Millisecond)); w != 500*time.Millisecond {
		t.Fatalf("6th write: wait = %v, want 500ms", w)
	}
	// Two seconds later everything has left the window.
	if w := g.reserve(t0.Add(2 * time.Second)); w != 0 {
		t.Fatalf("write after idle: wait = %v, want 0", w)
	}
}

func TestWriteGateBurstSpacing(t *testing.T) {
	t.Parallel()
	// 10/s (the venue's cap): 25 back-to-back writes must be booked so
	// that no 1-second window holds more than 10 instants.
	var g *writeGate = newWriteGate(10, time.Second)
	var t0 time.Time = time.Unix(1_700_000_000, 0)
	var booked []time.Time
	var i int
	for i = 0; i < 25; i++ {
		booked = append(booked, t0.Add(g.reserve(t0)))
	}
	var j int
	for i = 0; i < len(booked); i++ {
		var n int
		for j = 0; j < len(booked); j++ {
			var d time.Duration = booked[j].Sub(booked[i])
			if d >= 0 && d < time.Second {
				n++
			}
		}
		if n > 10 {
			t.Fatalf("window starting at write %d holds %d writes, cap is 10", i, n)
		}
	}
	// The 25th write is booked exactly 2s after the first.
	if booked[24].Sub(t0) != 2*time.Second {
		t.Fatalf("25th write booked at +%v, want +2s", booked[24].Sub(t0))
	}
}

func TestWriteGateDisabled(t *testing.T) {
	t.Parallel()
	var g *writeGate = newWriteGate(0, time.Second)
	var now time.Time = time.Now()
	var i int
	for i = 0; i < 100; i++ {
		if w := g.reserve(now); w != 0 {
			t.Fatalf("disabled gate must never wait, got %v", w)
		}
	}
}
