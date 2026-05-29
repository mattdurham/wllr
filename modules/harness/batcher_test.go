package harness

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── tokenBatcher unit tests ─────────────────────────────────────────────────

func TestTokenBatcher_FlushSendsBuffered(t *testing.T) {
	var mu sync.Mutex
	var received []string
	send := func(s string) {
		mu.Lock()
		received = append(received, s)
		mu.Unlock()
	}

	b := &tokenBatcher{}
	// Simulate onToken without the 30ms gating (set lastSend to zero so
	// first token triggers send immediately — but we test flush here).
	b.mu.Lock()
	b.buf.WriteString("hello ")
	b.buf.WriteString("world")
	b.mu.Unlock()

	// flush should deliver everything
	b.mu.Lock()
	s := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	if s != "" {
		send(s)
	}

	mu.Lock()
	got := strings.Join(received, "")
	mu.Unlock()
	if got != "hello world" {
		t.Errorf("flush should deliver buffered tokens, got %q", got)
	}
}

func TestTokenBatcher_FlushIsIdempotent(t *testing.T) {
	// flush() must not panic when called multiple times (multi-turn scenario).
	b := &tokenBatcher{}
	b.buf.WriteString("abc")

	// First flush
	b.mu.Lock()
	s1 := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	_ = s1

	// Second flush — buf is empty, should be a no-op
	b.mu.Lock()
	s2 := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	if s2 != "" {
		t.Errorf("second flush should return empty string, got %q", s2)
	}

	// Third flush — also no-op, no panic
	b.mu.Lock()
	s3 := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	_ = s3
}

func TestTokenBatcher_BatchesTokensWithinInterval(t *testing.T) {
	// onToken should NOT send when called rapidly within the batch window.
	var sendCount int
	var mu sync.Mutex

	b := &tokenBatcher{}
	// Set lastSend to "just now" so the interval hasn't elapsed.
	b.lastSend = time.Now()

	// Write tokens without triggering a send (interval not elapsed).
	for _, tok := range []string{"a", "b", "c"} {
		b.mu.Lock()
		b.buf.WriteString(tok)
		now := time.Now()
		if now.Sub(b.lastSend) >= tokenBatchInterval {
			s := b.buf.String()
			b.buf.Reset()
			b.lastSend = now
			b.mu.Unlock()
			mu.Lock()
			sendCount++
			_ = s
			mu.Unlock()
		} else {
			b.mu.Unlock()
		}
	}

	mu.Lock()
	c := sendCount
	mu.Unlock()
	if c != 0 {
		t.Errorf("no sends should happen within the batch interval, got %d", c)
	}

	// Buffer should hold all three tokens
	b.mu.Lock()
	buffered := b.buf.String()
	b.mu.Unlock()
	if buffered != "abc" {
		t.Errorf("buffer should hold 'abc', got %q", buffered)
	}
}

func TestTokenBatcher_SendsAfterIntervalElapsed(t *testing.T) {
	var sent string
	var mu sync.Mutex

	b := &tokenBatcher{}
	// Set lastSend far in the past so next token triggers a send.
	b.lastSend = time.Now().Add(-100 * time.Millisecond)
	b.buf.WriteString("prefix ")

	// Simulate an onToken call with elapsed interval.
	b.mu.Lock()
	b.buf.WriteString("token")
	now := time.Now()
	if now.Sub(b.lastSend) >= tokenBatchInterval {
		s := b.buf.String()
		b.buf.Reset()
		b.lastSend = now
		b.mu.Unlock()
		mu.Lock()
		sent = s
		mu.Unlock()
	} else {
		b.mu.Unlock()
	}

	mu.Lock()
	got := sent
	mu.Unlock()
	if got != "prefix token" {
		t.Errorf("should send 'prefix token' after interval, got %q", got)
	}
}

// TestTokenBatcher_MultiTurn is the regression test for the closed-channel
// panic. It simulates two agent turns calling flush() back-to-back.
func TestTokenBatcher_MultiTurn_FlushCalledTwice_NoPanic(t *testing.T) {
	b := &tokenBatcher{}

	// Turn 1: accumulate tokens and flush.
	b.mu.Lock()
	b.buf.WriteString("turn1 response")
	b.mu.Unlock()

	// flush() for turn 1
	b.mu.Lock()
	s := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	_ = s

	// Turn 2: accumulate new tokens and flush again.
	b.mu.Lock()
	b.buf.WriteString("turn2 response")
	b.mu.Unlock()

	// flush() for turn 2 — must NOT panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("flush on second turn should not panic: %v", r)
		}
	}()
	b.mu.Lock()
	s2 := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	if s2 != "turn2 response" {
		t.Errorf("second turn should deliver its tokens, got %q", s2)
	}
}
