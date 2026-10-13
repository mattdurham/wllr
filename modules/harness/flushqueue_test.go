package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Tests for the post-cancel queue flush: when the user cancels the main turn
// with Esc while messages are queued, the resulting canceled StreamDoneMsg
// starts a fresh turn that delivers those messages immediately, instead of
// leaving them stranded until the next manual submit.

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEsc_CancellingActiveTurnArmsQueueFlush(t *testing.T) {
	m := newTestModel()
	m.streaming = true

	m2, _, handled := m.updateKeyPress(keyMsg(tea.KeyEsc, 0))
	if !handled {
		t.Fatal("esc during an active main turn should be handled")
	}
	if !m2.cancelFlushQueue {
		t.Fatal("esc cancelling the turn should arm the queue flush")
	}
}

func TestEsc_IdleDoesNotArmQueueFlush(t *testing.T) {
	m := newTestModel() // main agent idle, not streaming

	m2, _, _ := m.updateKeyPress(keyMsg(tea.KeyEsc, 0))
	if m2.cancelFlushQueue {
		t.Fatal("idle esc should not arm the queue flush")
	}
}

func TestAbortStreamMsg_ArmsQueueFlushWhenTurnActive(t *testing.T) {
	m := newTestModel()
	m.streaming = true

	m2, _, handled := m.updateActions(abortStreamMsg{})
	if !handled {
		t.Fatal("abortStreamMsg should be handled")
	}
	if !m2.cancelFlushQueue {
		t.Fatal("aborting an active turn should arm the queue flush")
	}
}

func TestStreamDone_CancelFlushesQueuedMessages(t *testing.T) {
	m := newTestModel()
	queueTwoMessages(t, m)
	m.streaming = true
	m.cancelFlushQueue = true

	m2, _ := callUpdate(m, StreamDoneMsg{Err: context.Canceled})
	if !m2.streaming {
		t.Fatal("expected streaming to resume for the queued flush turn")
	}
	if m2.cancelFlushQueue {
		t.Fatal("cancelFlushQueue should be reset after handling")
	}
	// The flush submits to the main agent, which drains the inbox
	// synchronously at the start of the new turn.
	if got := inboxCount(t, m2); got != 0 {
		t.Fatalf("inbox still holds %d message(s) after flush, want 0", got)
	}
}

func TestStreamDone_CancelWithoutFlagPreservesQueue(t *testing.T) {
	m := newTestModel()
	queueTwoMessages(t, m)
	m.streaming = true
	// No cancelFlushQueue: this simulates a harness-shutdown cancel, which must
	// leave queued messages untouched (issue #48 semantics).

	m2, _ := callUpdate(m, StreamDoneMsg{Err: context.Canceled})
	if m2.streaming {
		t.Fatal("streaming should stay false when no flush is armed")
	}
	if got := inboxCount(t, m2); got != 2 {
		t.Fatalf("queue should be preserved, got %d message(s)", got)
	}
}

func TestStreamDone_NormalCompletionDoesNotFlushQueue(t *testing.T) {
	m := newTestModel()
	queueTwoMessages(t, m)
	m.streaming = true
	m.cancelFlushQueue = true // armed, but the turn ended normally

	m2, _ := callUpdate(m, StreamDoneMsg{Err: nil})
	if m2.cancelFlushQueue {
		t.Fatal("cancelFlushQueue should be reset even on normal completion")
	}
	if got := inboxCount(t, m2); got != 2 {
		t.Fatalf("normal completion should not flush the queue here, got %d", got)
	}
}
