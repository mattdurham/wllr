# mcp — Design Notes

## 1. EventBus-based dispatch

*Added: original*

**Decision:** MCP tool calls are intercepted via the EventBus (Go-native pub/sub), not via WASM extension dispatch.

**Rationale:** MCP servers are Go-native subprocesses, not WASM modules. Using the EventBus allows the MCP bridge to intercept tool calls without going through the WASM ABI, which would add unnecessary overhead and complexity.

**Consequence:** MCP and WASM extensions coexist without interference. MCP tools appear in the registered tool list alongside WASM-registered tools.
