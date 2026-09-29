// The tool-loop guard watches each agent's tool calls through the
// before_tool_call interceptor and, when the recent calls repeat the same
// pattern, BLOCKS the call with a stop-and-reorient reason. The model sees
// the block reason as the tool error, so the guidance reaches it in-band and
// the looping call never executes.
//
// Detection is deliberately input-sensitive: a call is identified by its tool
// name plus its normalized JSON input (key order and whitespace are
// canonicalized), so the common legitimate pattern read→edit→read→verify does
// not trigger — the second edit has different input. What triggers is the
// pathological case: the same call (or the same short cycle of calls)
// repeated because the model is not changing anything between attempts.
//
// This file is untagged so it can be tested natively (go test in this
// directory); main.go carries the wasip1 wiring.

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Default loop-guard settings. Missing or empty config uses these.
const (
	defaultLoopGuardWindow     = 10
	defaultLoopGuardMinRepeats = 3
	defaultLoopGuardMaxPeriod  = 5
)

// loopGuardConfig controls the guard. Window is how many recent calls are
// examined; MinRepeats is the consecutive-identical-call threshold (3 by
// default: one identical retry after a transient failure is forgiven, the
// third is a stuck pattern); MaxPeriod caps the cycle length considered a
// loop (longer cycles are almost always legitimate per-file batches); Scope
// is "all" or "subagents" (subagent IDs contain "/"; the main agent's ID
// does not).
//
// Enabled uses a pointer so a missing key means "unset" and the default
// (enabled) applies; an explicit `enabled: false` is the only way to turn
// the guard off. A plain bool would make a missing config disable the
// guard — the opposite of the fresh-install rule.
type loopGuardConfig struct {
	Enabled    *bool  `yaml:"enabled"`
	Window     int    `yaml:"window"`
	MinRepeats int    `yaml:"min_repeats"`
	MaxPeriod  int    `yaml:"max_period"`
	Scope      string `yaml:"scope"`
}

// EnabledOrDefault resolves the enabled flag with the default.
func (c loopGuardConfig) EnabledOrDefault() bool {
	return c.Enabled == nil || *c.Enabled
}

// applyDefaults fills zero fields with the built-in defaults so a partial
// config file only overrides what it sets.
func (c loopGuardConfig) applyDefaults() loopGuardConfig {
	if c.Window <= 0 {
		c.Window = defaultLoopGuardWindow
	}
	if c.MinRepeats <= 0 {
		c.MinRepeats = defaultLoopGuardMinRepeats
	}
	if c.MaxPeriod <= 0 {
		c.MaxPeriod = defaultLoopGuardMaxPeriod
	}
	if c.Scope == "" {
		c.Scope = "all"
	}
	return c
}

// appliesTo reports whether the guard covers this agent under the configured
// scope. Subagent IDs contain a "/" separator (e.g. "main/researcher"); the
// main agent's ID does not.
func (c loopGuardConfig) appliesTo(agentID string) bool {
	if strings.EqualFold(strings.TrimSpace(c.Scope), "subagents") {
		return strings.Contains(agentID, "/")
	}
	return true
}

// toolCallSig identifies one tool call for loop detection: the tool name plus
// its normalized input.
type toolCallSig struct {
	Name  string
	Input string
}

// normalizeLoopInput canonicalizes a raw tool-call input string so equivalent
// JSON compares equal regardless of key order or whitespace. Non-JSON input
// falls back to its trimmed raw text.
func normalizeLoopInput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return trimmed
	}
	out, err := json.Marshal(v) // map keys marshal in sorted order
	if err != nil {
		return trimmed
	}
	return string(out)
}

// loopDetection describes a detected repetition over the recent tool calls.
// Period 1 means identical back-to-back calls; a larger period means the tail
// of the window repeats as a cycle of that length. Repeats counts how many
// consecutive occurrences of the pattern sit at the tail (for period 1, the
// run length; for cycles, the number of full trailing blocks).
type loopDetection struct {
	Looped  bool
	Period  int
	Repeats int
	Tool    string
	LastN   int
}

// detectToolLoop inspects the recent tool-call signatures and reports whether
// they form a loop. Two triggers over the last `window` calls:
//
//  1. Consecutive: the last MinRepeats calls are identical. Identical calls
//     in a row with nothing in between change no state; the third identical
//     call (default) means the retry-after-failure allowance is spent.
//  2. Cycle: some period m (2..MaxPeriod) such that the tail of the window
//     consists of at least two identical consecutive blocks of m calls.
//     Because matching includes the normalized input, the legitimate
//     read→edit→read→verify pattern does not match — its edit inputs differ.
//
// Only the last `window` calls are examined; older history is ignored so a
// loop the model already escaped does not re-trigger.
func detectToolLoop(calls []toolCallSig, window, minRepeats, maxPeriod int) loopDetection {
	if window <= 0 {
		window = defaultLoopGuardWindow
	}
	if minRepeats <= 1 {
		minRepeats = defaultLoopGuardMinRepeats
	}
	if maxPeriod <= 0 {
		maxPeriod = defaultLoopGuardMaxPeriod
	}
	if len(calls) > window {
		calls = calls[len(calls)-window:]
	}
	n := len(calls)
	if n < 2 {
		return loopDetection{}
	}

	// Trigger 1: identical consecutive calls.
	last := calls[n-1]
	run := 1
	for i := n - 2; i >= 0 && calls[i] == last; i-- {
		run++
	}
	if run >= minRepeats {
		return loopDetection{Looped: true, Period: 1, Repeats: run, Tool: last.Name, LastN: n}
	}

	// Trigger 2: tail-aligned repeating cycle.
	maxP := window / 2
	if maxP > maxPeriod {
		maxP = maxPeriod
	}
	for m := 2; m <= maxP && 2*m <= n; m++ {
		k := 1 // trailing identical blocks of length m
		for (k+1)*m <= n && blocksEqual(calls, m, k+1) {
			k++
		}
		if k >= 2 {
			return loopDetection{
				Looped: true, Period: m, Repeats: k,
				Tool: calls[n-1].Name, LastN: n,
			}
		}
	}
	return loopDetection{}
}

// blocksEqual reports whether the last k*m calls of calls form k identical
// consecutive blocks of length m.
func blocksEqual(calls []toolCallSig, m, k int) bool {
	for block := 1; block < k; block++ {
		for i := 0; i < m; i++ {
			if calls[len(calls)-(block+1)*m+i] != calls[len(calls)-m+i] {
				return false
			}
		}
	}
	return true
}

// loopGuardMessage is the block reason surfaced to the model as the tool
// error when a loop is detected. Unlike the removed pool-level advisory
// injection, this is a denial: the call does not execute, so the message must
// both explain why and direct the model somewhere productive.
func loopGuardMessage(det loopDetection) string {
	var pattern string
	switch det.Period {
	case 1:
		pattern = fmt.Sprintf(
			"the identical %s call has now been made %d times in a row and was blocked", det.Tool, det.Repeats)
	default:
		pattern = fmt.Sprintf(
			"the last %d tool calls repeat a %d-call cycle ending in %s and the next one was blocked",
			det.Repeats*det.Period, det.Period, det.Tool)
	}
	return fmt.Sprintf(
		"[loop guard] %s. Identical calls produce identical results — repeating this pattern cannot make progress. Stop and reorient now: state the goal, what has been tried, and why it is failing. Then either take a materially different approach or end the turn with a summary of what you learned and what is blocked. If repeated calls are genuinely intentional (e.g. waiting on an external change), say so in words instead of calling the tool again.",
		pattern,
	)
}

// loopGuard holds per-agent call buffers and the active configuration. It is
// created at session start when the config loads; a nil guard means disabled.
type loopGuard struct {
	cfg     loopGuardConfig
	buffers map[string][]toolCallSig
}

// newLoopGuard builds a guard from a (defaults-applied) config.
func newLoopGuard(cfg loopGuardConfig) *loopGuard {
	return &loopGuard{cfg: cfg.applyDefaults(), buffers: make(map[string][]toolCallSig)}
}

// observe records a tool call for agentID and returns a detection when the
// recent pattern loops. On detection the call is rolled back out of the
// buffer: blocked calls do not consume window slots, so a model that keeps
// retrying keeps getting the same denial instead of pushing older calls out
// of the window and silently re-arming the pattern.
func (g *loopGuard) observe(agentID, toolName string, rawInput json.RawMessage) *loopDetection {
	if g == nil || !g.cfg.EnabledOrDefault() || !g.cfg.appliesTo(agentID) {
		return nil
	}
	buf := g.buffers[agentID]
	buf = append(buf, toolCallSig{Name: toolName, Input: normalizeLoopInput(string(rawInput))})
	det := detectToolLoop(buf, g.cfg.Window, g.cfg.MinRepeats, g.cfg.MaxPeriod)
	if det.Looped {
		g.buffers[agentID] = buf[:len(buf)-1]
		return &det
	}
	if len(buf) > g.cfg.Window {
		buf = buf[len(buf)-g.cfg.Window:]
	}
	g.buffers[agentID] = buf
	return nil
}
