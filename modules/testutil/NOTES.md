# testutil — Design Notes

## 1. Fake provider pattern

*Added: original*

**Decision:** Provide FakeProvider/FakeLM that stream preset text without touching the API.

**Rationale:** Agent and harness tests need deterministic LLM responses. Mocking the fantasy.LanguageModel interface allows tests to control what the LLM "says" without rate limits, latency, or cost.

## 2. Scripted turns for tool call testing

*Added: 2026-05-30*

**Decision:** Add SetScript([]ScriptedTurn) to FakeLM, where each ScriptedTurn can emit text and/or multiple tool calls in a single Stream invocation. Tool calls are emitted as StreamPartTypeToolCall (atomic) rather than the streaming ToolInputStart/Delta/End sequence.

**Rationale:** Integration tests for agent coordination patterns (orchestrator/worker/IDLE) require scripted LLM responses that include tool calls. Using StreamPartTypeToolCall avoids the complexity of managing streaming tool input state in the fake, and is fully supported by the fantasy.Agent agentic loop (which handles both the streaming and atomic forms).

**Consequence:** FakeLM now has two response modes: preset text responses (original) and scripted turns (new). Scripted turns take priority and are consumed in FIFO order. After exhaustion, FakeLM falls back to preset text responses. NewFakeLM() and NewFakeLMWithResponses() are the two constructors; SetScript() configures the script on either.
