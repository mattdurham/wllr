package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "sync"

// Hub is a fan-out broadcast of engine events to many concurrent subscribers.
//
// The engine's own observer hooks (per-agent SetOnToken/SetOnDone and the
// pool-level usage/lifecycle observers) were originally written for a single
// subscriber — the TUI. The Hub decouples those hooks from the number of
// consumers: a Bridge translates engine callbacks into Events and Publish
// delivers each Event to every subscribed connection.
//
// Publish never blocks the engine. A subscriber whose buffered channel is full
// drops the event and the drop is counted; because every Event carries a
// monotonic Seq, a client can detect a gap and re-sync (for example by
// re-fetching history). This is deliberate: a slow HTTP client must never stall
// an in-flight model turn.
type Hub struct {
	subs       map[int64]*subscriber
	backlog    []Event // ring buffer; grows to backlogCap and is then reused in place
	backlogCap int
	backlogN   int // retained events (<= backlogCap)
	backlogPos int // ring write cursor; the oldest entry when the ring is full
	nextID     int64
	seq        int64
	drops      int64
	mu         sync.Mutex
}

// subscriber is one live connection's delivery channel.
type subscriber struct {
	ch chan Event
}

// NewHub returns a Hub retaining at most backlogCap recent events for replay to
// late or reconnecting subscribers. A backlogCap <= 0 disables replay: a new
// subscriber then sees only events published after it subscribed.
func NewHub(backlogCap int) *Hub {
	if backlogCap < 0 {
		backlogCap = 0
	}
	return &Hub{
		subs:       map[int64]*subscriber{},
		backlogCap: backlogCap,
	}
}

// Publish stamps evt with the next sequence number, appends it to the replay
// backlog, and delivers it to every current subscriber. It returns the stamped
// event. Non-blocking: a full subscriber buffer drops the event (see Hub).
func (h *Hub) Publish(evt Event) Event {
	h.mu.Lock()
	h.seq++
	evt.Seq = h.seq
	if h.backlogCap > 0 {
		// Ring buffer: once full, overwrite the oldest slot in place. Appending
		// and then re-copying the retained window on every publish allocated
		// ~backlogCap*sizeof(Event) per event once saturated (3.6 MB with the
		// default 4096); overwriting is O(1) with no allocation.
		if len(h.backlog) < h.backlogCap {
			h.backlog = append(h.backlog, evt)
			h.backlogN++
			if h.backlogN == h.backlogCap {
				h.backlogPos = 0
			}
		} else {
			h.backlog[h.backlogPos] = evt
			h.backlogPos++
			if h.backlogPos == h.backlogCap {
				h.backlogPos = 0
			}
		}
	}
	targets := make([]*subscriber, 0, len(h.subs))
	for _, s := range h.subs {
		targets = append(targets, s)
	}
	h.mu.Unlock()

	for _, s := range targets {
		select {
		case s.ch <- evt:
		default:
			h.mu.Lock()
			h.drops++
			h.mu.Unlock()
		}
	}
	return evt
}

// Subscribe registers a subscriber and atomically captures the replay backlog
// of events with Seq > since. Registration and the backlog snapshot happen under
// one lock, so an event published concurrently is either in the replay slice or
// delivered on the returned channel — never both, never neither.
//
// The returned cancel function unregisters the subscriber and is idempotent
// and safe to call from any goroutine. It deliberately does NOT close the
// delivery channel: Publish snapshots the subscriber set under the lock and
// sends outside it, so a concurrent cancel that closed the channel could make
// Publish send on a closed channel and panic. The reader stops on its own
// ctx.Done() (see handleStream), so closure is unnecessary for unblocking.
func (h *Hub) Subscribe(since int64) (<-chan Event, []Event, func()) {
	s := &subscriber{ch: make(chan Event, subscriberBuffer)}
	h.mu.Lock()
	id := h.nextID
	h.nextID++
	h.subs[id] = s
	var replay []Event
	for i := 0; i < h.backlogN; i++ {
		idx := i
		if h.backlogN == h.backlogCap {
			idx = (h.backlogPos + i) % h.backlogCap
		}
		if e := h.backlog[idx]; e.Seq > since {
			replay = append(replay, e)
		}
	}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
	}
	return s.ch, replay, cancel
}

// LastSeq returns the sequence number of the most recently published event, or
// 0 when nothing has been published. A reconnecting client passes it as the
// `since` cursor to receive only what it missed.
func (h *Hub) LastSeq() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// Stats returns the number of live subscribers and the cumulative count of
// events dropped because a subscriber's buffer was full.
func (h *Hub) Stats() (subscribers int, drops int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs), h.drops
}

// subscriberBuffer bounds how many not-yet-read events one connection may
// accumulate before drops begin. Large enough to absorb a burst of tokens
// between HTTP writes, small enough that a stalled client cannot pin unbounded
// memory.
const subscriberBuffer = 512
