# MCP Extension for wllr

This extension adds support for loading and communicating with Model Context Protocol (MCP) servers.

## Architecture

- **MCP Client** (`sdk/mcp/client.go`): Manages stdio communication with MCP server subprocesses, handles JSON-RPC handshake, tool listing, and tool calls.
- **MCP Manager** (`extension/mcp.go`): Integrates MCP servers into the extension host, registers tools, and routes tool calls.
- **Host Integration** (`extension/host.go`): Hooks MCP tool handling via `OnMCPToolCall` callback, checked before dispatching to WASM extensions.

## Configuration

Add MCP servers to `~/.config/wllr/config.json` under the `"mcp"` group:

```json
{
  "mcp": {
    "servers": [
      {
        "name": "filesystem",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      },
      {
        "name": "github",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-github"]
      }
    ]
  }
}
```

## How It Works

1. On startup, `cmd/main.go` initializes the `MCPManager` and loads server configs from the `"mcp"` config group.
2. For each server, the manager:
   - Spawns the server subprocess with stdio pipes
   - Performs MCP handshake (`initialize` + `initialized` notification)
   - Queries available tools via `tools/list`
   - Registers each tool with the extension host using prefix `mcp_<server>_<tool>`
3. When a tool call is made:
   - The host checks `OnMCPToolCall` first
   - If the tool name matches an MCP tool, the manager forwards it to the corresponding server via `tools/call`
   - The result is returned directly, bypassing WASM extension dispatch
4. MCP tool results are formatted as plain text (concatenated `text` content items)

## Tool Naming

Tools from MCP servers are prefixed to avoid collisions:
- Server: `filesystem`, Tool: `read_file` → `mcp_filesystem_read_file`

## Example Usage

With the filesystem server configured:

```
User: Read /tmp/test.txt
LLM: <calls mcp_filesystem_read_file with path="/tmp/test.txt">
Extension: <forwards to MCP server>
MCP Server: <returns file content>
LLM: The file contains: ...
```

## Limitations

- Only stdio transport is supported (no SSE or HTTP)
- Only tools are supported (resources and prompts not yet implemented)
- Tool schemas are passed through as-is (no validation or transformation)
- Subprocess lifecycle is basic (no automatic restart on crash)

## Future Improvements

- Add resource and prompt support
- Implement subprocess monitoring and auto-restart
- Add connection pooling for multiple instances
- Support env var injection and working directory per server
- Add telemetry/metrics for MCP calls
