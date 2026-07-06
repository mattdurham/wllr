package agent

import (
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestMailbox_Snapshot tests snapshot returns a read-only copy
func TestMailbox_Snapshot(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "one"})
	b.append("a", sdk.Message{ID: "m2", Role: sdk.RoleAssistant, Content: "two"})

	snap := b.snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot length: got %d, want 2", len(snap))
	}
	if snap[0].ID != "m1" || snap[1].ID != "m2" {
		t.Errorf("snapshot IDs: got %v, want [m1 m2]", []string{snap[0].ID, snap[1].ID})
	}

	// Modifying the snapshot should NOT affect the original
	snap[0].Content = "modified"
	if b.len() != 2 {
		t.Errorf("snapshot mutation affected original; len = %d", b.len())
	}
	snap2 := b.snapshot()
	if snap2[0].Content != "one" {
		t.Errorf("original was modified; got %q", snap2[0].Content)
	}
}

// TestMailbox_DeleteByIndex tests deleteByIndex with various cases
func TestMailbox_DeleteByIndex(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "one"})
	b.append("a", sdk.Message{ID: "m2", Role: sdk.RoleAssistant, Content: "two"})
	b.append("a", sdk.Message{ID: "m3", Role: sdk.RoleUser, Content: "three"})

	// Delete middle
	old := b.deleteByIndex(1)
	if old == nil || old.ID != "m2" {
		t.Errorf("deleteByIndex(1): got %+v, want m2", old)
	}
	if b.len() != 2 {
		t.Errorf("len after delete: got %d, want 2", b.len())
	}

	// Delete first
	old = b.deleteByIndex(0)
	if old == nil || old.ID != "m1" {
		t.Errorf("deleteByIndex(0): got %+v, want m1", old)
	}
	if b.len() != 1 {
		t.Errorf("len after delete: got %d, want 1", b.len())
	}

	// Delete last
	old = b.deleteByIndex(0)
	if old == nil || old.ID != "m3" {
		t.Errorf("deleteByIndex(0): got %+v, want m3", old)
	}
	if b.len() != 0 {
		t.Errorf("len after delete: got %d, want 0", b.len())
	}

	// Delete from empty
	old = b.deleteByIndex(0)
	if old != nil {
		t.Errorf("delete from empty: got %+v, want nil", old)
	}

	// Out of bounds
	b.append("a", sdk.Message{ID: "x", Role: sdk.RoleUser, Content: "x"})
	old = b.deleteByIndex(5)
	if old != nil {
		t.Errorf("delete out of bounds: got %+v, want nil", old)
	}
	old = b.deleteByIndex(-1)
	if old != nil {
		t.Errorf("delete negative index: got %+v, want nil", old)
	}
}

// TestMailbox_EditByIndex tests editByIndex with various cases
func TestMailbox_EditByIndex(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "one"})
	b.append("a", sdk.Message{ID: "m2", Role: sdk.RoleAssistant, Content: "two"})

	// Edit first
	old := b.editByIndex(0, sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "updated one"})
	if old == nil || old.Content != "one" {
		t.Errorf("editByIndex(0): got old %+v, want content=one", old)
	}
	snap := b.snapshot()
	if snap[0].Content != "updated one" {
		t.Errorf("editByIndex(0) didn't persist: got %q", snap[0].Content)
	}

	// Edit middle
	old = b.editByIndex(1, sdk.Message{ID: "m2", Role: sdk.RoleAssistant, Content: "updated two"})
	if old == nil || old.Content != "two" {
		t.Errorf("editByIndex(1): got old %+v, want content=two", old)
	}

	// Out of bounds
	old = b.editByIndex(5, sdk.Message{ID: "x", Role: sdk.RoleUser, Content: "x"})
	if old != nil {
		t.Errorf("edit out of bounds: got %+v, want nil", old)
	}

	// Empty content should be rejected
	old = b.editByIndex(0, sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: ""})
	if old != nil {
		t.Errorf("edit with empty content: got %+v, want nil", old)
	}
}

// TestMailbox_DeleteByID tests deleteByID with various cases
func TestMailbox_DeleteByID(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "one"})
	b.append("a", sdk.Message{ID: "m2", Role: sdk.RoleAssistant, Content: "two"})
	b.append("a", sdk.Message{ID: "m3", Role: sdk.RoleUser, Content: "three"})

	// Delete middle
	old := b.deleteByID("m2")
	if old == nil || old.ID != "m2" {
		t.Errorf("deleteByID(m2): got %+v, want m2", old)
	}
	if b.len() != 2 {
		t.Errorf("len after delete: got %d, want 2", b.len())
	}

	// Delete non-existent
	old = b.deleteByID("nonexistent")
	if old != nil {
		t.Errorf("delete non-existent: got %+v, want nil", old)
	}

	// Delete last
	old = b.deleteByID("m3")
	if old == nil || old.ID != "m3" {
		t.Errorf("deleteByID(m3): got %+v", old)
	}

	// Delete first
	old = b.deleteByID("m1")
	if old == nil || old.ID != "m1" {
		t.Errorf("deleteByID(m1): got %+v", old)
	}
	if b.len() != 0 {
		t.Errorf("len after delete: got %d, want 0", b.len())
	}
}

// TestMailbox_EditByID tests editByID with various cases
func TestMailbox_EditByID(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "one"})
	b.append("a", sdk.Message{ID: "m2", Role: sdk.RoleAssistant, Content: "two"})

	// Edit first
	old := b.editByID("m1", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "updated one"})
	if old == nil || old.Content != "one" {
		t.Errorf("editByID(m1): got old %+v, want content=one", old)
	}
	snap := b.snapshot()
	if snap[0].Content != "updated one" {
		t.Errorf("editByID(m1) didn't persist: got %q", snap[0].Content)
	}

	// Edit non-existent
	old = b.editByID("nonexistent", sdk.Message{ID: "x", Role: sdk.RoleUser, Content: "x"})
	if old != nil {
		t.Errorf("edit non-existent: got %+v, want nil", old)
	}

	// Empty content should be rejected
	old = b.editByID("m1", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: ""})
	if old != nil {
		t.Errorf("edit with empty content: got %+v, want nil", old)
	}
}

// TestMailbox_ConcurrentAccess tests that all helper methods are goroutine-safe
func TestMailbox_ConcurrentAccess(t *testing.T) {
	var b mailbox

	// Pre-populate
	for i := 0; i < 10; i++ {
		b.append("a", sdk.Message{ID: msgKey(0, i), Role: sdk.RoleUser, Content: "initial"})
	}

	done := make(chan bool)

	// Snapshot goroutines
	for range 3 {
		go func() {
			defer func() { done <- true }()
			for i := 0; i < 10; i++ {
				_ = b.snapshot()
			}
		}()
	}

	// Delete goroutines
	for range 2 {
		go func() {
			defer func() { done <- true }()
			for i := 0; i < 5; i++ {
				b.deleteByID(msgKey(0, i))
			}
		}()
	}

	// Edit goroutines
	for range 2 {
		go func() {
			defer func() { done <- true }()
			for i := 0; i < 5; i++ {
				b.editByID(msgKey(0, i+5), sdk.Message{ID: msgKey(0, i+5), Role: sdk.RoleUser, Content: "updated"})
			}
		}()
	}

	// Wait for all goroutines
	for range 7 {
		<-done
	}
}

func TestMailbox_DeleteByID_DuplicateIDs(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "same", Role: sdk.RoleUser, Content: "first"})
	b.append("a", sdk.Message{ID: "same", Role: sdk.RoleAssistant, Content: "second"})

	// deleteByID should remove the first occurrence only
	old := b.deleteByID("same")
	if old == nil || old.Content != "first" {
		t.Errorf("deleteByID should remove first: got %+v", old)
	}
	if b.len() != 1 {
		t.Errorf("after delete, len = %d, want 1", b.len())
	}

	old = b.deleteByID("same")
	if old == nil || old.Content != "second" {
		t.Errorf("delete second occurrence: got %+v", old)
	}
	if b.len() != 0 {
		t.Errorf("after delete, len = %d, want 0", b.len())
	}
}

func TestMailbox_EditByID_DuplicateIDs(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "same", Role: sdk.RoleUser, Content: "first"})
	b.append("a", sdk.Message{ID: "same", Role: sdk.RoleAssistant, Content: "second"})

	// editByID should update the first occurrence only
	old := b.editByID("same", sdk.Message{ID: "same", Role: sdk.RoleUser, Content: "updated"})
	if old == nil || old.Content != "first" {
		t.Errorf("editByID should update first: got %+v", old)
	}

	snap := b.snapshot()
	if len(snap) != 2 || snap[0].Content != "updated" || snap[1].Content != "second" {
		t.Errorf("edit only first: got %+v", snap)
	}
}

func TestMailbox_DeleteByIndex_DuplicateIDs(t *testing.T) {
	var b mailbox
	b.append("a", sdk.Message{ID: "m1", Role: sdk.RoleUser, Content: "one"})
	b.append("a", sdk.Message{ID: "m2", Role: sdk.RoleUser, Content: "two"})

	// Delete first occurrence
	old := b.deleteByIndex(0)
	if old == nil || old.ID != "m1" {
		t.Errorf("deleteByIndex(0): got %+v", old)
	}
}
