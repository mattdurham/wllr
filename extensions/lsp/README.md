# LSP Extension

Provides Language Server Protocol (LSP) integration for advanced code intelligence.

## Features

- **Auto-detection**: Automatically detects language from file extension and starts appropriate LSP server
- **Multi-server**: Run multiple LSP servers simultaneously for different languages
- **Full LSP support**: All LSP methods (completion, hover, goto definition, diagnostics, etc.)
- **Server discovery**: Detect which LSP servers are installed on your system
- **Graceful lifecycle**: Proper initialization and shutdown handshake

## Supported Languages

The extension includes built-in mappings for common languages:

| Language   | LSP Server                     | Extension          |
|------------|--------------------------------|--------------------|
| Go         | gopls                          | .go                |
| Python     | pylsp                          | .py                |
| JavaScript | typescript-language-server     | .js, .jsx          |
| TypeScript | typescript-language-server     | .ts, .tsx          |
| Rust       | rust-analyzer                  | .rs                |
| C/C++      | clangd                         | .c, .cpp, .h, .hpp |
| Java       | jdtls                          | .java              |
| Ruby       | solargraph                     | .rb                |
| PHP        | intelephense                   | .php               |
| C#         | omnisharp                      | .cs                |
| Lua        | lua-language-server            | .lua               |
| Bash       | bash-language-server           | .sh                |
| JSON       | vscode-json-languageserver     | .json              |
| YAML       | yaml-language-server           | .yaml, .yml        |
| HTML       | html-languageserver            | .html              |
| CSS        | css-languageserver             | .css               |

## Usage

### 1. Detect installed LSP servers

Find out which LSP servers are available on your system:

```json
{
  "action": "detect"
}
```

### 2. Auto-start server (easiest way)

Just provide a file path and the extension will detect the language and start the appropriate server:

```json
{
  "file": "main.go"
}
```

This will:
- Detect language as "go" from the .go extension
- Start gopls server
- Initialize it with the current workspace

### 3. Manual server start

Start a specific LSP server with custom configuration:

```json
{
  "action": "start",
  "name": "my-python-server",
  "cmd": "pylsp",
  "args": ["-v"]
}
```

### 4. List running servers

```json
{
  "action": "list"
}
```

### 5. Send LSP requests

Once a server is running, call any LSP method:

**Code Completion:**
```json
{
  "action": "call",
  "server": "go",
  "method": "textDocument/completion",
  "params": {
    "textDocument": {
      "uri": "file:///home/user/project/main.go"
    },
    "position": {
      "line": 10,
      "character": 5
    }
  }
}
```

**Hover (documentation):**
```json
{
  "action": "call",
  "server": "go",
  "method": "textDocument/hover",
  "params": {
    "textDocument": {
      "uri": "file:///home/user/project/main.go"
    },
    "position": {
      "line": 15,
      "character": 8
    }
  }
}
```

**Go to Definition:**
```json
{
  "action": "call",
  "server": "go",
  "method": "textDocument/definition",
  "params": {
    "textDocument": {
      "uri": "file:///home/user/project/main.go"
    },
    "position": {
      "line": 20,
      "character": 12
    }
  }
}
```

**Diagnostics (errors/warnings):**
```json
{
  "action": "call",
  "server": "python",
  "method": "textDocument/publishDiagnostics",
  "params": {
    "textDocument": {
      "uri": "file:///home/user/project/script.py"
    }
  }
}
```

### 6. Open/Close documents

**Open a document:**
```json
{
  "action": "open",
  "server": "go",
  "uri": "file:///home/user/project/main.go",
  "languageId": "go",
  "version": 1,
  "text": "package main\n\nfunc main() {\n    fmt.Println(\"hello\")\n}\n"
}
```

**Change a document:**
```json
{
  "action": "change",
  "server": "go",
  "uri": "file:///home/user/project/main.go",
  "version": 2,
  "text": "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"hello\")\n}\n"
}
```

**Close a document:**
```json
{
  "action": "close",
  "server": "go",
  "uri": "file:///home/user/project/main.go"
}
```

### 7. Stop a server

```json
{
  "action": "stop",
  "name": "go"
}
```

## Common LSP Methods

- `textDocument/completion` - Code completion
- `textDocument/hover` - Documentation on hover
- `textDocument/definition` - Go to definition
- `textDocument/references` - Find references
- `textDocument/documentSymbol` - Document outline
- `textDocument/formatting` - Format document
- `textDocument/codeAction` - Quick fixes and refactorings
- `textDocument/rename` - Rename symbol
- `textDocument/signatureHelp` - Function signature help

## Installing LSP Servers

### Go
```bash
go install golang.org/x/tools/gopls@latest
```

### Python
```bash
pip install python-lsp-server
```

### TypeScript/JavaScript
```bash
npm install -g typescript-language-server typescript
```

### Rust
```bash
rustup component add rust-analyzer
```

### C/C++
```bash
# Ubuntu/Debian
sudo apt install clangd

# macOS
brew install llvm
```

### More servers
See [langserver.org](https://langserver.org/) for a comprehensive list.

## Implementation Details

- Written in Go using only the standard library
- Implements JSON-RPC 2.0 protocol over stdio
- Proper Content-Length header framing
- Async request/response correlation
- Graceful server initialization and shutdown
- Thread-safe multi-server management
- Stderr capturing for debugging

## Testing

Run the test suite:
```bash
cd extensions/lsp
go build
./test_integration.sh
```

## License

MIT
