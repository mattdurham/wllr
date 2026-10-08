# tools — Specification

## 1. Purpose

The `tools` package adapts `sdk.Tool` values (the wllr-internal tool schema) into
`fantasy.AgentTool` values (the LLM provider abstraction). It is a pure adapter —
no I/O, no state beyond the watchdog duration.

## 2. Primary Types

### sdkToolAdapter

Implements `fantasy.AgentTool`. Wraps an `sdk.Tool` schema and a `ToolExecutor`
(concrete `*extension.Host`, or a fake in tests) for tool execution dispatch.

**Invariants:**
1. A nil host produces an error response on Run — never panics.
2. ParseInputSchema returns empty maps for nil or empty JSON; never returns nil maps.
3. Run dispatches via `host.ExecuteTool`; errors from ExecuteTool become tool error responses, not Go errors.
4. **Tool-call watchdog**: every Run bounds ExecuteTool with `toolCallTimeout`
   (default 10m, settable via SetToolCallTimeout / config `tool_call_timeout`).
   On breach the model receives a `timed out and was abandoned` error response
   and the turn proceeds; the ExecuteTool goroutine is abandoned (buffered
   result channel — it can never block on send). A wedged host leaks that
   goroutine for the process lifetime; agent hard-kill ends it with the process.
5. When the *parent* context is already done at breach, the response says
   `tool call cancelled` (turn ending), never `timed out`.

### ToolExecutor

Minimal interface satisfied by `*extension.Host` (ExecuteTool + RegisteredTools).
`extension.ToolResult` is a type alias of the host's internal result struct.

### BuildFantasyTools

Converts all registered tools from a `ToolExecutor` into `[]fantasy.AgentTool`.

**Invariants:**
4. Returns nil (not empty slice) when host is nil or no tools are registered.
5. Tools that fail ParseInputSchema are skipped with a warning; they do not block other tools.
6. Each returned AgentTool holds a reference to the host; the host must outlive the tool list.

## 3. Input Schema Parsing

`ParseInputSchema` parses a JSON Schema `{"properties":{...},"required":[...]}` object.

**Invariants:**
7. Unknown keys in the schema are silently ignored.
8. Missing "properties" key → empty params map (not an error).
9. Missing "required" key → non-nil empty required slice (not an error; marshals as `[]`, not `null`).
10. Invalid JSON → error returned, no partial result.
