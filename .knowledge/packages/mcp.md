---
type: Go Package
title: mcp
description: Bridge between the wllr extension host and external MCP (Model Context Protocol) server subprocesses.
resource: ./modules/mcp
tags: [mcp, subprocess, tools, bridge]
timestamp: 2026-07-01T13:10:47Z
---

The `mcp` package bridges the extension host to external MCP server
subprocesses. MCP servers expose tools over a JSON-RPC protocol; this package
manages their lifecycle and surfaces their tools into wllr, so an MCP server's
capabilities become callable tools in a turn.

# Specification

- [Contracts and invariants](../../modules/mcp/SPECS.md)
- [Design decisions](../../modules/mcp/NOTES.md)
- [Test plan](../../modules/mcp/TESTS.md)
- [MCP integration guide](../../docs/mcp-integration.md)

# Key Interfaces

- The MCP subprocess bridge satisfying `extension.MCPBridge`.

# Dependencies

- `sdk`, `extension` (MCPBridge)
