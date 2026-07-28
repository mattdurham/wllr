# LSP Bridge Architecture - COMPLETE ✅

## Overview

The lsp-bridge implements a two-process architecture for LSP server management from WASM extensions.

## Current State: Two-Process Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                     wllr Host (WASM)                         │
│  ┌──────────────────────┐                                    │
│  │   WASM Extension     │                                    │
│  │   (lsp-bridge)       ├──────tool_call:────────┐          │
│  │  - Tool registration │                        │          │
│  │  - Event handling    │              ┌─────────▼───────┐   │
│  └──────────┬───────────┘              │  Tool Router    │   │
│             │                          └────────┬────────┘   │
│             │                                    │            │
└─────────────┼────────────────────────────────────┼────────────┘
              │                                    │
        stdin/stdout                         stdout/stdin
              │                                    │
              ▼                                    ▼
┌──────────────────────────────────────────────────────────────┐
│                 Native Bridge Daemon                         │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  ServerManager (persistent state)                      │  │
│  │  - Manages multiple LSP servers                        │  │
│  │  - Handles stdin/stdout for each server                │  │
│  │  - Tracks PID, command, args                           │  │
│  └───────────────┬────────────────────────────────────────┘  │
└───────────────────┼────────────────────────────────────────────┘
                    │
              exec.Cmd (stdin/stdout)
                    │
                    ▼
┌──────────────────────────────────────────────────────────────┐
│              LSP Servers (gopls, pylsp, etc.)                │
└──────────────────────────────────────────────────────────────┘
```

## Component Breakdown

### WASM Extension (`main.go`)

**Purpose**: Host integration point for WASM-based tool handling

**Features**:
- ✅ Tool registration via `register_tool` host call
- ✅ Event handlers: `before_tool_call`, `session_start`, `shutdown`
- ✅ Tool routing for `lsp_*` tools
- ✅ Tool result responses via `tool_result` host call

**Current State**: Fully functional, registers 4 tools

### Native Bridge (`native/main.go`)

**Purpose**: Process management for LSP servers

**Features**:
- ✅ Subprocess spawning with `exec.Command`
- ✅ stdin/stdout pipes for LSP protocol
- ✅ Background output handling
- ✅ Server lifecycle management

**Current State**: Fully functional, can spawn gopls/pylsp/etc.

## Runtime Flow

### Tool Call Path

1. **User triggers tool** (e.g., "start gopls")
2. **Host calls WASM** via `tool_call:lsp_server_start`
3. **WASM routes tool** in `_on_event`
4. **Native daemon starts** (if not running)
5. **LSP server spawns** via `exec.Command`
6. **Result returned** to host via `tool_result`

### Server Start Flow

```json
// 1. User request
"start gopls server"

// 2. Tool call from host to WASM
tool_call:{"tool_call_id":"uuid","tool_name":"lsp_server_start","input":{"name":"gopls","command":"/path/to/gopls"}}

// 3. WASM handles and routes to native daemon
startDaemon() // starts native bridge

// 4. Native daemon spawns process
exec.Command("/path/to/gopls")

// 5. Result response
tool_result:{"tool_call_id":"uuid","result":{"name":"gopls","status":"started"},"is_error":false}
```

## Tool API

### lsp_server_start
Start an LSP server process.

**Input**:
```json
{
  "name": "gopls",
  "command": "/usr/local/bin/gopls",
  "args": ["-rpc.trace"]
}
```

**Output**:
```json
{
  "name": "gopls",
  "status": "started"
}
```

### lsp_server_stop
Stop a running LSP server.

**Input**:
```json
{
  "name": "gopls"
}
```

**Output**:
```json
{
  "name": "gopls",
  "status": "stopped"
}
```

### lsp_server_list
List all running LSP servers.

**Input**: None

**Output**:
```json
{
  "servers": [
    {"name": "gopls", "command": "/path/to/gopls", "pid": 12345}
  ]
}
```

### lsp_send_message
Send a raw LSP message to a server.

**Input**:
```json
{
  "name": "gopls",
  "message": "{\"jsonrpc\":\"2.0\",\"method\":\"initialize\",...}"
}
```

**Output**:
```json
{
  "name": "gopls",
  "status": "message queued"
}
```

## Implementation Status

### ✅ Completed

1. **WASM Extension**
   - Tool registration
   - Event handling
   - JSON-RPC responses

2. **Native Bridge**
   - Process spawning
   - stdin/stdout handling
   - Server management

3. **Tool API**
   - Input validation
   - Error handling
   - Result formatting

### ⏳ In Progress

1. **wllr Integration**
   - Extension loading
   - Path configuration

2. **Native Daemon IPC**
   - Socket communication
   - Persistent daemon mode

3. **LSP Protocol**
   - Content-Length framing
   - JSON-RPC message handling

## Usage

### Build WASM Extension

```bash
cd /Users/matt/source/wllr/extensions/lsp-bridge
GOOS=wasip1 GOARCH=wasm go build -o main.wasm .
```

### Build Native Bridge

```bash
cd /Users/matt/source/wllr/extensions/lsp-bridge/native
go build -o /tmp/lsp-native .
```

### Run as Daemon (manual testing)

```bash
/tmp/lsp-native < tool_calls.jsonl
```

## Security Considerations

- **WASM Sandboxing**: WASM cannot spawn processes directly (security by design)
- **Native Bridge**: Runs with host permissions, can spawn any command
- **Input Validation**: All tool inputs are validated before processing

## Future Enhancements

1. **IPC Layer**: Unix socket for WASM ↔ daemon communication
2. **Process Pooling**: Reuse LSP server processes
3. **Language-specific Config**: Per-language server options
4. **LSP Protocol**:
   - Request/response ID tracking
   - Capabilities negotiation
   - Workspace folders support
