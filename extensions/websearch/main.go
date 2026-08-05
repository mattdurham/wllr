//go:build wasip1

// Package main is the websearch extension for wllr.
// It provides web search capabilities through host HTTP requests
package main

import (
	"encoding/json"
	"fmt"
)

// ─── SDK initialization ───────────────────────────────────────────────────────

func init() {
	RegisterToolWithOutput(
		"web_search",
		`Search DuckDuckGo's HTML web results without an API key. Returns ranked results with title, link, and snippet.`,
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

		// DuckDuckGo's HTML endpoint returns normal ranked web results without an API key.
		if results, err := searchDuckDuckGo(params.Query); err == nil {
			out, _ := json.Marshal(results)
			return string(out), false
		}

		return "web_search: DuckDuckGo returned no usable search results", true
	})
}

// searchDuckDuckGo performs a search using DuckDuckGo's HTML results page.
func searchDuckDuckGo(query string) ([]SearchResult, error) {
	statusCode, body, err := HTTPGet(duckDuckGoHTMLURL(query), map[string]string{
		"User-Agent": "wllr-websearch/1.0",
	})
	if err != nil {
		return nil, err
	}

	if statusCode != 200 {
		return nil, fmt.Errorf("duckduckgo: unexpected HTTP status %d", statusCode)
	}
	results := parseDuckDuckGoHTML(body)
	if len(results) == 0 {
		return nil, fmt.Errorf("duckduckgo: no search results")
	}
	return results, nil
}

func main() {}
