# wllr — Agent Guidelines

## Project Overview

wllr is a terminal AI coding assistant built with bubbletea. It has two primary extension points:
1. **WASM extensions** — loaded from `~/.wllr/extensions/`, written in any WASM-capable language
2. **Go modules** — the core subsystems in `modules/`, each with a typed interface boundary

### Module Map

```
modules/
  sdk/        — shared wire types and ABI constants (leaf, no wllr deps)
  agent/      — LLM turn execution, agent pool, sub-agent spawning
  extension/  — WASM host, event dispatch, 5 bridge interfaces
  tools/      — sdk.Tool → fantasy.AgentTool adapter
  session/    — subsystem wiring, lifecycle, liveState
  harness/    — bubbletea TUI, rendering, input, Renderer interface
  mcp/        — MCP server subprocess bridge
  testutil/   — fake LM/provider for tests
cmd/          — binary entry point
extensions/   — bundled WASM extensions (agents, history, statusline, …)
```

### Subsystem Interfaces

Extensions communicate with the host via 5 typed interfaces (defined in `modules/extension/interfaces.go`):
- `AgentBridge` — spawn, close, message agents
- `TeamBridge` — create and manage agent teams
- `UIBridge` — notify, modal, picker, status bar
- `CapabilityProvider` — exec, file I/O, HTTP, env
- `MCPBridge` — MCP server subprocess management

The TUI is decoupled from subsystems via `harness.Renderer` (defined in `modules/harness/renderer.go`).
To swap the TUI: implement `Renderer` + `UIBridge`, call `session.Wire(host, pool, mainID, yourRenderer)`.

---

## Spec-Driven Development

Every module in `modules/` is **spec-driven**. This means:

### The Invariant

Every non-test `.go` file in a spec-driven module carries:
```go
// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.
```

This is a hard rule. Do not remove it. If you add a new file to a module, add this comment.

### The Four Spec Files

Each module has four living documentation files:

| File | Purpose |
|------|---------|
| `SPECS.md` | Interface contracts, invariants, behavioral guarantees. The source of truth for what the module promises. |
| `NOTES.md` | Design decisions with dated entries (append-only). WHY things are the way they are. |
| `TESTS.md` | Test specifications: scenarios, setup, assertions. Maps to actual test functions. |
| `BENCHMARKS.md` | Benchmark specs with metric targets. |

### What Must Be Updated

**Any code change in a spec-driven module MUST be accompanied by:**
- `SPECS.md` — if the public API, invariants, or behavior changes
- `NOTES.md` — if a non-obvious design decision was made (add a new dated entry)
- `TESTS.md` — if new tests were added or existing test intent changed
- `BENCHMARKS.md` — if performance characteristics changed

**NOTES.md rules:**
- Each entry: `## N. Title`, `*Added: YYYY-MM-DD*`, **Decision:**, **Rationale:**, **Consequence:**
- Never delete entries — add `*Addendum (date):*` if a decision is reversed
- New decisions go in new numbered sections at the end

### What Does NOT Need Updating

- Pure refactors with no behavior change (rename, move, reformat) — no spec update needed
- Adding a test that was already specified in TESTS.md
- Bug fixes where the fix makes behavior match the existing spec

---

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
