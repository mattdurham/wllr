package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"math"
	"math/rand"
	"time"
)

// Backoff computes the delay before the next reconnect attempt. It grows
// exponentially from Base to Max and adds jitter, so N instances that dropped
// together (for example when a host restarts) do not reconnect in lockstep and
// stampede the servers.
type Backoff struct {
	// Rand is the jitter source; defaults to the package-global generator.
	// Injecting one makes tests deterministic. It must NOT be shared across
	// concurrent followers: a *rand.Rand is not safe for concurrent use, and
	// Multiplexer copies FollowOptions (and therefore this pointer) to every
	// follower, so one injected source would race. A caller merging instances
	// should leave Rand nil (the package-global source is concurrency-safe) or
	// give each follower its own.
	Rand *rand.Rand

	// Base is the first delay. Defaults to DefaultBackoffBase.
	Base time.Duration
	// Max caps the delay. Defaults to DefaultBackoffMax.
	Max time.Duration
	// Jitter is the fraction of the delay randomized, in [0,1). The zero value
	// uses DefaultBackoffJitter; set NoJitter to disable randomization.
	Jitter float64

	attempt int
	// NoJitter disables randomization when true.
	NoJitter bool
}

// Default backoff parameters: fast first retry, gentle ceiling, ~20% jitter.
const (
	DefaultBackoffBase   = 500 * time.Millisecond
	DefaultBackoffMax    = 30 * time.Second
	DefaultBackoffJitter = 0.2
)

// Next returns the delay for the next attempt and advances the attempt counter.
// The first call returns Base (plus jitter); each subsequent call doubles until
// Max. It is not safe for concurrent use; one Backoff belongs to one follower.
func (b *Backoff) Next() time.Duration {
	base := b.Base
	if base <= 0 {
		base = DefaultBackoffBase
	}
	max := b.Max
	if max <= 0 {
		max = DefaultBackoffMax
	}
	// Cap the shift so a long-lived instance cannot overflow the duration.
	shift := b.attempt
	if shift > 32 {
		shift = 32
	}
	d := time.Duration(float64(base) * math.Pow(2, float64(shift)))
	if d > max || d <= 0 {
		d = max
	}
	b.attempt++
	frac := b.Jitter
	if b.NoJitter {
		frac = 0
	} else if frac <= 0 {
		frac = DefaultBackoffJitter
	}
	return jitter(d, frac, b.Rand)
}

// Reset clears the attempt counter after a successful connection, so a later
// drop starts from Base again.
func (b *Backoff) Reset() { b.attempt = 0 }

// jitter spreads d by +/- frac. frac <= 0 returns d unchanged. A nil rng uses
// the package-global source, which is safe for concurrent use; injecting one
// makes tests deterministic.
func jitter(d time.Duration, frac float64, rng *rand.Rand) time.Duration {
	if frac <= 0 {
		return d
	}
	if frac > 1 {
		frac = 1
	}
	//nolint:gosec // Reconnect jitter only needs to de-synchronize attempts; it is not a security decision, so math/rand is correct here.
	u := rand.Float64()
	if rng != nil {
		u = rng.Float64()
	}
	// Span is the total randomization window; offset is uniform within it.
	span := float64(d) * frac
	offset := (u*2 - 1) * span
	out := time.Duration(float64(d) + offset)
	if out < 0 {
		return 0
	}
	return out
}
