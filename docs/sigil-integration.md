# Sigil SDK Integration for wllr

## Overview

This document describes how Grafana's released sigil-sdk has been integrated into wllr as a WASM extension. The integration allows agents to instrument their LLM generations and tool calls for observability with Grafana Cloud's AI Observability.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        wllr Host                             │
│  ┌──────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │  Main    │  │   Agent      │  │   Extension Pool     │   │
│  │  Thread  │──│  Thread      │──│                      │   │
│  └──────────┘  └──────────────┘  │  ┌────────────────┐  │   │
│                                   │  │ sigil-extension │  │   │
│                                   │  └────────────────┘  │   │
│                                   └──────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                            │
                    ┌───────▼───────┐
                    │   host_call   │
                    └───────────────┘
                            │
┌─────────────────────────────────────────────────────────────┐
│                   WASM Extension (sigil)                     │
│  ┌──────────────┐   ┌──────────────┐   ┌────────────────┐ │
│  │ sigil-sdk    │──▶│ Recorders    │──▶│ Grafana Cloud  │ │
│  │ (Go module)  │   │ LLM Calls    │   │ (Sigil endpoint)│ │
│  └──────────────┘   └──────────────┘   └────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## Extension Structure

The extension is located at:
- `extensions/sigil/`

Key files:

1. **main.go** - Extension entry point and tool handlers
2. **go.mod** - Go module with sigil-sdk dependencies
3. **wllrsdk.go** - wllr extension SDK (copied from parent)
4. **manifest.json** - Extension metadata and tool definitions
5. **README.md** - User-facing documentation

## Tool Contracts

### sigil_start_generation

**Description:** Start a generation span for LLM observability.

**Input:**
- `conversation_id` (optional): Conversation ID for grouping generations
- `conversation_title` (optional): Human-readable title for the conversation
- `agent_name` (required): Name of the agent making the call
- `agent_version` (optional): Version of the agent
- `model` (required): Model name (e.g., "anthropic/claude-sonnet-4-5")
- `system_prompt` (optional): System prompt for the generation
- Other OpenTelemetry GenAI attributes

**Output:**
- `generation_id`: Generated ID for this generation
- `metadata`: Tool call metadata

### sigil_end_generation

**Description:** End a generation span and export it to Grafana Sigil.

**Input:**
- `generation_id` (required): Generation ID from sigil_start_generation
- `input_tokens`: Input token count
- `output_tokens`: Output token count
- Additional usage fields (cache_read, cache_write, reasoning)
- `model`: Model name for response
- `response_id`: Response ID from provider
- `finish_reason`: Finish reason from provider
- `error` (optional): Error message if generation failed

**Output:**
- `status`: "ended"
- `generation_id`: ID of the ended generation

### sigil_set_result

**Description:** Set the result for a generation (messages, tool calls).

**Input:**
- `generation_id` (required): Generation ID from sigil_start_generation
- `messages`: Array of messages in the generation
- `usage`: Token usage information

**Output:**
- `status`: "result_set"

### sigil_start_tool_execution

**Description:** Start a tool execution span for observability.

**Input:**
- `tool_name` (required): Name of the tool being executed
- `tool_call_id` (optional): Tool call ID for correlation
- Other tool execution metadata

**Output:**
- `execution_id`: Generated ID for this tool execution
- `status`: "started"

### sigil_end_tool_execution

**Description:** End a tool execution span and export it to Grafana Sigil.

**Input:**
- `execution_id` (required): Execution ID from sigil_start_tool_execution
- `arguments`: Tool call arguments as JSON string
- `result`: Tool execution result as JSON string
- Error information (optional)
- Token usage

**Output:**
- `status`: "ended"
- `execution_id`

## Build Process

### Building the Extension

```bash
cd extensions/sigil
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o sigil.wasm .
```

This creates `sigil.wasm` that can be loaded as a WASM extension. Sigil is explicitly
built with standard Go because its SDK is not TinyGo-compatible.

### Building wllr with sigil

The extension is embedded in the main binary:

```bash
cd ../..
make build
```

This builds the built-in extensions and embeds `sigil.wasm` in the main binary.

### Manual Build (if needed)

```bash
# 1. Copy sigil.wasm to builtins
cp extensions/sigil/sigil.wasm cmd/builtins/

# 2. Build wllr binary
cd ../..
go build -o dist/wllr ./cmd/
```

## Host Integration

The extension is integrated via the wllr extension system:

1. **Event Subscription**: On init, the extension adds concise Sigil guidance before agent turns
2. **Tool Registration**: Tools are registered through the wllr extension SDK
3. **Command Handler**: Slash command handler for `/sigil`
4. **System Prompt Injection**: Adds Sigil instrumentation guidance to system prompt
5. **SDK lifecycle**: Recorder start, result, and end calls are delegated to the Sigil SDK

## OpenTelemetry Integration

The extension uses sigil-sdk which emits:

1. **Traces**: Generation and tool execution spans
2. **Metrics**: Operation duration, token usage, time-to-first-token
3. **Export**: The Sigil SDK batches, retries, and sends normalized payloads to Grafana Cloud Sigil

## Usage Example

In an agent's conversation:

```
> You can use sigil tools to instrument your LLM calls for observability.

Example:

1. Instrument a generation:
   - Call sigil_start_generation with conversation_id, agent_name, model
   - Make your LLM call using the conversation_id if provided
   - Call sigil_set_result with messages and token usage
   - Call sigil_end_generation to export

2. Instrument a tool execution:
   - Call sigil_start_tool_execution with tool_name
   - Execute the tool
   - Call sigil_end_tool_execution with arguments and result

For more information, see /sigil.
```

## Grafana Cloud Setup

To enable exports to Grafana Cloud:

1. **Create a Grafana Cloud account** at https://grafana.com/cloud/
2. **Enable AI Observability** in your Grafana Cloud stack
3. **Get credentials**:
   - Tenant ID (instance ID)
   - API key
4. **Set environment variables**:
   ```bash
   export AGENTO11Y_ENDPOINT="https://sigil-prod-<region>.grafana.net"
   export AGENTO11Y_PROTOCOL="http"
   export AGENTO11Y_AUTH_MODE="basic"
export AGENTO11Y_AUTH_TENANT_ID="<tenant-id>"
export AGENTO11Y_AUTH_TOKEN="<api-key>"
# Optional: log export lifecycle and HTTP response statuses.
export DEBUG_LOG=1
   ```
5. **Configure OTEL exporters** for traces/metrics (if needed)

## Development

### Debugging the Extension

The extension can be debugged using:

```bash
# Build with verbose logging
go build -v ./extensions/sigil

# Check for issues
staticcheck ./extensions/sigil
nilaway -include-pkgs "github.com/mattdurham/wllr/extensions/sigil" ./extensions/sigil
```

### Adding Features

To add new features:

1. Add tool definitions in `init()`
2. Implement handler functions
3. Update tool contracts in README.md and manifest.json
4. Rebuild the extension: `make builtins`

### Testing

```bash
# Run unit tests
go test ./extensions/sigil/...

# Test extension loading
./dist/wllr --help | grep sigil

# Run full build
make precommit
```

## Performance Considerations

1. **WASM Module Size**: ~3.4MB due to sigil-sdk dependencies
2. **Memory Usage**: Sigil SDK buffers generations for batch export
3. **Network Calls**: Export is asynchronous via background worker

## Troubleshooting

### Extension fails to load
- Check that `sigil.wasm` is in `cmd/builtins/`
- Verify WASM compilation: `file cmd/builtins/sigil.wasm`

### Tools not available
- Check agent system prompt includes sigil guidance
- Verify tool registration via extension manifest

### Data not exporting to Grafana Cloud
- Check environment variables are set
- Verify network permissions for the extension

## Future Enhancements

1. **Metrics-only mode**: Export metrics without generation payloads
2. **Sampling**: Configurable sampling rate for high-volume scenarios
3. **Hooks/Guardrails**: Support sigil's preflight/postflight hooks
4. **Export batching**: Configure batch size and flush interval
5. **Error handling**: Better error handling for export failures
