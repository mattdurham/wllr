# LSP Component Fix Summary

## Problem Identified

The LSP extension had a critical bug where it tried to run LSP servers with the `--version` flag, but most LSP servers (including gopls) use `version` as a subcommand instead.

### Affected Functions

1. **runDiagnosticsFile()** (line 197)
   - Used for running diagnostics on files
   
2. **diagnosticsHealth()** (line 272)  
   - Used for checking server health status
   
3. **startPrimaryServer()** (line 378)
   - Used for starting primary LSP servers

## Fix Applied

Changed from:
```go
err := exec.Command(cmdPath, "--version").Run()
```

To:
```go
err := exec.Command(cmdPath, "version").Run()  // Fixed: use "version" not "--version"
```

### Files Modified
- `/Users/matt/source/wllr/extensions/lsp/main.go` (3 locations)

### Verification

After the fix, `diagnosticsHealth()` correctly detects available servers:

```json
{
  "available": 1,
  "not_installed": 0,
  "servers": [
    {
      "id": "gopls",
      "command": "/Users/matt/go/bin/gopls",
      "status": "available"
    }
  ],
  "total": 1
}
```

## Testing Performed

### 1. Extension Build
✅ Successfully built WASM extension:
```bash
make extensions
# Output: /Users/matt/.wllr/extensions/lsp/lsp.wasm (3.8MB)
```

### 2. Server Detection Test
✅ Created test program to verify server detection

### 3. gopls check Command  
✅ Verified `gopls check <file>` works correctly for diagnostics

### 4. Shell Diagnostics
✅ Tested auxiliary server diagnostics (ruff, shellcheck, etc.)

## Known Limitations

### Stdio Not Available in WASM
The core LSP server functionality uses stdio pipes (`exec.Cmd.StdinPipe`, `StdoutPipe`) which aren't available in WASM environments. This means:

- ✅ Shell-based diagnostics work (gopls check, ruff check)
- ❌ Full LSP protocol with JSON-RPC over stdio doesn't work
- ⚠️ Server startup is limited to version checking

### Path Detection
The code uses `exec.LookPath()` which may not find binaries in non-standard locations. For gopls on macOS, the binary is typically at `/Users/matt/go/bin/gopls`.

## Recommendations

1. **For now**: Use the shell-based diagnostics which work correctly
2. **Long-term**: Implement HTTP-based LSP server support for WASM environments
3. **Alternative**: Use named pipes or sockets instead of stdio

## Test Commands

```bash
# Rebuild extension with fix
make extensions

# Test server detection (after building)
go run cmd/test_diagnostics_health.go

# Test gopls check on a Go file
gopls check /path/to/file.go

# Check version command works
gopls version  # Output: golang.org/x/tools/gopls v0.22.0
```

## Status

✅ **FIXED** - Critical bug with version detection command resolved
⚠️ **LIMITED** - Full LSP protocol support blocked by WASM stdio limitations
✅ **WORKING** - Shell-based diagnostics fully functional
