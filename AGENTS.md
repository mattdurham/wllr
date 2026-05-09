# wllr — Agent Guidelines

## WASM Extension API Documentation

`docs/extensions.md` is the authoritative reference for the WASM extension author API.

**Rule:** Any change to the host↔extension ABI must be reflected in `docs/extensions.md` in the same commit. This includes:

- Adding, removing, or renaming a `host_call` method
- Adding, removing, or changing parameters on any `host_call` method
- Adding, removing, or renaming a lifecycle event (`EventType`)
- Adding, removing, or changing fields on any event payload struct
- Adding, removing, or changing required WASM exports (`_init`, `_on_event`, `_alloc`, `_free`)
- Adding new permission types

When modifying the extension host (`extension/host.go`, `sdk/types.go`, `sdk/methods.go`), open `docs/extensions.md` alongside and update the relevant section before finishing the task.

## Writing Go Extensions

If you are writing a wllr extension in Go, copy `extensions/wllrsdk.go` from the wllr repository into your extension directory. This file provides the complete extension API — you do not need to write any WASM boilerplate.

Your extension only needs:
1. `wllrsdk.go` — copied from the wllr repo (provides _alloc, _free, _init, _on_event, and all host wrappers)
2. `main.go` — your logic only

Minimal extension structure:

```go
//go:build wasip1

package main

import "encoding/json"

func init() {
    RegisterTool(
        "my_tool",
        "What this tool does",
        json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`),
    )
    OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
        if name != "my_tool" {
            return "", false // not our tool — pass through
        }
        var in struct{ Input string `json:"input"` }
        json.Unmarshal(input, &in)
        return "result: " + in.Input, false
    })
}

func main() {}
```

Available SDK functions:
- `RegisterTool(name, description, schema)` — register a tool the LLM can call
- `RegisterCommand(name, description)` — register a slash command
- `OnToolCall(fn)` — handle tool calls; return `("", false)` to pass through
- `OnCommand(name, fn)` — handle a specific slash command
- `OnSessionStart(fn)` — called when a session starts
- `OnBeforeAgentStart(fn)` — called with the user's prompt before each turn
- `OnMessageEnd(fn)` — called with (role, content) when a message completes
- `ToolResult(callID, result, isError)` — send a tool result manually
- `Modal(text)` — show text in the modal overlay
- `Notify(text)` — show a notification in chat
- `SetStatus(key, value)` — update the status bar
- `SetSystemPrompt(prompt)` / `AppendSystemPrompt(text)` — modify system prompt
- `ShowPicker(title, items, callback)` — open an interactive TUI picker
- `AgentResetHistory(messages)` — replace the agent's conversation history
- `Exec(command, dir)` — run a shell command via the host
- `GetEnv(name)` — read an environment variable
- `StoreSet(key, value)` / `StoreGet(key)` — per-extension persistent key-value store
- `Log(level, msg)` / `Logf(level, format, args...)` — structured logging

Build command (same as all extensions):
```
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o my-extension.wasm .
```
