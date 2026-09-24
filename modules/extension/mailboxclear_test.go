package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Host handler tests for the mailbox_clear host call (issue #48): the /queue
// extension and the harness UI use it to bulk-discard an agent's queued
// messages, including while a turn is running.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestHost_MailboxClear_RemovesAll(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	var gotID string
	h.SetAgentBridge(&testAgentBridge{onClearInbox: func(id string) (int, error) {
		gotID = id
		return 3, nil
	}})

	resp := h.handleMailboxClear(nil, sdk.HostCallRequest{
		Method: sdk.MethodMailboxClear,
		Params: json.RawMessage(`{"id":"main"}`),
	})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotID != "main" {
		t.Errorf("bridge got id %q, want \"main\"", gotID)
	}
	var result struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Removed != 3 {
		t.Errorf("removed = %d, want 3", result.Removed)
	}
}

func TestHost_MailboxClear_NoBridge(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	resp := h.handleMailboxClear(nil, sdk.HostCallRequest{
		Method: sdk.MethodMailboxClear,
		Params: json.RawMessage(`{"id":"main"}`),
	})
	if !strings.Contains(resp.Error, "not supported by host") {
		t.Errorf("error = %q, want \"not supported by host\"", resp.Error)
	}
}

func TestHost_MailboxClear_EmptyID(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetAgentBridge(&testAgentBridge{})
	resp := h.handleMailboxClear(nil, sdk.HostCallRequest{
		Method: sdk.MethodMailboxClear,
		Params: json.RawMessage(`{}`),
	})
	if !strings.Contains(resp.Error, "id must be non-empty") {
		t.Errorf("error = %q, want id validation error", resp.Error)
	}
}

func TestHost_MailboxClear_BridgeErrorPropagates(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetAgentBridge(&testAgentBridge{onClearInbox: func(string) (int, error) {
		return 0, context.DeadlineExceeded
	}})
	resp := h.handleMailboxClear(nil, sdk.HostCallRequest{
		Method: sdk.MethodMailboxClear,
		Params: json.RawMessage(`{"id":"main"}`),
	})
	if resp.Error == "" {
		t.Error("expected bridge error to surface in response")
	}
}

// TestHost_MailboxDelete_ByIDOnlyDoesNotPanic pins the nil-ByIndex guard: an
// ID-only delete (no by_index) must reach the bridge with byIndex=-1, not
// dereference a nil pointer (which crashed the host call as a wazero trap).
func TestHost_MailboxDelete_ByIDOnlyDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	var gotIndex int
	var gotID string
	h.SetAgentBridge(&testAgentBridge{onDeleteFromInbox: func(id string, byIndex int, byMessageID string) (int, error) {
		gotID, gotIndex = id, byIndex
		return 1, nil
	}})
	resp := h.handleMailboxDelete(nil, sdk.HostCallRequest{
		Method: sdk.MethodMailboxDelete,
		Params: json.RawMessage(`{"id":"main","by_message_id":"q1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotID != "main" || gotIndex != -1 {
		t.Errorf("bridge got (id=%q, byIndex=%d), want (main, -1)", gotID, gotIndex)
	}
}

// TestHost_MailboxDelete_ByIndexOnly passes the index through.
func TestHost_MailboxDelete_ByIndexOnly(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	var gotIndex int
	h.SetAgentBridge(&testAgentBridge{onDeleteFromInbox: func(_ string, byIndex int, _ string) (int, error) {
		gotIndex = byIndex
		return 1, nil
	}})
	resp := h.handleMailboxDelete(nil, sdk.HostCallRequest{
		Method: sdk.MethodMailboxDelete,
		Params: json.RawMessage(`{"id":"main","by_index":2}`),
	})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotIndex != 2 {
		t.Errorf("bridge got byIndex=%d, want 2", gotIndex)
	}
}

// TestHost_MailboxEdit_ByIDOnlyDoesNotPanic pins the same guard for edit.
func TestHost_MailboxEdit_ByIDOnlyDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	var gotIndex int
	h.SetAgentBridge(&testAgentBridge{onEditInboxMessage: func(_ string, byIndex int, _ string, _ string) error {
		gotIndex = byIndex
		return nil
	}})
	resp := h.handleMailboxEdit(nil, sdk.HostCallRequest{
		Method: sdk.MethodMailboxEdit,
		Params: json.RawMessage(`{"id":"main","by_message_id":"q1","new_content":"fixed"}`),
	})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if gotIndex != -1 {
		t.Errorf("bridge got byIndex=%d, want -1", gotIndex)
	}
}
