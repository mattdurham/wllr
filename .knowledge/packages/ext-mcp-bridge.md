---
type: Installed Extension
title: mcp-bridge (installed)
description: Extension-side glue for driving MCP server subprocesses through the MCPBridge.
resource: ./extensions/mcp-bridge
tags: [installed, mcp, bridge]
timestamp: 2026-07-01T13:10:47Z
---

The `mcp-bridge` installed extension is the extension-side counterpart to the `mcp` module, driving MCP server subprocesses through the MCPBridge. Unlike most extensions, it does **not** use `wllrsdk.go` — it uses raw WASM `host_call` imports directly, because it needs tighter control over memory and JSON-RPC framing for the MCP subprocess protocol.

# Source

- [extensions/mcp-bridge](../../extensions/mcp-bridge) — installed to `~/.wllr/extensions/mcp-bridge/` via `make extensions`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
- [mcp package](../packages/mcp.md)
