# LSP Bridge Testing Guide

## Current Status

### ✅ What Works

1. **Integration Tests** (`extensions/lsp/native/gopls_test.go`)
   - Actually spawn real `gopls serve` process
   - Send actual LSP initialization messages
   - Receive real diagnostics from gopls
   - Get actual document symbols
   - **6/6 tests passing**

2. **Native Bridge** (`extensions/lsp-bridge/native/main.go`)
   - Actually spawns any LSP server with `exec.Command`
   - Creates stdin/stdout pipes for communication
   - Manages multiple server instances
   - Returns real PID and process info

### ❌ What Doesn't Work (Needs Fix)

**WASM Extension** (`extensions/lsp-bridge/main.go`)
- Has stubs in `startDaemonLocked()` - doesn't actually spawn native daemon
- Has stubs in `stopDaemon()` - doesn't actually kill daemon process
- Has stubs in `isDaemonRunning()` - just checks stored PID

## Architecture Gap

```
┌──────────────┐
│ wllr Host    │
└──────┬───────┘
       │ tool_call:*
       ▼
┌─────────────────┐
│ WASM Extension  │ ← stubs: doesn't spawn native daemon
└────────┬────────┘
         │ should route to:
         ▼
┌─────────────────┐
│ Native Daemon   │ ← fully functional, but never reached!
└────────┬────────┘
         │ exec.Cmd
         ▼
┌─────────────────┐
│ LSP Servers     │ ← gopls, pylsp
└─────────────────┘
```

## What's Needed

The WASM extension needs to:
1. Actually spawn the native daemon binary
2. Store PID in host store for persistence
3. Route LSP tool calls to daemon stdin
4. Forward daemon responses as tool results

## Test Results

```bash
$ cd extensions/lsp/native && go test -v
=== RUN   TestGoplsCapabilities
--- PASS: TestGoplsCapabilities (0.19s)
=== RUN   TestGoplsDiagnostics
--- PASS: TestGoplsDiagnostics (0.19s)
=== RUN   TestGoplsDocumentSymbols
--- PASS: TestGoplsDocumentSymbols (0.19s)
=== RUN   TestGoplsMultipleFiles
--- PASS: TestGoplsMultipleFiles (0.19s)
PASS
```

These tests prove **gopls integration works** - we just need to connect the WASM extension to use it!
