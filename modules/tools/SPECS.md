# tools — Specification

## 1. Purpose

The `tools` package adapts `sdk.Tool` values (the wllr-internal tool schema) into
`fantasy.AgentTool` values (the LLM provider abstraction). It is a pure adapter —
no I/O, no state, no goroutines.

## 2. Primary Types

### sdkToolAdapter

Implements `fantasy.AgentTool`. Wraps an `sdk.Tool` schema and an `*extension.Host`
for tool execution dispatch.

**Invariants:**
1. A nil `*extension.Host` produces an error response on Run — never panics.
2. ParseInputSchema returns empty maps for nil or empty JSON; never returns nil maps.
3. Run dispatches via `host.ExecuteTool`; errors from ExecuteTool become tool error responses, not Go errors.

### BuildFantasyTools

Converts all registered tools from an `*extension.Host` into `[]fantasy.AgentTool`.

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
