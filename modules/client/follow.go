package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"io"
	"time"

	"github.com/mattdurham/wllr/modules/wire"
)

// Follow connects to the instance's event stream and calls onEvent for every
// frame, reconnecting with backoff until ctx is cancelled. It resumes from the
// last seen sequence number after a drop, so a reconnect does not replay events
// the caller already saw (subject to the server's backlog limit), and it marks
// Tagged.Gap when a sequence discontinuity means events were missed anyway.
//
// Follow returns nil on graceful cancellation (ctx done) and a non-nil error
// only when OnConnect asks to stop.
func (c *Client) Follow(ctx context.Context, onEvent OnEvent, opts FollowOptions) error {
	since := opts.Since
	// lastSeq is the resume cursor: the highest sequence number of an event we
	// actually delivered. It starts at Since so a requested resume point is not
	// mistaken for a gap, and the ready frame never advances it (it carries the
	// server's last seq, not a delivered event).
	lastSeq := opts.Since

	for {
		if err := ctx.Err(); err != nil {
			return nil //nolint:nilerr // ctx done: graceful stop (see Follow doc).
		}
		s, err := c.openStream(ctx, since)
		if err != nil {
			if ctx.Err() != nil {
				return nil //nolint:nilerr // ctx done: a cancelled connect is a graceful stop.
			}
			if opts.OnError != nil {
				opts.OnError(err)
			}
			if !sleepCtx(ctx, opts.Backoff.Next()) {
				return nil
			}
			continue
		}
		// A successful open resets the backoff schedule.
		opts.Backoff.Reset()

		if opts.OnConnect != nil {
			// Pump one frame to learn the protocol from the ready event, then
			// hand it to onEvent like any other. The ready frame always arrives
			// first (server.go writes it before replay).
			ev, rerr := s.next()
			if rerr != nil {
				_ = s.Close()
				if ctx.Err() != nil {
					return nil //nolint:nilerr // ctx done: a cancelled ready-frame read is a graceful stop.
				}
				if opts.OnError != nil {
					opts.OnError(rerr)
				}
				if !sleepCtx(ctx, opts.Backoff.Next()) {
					return nil
				}
				continue
			}
			if cerr := opts.OnConnect(s.protocol); cerr != nil {
				_ = s.Close()
				return cerr
			}
			var gap bool
			lastSeq, gap = advance(lastSeq, ev)
			onEvent(Tagged{Instance: c.id, Event: ev, Gap: gap})
		}

		// Read until the connection drops or the context is cancelled.
		c.drain(ctx, s, &lastSeq, onEvent)
		_ = s.Close()
		if ctx.Err() != nil {
			return nil //nolint:nilerr // ctx done: a drained stream ends gracefully.
		}
		// Resume from where we got to.
		since = lastSeq
		if opts.OnError != nil {
			opts.OnError(io.EOF)
		}
		if !sleepCtx(ctx, opts.Backoff.Next()) {
			return nil
		}
	}
}

// drain reads frames until the stream ends or errors, invoking onEvent for each.
// It updates *lastSeq as the resume cursor. A read fault ends the loop; the
// caller reconnects.
func (c *Client) drain(ctx context.Context, s *stream, lastSeq *int64, onEvent OnEvent) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		ev, err := s.next()
		if err != nil {
			// EOF/transport error: reconnect. A decode error also lands here so
			// the follower resynchronizes rather than spinning on a bad frame.
			return
		}
		next, gap := advance(*lastSeq, ev)
		*lastSeq = next
		onEvent(Tagged{Instance: c.id, Event: ev, Gap: gap})
	}
}

// advance updates the resume cursor with ev.Seq and reports whether a gap
// occurred (the new Seq skipped ahead of the last seen one). Events that carry
// no cursor information — a zero Seq, or the ready frame, whose Seq is the
// server's last sequence rather than a delivered event — leave the cursor
// unchanged and never report a gap.
func advance(prev int64, ev wire.Event) (int64, bool) {
	if ev.Seq == 0 || ev.Kind == wire.KindReady {
		return prev, false
	}
	if prev != 0 && ev.Seq > prev+1 {
		return ev.Seq, true
	}
	return ev.Seq, false
}

// sleepCtx waits for d or until ctx is cancelled. It reports false when the
// context ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
