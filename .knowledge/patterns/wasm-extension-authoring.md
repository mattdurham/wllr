---
type: Pattern
title: Authoring a WASM extension
description: Copy wllrsdk.go, write main.go with hooks, build to wasip1 — the SDK hides all WASM boilerplate.
tags: [wasm, extension, sdk, authoring]
timestamp: 2026-07-01T13:10:47Z
---

Extensions are `.wasm` modules loaded at startup. They must export `_init`,
`_on_event`, `_alloc`, `_free` — but `extensions/wllrsdk.go` provides all of
that. An author copies `wllrsdk.go` into their extension directory and writes
only `main.go`: register tools/commands in `init()`, subscribe to lifecycle
events with `On*` hooks, and call host helpers.

The host↔extension ABI is defined in [docs/extensions.md](../../docs/extensions.md):

- **Events** (17): session_start, before_agent_start, before_provider_request,
  after_provider_response, on_tool_call, on_tool_result, message_start,
  message_end, before_tool_call, after_tool_call, on_command, context_usage,
  token, notify, log, tick, shutdown.
- **host_call methods**: subscribe, register_tool, register_command,
  send_message, set_status, notify, tool_result, get_os, store_set, store_get,
  abort, exec, read_file, write_file, append_file, get_env, ui_create_area,
  ui_update_area, ui_patch, ui_remove_area.
- **Permissions**: exec, file_open, file_read, file_write, network_read,
  network_write, ui.

# Example

```go
//go:build wasip1
package main

import "encoding/json"

func init() {
    RegisterTool("my_tool", "What it does",
        json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`))
    OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
        if name != "my_tool" {
            return "", false // pass through
        }
        var in struct{ Input string `json:"input"` }
        json.Unmarshal(input, &in)
        return "result: " + in.Input, false
    })
}
func main() {}
```

Build: `GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o my-extension.wasm .`

# Applies To

- [extension package](../packages/extension.md), [sdk package](../packages/sdk.md)
- All built-in and installed extensions in [packages/index.md](../packages/index.md)
