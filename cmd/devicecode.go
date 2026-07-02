package main

import (
	"context"
	"errors"
	"time"
)

// Device-code (RFC 8628) polling primitive. Providers that support headless
// login (no local callback server / browser redirect) issue a user code the
// user enters on a verification URL, then the client polls a token endpoint
// until the user approves. This is the remote/SSH-friendly alternative to the
// callback-server flow. Mirrors pi's pollOAuthDeviceCodeFlow semantics.
//
// Anthropic does NOT offer device-code; this substrate is for Codex/Copilot-style
// providers (added separately).

// deviceCodeStatus is the outcome of a single poll attempt.
type deviceCodeStatus int

const (
	// devicePending: authorization not yet granted; keep polling at the current interval.
	devicePending deviceCodeStatus = iota
	// deviceSlowDown: server asked us to back off; increase the interval by 5s (RFC 8628 §3.5).
	deviceSlowDown
	// deviceComplete: authorization granted; PollResult.Value holds the result.
	deviceComplete
	// deviceFailed: a terminal error; PollResult.Err explains it.
	deviceFailed
)

// devicePollResult is what a poll function returns for one attempt.
type devicePollResult[T any] struct {
	Status deviceCodeStatus
	Value  T
	Err    error
}

// devicePollOptions configures pollDeviceCode.
type devicePollOptions[T any] struct {
	// IntervalSeconds is the initial poll interval. Values below 1s are clamped
	// to 1s; a zero/negative value defaults to 5s (RFC 8628 §3.2).
	IntervalSeconds int
	// ExpiresInSeconds bounds the whole flow. Zero means no deadline.
	ExpiresInSeconds int
	// Poll performs one attempt against the provider's device-token endpoint.
	Poll func(context.Context) devicePollResult[T]
}

const (
	deviceMinInterval       = 1 * time.Second
	deviceDefaultInterval   = 5 * time.Second
	deviceSlowDownIncrement = 5 * time.Second
)

// errDeviceTimeout is returned when the flow exceeds ExpiresInSeconds.
var errDeviceTimeout = errors.New("device-code flow timed out")

// pollDeviceCode drives an RFC 8628 device-authorization poll loop: it calls
// opts.Poll at the current interval until it returns deviceComplete (value),
// deviceFailed (error), the context is cancelled, or the deadline passes. On
// deviceSlowDown the interval increases by 5s for this and all later attempts.
func pollDeviceCode[T any](ctx context.Context, opts devicePollOptions[T]) (T, error) {
	var zero T

	interval := time.Duration(opts.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = deviceDefaultInterval
	}
	if interval < deviceMinInterval {
		interval = deviceMinInterval
	}

	var deadline time.Time
	if opts.ExpiresInSeconds > 0 {
		deadline = time.Now().Add(time.Duration(opts.ExpiresInSeconds) * time.Second)
	}

	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return zero, errDeviceTimeout
		}

		res := opts.Poll(ctx)
		switch res.Status {
		case deviceComplete:
			return res.Value, nil
		case deviceFailed:
			if res.Err != nil {
				return zero, res.Err
			}
			return zero, errors.New("device-code flow failed")
		case deviceSlowDown:
			interval += deviceSlowDownIncrement
		case devicePending:
			// keep waiting
		}

		// Sleep until the next attempt, but never past the deadline, and wake on
		// cancellation.
		wait := interval
		if !deadline.IsZero() {
			if remaining := time.Until(deadline); remaining <= 0 {
				return zero, errDeviceTimeout
			} else if remaining < wait {
				wait = remaining
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}
