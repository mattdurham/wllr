---
name: lsp
version: 0.1.0
description: Language Server Protocol (LSP) client for code intelligence
author: Assistant
command: ./lsp
schema:
  start:
    description: Start an LSP server
    parameters:
      server:
        type: string
        description: Unique name for this server instance
      command:
        type: string
        description: LSP server command to execute
      args:
        type: array
        description: Command line arguments
      rootUri:
        type: string
        description: Workspace root URI (e.g., file:///path/to/project)
  request:
    description: Send a request to an LSP server
    parameters:
      server:
        type: string
        description: Server name
      method:
        type: string
        description: LSP method name (e.g., textDocument/completion)
      params:
        type: object
        description: Method parameters
  shutdown:
    description: Shutdown an LSP server
    parameters:
      server:
        type: string
        description: Server name
  list:
    description: List running LSP servers
examples:
  - description: Start Python LSP server
    input: |
      {"action": "start", "server": "python", "args": {"command": "pylsp", "args": [], "rootUri": "file:///workspace"}}
  - description: Get code completion
    input: |
      {"action": "request", "server": "python", "args": {"method": "textDocument/completion", "params": {"textDocument": {"uri": "file:///workspace/test.py"}, "position": {"line": 10, "character": 5}}}}
  - description: Get hover information
    input: |
      {"action": "request", "server": "python", "args": {"method": "textDocument/hover", "params": {"textDocument": {"uri": "file:///workspace/test.py"}, "position": {"line": 10, "character": 5}}}}
---

# LSP Extension

This extension provides Language Server Protocol (LSP) client functionality, allowing you to start and interact with LSP servers for code intelligence features like:

- **Code completion** (textDocument/completion)
- **Hover information** (textDocument/hover)
- **Go to definition** (textDocument/definition)
- **Find references** (textDocument/references)
- **Diagnostics** (textDocument/publishDiagnostics)
- **Code actions** (textDocument/codeAction)
- **Rename** (textDocument/rename)
- **Formatting** (textDocument/formatting)

## Usage

### Starting an LSP Server

```json
{
  "action": "start",
  "server": "python",
  "args": {
    "command": "pylsp",
    "args": [],
    "rootUri": "file:///path/to/workspace"
  }
}
```

### Sending Requests

```json
{
  "action": "request",
  "server": "python",
  "args": {
    "method": "textDocument/completion",
    "params": {
      "textDocument": {"uri": "file:///workspace/main.py"},
      "position": {"line": 10, "character": 5}
    }
  }
}
```

### Common LSP Servers

- **Python**: `pylsp` (install: `pip install python-lsp-server`)
- **JavaScript/TypeScript**: `typescript-language-server` (install: `npm i -g typescript-language-server`)
- **Go**: `gopls` (install: `go install golang.org/x/tools/gopls@latest`)
- **Rust**: `rust-analyzer`
- **C/C++**: `clangd`

## Features

- Multiple concurrent LSP servers
- Full LSP protocol support
- Automatic server lifecycle management
- Error handling and diagnostics
