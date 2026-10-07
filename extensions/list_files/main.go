//go:build wasip1

// main.go — wllr wiring for the list_files extension. All logic lives in
// list_files_logic.go (host-testable); this file only registers tools and
// connects the SDK's Exec/env to the injectable dependencies.
package main

import (
	"encoding/json"
	"os"
)

func init() {
	execFn = Exec
	getenvFn = os.Getenv

	RegisterToolWithOutput(
		"list_files",
		`List files under a directory, gitignore-aware and bounded, returning ONE PAGE plus a result_id for paging.

Prefer this over find/ls via exec:
- Output is bounded: default 200 entries (max 1000) with an explicit truncated/complete signal — no silent | head -N guesswork.
- Respects .gitignore and skips hidden files by default (pass hidden:true to include them).
- 'total' is the full entry count, so you know up-front how much exists.
- More entries come from list_files_page(result_id, offset) — served from a 5-minute cache with ZERO re-walk. Never re-run the same listing with a deeper head/limit.
- Identical listings within 5 minutes are served from cache (cached: true).
- No-match returns an empty list, not a shell exit-code error.

path is relative to the project root; '..' and paths outside the project are rejected. type selects files (default), dirs, or both — directories are returned with a trailing "/". glob filters with ripgrep glob syntax (e.g. "*.go", "!*_test.go"). max_depth limits recursion.`,
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"path":      {"type": "string", "description": "Directory relative to the project root (default \".\")"},
				"glob":      {"type": "string", "description": "ripgrep glob filter, e.g. \"*.go\" or \"!*_test.go\""},
				"type":      {"type": "string", "enum": ["files", "dirs", "both"], "description": "Entries to return; directories carry a trailing \"/\"", "default": "files"},
				"max_depth": {"type": "integer", "description": "Maximum directory recursion depth, 0 = unlimited, max 100", "default": 0},
				"hidden":    {"type": "boolean", "description": "Include hidden files/dirs (default false)", "default": false},
				"page_size": {"type": "integer", "description": "Entries returned now, 1-1000", "default": 200},
				"refresh":   {"type": "boolean", "description": "Bypass the cache and re-walk", "default": false}
			}
		}`),
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"result_id":    {"type": "string", "description": "Pass to list_files_page for more entries"},
				"files":        {"type": "array", "items": {"type": "string"}, "description": "Relative paths; directories end with \"/\""},
				"total":        {"type": "integer", "description": "Full entry count"},
				"returned":     {"type": "integer"},
				"next_offset":  {"type": ["integer", "null"], "description": "Pass as offset to list_files_page; null when exhausted"},
				"truncated":    {"type": "boolean", "description": "true when the 25MB stream cap cut the listing"},
				"complete":     {"type": "boolean", "description": "false when the listing was truncated"},
				"cached":       {"type": "boolean"},
				"stale":        {"type": "boolean", "description": "git HEAD changed since the listing ran"},
				"expires_in_s": {"type": "integer"},
				"elapsed_ms":   {"type": "integer"}
			}
		}`),
	)

	RegisterToolWithOutput(
		"list_files_page",
		`Return the next window of entries for a previous list_files result. Pass result_id from list_files and an offset (use the prior response's next_offset). Served from a 5-minute cache — no re-walk. Errors with result_not_found if expired: re-run list_files. 'stale: true' means the git HEAD changed since the listing ran; re-run with refresh if you need current results.`,
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"result_id": {"type": "string", "description": "result_id returned by list_files"},
				"offset":    {"type": "integer", "description": "0-based entry offset (use prior next_offset)", "default": 0},
				"limit":     {"type": "integer", "description": "Entries to return, 1-1000", "default": 200}
			},
			"required": ["result_id"]
		}`),
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"result_id":    {"type": "string"},
				"files":        {"type": "array", "items": {"type": "string"}},
				"total":        {"type": "integer"},
				"returned":     {"type": "integer"},
				"next_offset":  {"type": ["integer", "null"]},
				"truncated":    {"type": "boolean"},
				"complete":     {"type": "boolean"},
				"stale":        {"type": "boolean"},
				"expires_in_s": {"type": "integer"}
			}
		}`),
	)

	OnToolCall(func(callID, toolName string, input json.RawMessage) (string, bool) {
		switch toolName {
		case "list_files":
			return runListFiles(input)
		case "list_files_page":
			return runListFilesPage(input)
		}
		return "", false
	})
}

func main() {}
