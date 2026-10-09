package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// context.go exposes the /context breakdown (agent.ContextBreakdown) over the
// API so a browser can show the same composition view the terminal's `/context`
// modal does.
//
// The breakdown is a rich engine struct with no JSON tags (and a fantasy.Usage
// sub-struct), so the wire shape is an explicit view rather than a marshal of
// the engine type. That keeps the API a stable contract independent of internal
// field renames, matching how the rest of the surface is defined.

import (
	"net/http"

	"github.com/mattdurham/wllr/modules/agent"
)

// contextView is the GET /api/context response: the context window, the last
// request's usage (provider-reported or the chars/4 baseline seed), and the
// estimated composition of the next request, plus the canonical transcript that
// is not in context but is recall-able.
type contextView struct {
	// Canonical is the transcript that is NOT sent to the provider; the recall
	// tool can retrieve it. Omitted when there is nothing recorded.
	Canonical *contextCanonical `json:"canonical,omitempty"`
	AgentID   string            `json:"agent_id"`

	SystemComponents []contextSource `json:"system_components,omitempty"`
	ToolsByTool      []contextTool   `json:"tools_by_tool,omitempty"`
	LargestMessages  []contextMsg    `json:"largest_messages,omitempty"`
	// LastRequest is the most recently completed turn's usage. IsEstimate is
	// true when it carries the chars/4 baseline seed rather than a real provider
	// report, so a client can label it.
	LastRequest struct {
		InputTokens         int64 `json:"input_tokens"`
		OutputTokens        int64 `json:"output_tokens"`
		CacheReadTokens     int64 `json:"cache_read_tokens"`
		CacheCreationTokens int64 `json:"cache_creation_tokens"`
		IsEstimate          bool  `json:"is_estimate"`
	} `json:"last_request"`

	ContextWindow int64 `json:"context_window"`

	// NextRequest is the estimated composition (chars/4 heuristic).
	SystemTokens    int64 `json:"system_tokens"`
	ToolCount       int   `json:"tool_count"`
	ToolTokens      int64 `json:"tool_tokens"`
	HistoryTokens   int64 `json:"history_tokens"`
	UserTokens      int64 `json:"user_tokens"`
	AssistantTokens int64 `json:"assistant_tokens"`
	TurnCount       int   `json:"turn_count"`
	FilteredTokens  int64 `json:"filtered_tokens"`
	EstimatedTotal  int64 `json:"estimated_total"`
}

type contextSource struct {
	Source string `json:"source"`
	Tokens int64  `json:"tokens"`
}

type contextTool struct {
	Name      string `json:"name"`
	Extension string `json:"extension,omitempty"`
	Tokens    int64  `json:"tokens"`
	Chars     int64  `json:"chars,omitempty"`
}

type contextMsg struct {
	Role   string `json:"role"`
	Tokens int64  `json:"tokens"`
}

type contextCanonical struct {
	ByKind     map[string]int64 `json:"by_kind,omitempty"`
	ByRole     map[string]int64 `json:"by_role,omitempty"`
	ByTool     []contextTool    `json:"by_tool,omitempty"`
	Chars      int64            `json:"chars"`
	Entries    int              `json:"entries"`
	Recallable bool             `json:"recallable"`
}

// buildContextView flattens an agent.ContextBreakdown into the wire shape.
func buildContextView(id string, b agent.ContextBreakdown) contextView {
	v := contextView{
		AgentID:         id,
		ContextWindow:   b.ContextWindow,
		SystemTokens:    b.SystemTokens,
		ToolCount:       b.ToolCount,
		ToolTokens:      b.ToolTokens,
		HistoryTokens:   b.HistoryTokens,
		UserTokens:      b.UserTokens,
		AssistantTokens: b.AssistantTokens,
		TurnCount:       b.TurnCount,
		FilteredTokens:  b.FilteredTokens,
		EstimatedTotal:  b.Sum(),
	}
	v.LastRequest.InputTokens = b.LastRequest.InputTokens
	v.LastRequest.OutputTokens = b.LastRequest.OutputTokens
	v.LastRequest.CacheReadTokens = b.LastRequest.CacheReadTokens
	v.LastRequest.CacheCreationTokens = b.LastRequest.CacheCreationTokens
	v.LastRequest.IsEstimate = b.LastRequestIsEstimate

	for _, c := range b.SystemComponents {
		v.SystemComponents = append(v.SystemComponents, contextSource{
			Source: c.Source,
			// Components are reported as raw char counts; the modal shows the
			// chars/4 estimate, which is the same heuristic SystemTokens uses.
			Tokens: int64(c.Chars) / 4,
		})
	}
	for _, ts := range b.ToolsByTool {
		v.ToolsByTool = append(v.ToolsByTool, contextTool{
			Name:      ts.Name,
			Extension: ts.Extension,
			Tokens:    ts.Tokens,
		})
	}
	for _, ms := range b.LargestMessages {
		v.LargestMessages = append(v.LargestMessages, contextMsg{Role: ms.Role, Tokens: ms.Tokens})
	}
	if b.CanonicalEntries > 0 {
		c := &contextCanonical{
			Chars:      b.CanonicalChars,
			Entries:    b.CanonicalEntries,
			ByKind:     b.CanonicalByKind,
			ByRole:     b.CanonicalMessagesByRole,
			Recallable: true,
		}
		for _, ts := range b.CanonicalByTool {
			c.ByTool = append(c.ByTool, contextTool{Name: ts.Name, Chars: ts.Chars})
		}
		v.Canonical = c
	}
	return v
}

// handleContext returns the /context breakdown for an agent (default: root).
// The breakdown is safe to snapshot mid-turn (the engine takes the same locks
// the submit paths do), so the modal can be opened while a turn runs.
func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if !s.requirePool(w) {
		return
	}
	id, err := s.resolveAgent(r.URL.Query().Get("agent"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	a := s.pool.Get(id)
	if a == nil {
		writeError(w, http.StatusNotFound, resourceMissing(id))
		return
	}
	writeJSON(w, http.StatusOK, buildContextView(id, a.ContextBreakdown()))
}
