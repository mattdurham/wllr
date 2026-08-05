# WebSearch Extension Notes

The extension fetches DuckDuckGo's HTML results through the host `http_get`
capability and parses up to ten ranked results. It requires `network_read` in
the extension manifest.

DuckDuckGo can return an anti-bot challenge instead of results. The tool treats
that as an unavailable search response and uses wllr's existing fallback.

## Follow-up options

- Add a configurable SearXNG provider for users who operate a private instance.
- Add provider-specific rate limiting and caching.
- Consider a separate page-fetching tool; search results and page content have
  different security and size-limit requirements.
