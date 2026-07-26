//go:build demo

package main

import (
	"encoding/json"
	"fmt"
)

// This file demonstrates how the websearch extension would work
// with HTTP GET support from the host.
//
// To use: go build -tags=demo
// This is separated into a conditional file to avoid breaking
// builds when HTTP GET is not available.

// SearchResult represents a single search result from DuckDuckGo API
type DDGResult struct {
	AbstractText  string `json:"AbstractText"`
	Heading       string `json:"Heading"`
	RelatedTopics []struct {
		Text  string `json:"Text"`
		URL   string `json:"URL"`
		Image string `json:"Image,omitempty"`
	} `json:"RelatedTopics"`
	URL string `json:"Redirect,omitempty"`
}

// performWebSearchWithHTTP would make real HTTP requests when available
func performWebSearchWithHTTP(query string) SearchResponse {
	// Example 1: DuckDuckGo Instant Answer API (no key required)
	// url := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json", escapeQuery(query))

	// Example 2: Bing Search API (requires API key)
	// url := fmt.Sprintf("https://api.bing.microsoft.com/v7.0/search?q=%s", escapeQuery(query))
	// headers := map[string]string{
	//     "Ocp-Apim-Subscription-Key": "<your-api-key>",
	// }

	// Example 3: Google Custom Search JSON API (requires API key + CX)
	// url := fmt.Sprintf("https://www.googleapis.com/customsearch/v1?key=<api-key>&cx=<cx-id>&q=%s", escapeQuery(query))

	// Example 4: SearXNG instance (self-hosted, no key required)
	// url := fmt.Sprintf("https://searx.example.com/search?q=%s&format=json", escapeQuery(query))

	// With HTTP GET support, the implementation would be:
	return SearchResponse{
		Error: "HTTP GET not yet supported - see example_http_get.go for implementation plan",
	}
}

// Example API response parsing functions

func parseDuckDuckGoResponse(data []byte) ([]SearchResult, error) {
	var apiResp DDGResult
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	var results []SearchResult

	// Add abstract as first result
	if apiResp.AbstractText != "" {
		results = append(results, SearchResult{
			Title:       apiResp.Heading,
			Link:        fmt.Sprintf("https://duckduckgo.com/%s", escapeQuery(apiResp.Heading)),
			Description: apiResp.AbstractText,
		})
	}

	// Add related topics
	for _, topic := range apiResp.RelatedTopics {
		results = append(results, SearchResult{
			Title:       topic.Text,
			Link:        topic.URL,
			Description: topic.Text,
		})
	}

	// Add redirect URL if available
	if apiResp.URL != "" && len(results) == 0 {
		results = append(results, SearchResult{
			Title:       apiResp.Heading,
			Link:        apiResp.URL,
			Description: "Direct link to resource",
		})
	}

	return results, nil
}

func parseBingResponse(data []byte) ([]SearchResult, error) {
	// Bing response structure is different
	// This is a placeholder for the actual implementation
	return []SearchResult{
		{
			Title:       "Bing Search Results",
			Link:        "https://www.bing.com/search?q=demo",
			Description: "Bing search results would be parsed here",
		},
	}, nil
}

func parseGoogleResponse(data []byte) ([]SearchResult, error) {
	// Google response structure is different
	var resp struct {
		Items []struct {
			Title     string `json:"title"`
			Link      string `json:"link"`
			_snippet_ string `json:"snippet"`
		} `json:"items"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	var results []SearchResult
	for _, item := range resp.Items {
		results = append(results, SearchResult{
			Title:       item.Title,
			Link:        item.Link,
			Description: item._snippet_,
		})
	}

	return results, nil
}

// Example usage in main.go when HTTP GET becomes available:

/*
func performWebSearch(query string) SearchResponse {
	if query == "" {
		return SearchResponse{Error: "query is required"}
	}

	// Use DuckDuckGo API for demo purposes
	url := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json", escapeQuery(query))

	// When host supports http_get:
	response, err := sdk.HTTPGet(url)
	if err != nil {
		return SearchResponse{Error: fmt.Sprintf("request failed: %v", err)}
	}

	results, err := parseDuckDuckGoResponse(response)
	if err != nil {
		return SearchResponse{Error: err.Error()}
	}

	return SearchResponse{Results: results}
}
*/
