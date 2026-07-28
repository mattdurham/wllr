# LSP Bridge - Remove Stubs Plan

## Current State Analysis

### ✅ What Works (No Stubs)

**Integration Tests** (`extensions/lsp/native/gopls_test.go`):
- Actually spawn real `gopls serve` process
- Send actual LSP initialization messages
- Receive real diagnostics from gopls
- Get actual document symbols
- All 6 tests passing

**Native Bridge** (`extensions/lsp-bridge/native/main.go`):
- Actually spawns any LSP server with `exec.Command`
- Creates stdin/stdout pipes for communication
- Manages multiple server instances
- Returns real PID and process info
- No stubs!

### ❌ What Doesn't Work (Has Stubs)

**WASM Extension** (`extensions/lsp-bridge/main.go`):
- Line ~206: `startDaemonLocked()` - stores "0" as PID, doesn't actually spawn daemon
- Line ~235: `stopDaemon()` - just clears stored PID, doesn't kill process
- Line ~217: `isDaemonRunning()` - just checks if PID stored, doesn't verify process alive
- Line ~165: `handleLSPServerList()` - returns empty list, doesn't query daemon
- Line ~175: `handleLSPSendMessage()` - just queues message, doesn't send to daemon

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

## Implementation Plan

### Phase 1: Implement Daemon Spawning

**Goal**: Actually spawn the native daemon binary and track its PID

```go
func startDaemonLocked() {
    // 1. Find native binary (check common paths)
    binaryPath := findNativeBinary()
    
    // 2. Spawn as subprocess with pipes
    cmd := exec.Command(binaryPath)
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    
    // 3. Start process
    err := cmd.Start()
    
    // 4. Store PID in host store
    storeSet(storeKeyDaemonPID, fmt.Sprintf("%d", cmd.Process.Pid))
    
    // 5. Start background goroutine to read stdout
    go readDaemonOutput(stdout)
    
    // 6. Store stdin for sending tool calls
    storeSet(storeKeyDaemonStdin, "open")
}
```

### Phase 2: Implement Daemon Communication

**Goal**: Route LSP tool calls to daemon stdin and forward responses

```go
func sendToDaemon(message map[string]any) error {
    // 1. Get stored stdin
    stdinStr, found := storeGet(storeKeyDaemonStdin)
    
    // 2. Write to daemon stdin
    data, _ := json.Marshal(message)
    fmt.Fprintf(stdin, "tool_call:%s\n", string(data))
    
    // 3. Wait for response
    return readDaemonResponse()
}
```

### Phase 3: Implement List/Message Tools

**Goal**: Actually query daemon for server list and send messages

```go
func handleLSPServerList() {
    // 1. Query daemon for server list
    request := map[string]any{
        "tool_call_id": fmt.Sprintf("list_%d", timeNow()),
        "tool_name":    "lsp_server_list",
        "input":        json.RawMessage("{}"),
    }
    
    // 2. Send to daemon
    if err := sendToDaemon(request); err != nil {
        logMsg(3, "failed to query daemon: "+err.Error())
        return
    }
    
    // 3. Forward response to host
    result := map[string]any{"servers": getDaemonServers()}
    jsonResp, _ := json.Marshal(result)
    
    hostCallJSON("tool_result", map[string]any{
        "tool_call_id": request["tool_call_id"].(string),
        "result":       string(jsonResp),
        "is_error":     false,
    })
}
```

### Phase 4: Implement Daemon Output Reading

**Goal**: Read daemon stdout and forward tool results to host

```go
func readDaemonOutput(stdout io.ReadCloser) {
    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        line := scanner.Text()
        
        // Check if it's a tool_result
        if strings.HasPrefix(line, "tool_result:") {
            jsonStr := strings.TrimPrefix(line, "tool_result:")
            
            // Forward to host
            hostCallJSON("tool_result", json.RawMessage(jsonStr))
        }
    }
}
```

## Files to Modify

1. **extensions/lsp-bridge/main.go**:
   - Update `startDaemonLocked()` to spawn actual process
   - Update `stopDaemon()` to kill daemon process
   - Update `isDaemonRunning()` to check actual process state
   - Update `handleLSPServerList()` to query daemon
   - Add `readDaemonOutput()` goroutine
   - Update `handleLSPSendMessage()` to send to daemon

2. **extensions/lsp-bridge/native/main.go**:
   - Already has full implementation, no changes needed
   - Ensure it reads from stdin and outputs tool_result format

## Testing Strategy

1. Build both binaries:
   ```bash
   cd extensions/lsp-bridge && GOOS=wasip1 GOARCH=wasm go build -o main.wasm .
   cd extensions/lsp-bridge/native && go build -o /tmp/lsp-native .
   ```

2. Test daemon starts:
   ```bash
   # Check if PID gets stored in host store
   ~/bin/lth prompt "check daemon pid storage"
   ```

3. Test server list:
   ```bash
   # Check if actual servers are returned
   ~/bin/lth prompt "check server list from daemon"
   ```

4. Test with real gopls:
   ```bash
   # Actually spawn gopls and verify diagnostics
   ~/bin/lth prompt "test gopls integration"
   ```

## Success Criteria

- [ ] WASM extension compiles without warnings
- [ ] Native bridge binary builds successfully  
- [ ] Daemon starts when first LSP tool is called
- [ ] Server list returns actual servers from daemon
- [ ] gopls integration tests still pass
- [ ] No stubs in main.go
