package agent_test

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Tests for issue #48: queued messages must be preserved across Esc-cancel
// (nothing silently drains or discards them), and an explicit bulk clear must
// work while a turn is running — that is the common window in which the user
// looks at the queue pane and decides to discard what they typed.

import (
	"context"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// blockingLM is defined in concurrent_submit_test.go and blocks until
// Release() or context cancellation — exactly a long-running streaming turn.

// TestAgent_Cancel_PreservesQueuedInbox pins the chosen cancel semantics
// (issue #48 option (a)): Esc-cancel terminates the running turn but leaves
// the inbox untouched — finishTurn must skip the drain on a canceled turn so
// queued messages are never silently dropped or folded into a replay.
func TestAgent_Cancel_PreservesQueuedInbox(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("cancel-inbox", &blockingLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "slow request")
	// Give the turn a moment to start, then queue two messages mid-turn.
	time.Sleep(20 * time.Millisecond)
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "queued one"})
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "queued two"})
	a.Cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: Cancel did not stop the turn")
	}

	got := a.SnapshotInbox()
	if len(got) != 2 {
		t.Fatalf("cancel must preserve queued messages; got %d, want 2", len(got))
	}
	if got[0].Content != "queued one" || got[1].Content != "queued two" {
		t.Errorf("queued content changed across cancel: %q, %q", got[0].Content, got[1].Content)
	}
}

// TestAgent_ClearInbox_WorksWhileRunning pins that ClearInbox is deliberately
// not gated on IsRunning: clearing is the common action while a turn is
// streaming (issue #48 acceptance criteria).
func TestAgent_ClearInbox_WorksWhileRunning(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("clear-running", &blockingLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "slow request")
	time.Sleep(20 * time.Millisecond)
	if !a.IsRunning() {
		t.Fatal("expected turn to be running; blockingLM should block until cancel")
	}
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "mistyped"})

	if n := a.ClearInbox(); n != 1 {
		t.Fatalf("ClearInbox removed %d messages while running, want 1", n)
	}
	if got := a.SnapshotInbox(); len(got) != 0 {
		t.Errorf("inbox not empty after clear: %d message(s)", len(got))
	}
	a.Cancel()
	<-done

	// Cleared messages must not be replayed into a later turn: the post-turn
	// drain observed an empty inbox.
	if got := a.SnapshotInbox(); len(got) != 0 {
		t.Errorf("cleared messages reappeared after turn end: %d message(s)", len(got))
	}
}

// TestAgent_ClearInbox_EmptyReturnsZero documents the no-op case.
func TestAgent_ClearInbox_EmptyReturnsZero(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("clear-empty", &blockingLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if n := a.ClearInbox(); n != 0 {
		t.Errorf("ClearInbox on empty inbox returned %d, want 0", n)
	}
}

// TestPool_ClearInbox checks the pool passthrough and the unknown-agent error.
func TestPool_ClearInbox(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("pool-clear", &blockingLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "one"})
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "two"})

	n, err := pool.ClearInbox("pool-clear")
	if err != nil {
		t.Fatalf("ClearInbox: %v", err)
	}
	if n != 2 {
		t.Errorf("ClearInbox removed %d, want 2", n)
	}
	if _, err := pool.ClearInbox("no-such-agent"); err == nil {
		t.Error("expected error clearing inbox of unknown agent")
	}
}

// TestAgent_DeleteFromInbox_ByIDWorksWhileRunning pins the selective gating:
// a message ID is stable while messages arrive, so deleting by ID mid-turn is
// safe (either the drain takes the message or the delete wins — never both).
// This is what lets a running agent cancel one of its own queued messages.
func TestAgent_DeleteFromInbox_ByIDWorksWhileRunning(t *testing.T) {
	pool := agent.NewPool()
	a := func() *agent.Agent {
		a, err := pool.Spawn("del-running", newGatedLM(), agent.SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		return a
	}()

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "slow request")
	if a.IsRunning() == false {
		// unreachable guard; kept for symmetry with the other running tests
		_ = a
	}
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "obsolete"})
	queued := a.SnapshotInbox()
	if len(queued) != 1 || queued[0].ID == "" {
		t.Fatalf("queued message must carry an assigned ID, got %+v", queued)
	}

	n, err := a.DeleteFromInbox(-1, queued[0].ID)
	if err != nil {
		t.Fatalf("DeleteFromInbox by ID while running: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted %d messages, want 1", n)
	}
	if got := a.SnapshotInbox(); len(got) != 0 {
		t.Errorf("inbox not empty after delete: %d message(s)", len(got))
	}
	a.Cancel()
	<-done
}

// TestAgent_DeleteFromInbox_ByIndexStillGated pins that the by-index path
// remains gated on IsRunning: indexes shift as messages arrive, so an index is
// only meaningful against a quiescent snapshot.
func TestAgent_DeleteFromInbox_ByIndexStillGated(t *testing.T) {
	pool := agent.NewPool()
	lm := newGatedLM()
	a, err := pool.Spawn("del-index-running", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "slow request")
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the gated turn to start")
	}
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "one"})

	if _, err := a.DeleteFromInbox(0, ""); err == nil {
		t.Fatal("expected by-index delete to fail while running")
	}
	a.Cancel()
	<-done
	// finishTurn releases isRunning before firing onDone, so the agent is idle
	// here; the canceled turn skipped the drain, so "one" is still queued and
	// the by-index delete now has a quiescent target.
	if _, err := a.DeleteFromInbox(0, ""); err != nil {
		t.Fatalf("by-index delete should succeed once idle: %v", err)
	}
}

// TestAgent_EditInboxMessage_ByIDWorksWhileRunning pins the edit counterpart
// of the by-ID delete: an agent can correct a queued message mid-turn by ID.
func TestAgent_EditInboxMessage_ByIDWorksWhileRunning(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("edit-running", newGatedLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "slow request")
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "original"})
	queued := a.SnapshotInbox()
	if len(queued) != 1 || queued[0].ID == "" {
		t.Fatalf("queued message must carry an assigned ID, got %+v", queued)
	}

	if err := a.EditInboxMessage(-1, queued[0].ID, "corrected"); err != nil {
		t.Fatalf("EditInboxMessage by ID while running: %v", err)
	}
	got := a.SnapshotInbox()
	if len(got) != 1 || got[0].Content != "corrected" {
		t.Fatalf("edit not applied: %+v", got)
	}
	a.Cancel()
	<-done
}
