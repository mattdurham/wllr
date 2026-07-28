# LSP Bridge Implementation

## Overview

This document describes the implementation details of the lsp-bridge.

## Files

### `/Users/matt/source/wllr/extensions/lsp-bridge/main.go`

WASM extension that handles tool routing and host integration.

**Key Functions**:
- `extensionInit()`: Registers tools with wllr
- `extensionOnEvent()`: Handles events (tool calls, session start, shutdown)
- `handleBeforeToolCall()`: Routes LSP tools
- `startDaemon()`: Starts native bridge (stub mode)
- `stopDaemon()`: Stops native bridge on shutdown

### `/Users/matt/source/wllr/extensions/lsp-bridge/native/main.go`

Native daemon for LSP server process management.

**Key Structures**:
- `LSPServer`: Represents a running LSP server
- `ServerManager`: Manages multiple LSP servers

**Key Functions**:
- `StartServer()`: Spawns LSP server process
- `StopServer()`: Terminates server process
- `ListServers()`: Returns running servers
- `handleOutput()`: Background goroutine for stdout

## Tool Registration

Tools are registered in `extensionInit()`:

```go
registerTool(
    "lsp_server_start",
    "Start an LSP server process (gopls, pylsp, etc.)",
    inputSchema,
)
```

## Event Handling

### before_tool_call

When a tool is called, `_on_event` receives:

```json
{
  "type": "before_tool_call",
  "payload": {
    "tool_call_id": "uuid",
    "tool_name": "lsp_server_start",
    "input": {...}
  }
}
```

The handler:
1. Checks if tool name starts with "lsp_"
2. Routes to appropriate handler
3. Returns `tool_result` via host call

## Native Bridge Communication

### Tool Call Format (to daemon)

```
tool_call:{"tool_call_id":"...","tool_name":"...","input":{...}}
```

### Tool Result Format (from daemon)

```json
{
  "tool_call_id": "...",
  "result": {...},
  "is_error": false
}
```

## Process Spawning

```go
func (sm *ServerManager) StartServer(name, command string, args []string) error {
    cmd := exec.Command(command, args...)
    
    stdin, err := cmd.StdinPipe()
    stdout, err := cmd.StdoutPipe()
    
    cmd.Start()
    
    sm.servers[name] = &LSPServer{
        Cmd:   cmd,
        Stdin: stdin,
        Stdout: stdout,
    }
    
    go sm.handleOutput(name)
}
```

## LSP Protocol Support

### Content-Length Framing

```go
// Send message with Content-Length header
func (sm *ServerManager) sendMessage(server string, msg []byte) error {
    contentLength := len(msg)
    header := fmt.Sprintf("Content-Length: %d\r\n\r\n", contentLength)
    
    _, err := server.Stdin.Write(append([]byte(header), msg...))
    return err
}
```

### JSON-RPC Message

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {...}
}
```

## Current Implementation Status

### ✅ Completed

- WASM extension with tool registration
- Event handling for LSP tools
- Native bridge subprocess management
- stdin/stdout pipes for servers

### ⏳ Pending

- IPC mechanism (Unix socket)
- LSP Content-Length framing
- JSON-RPC response routing

## Testing

### Manual Test

```bash
# Build native bridge
go build -o /tmp/lsp-native .

# Create test input
cat > test_input.jsonl <<EOF
tool_call:{"tool_call_id":"test1","tool_name":"lsp_server_list","input":{}}
EOF

# Run
/tmp/lsp-native < test_input.jsonl
```

## Integration with wllr

The extension integrates via:
1. **Tool Registration**: Register tools on init
2. **Event Handlers**: Handle before_tool_call events
3. **Tool Results**: Send results back via host_call

## Performance Considerations

- Each server gets background output handler
- Mutex protection for concurrent access
- Non-blocking stdin/stdout
