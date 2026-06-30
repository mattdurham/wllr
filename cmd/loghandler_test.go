package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/extension"
)

func TestLevelName(t *testing.T) {
	cases := []struct {
		lvl  slog.Level
		want string
	}{
		{slog.LevelDebug, "debug"},
		{slog.LevelInfo, "info"},
		{slog.LevelWarn, "warn"},
		{slog.LevelError, "error"},
		{slog.LevelInfo - 1, "debug"},
	}
	for _, c := range cases {
		if got := levelName(c.lvl); got != c.want {
			t.Errorf("levelName(%v) = %q, want %q", c.lvl, got, c.want)
		}
	}
}

func TestDispatchLogHandler_RecordConversion(t *testing.T) {
	h := extension.NewHost(nil)
	defer func() { _ = h.Close(context.Background()) }()
	dh, stop := newDispatchLogHandler(h, slog.LevelDebug)
	defer stop()

	// Enabled honors the level.
	if dh.Enabled(context.Background(), slog.LevelDebug-1) {
		t.Error("below-level must be disabled")
	}
	if !dh.Enabled(context.Background(), slog.LevelError) {
		t.Error("error must be enabled at Debug threshold")
	}

	// WithAttrs/WithGroup produce records with qualified, stringified attrs.
	sub := dh.WithGroup("svc").WithAttrs([]slog.Attr{slog.String("ext", "x")})
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	rec.AddAttrs(slog.Int("n", 7))
	if err := sub.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// The record is enqueued; we can't read the private queue directly, so this
	// test asserts Handle does not error and the conversion path runs. End-to-end
	// dispatch is covered by TestDispatchLogHandler_EndToEnd.
}

// TestDispatchLogHandler_ReentrancyGuard verifies that a record emitted while
// inDispatch is set is dropped (not enqueued), breaking the log→dispatch→log loop.
func TestDispatchLogHandler_ReentrancyGuard(t *testing.T) {
	h := extension.NewHost(nil)
	defer func() { _ = h.Close(context.Background()) }()
	dh, stop := newDispatchLogHandler(h, slog.LevelDebug)
	defer stop()

	dh.shared.inDispatch.Store(true)
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "during dispatch", 0)
	_ = dh.Handle(context.Background(), rec)
	// Ring must stay empty: the guarded record was dropped.
	dh.shared.ringMu.Lock()
	n := len(dh.shared.ring)
	dh.shared.ringMu.Unlock()
	if n != 0 {
		t.Errorf("reentrancy guard failed: ring has %d records, want 0", n)
	}
}

// TestDispatchLogHandler_RingBuffersBeforeSubscriber verifies that records
// emitted before any EventLog subscriber exists accumulate in the ring buffer
// (bounded), rather than being lost or dispatched into the void.
func TestDispatchLogHandler_RingBuffersBeforeSubscriber(t *testing.T) {
	h := extension.NewHost(nil)
	defer func() { _ = h.Close(context.Background()) }()
	dh, stop := newDispatchLogHandler(h, slog.LevelDebug)
	defer stop()

	// No subscriber: records go to the ring.
	for i := 0; i < 5; i++ {
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "early", 0)
		_ = dh.Handle(context.Background(), rec)
	}
	dh.shared.ringMu.Lock()
	n := len(dh.shared.ring)
	dh.shared.ringMu.Unlock()
	if n != 5 {
		t.Errorf("ring buffered %d records, want 5", n)
	}
}

func TestDispatchLogHandler_RingCapsAtSize(t *testing.T) {
	h := extension.NewHost(nil)
	defer func() { _ = h.Close(context.Background()) }()
	dh, stop := newDispatchLogHandler(h, slog.LevelDebug)
	defer stop()

	for i := 0; i < logRingSize+50; i++ {
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "spam", 0)
		_ = dh.Handle(context.Background(), rec)
	}
	dh.shared.ringMu.Lock()
	n := len(dh.shared.ring)
	dh.shared.ringMu.Unlock()
	if n != logRingSize {
		t.Errorf("ring length = %d, want capped at %d", n, logRingSize)
	}
}
