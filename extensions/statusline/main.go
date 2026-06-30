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
//	  sl-model       (text)
//	  sl-sep2        (text, "  ")
//	  sl-tokens      (text, fg:muted)   — empty when tokens=0
//	  sl-sep3        (text, "  ")
//	  sl-working     (text, fg:accent)  — empty when idle
//	  sl-ctx         (text, fg:muted)   — appended when context window is configured
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
	modelID    = "sl-model"
	sep2ID     = "sl-sep2"
	tokensID   = "sl-tokens"
	sep3ID     = "sl-sep3"
	workingID  = "sl-working"
	ctxID      = "sl-ctx"
)

// ─── display state ───────────────────────────────────────────────────────────

var (
	lastProvider string
	lastModel    string
	lastTokens   int
	lastWorking  string // rendered working indicator text or ""
	lastCtx      string // ctx rem text or "" when no window configured
)

// ─── init ────────────────────────────────────────────────────────────────────

func init() {
	OnSessionStart(func() {
		info, _ := GetStatusInfo()
		lastProvider = info.Provider
		lastModel = info.Model
		lastTokens = info.Tokens
		patchAll()
	})

	// EventToken fires during streaming — update the working indicator.
	OnToken(func(_, _ string) {
		info, _ := GetStatusInfo()
		working := renderWorking(info)
		tokens := info.Tokens
		if working == lastWorking && tokens == lastTokens {
			return
		}
		lastWorking = working
		lastTokens = tokens
		patchAll()
	})

	// EventAfterProviderResponse fires when the LLM turn completes.
	OnAfterProviderResponse(func(_, _ int) {
		info, _ := GetStatusInfo()
		working := renderWorking(info)
		tokens := info.Tokens
		if working == lastWorking && tokens == lastTokens {
			return
		}
		lastWorking = working
		lastTokens = tokens
		patchAll()
	})

	// EventContextUsage fires after each completed turn.
	OnContextUsage(func(_, _ int64, ctxWindow int64, percent float64, _ bool) {
		desired := ""
		if ctxWindow > 0 {
			remaining := 80.0 - percent
			desired = fmt.Sprintf("  ctx:%+.0f%%", remaining)
		}
		if desired == lastCtx {
			return
		}
		lastCtx = desired
		patchAll()
	})

	// EventTick (1s) — refresh provider/model in case they change.
	OnTick(func() {
		info, _ := GetStatusInfo()
		if info.Provider == lastProvider && info.Model == lastModel {
			return
		}
		lastProvider = info.Provider
		lastModel = info.Model
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
		{ID: providerID, Type: "text", Text: lastProvider, Props: &muted},
		UIText(sep1ID, "  "),
		{ID: modelID, Type: "text", Text: lastModel},
		UIText(sep2ID, "  "),
		{ID: tokensID, Type: "text", Text: renderTokens(lastTokens), Props: &muted},
		UIText(sep3ID, "  "),
		{ID: workingID, Type: "text", Text: lastWorking, Props: &accent},
	}
	if lastCtx != "" {
		nodes = append(nodes, UINode{ID: ctxID, Type: "text", Text: lastCtx, Props: &muted})
	}
	UIPatch(areaID, OpSetRoot(UIHStack(rootID, nodes...)))
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

func renderTokens(n int) string {
	if n == 0 {
		return ""
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("tokens:%.1fm", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("tokens:%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("tokens:%d", n)
}

func formatElapsed(ms int64) string {
	s := ms / 1000
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%ds", s/60, s%60)
}

func main() {}
