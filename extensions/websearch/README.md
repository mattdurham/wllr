# WebSearch Extension for wllr

This extension provides web search by fetching and parsing DuckDuckGo's HTML results page.

## Features

- Search the web without an API key
- Parse normal ranked DuckDuckGo HTML results
- Structured search results with title, link, and snippet

## Current Status

The extension uses the host's `http_get` capability and requires the `network_read` permission.
DuckDuckGo may occasionally return an anti-bot challenge instead of search results.

## Usage

### DuckDuckGo HTML

No API key is required. The extension requests `https://html.duckduckgo.com/html/`, then returns
up to ten results as structured title, link, and snippet fields.

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
- `parser.go` - DuckDuckGo HTML result parser
- `parser_test.go` - Parser and URL construction tests
- `wllrsdk.go` - wllr SDK (host_call wrappers and API)
- `Makefile` - Build automation
- `TODO.md` - Follow-up options and operational notes
- `example_http_get.go` - Example implementations (placeholder)

### TODO

See [TODO.md](TODO.md) for follow-up provider and operational ideas.

## Security Considerations

- The extension requires `network_read` permission for HTTP GET
- Search responses are external HTML and should be treated as untrusted input.
- DuckDuckGo may rate-limit or challenge automated requests.

## License

This extension is part of the wllr project.
