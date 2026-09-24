package harness

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
)

// stubTool stands in for any tool already in the agent's list. Its name is
// configurable so a test can cover both "an unrelated tool is preserved" and
// "an extension tool already named recall is not shadowed".
type stubTool struct {
	name        string
	description string
}

func (s stubTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: s.name, Description: s.description}
}

func (s stubTool) Run(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return fantasy.NewTextResponse("stub"), nil
}

func (s stubTool) ProviderOptions() fantasy.ProviderOptions { return nil }

func (s stubTool) SetProviderOptions(fantasy.ProviderOptions) {}

// recallNameStub is an extension tool that already owns the "recall" name.
func recallNameStub() stubTool {
	return stubTool{name: agent.RecallToolName, description: "extension-owned"}
}

// The main agent (and every sub-agent) must receive the canonical-transcript
// recall tool, so exact pre-compaction detail stays retrievable after
// compaction has summarized it away (issue #42).
func TestWithRecallTool_AppendsForKnownAgent(t *testing.T) {
	pool := agent.NewPool()
	if _, err := pool.Spawn(agent.MainAgentID, newMockLM("hi"), agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	got := withRecallTool(nil, pool, agent.MainAgentID)
	if len(got) != 1 {
		t.Fatalf("got %d tools, want 1", len(got))
	}
	if name := got[0].Info().Name; name != agent.RecallToolName {
		t.Errorf("tool name = %q, want %q", name, agent.RecallToolName)
	}
}

func TestWithRecallTool_PreservesExistingTools(t *testing.T) {
	pool := agent.NewPool()
	if _, err := pool.Spawn(agent.MainAgentID, newMockLM("hi"), agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	base := []fantasy.AgentTool{stubTool{name: "exec", description: "unrelated"}}
	got := withRecallTool(base, pool, agent.MainAgentID)
	if len(got) != 2 {
		t.Fatalf("got %d tools, want 2 (existing + recall)", len(got))
	}
	if got[0].Info().Name != "exec" {
		t.Errorf("existing tool was reordered or replaced: %q", got[0].Info().Name)
	}
}

// An unknown agent yields no recall tool rather than a nil-dereference.
func TestWithRecallTool_UnknownAgentLeavesBaseUnchanged(t *testing.T) {
	pool := agent.NewPool()
	if got := withRecallTool(nil, pool, "does-not-exist"); len(got) != 0 {
		t.Errorf("got %d tools for an unknown agent, want 0", len(got))
	}
	if got := withRecallTool(nil, nil, agent.MainAgentID); len(got) != 0 {
		t.Errorf("got %d tools for a nil pool, want 0", len(got))
	}
}

// If an extension already registered a tool named "recall", it keeps ownership:
// the harness must not add a second tool under the same name.
func TestWithRecallTool_DoesNotShadowExtensionTool(t *testing.T) {
	pool := agent.NewPool()
	if _, err := pool.Spawn(agent.MainAgentID, newMockLM("hi"), agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	existing := recallNameStub()
	got := withRecallTool([]fantasy.AgentTool{existing}, pool, agent.MainAgentID)
	if len(got) != 1 {
		t.Fatalf("got %d tools, want 1 (the extension's own)", len(got))
	}
	if got[0].Info().Description != "extension-owned" {
		t.Error("the harness recall tool shadowed the extension's own")
	}
}
