# MCP Bridge for wllr

The MCP (Model Context Protocol) bridge enables wllr to discover and invoke tools provided by MCP servers.

## Architecture

The MCP bridge consists of:

- **Config** (`config.go`): Loads MCP server configuration from `~/.config/wllr/config.json`
- **Protocol** (`protocol.go`): JSON-RPC 2.0 protocol types for MCP communication
- **Server** (`server.go`): Manages individual MCP server subprocesses via stdio
- **Bridge** (`bridge.go`): Coordinates multiple MCP servers and routes tool calls
- **Extension** (`extension.go`): Integrates with wllr's extension system

## Configuration

Add MCP servers to your wllr config file (`~/.config/wllr/config.json`):

```json
{
  "mcp-bridge": {
    "mcpServers": {
      "filesystem": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
        "env": {}
      },
      "brave-search": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-brave-search"],
        "env": {
          "BRAVE_API_KEY": "your-api-key-here"
        }
      }
    }
  }
}
```

## How It Works

1. **Startup**: On wllr startup, the MCP bridge:
   - Loads configuration from `~/.config/wllr/config.json`
   - Spawns each configured MCP server as a subprocess
   - Performs the MCP `initialize` handshake
   - Calls `tools/list` to discover available tools
   - Registers all discovered tools with wllr's extension host

2. **Tool Invocation**: When the AI agent calls a tool:
   - The extension host dispatches a `before_tool_call` event
   - The MCP bridge checks if the tool belongs to an MCP server
   - If yes, it calls `tools/call` on the appropriate MCP server via JSON-RPC
   - The result is sent back to the agent via the host's `SendToolResult` method

3. **Shutdown**: On wllr exit:
   - The MCP bridge closes stdin for each MCP server (signaling shutdown)
   - Waits for each subprocess to exit gracefully

## Tool Name Collisions

If multiple MCP servers provide tools with the same name, the first server to register wins. A warning is logged for collisions.

Built-in wllr tools (read_file, write_file, exec, etc.) always take precedence over MCP tools.

## JSON-RPC Communication

MCP servers communicate via newline-delimited JSON-RPC 2.0 over stdio:

- **stdin**: JSON-RPC requests from wllr (one per line)
- **stdout**: JSON-RPC responses from the MCP server (one per line)
- **stderr**: Diagnostic logs (logged to wllr's log)

Example request:
```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"wllr","version":"0.1.0"}}}
```

Example response:
```json
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"filesystem","version":"1.0.0"}}}
```

## Error Handling

- If an MCP server fails to start, wllr logs a warning and continues without that server
- If an MCP server crashes during operation, tool calls to that server will fail
- Tool call errors are returned to the AI agent as error messages

## Development

To test the MCP bridge:

1. Install an MCP server (e.g., `@modelcontextprotocol/server-filesystem`)
2. Add it to your wllr config
3. Start wllr and check logs for "mcp: bridge started"
4. Ask the AI to use a tool from the MCP server

## Future Enhancements

- [ ] Automatic MCP server restart on crash
- [ ] Support for MCP resources and prompts (currently only tools)
- [ ] Streaming tool responses
- [ ] Better handling of image/binary content in tool results
- [ ] MCP server health monitoring
