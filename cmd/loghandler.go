package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// logDispatchInterval is the coalescing window for log batches dispatched to
// WASM extensions, matching the token batcher cadence (~33 dispatches/sec max).
const logDispatchInterval = 30 * time.Millisecond

// logRingSize bounds the bootstrap ring buffer holding records emitted before
// any extension subscribes to EventLog. Older records are dropped on overflow.
const logRingSize = 256

// logQueueSize bounds the in-flight record queue. When full, new records are
// dropped (the file is best-effort, like the previous swallowed file errors).
const logQueueSize = 4096

// dispatchLogHandler is an slog.Handler that forwards records to WASM extensions
// via EventLog instead of writing a file in core. It is the replacement for the
// file half of the previous teeHandler:
//
//   - Records are converted to sdk.LogRecord and enqueued non-blocking.
//   - A single drain goroutine coalesces records (~30ms) and dispatches a
//     LogBatchPayload via Host.DispatchEvent.
//   - A reentrancy guard suppresses records emitted *while* dispatching EventLog
//     (e.g. the dispatch machinery's own logs), breaking the log→dispatch→log loop.
//   - Records emitted before any extension subscribes are held in a ring buffer
//     and flushed once a subscriber appears, so startup logs are not lost.
//
// The handler holds the configured level (Debug) and its own attr/group state so
// slog.With works. It never writes to stderr — the core teeHandler still owns
// the stderr path; this handler owns the WASM-routed file/sink path.
type dispatchLogHandler struct {
	shared *logDispatcher
	attrs  []slog.Attr
	groups []string
	level  slog.Level
}

// logDispatcher is the process-wide shared state behind every cloned
// dispatchLogHandler (WithAttrs/WithGroup return clones that share this).
type logDispatcher struct {
	host       *extension.Host
	queue      chan sdk.LogRecord
	stop       chan struct{}
	ring       []sdk.LogRecord
	wg         sync.WaitGroup
	ringMu     sync.Mutex
	flushed    atomic.Bool // true once the ring has been flushed to the queue
	inDispatch atomic.Bool // reentrancy guard: true while dispatching EventLog
	started    atomic.Bool
}

// newDispatchLogHandler creates the handler and starts its drain goroutine.
// host must be non-nil; the returned stop func halts the drain goroutine.
func newDispatchLogHandler(host *extension.Host, level slog.Level) (*dispatchLogHandler, func()) {
	d := &logDispatcher{
		host:  host,
		queue: make(chan sdk.LogRecord, logQueueSize),
		stop:  make(chan struct{}),
	}
	d.started.Store(true)
	d.wg.Add(1)
	go d.drain()
	stop := func() {
		if d.started.CompareAndSwap(true, false) {
			close(d.stop)
			d.wg.Wait()
		}
	}
	return &dispatchLogHandler{shared: d, level: level}, stop
}

func (h *dispatchLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *dispatchLogHandler) Handle(_ context.Context, r slog.Record) error {
	// Reentrancy guard: drop records emitted while we are dispatching EventLog
	// (the dispatch path itself logs on error). This deterministically breaks the
	// log→dispatch→log feedback loop.
	if h.shared.inDispatch.Load() {
		return nil
	}

	rec := sdk.LogRecord{
		Time:    r.Time.UTC().Format(time.RFC3339Nano),
		Level:   levelName(r.Level),
		Message: r.Message,
	}
	// Pre-stringify handler attrs (from slog.With) then record attrs, preserving order.
	for _, a := range h.attrs {
		rec.Attrs = append(rec.Attrs, sdk.LogAttr{Key: h.qualify(a.Key), Value: a.Value.String()})
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs = append(rec.Attrs, sdk.LogAttr{Key: h.qualify(a.Key), Value: a.Value.String()})
		return true
	})

	h.shared.enqueue(rec)
	return nil
}

func (h *dispatchLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	n := &dispatchLogHandler{shared: h.shared, level: h.level, groups: h.groups}
	n.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return n
}

func (h *dispatchLogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	n := &dispatchLogHandler{shared: h.shared, level: h.level, attrs: h.attrs}
	n.groups = append(append([]string{}, h.groups...), name)
	return n
}

// qualify prefixes an attr key with the active group path (slog semantics).
func (h *dispatchLogHandler) qualify(key string) string {
	if len(h.groups) == 0 {
		return key
	}
	out := ""
	for _, g := range h.groups {
		out += g + "."
	}
	return out + key
}

// enqueue buffers a record. Before the ring is flushed (no subscriber yet),
// records accumulate in the bounded ring. After flush, they go to the queue.
// Both paths drop-on-overflow.
func (d *logDispatcher) enqueue(rec sdk.LogRecord) {
	if !d.flushed.Load() {
		d.ringMu.Lock()
		if !d.flushed.Load() {
			if len(d.ring) >= logRingSize {
				d.ring = d.ring[1:] // drop oldest
			}
			d.ring = append(d.ring, rec)
			d.ringMu.Unlock()
			return
		}
		d.ringMu.Unlock()
	}
	select {
	case d.queue <- rec:
	default:
		// Queue full — drop (best-effort sink).
	}
}

// drain coalesces queued records and dispatches them as EventLog batches once an
// extension subscribes. It flushes the bootstrap ring on the first subscriber.
func (d *logDispatcher) drain() {
	defer d.wg.Done()
	ticker := time.NewTicker(logDispatchInterval)
	defer ticker.Stop()

	var batch []sdk.LogRecord
	for {
		select {
		case <-d.stop:
			d.flushBatch(batch)
			return
		case rec := <-d.queue:
			batch = append(batch, rec)
		case <-ticker.C:
			if !d.host.HasSubscribers(sdk.EventLog) {
				continue // no sink yet; keep batching (queue caps growth)
			}
			d.flushRing()
			batch = d.flushBatch(batch)
		}
	}
}

// flushRing moves bootstrap-buffered records into the live batch path exactly
// once, the first time a subscriber exists.
func (d *logDispatcher) flushRing() {
	if d.flushed.Load() {
		return
	}
	d.ringMu.Lock()
	pending := d.ring
	d.ring = nil
	d.flushed.Store(true)
	d.ringMu.Unlock()
	if len(pending) > 0 {
		d.dispatch(pending)
	}
}

// flushBatch dispatches a batch (if non-empty) and returns a reset slice.
func (d *logDispatcher) flushBatch(batch []sdk.LogRecord) []sdk.LogRecord {
	if len(batch) == 0 {
		return batch
	}
	d.dispatch(batch)
	return batch[:0]
}

// dispatch sends one EventLog batch under the reentrancy guard.
func (d *logDispatcher) dispatch(records []sdk.LogRecord) {
	payload, err := json.Marshal(sdk.LogBatchPayload{Records: records})
	if err != nil {
		return
	}
	d.inDispatch.Store(true)
	_, _ = d.host.DispatchEvent(context.Background(), sdk.Event{Type: sdk.EventLog, Payload: payload})
	d.inDispatch.Store(false)
}

func levelName(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "debug"
	case l < slog.LevelWarn:
		return "info"
	case l < slog.LevelError:
		return "warn"
	default:
		return "error"
	}
}
