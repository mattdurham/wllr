# testutil — Specification

## 1. Purpose

The `testutil` package provides test doubles for the fantasy LLM provider
abstraction, enabling agent and harness tests to run without hitting any real API.

## 2. Primary Types

### FakeProvider / FakeLM

Implements `fantasy.Provider` and `fantasy.LanguageModel` using preset responses.

**Invariants:**
1. FakeLM streams preset response tokens synchronously (no goroutines).
2. FakeLM records all calls via RecordedCall for assertion in tests.
3. FakeProvider.LanguageModel always succeeds for any model ID.
4. FakeLM never makes network calls.
5. Scripted turns (SetScript) are popped in FIFO order; once exhausted, FakeLM
   falls back to the preset text response list.
6. Each ScriptedTurn emits text parts (if Text is non-empty) before tool call
   parts, followed by a single finish part — all within one Stream invocation.
7. ScriptedToolCall is emitted as a single StreamPartTypeToolCall part (not as
   the streaming ToolInputStart/Delta/End sequence) so tool calls are dispatched
   atomically by the fantasy.Agent agentic loop.
