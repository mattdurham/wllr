//go:build wasip1

// Package main is the statusline extension for wllr.
//
// It drives the "statusline" scene area (pre-created by the harness) using the
// ui_patch scene graph API. The area holds a stable hstack tree; the whole root
// is re-patched whenever any displayed value changes (the tree has ~8 leaf nodes
// so full re-renders are negligible).
//
// Default node tree:
//
//	statusline-root  (hstack)
//	  sl-provider    (text, fg:muted)
//	  sl-sep1        (text, "  ")
//	  sl-cwd         (text, fg:muted)   — working directory; "~/"-collapsed against $HOME
//	  sl-sep2        (text, "  ")
//	  sl-model       (text)
//	  sl-sep3        (text, "  ")
//	  sl-agent       (text, fg:muted)   — "agent:<id>"; "agent:main" for the root
//	  sl-sep4        (text, "  ")
//	  sl-working     (text, fg:accent)  — empty when idle
//	  sl-tps         (text, fg:muted)   — "87 t/s"; omitted until the host reports a rate
//	  sl-ctx         (text, fg:muted)   — "ctx:P%/R%" when a context window is configured
//	  sl-compact     (text, fg:muted)   — "C<n>" after the first successful compaction
//
// Other extensions can insert additional nodes into "statusline-root" via
// ui_patch insert ops. Because this extension uses set_root to update the whole
// tree, injected nodes will be lost on the next patchAll(). A future refinement
// (OpUpdate per node) would preserve injected nodes; for now set_root is simpler
// and correct for the default case.
package main

import (
	"fmt"
	"strings"
)

// ─── node IDs ────────────────────────────────────────────────────────────────

const (
	areaID     = "statusline"
	rootID     = "statusline-root"
	providerID = "sl-provider"
	sep1ID     = "sl-sep1"
	cwdID      = "sl-cwd"
	sep2ID     = "sl-sep2"
	modelID    = "sl-model"
	sep3ID     = "sl-sep3"
	agentID    = "sl-agent"
	sep4ID     = "sl-sep4"
	workingID  = "sl-working"
	tpsID      = "sl-tps"
	ctxID      = "sl-ctx"
	compactID  = "sl-compact"
)

// ─── display state ───────────────────────────────────────────────────────────

var (
	lastProvider    string
	lastModel       string
	lastCwd         string // rendered cwd segment or "" when host_info fails
	lastAgent       string // focused-agent segment text
	lastWorking     string // rendered working indicator text or ""
	lastTps         string // generation-speed segment text or "" when hidden
	lastCtx         string // ctx text or "" when no window configured
	lastCompactions int    // cumulative successful compactions this session
)

// ─── init ────────────────────────────────────────────────────────────────────

func init() {
	OnSessionStart(func() {
		info, _ := GetStatusInfo()
		lastProvider = info.Provider
		lastModel = info.Model
		// The cwd is fetched once: it is the host working directory at
		// session start (the sandbox has no os.Getwd) and does not change.
		lastCwd = renderCwd()
		syncDynamicStatus(info)
		patchAll()
	})

	// EventToken fires during streaming — update the working indicator.
	OnToken(func(_, _ string) {
		info, _ := GetStatusInfo()
		if !syncDynamicStatus(info) {
			return
		}
		patchAll()
	})

	// EventAfterProviderResponse fires when the LLM turn completes.
	OnAfterProviderResponse(func(_, _ int) {
		info, _ := GetStatusInfo()
		if !syncDynamicStatus(info) {
			return
		}
		patchAll()
	})

	// EventContextUsage fires after each completed turn (main agent only).
	OnContextUsage(func(inputTokens, _ int64, ctxWindow int64, _ float64, _ bool, _ float64, compactions int) {
		desired := ""
		if ctxWindow > 0 {
			if inputTokens < 0 {
				inputTokens = 0
			}
			desired = fmt.Sprintf("  ctx:%d/%d", inputTokens, ctxWindow)
		}
		changed := desired != lastCtx || compactions != lastCompactions
		lastCtx = desired
		lastCompactions = compactions
		if !changed {
			return
		}
		patchAll()
	})

	OnTick(func() {
		info, _ := GetStatusInfo()
		if !syncDynamicStatus(info) {
			return
		}
		patchAll()
	})

	OnModelChanged(func(provider, model string) {
		info, _ := GetStatusInfo()
		changed := syncDynamicStatus(info)
		if provider == lastProvider && model == lastModel && !changed {
			return
		}
		lastProvider = provider
		lastModel = model
		patchAll()
	})
}

// ─── rendering ───────────────────────────────────────────────────────────────

// patchAll re-renders the full root hstack. The tree is tiny so a full set_root
// is simpler and cheaper than tracking per-node positions.
func patchAll() {
	muted := UIProps{Fg: "muted"}
	accent := UIProps{Fg: "accent"}

	nodes := []UINode{
		{ID: providerID, Type: "text", Text: ">> " + providerLabel(lastProvider), Props: &muted},
		UIText(sep1ID, "  "),
		{ID: cwdID, Type: "text", Text: lastCwd, Props: &muted},
		UIText(sep2ID, "  "),
		{ID: modelID, Type: "text", Text: modelLabel(lastModel)},
		UIText(sep3ID, "  "),
		{ID: agentID, Type: "text", Text: lastAgent, Props: &muted},
		UIText(sep4ID, "  "),
		{ID: workingID, Type: "text", Text: lastWorking, Props: &accent},
	}
	if lastTps != "" {
		nodes = append(nodes, UINode{ID: tpsID, Type: "text", Text: lastTps, Props: &muted})
	}
	if lastCtx != "" {
		nodes = append(nodes, UINode{ID: ctxID, Type: "text", Text: lastCtx, Props: &muted})
	}
	if lastCompactions > 0 {
		nodes = append(
			nodes,
			UINode{ID: compactID, Type: "text", Text: renderCompactions(lastCompactions), Props: &muted},
		)
	}
	UIPatch(areaID, OpSetRoot(UIHStack(rootID, nodes...)))
}

func providerLabel(provider string) string {
	switch provider {
	case "openai":
		return "ChatGPT"
	case "anthropic":
		return "Claude"
	case "gemini":
		return "Gemini"
	case "local":
		return "Local"
	case "":
		return "Provider"
	default:
		return provider
	}
}

func modelLabel(model string) string {
	if model == "" {
		return "select model"
	}
	return model
}

// renderCwd renders the working-directory segment as the host-reported cwd
// (the WASM sandbox's own os.Getwd is always "/") with the user's home prefix
// collapsed to "~/" when it matches. An empty string hides the segment when
// host_info is unavailable, so a host predating the method renders exactly as
// before this segment existed.
func renderCwd() string {
	cwd, _, err := HostInfo()
	if err != nil || cwd == "" {
		return ""
	}
	if home, herr := GetEnv("HOME"); herr == nil && home != "" {
		if cwd == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(cwd, home+"/"); ok {
			return "~/" + rest
		}
	}
	return cwd
}

func renderWorking(info StatusInfo) string {
	if info.HasError {
		return "error"
	}
	if !info.Working {
		return ""
	}
	elapsed := info.ElapsedMs
	phase := (elapsed / 400) % 3
	dots := strings.Repeat(".", int(phase)+1)
	if elapsed >= 1000 {
		return fmt.Sprintf("working%-3s %s", dots, formatElapsed(elapsed))
	}
	return fmt.Sprintf("working%s", dots)
}

func syncDynamicStatus(info StatusInfo) bool {
	working := renderWorking(info)
	tps := renderTps(info)
	ctx := renderContext(info)
	agent := renderAgent(info)
	if working == lastWorking && tps == lastTps && ctx == lastCtx && agent == lastAgent {
		return false
	}
	lastWorking = working
	lastTps = tps
	lastCtx = ctx
	lastAgent = agent
	return true
}

func renderContext(info StatusInfo) string {
	if info.Statuses == nil {
		return ""
	}
	// Prefer the new "ctx" key ("used/max" context tokens); fall back to
	// the legacy "ctx rem" key (maximum only) for hosts built before the ctx
	// key existed.
	value := strings.TrimSpace(info.Statuses["ctx"])
	if value == "" {
		remaining := strings.TrimSpace(info.Statuses["ctx rem"])
		if remaining == "" {
			return ""
		}
		return "  ctx:" + remaining
	}
	return "  ctx:" + value
}

// renderTps renders the generation-speed segment ("  87 t/s"). Hidden while
// the host reports no measurable rate — before the first token of a turn,
// during tool execution and sub-agent waits, after a failed turn, and for
// streams too short to divide meaningfully (the agent suppresses rates whose
// streamed span is under 100ms). The unit string comes from the host; this
// side only adds the two-space separator, matching how the ctx segment is
// composed.
func renderTps(info StatusInfo) string {
	if info.Statuses == nil {
		return ""
	}
	value := strings.TrimSpace(info.Statuses["tps"])
	if value == "" {
		return ""
	}
	return "  " + value
}

// rootAgentLabel is the segment text shown when the root agent has focus. An
// empty focused-agent status means the root, which is the first node in the
// tree rather than a special case — the same convention the bundled agents
// extension uses when it labels the transcript's owner.
const rootAgentLabel = "main"

// renderAgent renders the focused-agent segment ("agent:<id>"). An absent or
// empty "agent" status means the root agent, so the segment never renders
// empty.
func renderAgent(info StatusInfo) string {
	id := ""
	if info.Statuses != nil {
		id = strings.TrimSpace(info.Statuses["agent"])
	}
	if id == "" {
		id = rootAgentLabel
	}
	return "agent:" + id
}

// renderCompactions renders the session's successful-compaction count ("C<n>").
func renderCompactions(n int) string {
	return fmt.Sprintf("  C%d", n)
}

func formatElapsed(ms int64) string {
	s := ms / 1000
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%ds", s/60, s%60)
}

func main() {}
