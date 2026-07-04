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
