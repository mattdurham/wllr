# MCP Server Integration

wllr supports the Model Context Protocol (MCP), enabling integration with external tools and services via MCP servers.

## What is MCP?

MCP (Model Context Protocol) is a standard protocol for connecting AI assistants to external tools and data sources. MCP servers expose tools that can be called by the AI agent.

## Configuration

Configure MCP servers in `~/.config/wllr/config.json`:

```json
{
  "wllr": {
    "provider": "anthropic",
    "model": "claude-3-5-sonnet-20241022"
  },
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
      },
      "github": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-github"],
        "env": {
          "GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_..."
        }
      }
    }
  }
}
```

## Available MCP Servers

The Model Context Protocol community provides many pre-built servers:

- **@modelcontextprotocol/server-filesystem** - Read/write files in a directory
- **@modelcontextprotocol/server-brave-search** - Web search via Brave
- **@modelcontextprotocol/server-github** - GitHub API integration
- **@modelcontextprotocol/server-postgres** - PostgreSQL database access
- **@modelcontextprotocol/server-slack** - Slack messaging
- **@modelcontextprotocol/server-google-drive** - Google Drive access

See the [MCP servers directory](https://github.com/modelcontextprotocol/servers) for a complete list.

## Custom MCP Servers

You can write your own MCP server in any language. The server must:

1. Accept JSON-RPC 2.0 requests on stdin
2. Write JSON-RPC 2.0 responses to stdout (one per line)
3. Implement the MCP protocol:
   - `initialize` - Handshake and capability exchange
   - `tools/list` - Return available tools
   - `tools/call` - Execute a tool

Example minimal MCP server in Python:

```python
#!/usr/bin/env python3
import json
import sys

def handle_initialize(params):
    return {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "serverInfo": {"name": "my-server", "version": "1.0.0"}
    }

def handle_tools_list(params):
    return {
        "tools": [
            {
                "name": "say_hello",
                "description": "Says hello to a name",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "name": {"type": "string"}
                    },
                    "required": ["name"]
                }
            }
        ]
    }

def handle_tools_call(params):
    name = params["arguments"]["name"]
    return {
        "content": [{"type": "text", "text": f"Hello, {name}!"}],
        "isError": false
    }

for line in sys.stdin:
    req = json.loads(line)
    method = req["method"]
    
    if method == "initialize":
        result = handle_initialize(req.get("params"))
    elif method == "tools/list":
        result = handle_tools_list(req.get("params"))
    elif method == "tools/call":
        result = handle_tools_call(req.get("params"))
    else:
        result = {"error": {"code": -32601, "message": "Method not found"}}
    
    resp = {"jsonrpc": "2.0", "id": req["id"], "result": result}
    print(json.dumps(resp), flush=True)
```

## Tool Name Collisions

If multiple MCP servers provide tools with the same name:
- The first server to register wins
- A warning is logged
- The other tools are ignored

wllr's built-in tools (read_file, write_file, exec, etc.) always take precedence over MCP tools.

## Debugging

View MCP logs in wllr's output:

```
wllr --exec "use the filesystem tool to list /tmp"
```

Look for log lines like:

```
INFO  mcp: server started name=filesystem tools=3
INFO  mcp: bridge started servers=1 tools=3
```

MCP server stderr is logged at DEBUG level:

```
DEBUG mcp: server stderr name=filesystem line="Initialized with directory /tmp"
```

## Security Considerations

MCP servers run as subprocesses with full access to:
- Your filesystem (respecting their configured directory restrictions)
- Network (if they make API calls)
- Environment variables (you provide in config)

Only configure MCP servers you trust. Review server source code before use.

## Troubleshooting

### "mcp bridge init failed: no such file or directory"

The MCP server command is not in your PATH. For npx-based servers, ensure Node.js and npm are installed.

### "mcp: server stopped" immediately after start

The server crashed during startup. Check:
1. Server dependencies are installed
2. Environment variables (API keys) are correct
3. Server has required permissions

### Tools not appearing

Check logs for:
```
WARN mcp: failed to register tool tool=xyz error=...
```

This usually means a tool name collision with a built-in wllr tool.
