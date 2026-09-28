package agent_test

// contextbreakdown_test.go covers the /context breakdown: bucket math on
// seeded history, the system/steering filter exclusion, tool-definition
// estimates, the pool delegate to the main agent, and nil-safety before the
// main agent exists.

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestContextBreakdown_Buckets(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()
	bigTool := fantasy.NewAgentTool(
		"big_tool",
		strings.Repeat("d", 4000), // ~1000 tokens estimated
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)
	a, err := pool.Spawn("a", lm, agent.SpawnOpts{
		SystemPrompt: strings.Repeat("s", 800), // ~200 tokens
		Tools:        []fantasy.AgentTool{bigTool},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	msgs := []sdk.Message{
		{Role: sdk.RoleUser, Content: strings.Repeat("u", 400)},                                // 100
		{Role: sdk.RoleAssistant, Content: strings.Repeat("a", 800)},                           // 200
		{Role: sdk.RoleUser, Content: strings.Repeat("u", 200)},                                // 50
		{Role: sdk.RoleAssistant, Content: strings.Repeat("a", 400)},                           // 100
		{Role: sdk.RoleUser, Content: strings.Repeat("f", 400), Type: sdk.MessageTypeSteering}, // filtered
		{Role: sdk.RoleUser, Content: strings.Repeat("g", 400), Type: sdk.MessageTypeSystem},   // filtered
	}
	if err := pool.SetAgentHistory("a", msgs); err != nil {
		t.Fatalf("SetAgentHistory: %v", err)
	}

	b := a.ContextBreakdown()

	if b.SystemTokens != 200 {
		t.Errorf("SystemTokens = %d, want 200", b.SystemTokens)
	}
	// estimateToolTokens JSON-marshals the full ToolInfo (name + description
	// + schema), so the estimate exceeds the bare description's chars/4 but
	// stays in its neighborhood.
	if b.ToolTokens < 1000 || b.ToolTokens > 1200 {
		t.Errorf("ToolTokens = %d, want ~1000 (4000-char description plus json overhead)", b.ToolTokens)
	}
	if b.ToolCount != 1 {
		t.Errorf("ToolCount = %d, want 1", b.ToolCount)
	}
	if b.UserTokens != 150 {
		t.Errorf("UserTokens = %d, want 150", b.UserTokens)
	}
	if b.AssistantTokens != 300 {
		t.Errorf("AssistantTokens = %d, want 300", b.AssistantTokens)
	}
	if b.HistoryTokens != 450 {
		t.Errorf("HistoryTokens = %d, want 450 (user+assistant only)", b.HistoryTokens)
	}
	if b.FilteredTokens != 200 {
		t.Errorf("FilteredTokens = %d, want 200 (steering+system excluded from LLM-facing)", b.FilteredTokens)
	}
	if b.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", b.TurnCount)
	}
	// Sum is the additive identity over LLM-facing buckets (filtered and
	// canonical excluded).
	if want := b.SystemTokens + b.ToolTokens + b.HistoryTokens; b.Sum() != want {
		t.Errorf("Sum = %d, want %d", b.Sum(), want)
	}
}

func TestContextBreakdown_CanonicalReportedNotCreated(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	b := a.ContextBreakdown()
	if b.CanonicalEntries != 0 || b.CanonicalChars != 0 {
		t.Errorf("fresh agent canonical = (%d chars, %d entries), want zeros (no transcript created)",
			b.CanonicalChars, b.CanonicalEntries)
	}

	// Recording content creates the transcript and the breakdown reports it;
	// canonical size must not leak into the context sum.
	a.CanonicalTranscript().RecordMessage("user", strings.Repeat("x", 4000))
	b = a.ContextBreakdown()
	if b.CanonicalChars != 4000 || b.CanonicalEntries != 1 {
		t.Errorf("canonical = (%d chars, %d entries), want (4000, 1)", b.CanonicalChars, b.CanonicalEntries)
	}
	if b.HistoryTokens != 0 {
		t.Errorf("HistoryTokens = %d, want 0 (canonical is never context)", b.HistoryTokens)
	}
}

func TestContextBreakdown_PoolDelegatesToMainAgent(t *testing.T) {
	pool := agent.NewPool()

	// No main agent yet: zero-value breakdown, no panic.
	if b := pool.ContextBreakdown(); b.SystemTokens != 0 || b.HistoryTokens != 0 {
		t.Errorf("pool breakdown without main agent = %+v, want zero value", b)
	}

	if _, err := pool.Spawn(agent.MainAgentID, newMockLM(), agent.SpawnOpts{
		SystemPrompt: strings.Repeat("s", 400),
	}); err != nil {
		t.Fatalf("Spawn main: %v", err)
	}
	b := pool.ContextBreakdown()
	if b.SystemTokens != 100 {
		t.Errorf("pool breakdown SystemTokens = %d, want 100", b.SystemTokens)
	}
}

func TestContextBreakdown_WindowFromAgent(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{ContextWindow: 12345})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if b := a.ContextBreakdown(); b.ContextWindow != 12345 {
		t.Errorf("ContextWindow = %d, want 12345", b.ContextWindow)
	}
}

func TestContextBreakdown_PerToolTokens(t *testing.T) {
	pool := agent.NewPool()
	big := fantasy.NewAgentTool("big_tool", strings.Repeat("d", 4000),
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		})
	small := fantasy.NewAgentTool("small_tool", strings.Repeat("d", 400),
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		})
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{Tools: []fantasy.AgentTool{small, big}})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	b := a.ContextBreakdown()
	if len(b.ToolsByTool) != 2 {
		t.Fatalf("ToolsByTool has %d entries, want 2", len(b.ToolsByTool))
	}
	// Heaviest first regardless of registration order.
	if b.ToolsByTool[0].Name != "big_tool" || b.ToolsByTool[1].Name != "small_tool" {
		t.Errorf("ToolsByTool order = [%s %s], want [big_tool small_tool]",
			b.ToolsByTool[0].Name, b.ToolsByTool[1].Name)
	}
	// Each entry is the individually-rounded estimate of its own ToolInfo:
	// well above bare description/4 (json overhead) and well below the other
	// tool's scale.
	if b.ToolsByTool[0].Tokens < 1000 || b.ToolsByTool[0].Tokens > 1200 {
		t.Errorf("big_tool tokens = %d, want ~1000", b.ToolsByTool[0].Tokens)
	}
	if b.ToolsByTool[1].Tokens < 100 || b.ToolsByTool[1].Tokens > 200 {
		t.Errorf("small_tool tokens = %d, want ~100", b.ToolsByTool[1].Tokens)
	}
	// The aggregate must stay the total-chars/4 value, within the documented
	// per-tool rounding slack (up to ToolCount-1 tokens).
	if b.ToolTokens < b.ToolsByTool[0].Tokens+b.ToolsByTool[1].Tokens ||
		b.ToolTokens > b.ToolsByTool[0].Tokens+b.ToolsByTool[1].Tokens+1 {
		t.Errorf("ToolTokens = %d vs per-tool sum %d: outside documented rounding slack",
			b.ToolTokens, b.ToolsByTool[0].Tokens+b.ToolsByTool[1].Tokens)
	}
}

func TestContextBreakdown_LargestMessages(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	msgs := []sdk.Message{
		{Role: sdk.RoleUser, Content: strings.Repeat("u", 200)},      // 50
		{Role: sdk.RoleAssistant, Content: strings.Repeat("a", 800)}, // 200
		{Role: sdk.RoleUser, Content: strings.Repeat("p", 4000)},     // 1000 — the pasted doc
		{
			Role:    sdk.RoleUser,
			Content: strings.Repeat("s", 400),
			Type:    sdk.MessageTypeSteering,
		}, // filtered: never listed
	}
	if err := pool.SetAgentHistory("a", msgs); err != nil {
		t.Fatalf("SetAgentHistory: %v", err)
	}
	b := a.ContextBreakdown()
	if len(b.LargestMessages) != 3 {
		t.Fatalf("LargestMessages has %d entries, want 3 (steering filtered)", len(b.LargestMessages))
	}
	// Heaviest first: the pasted document must surface despite arriving
	// third in the history.
	if b.LargestMessages[0].Tokens != 1000 || b.LargestMessages[0].Role != "user" {
		t.Errorf("LargestMessages[0] = {%s %d}, want {user 1000}",
			b.LargestMessages[0].Role, b.LargestMessages[0].Tokens)
	}
	if b.LargestMessages[1].Tokens != 200 || b.LargestMessages[1].Role != "assistant" {
		t.Errorf("LargestMessages[1] = {%s %d}, want {assistant 200}",
			b.LargestMessages[1].Role, b.LargestMessages[1].Tokens)
	}
	// Chars are the verbatim length, matching the transcript-side accounting.
	if b.LargestMessages[0].Chars != 4000 {
		t.Errorf("LargestMessages[0].Chars = %d, want 4000", b.LargestMessages[0].Chars)
	}
}

func TestContextBreakdown_CanonicalAttribution(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	tr := a.CanonicalTranscript()
	tr.RecordMessage("user", strings.Repeat("u", 400))          // messages/user
	tr.RecordMessage("assistant", strings.Repeat("a", 800))     // messages/assistant
	tr.RecordToolCall("read_file", strings.Repeat("i", 100))    // tool calls/read_file
	tr.RecordToolResult("read_file", strings.Repeat("o", 2000)) // tool results/read_file
	tr.RecordToolResult("exec", strings.Repeat("o", 1000))      // tool results/exec

	b := a.ContextBreakdown()
	if b.CanonicalChars != 4300 || b.CanonicalEntries != 5 {
		t.Fatalf("canonical totals = (%d chars, %d entries), want (4300, 5)",
			b.CanonicalChars, b.CanonicalEntries)
	}
	if b.CanonicalByKind[agent.TranscriptKindMessage] != 1200 {
		t.Errorf("byKind messages = %d, want 1200", b.CanonicalByKind[agent.TranscriptKindMessage])
	}
	if b.CanonicalByKind[agent.TranscriptKindToolCall] != 100 {
		t.Errorf("byKind tool calls = %d, want 100", b.CanonicalByKind[agent.TranscriptKindToolCall])
	}
	if b.CanonicalByKind[agent.TranscriptKindToolResult] != 3000 {
		t.Errorf("byKind tool results = %d, want 3000", b.CanonicalByKind[agent.TranscriptKindToolResult])
	}
	if b.CanonicalMessagesByRole["user"] != 400 || b.CanonicalMessagesByRole["assistant"] != 800 {
		t.Errorf("messagesByRole = (user %d, assistant %d), want (400, 800)",
			b.CanonicalMessagesByRole["user"], b.CanonicalMessagesByRole["assistant"])
	}
	// Tool traffic attributed and sorted: read_file (2100 = call + result)
	// before exec (1000). Tool call inputs and results share the bucket.
	if len(b.CanonicalByTool) != 2 ||
		b.CanonicalByTool[0].Name != "read_file" || b.CanonicalByTool[0].Chars != 2100 ||
		b.CanonicalByTool[1].Name != "exec" || b.CanonicalByTool[1].Chars != 1000 {
		t.Errorf("CanonicalByTool = %v, want [read_file 2100 exec 1000]", b.CanonicalByTool)
	}
	// Tool traffic is canonical, never context.
	if b.HistoryTokens != 0 {
		t.Errorf("HistoryTokens = %d, want 0", b.HistoryTokens)
	}
}

func TestContextBreakdown_SystemComponentsFromPool(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Nothing set yet: no components.
	if b := a.ContextBreakdown(); len(b.SystemComponents) != 0 {
		t.Errorf("SystemComponents before any Set = %v, want empty", b.SystemComponents)
	}

	// The Set seeds the fallback component...
	pool.SetBaseSystemPrompt(strings.Repeat("s", 400))
	b := a.ContextBreakdown()
	if len(b.SystemComponents) != 1 || b.SystemComponents[0].Source != "system prompt" ||
		b.SystemComponents[0].Chars != 400 {
		t.Errorf("fallback components = %v, want [{system prompt 400}]", b.SystemComponents)
	}

	// ...the prompt extension's report replaces it...
	pool.SetBaseSystemPromptComponents([]sdk.SystemPromptComponent{
		{Source: "built-in rules", Chars: 100},
		{Source: "file:~/.wllr/AGENTS.md", Chars: 200},
	})
	b = a.ContextBreakdown()
	if len(b.SystemComponents) != 2 || b.SystemComponents[0].Source != "built-in rules" {
		t.Errorf("reported components = %v, want the 2-entry report", b.SystemComponents)
	}

	// ...and labeled appends add entries in arrival order.
	pool.AppendBaseSystemPromptFrom("skills", strings.Repeat("k", 100))
	b = a.ContextBreakdown()
	if len(b.SystemComponents) != 3 || b.SystemComponents[2].Source != "skills" || b.SystemComponents[2].Chars != 100 {
		t.Errorf("components after append = %v, want skills appended", b.SystemComponents)
	}

	// An empty report must not erase attribution (the pool ignores it).
	pool.SetBaseSystemPromptComponents(nil)
	b = a.ContextBreakdown()
	if len(b.SystemComponents) != 3 {
		t.Errorf("components after empty report = %v, want unchanged (3 entries)", b.SystemComponents)
	}

	// PromptComponents returns a copy — mutating it must not corrupt the pool.
	comps := pool.PromptComponents()
	comps[0].Source = "tampered"
	if again := pool.PromptComponents(); again[0].Source == "tampered" {
		t.Errorf("PromptComponents leaked internal state")
	}
}

func TestContextBreakdown_ToolOwners(t *testing.T) {
	pool := agent.NewPool()
	pool.SetToolOwnersFn(func() map[string]string {
		return map[string]string{"ext_tool": "skills", "other_tool": "agents"}
	})
	big := fantasy.NewAgentTool("ext_tool", strings.Repeat("d", 400),
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		})
	native := fantasy.NewAgentTool("native_tool", strings.Repeat("d", 400),
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		})
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{Tools: []fantasy.AgentTool{native, big}})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	b := a.ContextBreakdown()
	byName := map[string]string{}
	for _, ts := range b.ToolsByTool {
		byName[ts.Name] = ts.Extension
	}
	if byName["ext_tool"] != "skills" {
		t.Errorf("ext_tool owner = %q, want skills", byName["ext_tool"])
	}
	// No owner in the map: the field stays empty (renderer labels "harness").
	if byName["native_tool"] != "" {
		t.Errorf("native_tool owner = %q, want empty", byName["native_tool"])
	}

	// No resolver installed: nil map, empty owners, no panic.
	pool2 := agent.NewPool()
	if owners := pool2.ToolOwners(); owners != nil {
		t.Errorf("ToolOwners without resolver = %v, want nil", owners)
	}
}
