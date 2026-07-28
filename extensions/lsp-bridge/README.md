# LSP Bridge Extension

Provides native process management for LSP servers (gopls, pylsp, etc.) from WASM extensions.

## Overview

The lsp-bridge implements a two-process architecture:
1. **WASM Extension**: Host integration and tool routing
2. **Native Bridge**: LSP server process management

## Quick Start

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

## Tools

### lsp_server_start
Start an LSP server.

```json
{
  "tool_call_id": "uuid",
  "tool_name": "lsp_server_start",
  "input": {
    "name": "gopls",
    "command": "/path/to/gopls",
    "args": ["-rpc.trace"]
  }
}
```

### lsp_server_stop
Stop a running server.

```json
{
  "tool_call_id": "uuid",
  "tool_name": "lsp_server_stop",
  "input": {
    "name": "gopls"
  }
}
```

### lsp_server_list
List all running servers.

```json
{
  "tool_call_id": "uuid",
  "tool_name": "lsp_server_list",
  "input": {}
}
```

### lsp_send_message
Send raw LSP message.

```json
{
  "tool_call_id": "uuid",
  "tool_name": "lsp_send_message",
  "input": {
    "name": "gopls",
    "message": "{\"jsonrpc\":\"2.0\",\"method\":\"initialize\",...}"
  }
}
```

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for full details.

## Status

See [STATUS.md](STATUS.md) for current implementation status.
