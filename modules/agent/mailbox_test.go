package agent

import (
	"fmt"
	"sync"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestMailbox_AppendDrainLen(t *testing.T) {
	var b mailbox
	if b.len() != 0 {
		t.Fatalf("zero-value mailbox len = %d, want 0", b.len())
	}
	b.append("a", sdk.Message{Role: sdk.RoleUser, Content: "one"})
	b.append("a", sdk.Message{Role: sdk.RoleUser, Content: "two"})
	if b.len() != 2 {
		t.Fatalf("len after 2 appends = %d, want 2", b.len())
	}
	msgs := b.drain()
	if len(msgs) != 2 || msgs[0].Content != "one" || msgs[1].Content != "two" {
		t.Fatalf("drain returned %+v, want FIFO [one, two]", msgs)
	}
	if b.len() != 0 {
		t.Errorf("len after drain = %d, want 0", b.len())
	}
	if got := b.drain(); got != nil {
		t.Errorf("second drain = %+v, want nil", got)
	}
}

func TestMailbox_DropsEmptyContent(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{Role: sdk.RoleUser, Content: ""})
	b.append("a", sdk.Message{Role: sdk.RoleUser, Content: "   "})
	b.append("a", sdk.Message{Role: sdk.RoleUser, Content: "\t\n"})
	if b.len() != 0 {
		t.Errorf("blank-content messages must be dropped; len = %d, want 0", b.len())
	}
	b.append("a", sdk.Message{Role: sdk.RoleUser, Content: "real"})
	if b.len() != 1 {
		t.Errorf("len = %d, want 1 (only the non-blank message)", b.len())
	}
}

// TestMailbox_ConcurrentAppendDrain exercises the mutex: many goroutines append
// while others drain. The race detector validates there is no data race, and
// every appended message must be drained exactly once (none lost, none dup).
func TestMailbox_ConcurrentAppendDrain(t *testing.T) {
	var b mailbox
	const writers = 8
	const perWriter = 100

	var wg sync.WaitGroup
	var collectMu sync.Mutex
	collected := make(map[string]int)

	stop := make(chan struct{})

	// Drainers run until stop, then one final drain ensures nothing is left.
	var drainWG sync.WaitGroup
	drain := func() {
		for _, m := range b.drain() {
			collectMu.Lock()
			collected[m.Content]++
			collectMu.Unlock()
		}
	}
	for range 3 {
		drainWG.Add(1)
		go func() {
			defer drainWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
					drain()
				}
			}
		}()
	}

	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWriter {
				b.append("a", sdk.Message{
					Role:    sdk.RoleUser,
					Content: msgKey(w, i),
				})
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	drainWG.Wait()
	drain() // final sweep for anything queued after the last loop drain

	if len(collected) != writers*perWriter {
		t.Errorf("distinct messages drained = %d, want %d", len(collected), writers*perWriter)
	}
	for k, n := range collected {
		if n != 1 {
			t.Errorf("message %q drained %d times, want 1", k, n)
		}
	}
}

func msgKey(w, i int) string {
	return fmt.Sprintf("w%d-%02d", w, i)
}
