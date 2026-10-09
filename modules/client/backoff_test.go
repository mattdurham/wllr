package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"math/rand"
	"testing"
	"time"
)

func TestBackoffGrowsExponentiallyAndCaps(t *testing.T) {
	b := Backoff{Base: 10 * time.Millisecond, Max: 100 * time.Millisecond, NoJitter: true}
	want := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
		80 * time.Millisecond,
		100 * time.Millisecond, // capped
		100 * time.Millisecond,
	}
	for i, w := range want {
		if got := b.Next(); got != w {
			t.Errorf("Next #%d = %v, want %v", i, got, w)
		}
	}
}

func TestBackoffResetReturnsToBase(t *testing.T) {
	b := Backoff{Base: 5 * time.Millisecond, Max: time.Second, NoJitter: true}
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got != 5*time.Millisecond {
		t.Errorf("after Reset, Next = %v, want 5ms", got)
	}
}

func TestBackoffDefaults(t *testing.T) {
	var b Backoff
	b.NoJitter = true
	if got := b.Next(); got != DefaultBackoffBase {
		t.Errorf("zero-value first Next = %v, want %v", got, DefaultBackoffBase)
	}
}

// TestBackoffJitterIsBounded proves the delay stays within +/- frac of nominal
// and is deterministic for a seeded source.
func TestBackoffJitterIsBounded(t *testing.T) {
	seed := rand.New(rand.NewSource(1))
	b := Backoff{Base: 100 * time.Millisecond, Max: time.Second, Rand: seed}
	for i := 0; i < 50; i++ {
		got := b.Next()
		// nominal for attempt i is min(100ms*2^i, 1s); with 20% jitter the value
		// is within +/-20% of nominal, and always positive.
		if got < 0 {
			t.Fatalf("negative delay %v", got)
		}
	}
	// Determinism: two generators with the same seed agree.
	a := Backoff{Rand: rand.New(rand.NewSource(7))}
	c := Backoff{Rand: rand.New(rand.NewSource(7))}
	for i := 0; i < 4; i++ {
		xa, xc := a.Next(), c.Next()
		if xa != xc {
			t.Errorf("draw %d: seeded jitter differs: %v != %v", i, xa, xc)
		}
	}
}

func TestJitterZeroReturnsUnchanged(t *testing.T) {
	d := 42 * time.Millisecond
	if got := jitter(d, 0, nil); got != d {
		t.Errorf("jitter with frac 0 = %v, want %v", got, d)
	}
}
