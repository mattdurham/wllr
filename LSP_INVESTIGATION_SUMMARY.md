# LSP Extension Investigation Summary

## Problem
The LSP WASM extension was not loading even though it existed in the source tree.

## Root Cause
The LSP extension was in the **source directory** (`./extensions/lsp/lsp.wasm`) but NOT installed in the **user extensions directory** (`~/.wllr/extensions/lsp/`).

## How Extension Loading Works

### Built-in Extensions
These are compiled into the `wllr` binary:
- `readfile.wasm`
- `writefile.wasm`
- `exec.wasm`
- `env.wasm`
- `agents.wasm`

Located at: `cmd/builtins/*.wasm` (embedded at compile time)

### User Extensions (WASM)
Loaded from two locations in order:

1. **`~/.wllr/extensions/`** (subdirectory layout)
   - Scans subdirectories for `.wasm` files
   - One WASM per subdirectory
   - Example: `~/.wllr/extensions/lsp/lsp.wasm`

2. **`$WLLR_EXTENSIONS_DIR`** (flat layout)
   - If set, scans for `.wasm` files directly
   - Flat directory structure

### Extension Loading Code
Located in `cmd/context.go`:
- `loadExtensionsFromSubdirs()` - subdirectory scanner
- `loadExtensionsFlat()` - flat directory scanner

## Solution Applied
Copied the LSP WASM extension to the user directory:
```bash
mkdir -p ~/.wllr/extensions/lsp
cp ./extensions/lsp/lsp.wasm ~/.wllr/extensions/lsp/
```

## Next Steps
To activate the LSP extension, use one of:
1. `/reload` command in the running session
2. Restart wllr

## LSP Extension Capabilities
Once loaded, provides these tools:
- **detect** - Find installed LSP servers
- **start** - Start an LSP server (auto or manual)
- **call** - Call LSP methods (completion, hover, definition, etc.)
- **list** - List running servers
- **stop** - Stop a server

### Supported Languages
Go, Python, JavaScript/TypeScript, Rust, C/C++, Java, Ruby, PHP, C#, Lua, Bash, JSON, YAML, and more.

### Example Usage
```json
{
  "action": "detect"
}
```
or
```json
{
  "file": "main.go"
}
```

## Additional Findings

### Working Extensions (in ~/.wllr/extensions/)
- context
- skills  
- tasks

These were already installed and working.

### Extension Configuration
- Extensions can have optional metadata in `extension.yaml`
- The LSP extension has: `name: lsp`, `enabled: true`, `wasm: lsp.wasm`
- Config file location: `~/.config/wllr/config.json`

### WASM Runtime
- Uses **wazero** (Go WASM runtime)
- Validates required exports: `_init`, `_on_event`, `_alloc`, `_free`
- Supports both WASI reactor and command models

## Why Other Extensions Loaded
The other extensions (exec, readfile, etc.) loaded because they are:
1. Built-in (embedded in the binary), OR
2. Already installed in `~/.wllr/extensions/`

The LSP extension was unique in being:
- A user extension (not built-in)
- Present in source but not installed
- A WASM extension requiring proper installation location
