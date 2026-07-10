//go:build wasip1

// Package main is the websearch extension for wllr.
// It provides web search capabilities through host HTTP requests
package main

import (
	"encoding/json"
)

// ─── Search result structures ─────────────────────────────────────────────────

type SearchResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

// ─── SDK initialization ───────────────────────────────────────────────────────

func init() {
	RegisterToolWithOutput(
		"web_search",
		`Search the web using DuckDuckGo Lite. Returns a list of search results with title, link, and snippet.
		
NOTE: This tool requires HTTP GET support from the host. Currently returns a placeholder.`,
		json.RawMessage(
			`{"type":"object","properties":{"query":{"type":"string","description":"The search query to execute"}},"required":["query"]}`,
		),
		json.RawMessage(
			`{"type":"array","description":"Array of search results","items":{"type":"object","properties":{"title":{"type":"string"},"link":{"type":"string"},"snippet":{"type":"string"}}}}`,
		),
	)

	OnToolCall(func(callID, toolName string, input json.RawMessage) (string, bool) {
		if toolName != "web_search" {
			return "", false
		}

		var params struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(input, &params); err != nil || params.Query == "" {
			return "web_search: query is required", true
		}

		results := []SearchResult{
			{
				Title:   "Placeholder Result",
				Link:    "https://example.com/search?q=" + params.Query,
				Snippet: "HTTP GET not supported in current TinyGo environment. This extension needs host HTTP support.",
			},
		}

		out, _ := json.Marshal(results)
		return string(out), false
	})
}

func main() {}
