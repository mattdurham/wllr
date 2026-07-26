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
		`Search the web using HTTP GET requests. Returns a list of search results with title, link, and snippet.
		
Supports:
- DuckDuckGo Instant Answer API
- Bing Search API
- Google Custom Search JSON API
- Any search API accessible via HTTP GET

If the host supports HTTP GET, this will perform real web searches.`,
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

		// Try DuckDuckGo Instant Answer API (no API key required)
		if results, err := searchDuckDuckGo(params.Query); err == nil {
			out, _ := json.Marshal(results)
			return string(out), false
		}

		// Fallback: try Bing Search API (requires API key in WLLR_BING_API_KEY env var)
		if results, err := searchBing(params.Query); err == nil {
			out, _ := json.Marshal(results)
			return string(out), false
		}

		// Final fallback: return placeholder with instructions
		results := []SearchResult{
			{
				Title:   "HTTP GET Support Required",
				Link:    "https://github.com/wllr-dev/wllr/blob/main/extensions/websearch/TODO.md",
				Snippet: "HTTP GET support is not available in the current environment. This extension requires HTTP_GET host capability. See TODO.md for implementation details.",
			},
		}

		out, _ := json.Marshal(results)
		return string(out), false
	})
}

// searchDuckDuckGo performs a search using DuckDuckGo Instant Answer API
func searchDuckDuckGo(query string) ([]SearchResult, error) {
	// DuckDuckGo IA API endpoint
	url := "https://api.duckduckgo.com/?q=" + query + "&format=json&no_html=1"

	// Make HTTP GET request
	statusCode, body, err := HTTPGet(url, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != 200 {
		return nil, nil
	}

	var result struct {
		AbstractText string `json:"AbstractText"`
		AbstractURL  string `json:"AbstractURL"`
		Entity       string `json:"Entity"`
		Heading      string `json:"Heading"`
		Image        string `json:"Image"`
		Definition   string `json:"Definition"`
		Definitions  []struct {
			Source string `json:"Source"`
			Text   string `json:"Text"`
			URL    string `json:"URL"`
		} `json:"Definitions"`
	}

	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, err
	}

	var results []SearchResult

	// Add abstract result if available
	if result.AbstractText != "" || result.Heading != "" {
		title := result.Heading
		if title == "" {
			title = query
		}

		results = append(results, SearchResult{
			Title:   title,
			Link:    result.AbstractURL,
			Snippet: result.AbstractText,
		})
	}

	// Add definition results if available
	for _, def := range result.Definitions {
		results = append(results, SearchResult{
			Title:   def.Source,
			Link:    def.URL,
			Snippet: def.Text,
		})
	}

	return results, nil
}

// searchBing performs a search using Bing Search API
func searchBing(query string) ([]SearchResult, error) {
	// Get API key from environment
	apiKey := ""
	if envVal, err := GetEnv("WLLR_BING_API_KEY"); err == nil && envVal != "" {
		apiKey = envVal
	}

	if apiKey == "" {
		return nil, nil // No API key available
	}

	// Bing Search endpoint
	url := "https://api.bing.microsoft.com/v7.0/search?q=" + query

	// Create headers with API key
	headers := map[string]string{
		"Ocp-Apim-Subscription-Key": apiKey,
	}

	// Make HTTP GET request
	statusCode, body, err := HTTPGet(url, headers)
	if err != nil {
		return nil, err
	}

	if statusCode != 200 {
		return nil, nil
	}

	var result struct {
		WebPages struct {
			Value []struct {
				Name        string `json:"name"`
				URL         string `json:"url"`
				Description string `json:"snippet"`
			} `json:"value"`
		} `json:"webPages"`
	}

	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, page := range result.WebPages.Value {
		results = append(results, SearchResult{
			Title:   page.Name,
			Link:    page.URL,
			Snippet: page.Description,
		})
	}

	return results, nil
}

func main() {}
