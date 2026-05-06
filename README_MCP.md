# MCP Server Extension

Generic MCP (Model Context Protocol) server loader and communication layer for wllr.

## Features

- **Subprocess Management**: Spawn and manage MCP server processes via stdio
- **Automatic Tool Registration**: MCP tools are automatically registered with the extension host
- **JSON-RPC Communication**: Full MCP handshake, tool listing, and tool invocation
- **Config-Driven**: Load multiple MCP servers from `~/.config/wllr/config.json`

## Architecture

```
┌─────────────┐
│   wllr      │
│  (main.go)  │
└──────┬──────┘
       │
       ├───────────────┐
       │               │
┌──────▼───────┐  ┌───▼──────────┐
│ Extension    │  │ MCP Manager  │
│ Host         │  │              │
└──────┬───────┘  └───┬──────────┘
       │              │
       │         ┌────▼─────┐
       │         │ MCP      │
       │         │ Client   │
       │         └────┬─────┘
       │              │
       │         ┌────▼─────────┐
       │         │ MCP Server   │
       │         │ (subprocess) │
       │         └──────────────┘
       │
┌──────▼─────────────┐
│ WASM Extensions    │
└────────────────────┘
```

## Configuration

Add MCP servers to `~/.config/wllr/config.json`:

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

## Tool Naming

MCP tools are prefixed to avoid conflicts:

```
Server: filesystem
Tool: read_file
→ Registered as: mcp_filesystem_read_file
```

## Implementation

### MCP Client (`sdk/mcp/client.go`)
- Spawns subprocess with stdio pipes
- Implements JSON-RPC 2.0 over newline-delimited JSON
- Handles MCP handshake (`initialize` + `initialized`)
- Provides `ListTools()` and `CallTool()` methods

### MCP Manager (`extension/mcp.go`)
- Loads server configs from wllr config file
- Spawns MCP clients for each server
- Registers tools with extension host
- Routes tool calls to appropriate server

### Host Integration (`extension/host.go`)
- Added `OnMCPToolCall` callback
- Modified `ExecuteTool` to check MCP before dispatching to extensions
- MCP tools bypass WASM extension layer for direct subprocess communication

## Testing

Run integration tests (requires Node.js + npx):

```bash
go test ./sdk/mcp/... -v
```

Skip integration tests:

```bash
go test ./sdk/mcp/... -short
```

## Example Session

```
$ dist/wllr
> Read /tmp/test.txt

[LLM calls mcp_filesystem_read_text_file]
[Extension forwards to MCP server]
[Server returns file content]

Here's the content: ...
```

## Limitations

- Stdio transport only (no SSE/HTTP)
- Tools only (resources/prompts not implemented)
- Basic subprocess lifecycle (no auto-restart)
- No schema validation or transformation

## Future Work

- Add resource and prompt support
- Implement subprocess monitoring and health checks
- Support environment variable injection per server
- Add telemetry and error metrics
- Connection pooling for multiple instances
