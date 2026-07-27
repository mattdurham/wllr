# WebSearch Extension for wllr

This extension provides web search capabilities through HTTP requests to various search APIs.

## Features

- Search the web using multiple search APIs
- Support for DuckDuckGo Instant Answer API (no API key required)
- Support for Bing Search API (requires Azure API key)
- Structured search results with title, link, and snippet
- Environment variable support for API keys

## Current Status

**HTTP GET Support Required**

The extension is fully implemented but currently cannot make actual HTTP requests because the wllr host doesn't yet support `http_get` host calls.

When HTTP GET is available in the wllr host, this extension will:
1. Attempt to use DuckDuckGo Instant Answer API (no API key)
2. Fall back to Bing Search API if API key is set
3. Return helpful placeholder messages if neither is available

## Usage

### With DuckDuckGo (No API Key Required)

The extension will automatically use the DuckDuckGo Instant Answer API when no API key is set.

### With Bing Search (Requires API Key)

To enable Bing search, set the `WLLR_BING_API_KEY` environment variable:

```bash
export WLLR_BING_API_KEY=your_bing_api_key_here
```

## API Reference

### Tool: `web_search`

Searches the web and returns results.

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query to execute"
    }
  },
  "required": ["query"]
}
```

**Output Schema:**
```json
{
  "type": "array",
  "description": "Array of search results",
  "items": {
    "type": "object",
    "properties": {
      "title": {"type": "string"},
      "link": {"type": "string"},
      "snippet": {"type": "string"}
    }
  }
}
```

### HTTP Functions

The extension provides two HTTP functions:

#### `HTTPGet(url string, headers map[string]string) (int, string, error)`

Makes an HTTP GET request via the host.

**Parameters:**
- `url`: The URL to fetch
- `headers`: Optional HTTP headers (can be nil)

**Returns:**
- Status code (int)
- Response body (string)
- Error (error)

**Requires:** `network_read` permission

#### `HTTPPost(url string, headers map[string]string, body []byte) (int, string, error)`

Makes an HTTP POST request via the host.

**Parameters:**
- `url`: The URL to send the request to
- `headers`: Optional HTTP headers (can be nil)
- `body`: Request body bytes

**Returns:**
- Status code (int)
- Response body (string)
- Error (error)

**Requires:** `network_write` permission

## Search APIs Supported

### 1. DuckDuckGo Instant Answer API

**Endpoint:** `https://api.duckduckgo.com/?q={query}&format=json&no_html=1`

**Features:**
- No API key required
- Returns abstract text and definitions
- Limited results

**Example Response:**
```json
{
  "AbstractText": "wllr is a command-line interface tool...",
  "AbstractURL": "https://en.wikipedia.org/wiki/wllr",
  "Heading": "wllr",
  "Entity": "software"
}
```

### 2. Bing Search API

**Endpoint:** `https://api.bing.microsoft.com/v7.0/search`

**Features:**
- High quality search results
- Requires Azure API key
- Environment variable: `WLLR_BING_API_KEY`

**Example Request:**
```bash
curl -H "Ocp-Apim-Subscription-Key: YOUR_KEY" \
  https://api.bing.microsoft.com/v7.0/search?q=wllr
```

## Build Instructions

The extension is built using TinyGo via Docker:

```bash
cd extensions/websearch
make build
```

Or manually:

```bash
docker run --rm -v "${PWD}":/workspace -w /workspace tinygo/tinygo:latest \
    tinygo build -o websearch.wasm -target wasip1 ./...
```

## Development

### Files

- `main.go` - Main extension logic and tool handler
- `wllrsdk.go` - wllr SDK (host_call wrappers and API)
- `Makefile` - Build automation
- `TODO.md` - Future implementation plan
- `example_http_get.go` - Example implementations (placeholder)

### TODO

See [TODO.md](TODO.md) for:
- When HTTP GET support becomes available
- Implementation checklist
- Future enhancements

## Security Considerations

- The extension requires `network_read` permission for HTTP GET
- API keys should be stored as environment variables, not in code
- Rate limiting should be implemented for APIs with usage limits

## License

This extension is part of the wllr project.
