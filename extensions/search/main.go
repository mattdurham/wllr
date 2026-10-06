//go:build wasip1

// main.go — wllr wiring for the search extension. All logic lives in
// search_logic.go (host-testable); this file only registers tools and
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
		"search",
		`Search the codebase with ripgrep and return ONE BOUNDED PAGE of matches plus a result_id for paging.

Prefer this over grep/rg/git-grep via exec:
- Output is bounded: default 50 matches (max 200), long lines clipped — no multi-MB dumps flooding your context.
- 'total' is the full match count, so you know up-front how much exists.
- More results come from search_page(result_id, offset) — served from a 5-minute cache with ZERO re-execution. Never re-run the same search with a bigger head/limit.
- Identical searches within 5 minutes are served from cache (cached: true).
- No-match returns an empty list, not a shell exit-code error.

Pattern is a regex by default (set literal: true for plain text). path is relative to the project root; '..' and paths outside the project are rejected. Lines longer than 1000 chars (minified/generated) are skipped by ripgrep; per-file matches cap at 2000. If 'complete' is false the result set exceeded the 25MB stream cap — narrow with path/glob.`,
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern":        {"type": "string", "description": "Regex (ripgrep syntax) or literal text when literal=true"},
				"path":           {"type": "string", "description": "File or directory relative to the project root (default \".\")"},
				"glob":           {"type": "string", "description": "ripgrep glob filter, e.g. \"*.go\" or \"!*_test.go\""},
				"literal":        {"type": "boolean", "description": "Treat pattern as literal text", "default": false},
				"case_sensitive": {"type": "boolean", "default": true},
				"context":        {"type": "integer", "description": "Context lines around each match, 0-5", "default": 0},
				"page_size":      {"type": "integer", "description": "Matches returned now, 1-200", "default": 50},
				"refresh":        {"type": "boolean", "description": "Bypass the cache and re-run", "default": false}
			},
			"required": ["pattern"]
		}`),
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"result_id":    {"type": "string", "description": "Pass to search_page for more matches"},
				"matches":      {"type": "array", "items": {"type": "object", "properties": {"path": {"type": "string"}, "line": {"type": "integer"}, "text": {"type": "string"}, "before": {"type": "array", "items": {"type": "string"}}, "after": {"type": "array", "items": {"type": "string"}}}}},
				"total":        {"type": "integer", "description": "Full match count"},
				"returned":     {"type": "integer"},
				"next_offset":  {"type": ["integer", "null"], "description": "Pass as offset to search_page; null when exhausted"},
				"complete":     {"type": "boolean", "description": "false if the 25MB stream cap truncated the result set"},
				"cached":       {"type": "boolean"},
				"stale":        {"type": "boolean", "description": "git HEAD changed since the search ran"},
				"expires_in_s": {"type": "integer"},
				"elapsed_ms":   {"type": "integer"}
			}
		}`),
	)

	RegisterToolWithOutput(
		"search_page",
		`Return the next window of matches for a previous search result. Pass result_id from search and an offset (use the prior response's next_offset). Served from a 5-minute cache — no re-execution of the search. Errors with result_not_found if expired: re-run search. 'stale: true' means the git HEAD changed since the search ran; re-run with refresh if you need current results.`,
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"result_id": {"type": "string", "description": "result_id returned by search"},
				"offset":    {"type": "integer", "description": "0-based match offset (use prior next_offset)", "default": 0},
				"limit":     {"type": "integer", "description": "Matches to return, 1-200", "default": 50}
			},
			"required": ["result_id"]
		}`),
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"result_id":    {"type": "string"},
				"matches":      {"type": "array"},
				"total":        {"type": "integer"},
				"returned":     {"type": "integer"},
				"next_offset":  {"type": ["integer", "null"]},
				"complete":     {"type": "boolean"},
				"stale":        {"type": "boolean"},
				"expires_in_s": {"type": "integer"}
			}
		}`),
	)

	OnToolCall(func(callID, toolName string, input json.RawMessage) (string, bool) {
		switch toolName {
		case "search":
			return runSearch(input)
		case "search_page":
			return runSearchPage(input)
		}
		return "", false
	})
}

func main() {}
