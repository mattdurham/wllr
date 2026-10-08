package main

// tooltimeout.go — startup wiring for the tool-call watchdog duration.
// Read from the wllr config group: tool_call_timeout, a Go duration string
// ("90s", "5m", "1h") or a plain integer (milliseconds). Unset or invalid
// values keep the tools-package default (10m).

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mattdurham/wllr/modules/tools"
)

// applyToolCallTimeoutConfig reads tool_call_timeout from the wllr config
// group and applies it to the tools package watchdog. Duration strings and
// bare millisecond integers are both accepted; a zero/negative value restores
// the default (so `tool_call_timeout: 0` in config means "use the default",
// matching SetToolCallTimeout's contract).
func applyToolCallTimeoutConfig() {
	raw, err := loadConfigGroup(wllrConfigGroup)
	if err != nil {
		return // no config or unreadable: keep the default
	}
	var group struct {
		ToolCallTimeout any `json:"tool_call_timeout"`
	}
	if json.Unmarshal(raw, &group) != nil || group.ToolCallTimeout == nil {
		return
	}
	d, perr := parseToolCallTimeout(group.ToolCallTimeout)
	if perr != nil {
		slog.Warn("wllr: invalid tool_call_timeout; using default",
			"value", fmt.Sprint(group.ToolCallTimeout), "error", perr)
		return
	}
	tools.SetToolCallTimeout(d)
}

// parseToolCallTimeout accepts "90s"/"5m"/"1h" (Go duration) or a bare number
// interpreted as milliseconds. Zero/negative passes through: SetToolCallTimeout
// maps it to the default.
func parseToolCallTimeout(v any) (time.Duration, error) {
	switch t := v.(type) {
	case float64: // encoding/json numbers
		return time.Duration(t) * time.Millisecond, nil
	case int: // yaml-decoded integers (config not round-tripped through JSON)
		return time.Duration(t) * time.Millisecond, nil
	case string:
		if ms, serr := strconv.Atoi(t); serr == nil {
			return time.Duration(ms) * time.Millisecond, nil
		}
		if d, derr := time.ParseDuration(t); derr == nil {
			return d, nil
		}
		return 0, fmt.Errorf("not a duration or millisecond value: %q", t)
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}
