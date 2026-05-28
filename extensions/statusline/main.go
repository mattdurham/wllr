//go:build wasip1

// Package main is the statusline extension for wllr.
// It renders a custom status line every second using get_status_info,
// only calling set_status_line when the content changes.
package main

import (
	"fmt"
	"strings"
)

var prev string

func init() {
	OnTick(func() {
		line := renderStatusLine(GetStatusInfo())
		if line == prev {
			return
		}
		prev = line
		SetStatusLine(line)
	})
}

func renderStatusLine(info StatusInfo) string {
	var parts []string

	// Provider and model
	if info.Provider != "" {
		parts = append(parts, info.Provider)
	}
	if info.Model != "" {
		parts = append(parts, info.Model)
	}

	// Token count
	if info.Tokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens:%s", formatTokens(info.Tokens)))
	}

	// Active sub-agents
	if info.ActiveAgents > 0 {
		parts = append(parts, fmt.Sprintf("agents:%d", info.ActiveAgents))
	}

	// Streaming indicator
	if info.HasError {
		parts = append(parts, "error")
	} else if info.Working {
		elapsed := info.ElapsedMs
		dots := "."
		if elapsed > 0 {
			phase := (elapsed / 400) % 3
			dots = strings.Repeat(".", int(phase)+1)
		}
		if elapsed >= 1000 {
			parts = append(parts, fmt.Sprintf("working%-3s %s", dots, formatElapsed(elapsed)))
		} else {
			parts = append(parts, fmt.Sprintf("working%s", dots))
		}
	}

	// Any custom statuses set by other extensions (excluding _override and stream)
	for _, k := range sortedKeys(info.Statuses) {
		if k == "_override" || k == "stream" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s", k, info.Statuses[k]))
	}

	return strings.Join(parts, "  ")
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatElapsed(ms int64) string {
	s := ms / 1000
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%ds", s/60, s%60)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort — map is small
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func main() {}
