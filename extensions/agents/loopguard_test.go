package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func sig(name, input string) toolCallSig {
	return toolCallSig{Name: name, Input: normalizeLoopInput(input)}
}

// enabledPtr is a helper for guard configs in tests (Enabled is *bool so a
// missing key defaults to enabled).
func enabledPtr(v bool) *bool { return &v }

func TestNormalizeLoopInput(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"key order", `{"a":1,"b":2}`, `{"a":1,"b":2}`},
		{"key order reversed", `{"b":2,"a":1}`, `{"a":1,"b":2}`},
		{"whitespace", `{ "a" : 1 }`, `{"a":1}`},
		{"non-json raw", `some raw text`, `some raw text`},
		{"empty", `   `, ``},
	}
	for _, tc := range cases {
		if got := normalizeLoopInput(tc.in); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestDetectToolLoop_ConsecutiveAtThreshold(t *testing.T) {
	calls := []toolCallSig{
		sig("read_file", `{"path":"/x"}`),
		sig("exec", `{"command":"echo hi"}`),
		sig("exec", `{"command":"echo hi"}`),
		sig("exec", `{"command":"echo hi"}`),
	}
	det := detectToolLoop(calls, 10, 3, 5)
	if !det.Looped || det.Period != 1 || det.Repeats != 3 {
		t.Fatalf("expected consecutive loop at 3, got %+v", det)
	}
	if det.Tool != "exec" {
		t.Errorf("tool = %q, want exec", det.Tool)
	}
}

func TestDetectToolLoop_BelowThresholdForgivesRetry(t *testing.T) {
	// One identical retry after a failure is forgiven at the default
	// min_repeats=3: the model read the same file twice, nothing pathological.
	calls := []toolCallSig{
		sig("exec", `{"command":"flaky-cmd"}`),
		sig("exec", `{"command":"flaky-cmd"}`),
	}
	if det := detectToolLoop(calls, 10, 3, 5); det.Looped {
		t.Fatalf("expected no loop at 2 identical calls with min_repeats=3, got %+v", det)
	}
	// min_repeats=2 (tightened config) fires at two.
	if det := detectToolLoop(calls, 10, 2, 5); !det.Looped || det.Repeats != 2 {
		t.Fatalf("expected loop at 2 with min_repeats=2, got %+v", det)
	}
}

func TestDetectToolLoop_Cycle(t *testing.T) {
	// A,B,A,B,A,B — 2-call cycle repeated 3 times.
	calls := []toolCallSig{
		sig("a", `{"x":1}`), sig("b", `{"y":1}`),
		sig("a", `{"x":1}`), sig("b", `{"y":1}`),
		sig("a", `{"x":1}`), sig("b", `{"y":1}`),
	}
	det := detectToolLoop(calls, 10, 3, 5)
	if !det.Looped || det.Period != 2 || det.Repeats < 2 {
		t.Fatalf("expected 2-period cycle, got %+v", det)
	}
}

func TestDetectToolLoop_CycleLongerThanMaxPeriodNotLoop(t *testing.T) {
	// 6-call distinct cycle (period 6 > default max 5) is a legitimate
	// per-file batch, not a loop.
	calls := make([]toolCallSig, 0, 12)
	for i := 0; i < 2; i++ {
		for j := 1; j <= 6; j++ {
			calls = append(calls, sig("read_file", fmt.Sprintf(`{"path":"/f%d"}`, j)))
		}
	}
	if det := detectToolLoop(calls, 10, 3, 5); det.Looped {
		t.Fatalf("expected no loop for 6-period batch, got %+v", det)
	}
}

func TestDetectToolLoop_LegitimateEditVerifyDoesNotFire(t *testing.T) {
	// read→edit→read→verify twice: the edit inputs differ, so no cycle.
	calls := []toolCallSig{
		sig("read_file", `{"path":"/x"}`),
		sig("edit_file", `{"path":"/x","content":"v1"}`),
		sig("read_file", `{"path":"/x"}`),
		sig("exec", `{"command":"verify"}`),
		sig("read_file", `{"path":"/x"}`),
		sig("edit_file", `{"path":"/x","content":"v2"}`),
		sig("read_file", `{"path":"/x"}`),
		sig("exec", `{"command":"verify"}`),
	}
	if det := detectToolLoop(calls, 10, 3, 5); det.Looped {
		t.Fatalf("legitimate workflow flagged: %+v", det)
	}
}

func TestDetectToolLoop_BrokenCycleDoesNotFire(t *testing.T) {
	// The model escaped the loop with an unrelated call: the tail no longer
	// matches, so no fire even though the window holds old cycle calls.
	calls := []toolCallSig{
		sig("a", `{"x":1}`), sig("b", `{"y":1}`),
		sig("a", `{"x":1}`), sig("b", `{"y":1}`),
		sig("c", `{"z":1}`),
	}
	if det := detectToolLoop(calls, 10, 3, 5); det.Looped {
		t.Fatalf("broken cycle flagged: %+v", det)
	}
}

func TestDetectToolLoop_WindowLimits(t *testing.T) {
	// Old calls outside the window do not contribute.
	calls := []toolCallSig{
		sig("old", `{"i":0}`), sig("old", `{"i":0}`), sig("old", `{"i":0}`),
		sig("read_file", `{"path":"/a"}`),
		sig("read_file", `{"path":"/b"}`),
	}
	// window=2: only the two read_file calls are examined.
	if det := detectToolLoop(calls, 2, 3, 5); det.Looped {
		t.Fatalf("window limit violated: %+v", det)
	}
}

func TestLoopGuardConfig_ApplyDefaultsAndScope(t *testing.T) {
	cfg := loopGuardConfig{}.applyDefaults()
	if cfg.Window != defaultLoopGuardWindow || cfg.MinRepeats != defaultLoopGuardMinRepeats ||
		cfg.MaxPeriod != defaultLoopGuardMaxPeriod || cfg.Scope != "all" {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
	if !cfg.appliesTo("main") || !cfg.appliesTo("main/researcher") {
		t.Fatalf("scope=all should cover main and subagents")
	}
	sub := loopGuardConfig{Scope: "subagents"}.applyDefaults()
	if sub.appliesTo("main") {
		t.Fatalf("scope=subagents must exclude main")
	}
	if !sub.appliesTo("main/researcher") {
		t.Fatalf("scope=subagents must cover subagent IDs")
	}
}

func TestLoopGuard_ObserveAndRollback(t *testing.T) {
	g := newLoopGuard(loopGuardConfig{Enabled: enabledPtr(true), MinRepeats: 3})
	in := json.RawMessage(`{"command":"echo hi"}`)
	var det *loopDetection
	for i := 0; i < 3; i++ {
		det = g.observe("main/researcher", "exec", in)
	}
	if det == nil || !det.Looped {
		t.Fatalf("expected detection at third identical call, got %+v", det)
	}
	// Rollback: buffer length unchanged by blocked calls.
	if len(g.buffers["main/researcher"]) != 2 {
		t.Fatalf("blocked call not rolled back: %d entries", len(g.buffers["main/researcher"]))
	}
	// Repeated denial: the same pattern keeps firing deterministically.
	for i := 0; i < 3; i++ {
		if d := g.observe("main/researcher", "exec", in); d == nil || !d.Looped {
			t.Fatalf("expected repeated denial at attempt %d, got %+v", i, d)
		}
	}
}

func TestLoopGuard_PerAgentIsolation(t *testing.T) {
	g := newLoopGuard(loopGuardConfig{Enabled: enabledPtr(true), MinRepeats: 2})
	in := json.RawMessage(`{"path":"/x"}`)
	// Agent A builds a 2-run; agent B's first identical call must not fire.
	if d := g.observe("main/a", "read_file", in); d != nil {
		t.Fatalf("unexpected detection for first call: %+v", d)
	}
	if d := g.observe("main/a", "read_file", in); d == nil {
		t.Fatalf("expected detection for agent a at run 2")
	}
	if d := g.observe("main/b", "read_file", in); d != nil {
		t.Fatalf("agent b contaminated by agent a: %+v", d)
	}
}

func TestLoopGuard_ScopeSubagentsExcludesMain(t *testing.T) {
	g := newLoopGuard(loopGuardConfig{Enabled: enabledPtr(true), Scope: "subagents", MinRepeats: 2})
	in := json.RawMessage(`{"path":"/x"}`)
	for i := 0; i < 5; i++ {
		if d := g.observe("main", "read_file", in); d != nil {
			t.Fatalf("main agent should be exempt under scope=subagents: %+v", d)
		}
	}
}

func TestLoopGuard_DisabledNil(t *testing.T) {
	var g *loopGuard
	if d := g.observe("main", "exec", json.RawMessage(`{}`)); d != nil {
		t.Fatalf("nil guard must not detect: %+v", d)
	}
}

func TestLoopGuardMessage_ContainsGuidance(t *testing.T) {
	msg := loopGuardMessage(loopDetection{Looped: true, Period: 1, Repeats: 3, Tool: "exec"})
	for _, want := range []string{"[loop guard]", "exec", "3 times", "Stop and reorient"} {
		if !contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
