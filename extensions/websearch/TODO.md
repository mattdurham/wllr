# WebSearch Extension Implementation Plan

## Current Status

The websearch extension is implemented but currently returns placeholder results because HTTP GET support is not available in the wllr host.

## What's Missing

The wllr host needs to support the `http_get` method in addition to `http_post`. This would allow the extension to make actual HTTP requests to search APIs.

## When HTTP GET is Available

The implementation will work as follows:

### 1. SDK Update Required

The `wllrsdk.go` file will need to include the `HTTPGet` function:

```go
// HTTPGet makes an HTTP GET request via the host.
// headers may be nil. Returns (statusCode, responseBody, error).
// Requires the network_read permission in the extension manifest.
func HTTPGet(url string, headers map[string]string) (int, string, error) {
    raw := _sdkCallResult("http_get", map[string]any{"url": url, "headers": headers})
    if raw == nil {
        return 0, "", fmt.Errorf("http_get: no response")
    }
    var r struct {
        Status int    `json:"status"`
        Body   string `json:"body"`
    }
    if err := json.Unmarshal(raw, &r); err != nil {
        return 0, "", err
    }
    return r.Status, r.Body, nil
}
```

### 2. Manifest Update Required

The extension manifest (`websearch.json`) will need to declare `network_read` permission:

```json
{
    "permissions": ["network_read"]
}
```

### 3. Implementation Changes

When host supports `http_get`, replace the placeholder logic in `main.go` with:

```go
// searchDuckDuckGo performs a search using DuckDuckGo Instant Answer API
func searchDuckDuckGo(query string) ([]SearchResult, error) {
    url := "https://api.duckduckgo.com/?q=" + query + "&format=json&no_html=1"
    
    // Make HTTP GET request (when host supports it)
    statusCode, body, err := HTTPGet(url, nil)
    if err != nil {
        return nil, err
    }
    
    // ... parse response and return results ...
}
```

## Search API Options

The extension supports multiple search APIs:

### 1. DuckDuckGo Instant Answer API (No API Key)
- Endpoint: `https://api.duckduckgo.com/?q={query}&format=json&no_html=1`
- Pros: No API key required, free
- Cons: Limited results, may rate-limit

### 2. Bing Search API (Requires API Key)
- Endpoint: `https://api.bing.microsoft.com/v7.0/search`
- Environment Variable: `WLLR_BING_API_KEY`
- Pros: High quality results, reliable
- Cons: Requires API key from Azure

### 3. Google Custom Search JSON API (Requires API Key)
- Endpoint: `https://www.googleapis.com/customsearch/v1`
- Environment Variable: `WLLR_GOOGLE_API_KEY`
- Requires: Custom Search Engine ID
- Pros: Most comprehensive results
- Cons: Free tier limited to 100 searches/day

### 4. SearXNG Instance (Self-hosted)
- Endpoint: `https://searx.example.com/search`
- Pros: Privacy-focused, no API key
- Cons: Requires access to self-hosted instance

## Future Enhancements

1. **Caching**: Implement result caching with configurable TTL
2. **Multiple Results**: Support pagination for more than 3-5 results
3. **Filters**: Add options to filter by time range, site, etc.
4. **Language Support**: Add language selection for search results
5. **Safe Search**: Add safe search toggle

## Testing

Once HTTP GET is available, test with:

```bash
# Test DuckDuckGo search
echo '{"tool_call_id":"test-1","tool_name":"web_search","input":{"query":"wllr websearch"}}' | wllr --tool web_search

# Test with Bing API (if key is set)
export WLLR_BING_API_KEY=your-key-here
echo '{"tool_call_id":"test-2","tool_name":"web_search","input":{"query":"wllr permissions"}}' | wllr --tool web_search
```

## Implementation Checklist

- [ ] Add `HTTPGet` function to SDK
- [ ] Update manifest with `network_read` permission
- [ ] Replace placeholder logic with HTTP GET calls
- [ ] Test with DuckDuckGo API (no key required)
- [ ] Test with Bing API (requires key)
- [ ] Add error handling for rate limiting
- [ ] Update documentation
- [ ] Add integration tests
