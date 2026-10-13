package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"sync"
	"testing"
)

func TestHubPublishAssignsSequence(t *testing.T) {
	h := NewHub(8)
	if got := h.LastSeq(); got != 0 {
		t.Fatalf("fresh hub LastSeq = %d, want 0", got)
	}
	for i := int64(1); i <= 3; i++ {
		ev := h.Publish(Event{Kind: KindToken, Text: "x"})
		if ev.Seq != i {
			t.Fatalf("publish %d: Seq = %d, want %d", i, ev.Seq, i)
		}
	}
	if got := h.LastSeq(); got != 3 {
		t.Fatalf("LastSeq = %d, want 3", got)
	}
}

func TestHubFansOutToAllSubscribers(t *testing.T) {
	h := NewHub(8)
	ch1, _, cancel1 := h.Subscribe(0)
	defer cancel1()
	ch2, _, cancel2 := h.Subscribe(0)
	defer cancel2()

	h.Publish(Event{Kind: KindToken, Text: "hello"})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Kind != KindToken || ev.Text != "hello" {
				t.Fatalf("subscriber %d got %+v, want token hello", i, ev)
			}
		default:
			t.Fatalf("subscriber %d received nothing", i)
		}
	}
}

func TestHubReplaysBacklogAfterSince(t *testing.T) {
	h := NewHub(8)
	h.Publish(Event{Kind: KindToken, Text: "a"}) // seq 1
	h.Publish(Event{Kind: KindToken, Text: "b"}) // seq 2
	h.Publish(Event{Kind: KindToken, Text: "c"}) // seq 3

	_, replay, cancel := h.Subscribe(1)
	defer cancel()
	if len(replay) != 2 {
		t.Fatalf("replay length = %d, want 2", len(replay))
	}
	if replay[0].Text != "b" || replay[1].Text != "c" {
		t.Fatalf("replay = %+v, want b,c", replay)
	}
	if replay[0].Seq != 2 || replay[1].Seq != 3 {
		t.Fatalf("replay seqs = %d,%d, want 2,3", replay[0].Seq, replay[1].Seq)
	}
}

func TestHubBacklogIsBounded(t *testing.T) {
	h := NewHub(2)
	for i := 0; i < 5; i++ {
		h.Publish(Event{Kind: KindToken, Text: "x"})
	}
	_, replay, cancel := h.Subscribe(0)
	defer cancel()
	if len(replay) != 2 {
		t.Fatalf("replay length = %d, want 2 (bounded)", len(replay))
	}
	// The two retained events must be the newest (seq 4,5).
	if replay[0].Seq != 4 || replay[1].Seq != 5 {
		t.Fatalf("retained seqs = %d,%d, want 4,5", replay[0].Seq, replay[1].Seq)
	}
}

func TestHubDropWhenSubscriberFull(t *testing.T) {
	h := NewHub(0)
	ch, _, cancel := h.Subscribe(0)
	defer cancel()

	// Publish more than the buffer holds without reading.
	for i := 0; i < subscriberBuffer+10; i++ {
		h.Publish(Event{Kind: KindToken})
	}
	subs, drops := h.Stats()
	if subs != 1 {
		t.Fatalf("subscribers = %d, want 1", subs)
	}
	if drops == 0 {
		t.Fatalf("drops = 0, want > 0 once the buffer is full")
	}
	// The channel must still hold exactly the buffer's worth.
	if got := len(ch); got != subscriberBuffer {
		t.Fatalf("buffered = %d, want %d", got, subscriberBuffer)
	}
}

func TestHubCancelUnsubscribes(t *testing.T) {
	h := NewHub(4)
	_, _, cancel := h.Subscribe(0)
	cancel()
	cancel() // idempotent

	if subs, _ := h.Stats(); subs != 0 {
		t.Fatalf("subscribers = %d, want 0 after cancel", subs)
	}
	// A publish after cancel must not reach the retired subscriber. The channel
	// is intentionally left open (never closed), so a publish that raced the
	// snapshot cannot send on a closed channel.
	h.Publish(Event{Kind: KindToken, Text: "after-cancel"})
	if subs, _ := h.Stats(); subs != 0 {
		t.Fatalf("subscribers = %d after publish, want 0", subs)
	}
}

// TestHubPublishConcurrentWithCancel is the -race guard for the send-on-closed
// panic: a publish that snapshots the subscriber set just before a cancel must
// not send on the now-closed channel. Run with -race.
func TestHubPublishConcurrentWithCancel(t *testing.T) {
	h := NewHub(8)
	const iterations = 2000

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			h.Publish(Event{Kind: KindToken, Text: "x"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _, cancel := h.Subscribe(0)
			cancel()
		}
	}()
	wg.Wait()
}

func TestHubSubscribeAfterPublishReplaysAll(t *testing.T) {
	h := NewHub(4)
	h.Publish(Event{Kind: KindToken, Text: "one"})
	h.Publish(Event{Kind: KindToken, Text: "two"})

	_, replay, cancel := h.Subscribe(0)
	defer cancel()
	if len(replay) != 2 {
		t.Fatalf("replay length = %d, want 2", len(replay))
	}
}
