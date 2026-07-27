# Sigil Extension for wllr

This extension provides a bridge between Grafana's sigil-sdk and wllr, allowing agents to instrument their LLM generations and tool calls for observability with Grafana Cloud's AI Observability.

## Features

The extension exposes the following tools:

- **sigil_start_generation** - Start a generation span for LLM observability
- **sigil_end_generation** - End a generation span; the SDK batches and exports it
- **sigil_set_result** - Set the result for a generation (messages and token usage)
- **sigil_start_tool_execution** - Start a tool execution span for observability
- **sigil_end_tool_execution** - End a tool execution span; the SDK batches and exports it

## Usage Pattern

### For LLM Generations

```json
// 1. Start generation
{
  "tool": "sigil_start_generation",
  "arguments": {
    "conversation_id": "conv-123",
    "agent_name": "main",
    "model_provider": "anthropic",
    "model_name": "claude-sonnet-4-5"
  }
}
// Returns: {"generation_id": "gen-456", ...}

// 2. Make your LLM call (using conversation_id if provided)

// 3. Set result
{
  "tool": "sigil_set_result",
  "arguments": {
    "generation_id": "gen-456",
    "output": "model response text"
  }
}

// 4. End generation
{
  "tool": "sigil_end_generation",
  "arguments": {
    "generation_id": "gen-456",
    "output": "model response text"
  }
}
```

### For Tool Executions

```json
// 1. Start tool execution
{
  "tool": "sigil_start_tool_execution",
  "arguments": {
    "tool_name": "search",
    "conversation_id": "conv-123"
  }
}
// Returns: {"execution_id": "exec-101", ...}

// 2. Execute the tool

// 3. End tool execution
{
  "tool": "sigil_end_tool_execution",
  "arguments": {
    "tool_id": "exec-101",
    "arguments": '{"query": "test"}',
    "result": "[...]",
    "result": "tool result"
  }
}
```

## Integration with Grafana Cloud

To send instrumentation data to Grafana Cloud:

1. Set environment variables:
   - `AGENTO11Y_ENDPOINT` - Grafana Cloud Sigil endpoint
   - `AGENTO11Y_PROTOCOL` - Protocol (http or grpc)
   - `AGENTO11Y_AUTH_MODE` - Auth mode (none, tenant, bearer, basic)
   - `AGENTO11Y_AUTH_TENANT_ID` - Grafana tenant ID for basic auth
   - `AGENTO11Y_AUTH_TOKEN` - Basic password or bearer token
   - `DEBUG_LOG=1` - logs generation lifecycle, export failures, and HTTP response statuses

2. The extension will automatically configure sigil-sdk with these settings

3. Start/End generation/tool_execution tools will export to Grafana Cloud

## Building

```bash
# Build the extension
cd extensions/sigil
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o sigil.wasm .

# Build the main binary with sigil included
cd ../..
make build

# Or build everything including optional extensions
make extensions
```

## Extension Manifest

The extension is registered as a built-in WASM component in `cmd/main.go`.

## Implementation Details

- Uses the wllr extension SDK (`wllrsdk.go`) for host communication
- Uses the released Sigil Go SDK for recorder lifecycle, batching, retries, and export
- Adds Sigil instrumentation guidance to the system prompt

## Files

- `main.go` - Extension implementation
- `go.mod` - Go module definition with sigil-sdk dependencies
- `wllrsdk.go` - wllr extension SDK (copied from parent directory)
- `sigil.wasm` - Compiled WASM binary
