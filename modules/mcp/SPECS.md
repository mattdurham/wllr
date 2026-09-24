# mcp — Specification

## 1. Purpose

The `mcp` package implements a bridge between the wllr extension host and external
MCP (Model Context Protocol) server subprocesses. MCP servers expose tools as
JSON-RPC services; the bridge makes those tools available to the LLM agent.

## 2. Primary Types

### Extension

Subscribes to the extension host's EventBus to intercept `before_tool_call` events
for MCP-owned tools and dispatches them to the appropriate MCP server.

**Invariants:**
1. The Extension subscribes exactly once to the EventBus at construction.
2. Tools owned by MCP are identified by their `toolOwner` field containing the MCP server ID.
3. MCP tools are not registered via RegisterNativeTool; they appear in the tool list only after the MCP server sends its tool manifest.

### Bridge

Manages MCP server subprocess lifecycle: spawn, I/O, and teardown.

**Invariants:**
4. Each MCP server has a unique string ID within a Bridge instance.
5. Spawning a server with a duplicate ID returns an error.
6. Bridge sends tool results back through the host via the MCPBridge interface.

### Tool

Represents a tool advertised by an MCP server.

| Field          | JSON           | Type            | Description                                  |
|----------------|----------------|-----------------|----------------------------------------------|
| `Name`         | `name`         | string          | MCP tool name                                |
| `Description`  | `description`  | string          | Human-readable tool description              |
| `InputSchema`  | `inputSchema`  | json.RawMessage | MCP input JSON Schema, preserved verbatim    |
| `OutputSchema` | `outputSchema` | json.RawMessage | Optional MCP output JSON Schema, preserved verbatim |

### Config

The set of configured MCP servers, loaded from the mcp-bridge extension's own
config file, `<wllr home>/extensions/mcp-bridge/config.yaml` (YAML). The
file's contents ARE the config; there is no shared-config fallback.

**Invariants:**
7. `LoadConfig` reads only the mcp-bridge extension's own config.yaml and never
   the app config file; extension config lives beside the extension's WASM.
8. Servers are declared under the `servers` key; the legacy `mcpServers` key is
   accepted when `servers` is absent (the startup migration preserves it
   verbatim when moving an old mcp-bridge group into this file).
9. A missing config file yields an empty config, never an error: no MCP
   servers configured is a fine default state.
10. A malformed config file is an error, so a typo'd rule never silently
    disables every MCP server.
