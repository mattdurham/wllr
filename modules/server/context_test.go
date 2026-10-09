package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestHandleContextReturnsBreakdown proves GET /api/context flattens the engine
// breakdown into the wire view: the context window, the next-request estimate
// (system + tools + history), and the derived estimated total, which must equal
// the engine's own Sum() so the browser and the terminal agree.
func TestHandleContextReturnsBreakdown(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/context", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var v contextView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.AgentID != agent.MainAgentID {
		t.Errorf("agent_id = %q, want %q", v.AgentID, agent.MainAgentID)
	}
	if v.ContextWindow != 100000 {
		t.Errorf("context_window = %d, want 100000", v.ContextWindow)
	}
	want := pool.ContextBreakdown().Sum()
	if v.EstimatedTotal != want {
		t.Errorf("estimated_total = %d, want engine Sum() = %d", v.EstimatedTotal, want)
	}
	// Before any turn there is no completed request, so the last request must be
	// empty and not labeled an estimate.
	if v.LastRequest.InputTokens != 0 {
		t.Errorf("last_request.input_tokens = %d, want 0 before a turn", v.LastRequest.InputTokens)
	}
}

// TestHandleContextUnknownAgent proves an unknown agent is a 404 rather than a
// breakdown of the wrong agent.
func TestHandleContextUnknownAgent(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/context?agent=nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleContextRejectsNonGet proves the endpoint is read-only.
func TestHandleContextRejectsNonGet(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/context", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestBuildContextViewMapsFields proves the flattening carries the sub-slices
// through: system components (chars/4), tools by tool, and largest messages, so
// the modal is not silently empty when the engine has the data.
func TestBuildContextViewMapsFields(t *testing.T) {
	b := agent.ContextBreakdown{
		ContextWindow:   200000,
		SystemTokens:    1000,
		ToolTokens:      2000,
		ToolCount:       3,
		HistoryTokens:   4000,
		UserTokens:      1000,
		AssistantTokens: 3000,
		TurnCount:       2,
		FilteredTokens:  50,
	}
	b.SystemComponents = append(b.SystemComponents, sdk.SystemPromptComponent{Source: "rules", Chars: 400})
	b.ToolsByTool = append(b.ToolsByTool, agent.ToolSize{Name: "read_file", Extension: "fs", Tokens: 100})
	b.LargestMessages = append(b.LargestMessages, agent.MessageSize{Role: "user", Tokens: 900})
	b.CanonicalChars = 12345
	b.CanonicalEntries = 9
	b.CanonicalByKind = map[string]int64{"message": 1000, "tool_call": 200}

	v := buildContextView("main", b)

	if v.EstimatedTotal != 7000 {
		t.Errorf("estimated_total = %d, want 7000 (1000+2000+4000)", v.EstimatedTotal)
	}
	if len(v.SystemComponents) != 1 || v.SystemComponents[0].Tokens != 100 {
		t.Errorf("system_components = %+v, want one entry of 100 tokens (400/4)", v.SystemComponents)
	}
	if len(v.ToolsByTool) != 1 || v.ToolsByTool[0].Extension != "fs" {
		t.Errorf("tools_by_tool = %+v, want the fs tool", v.ToolsByTool)
	}
	if len(v.LargestMessages) != 1 || v.LargestMessages[0].Role != "user" {
		t.Errorf("largest_messages = %+v, want one user entry", v.LargestMessages)
	}
	if v.Canonical == nil || v.Canonical.Entries != 9 || !v.Canonical.Recallable {
		t.Errorf("canonical = %+v, want 9 recallable entries", v.Canonical)
	}
	if v.Canonical.ByKind["tool_call"] != 200 {
		t.Errorf("canonical by_kind lost tool_call: %+v", v.Canonical.ByKind)
	}
}
