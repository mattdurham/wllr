# testutil — Design Notes

## 1. Fake provider pattern

*Added: original*

**Decision:** Provide FakeProvider/FakeLM that stream preset text without touching the API.

**Rationale:** Agent and harness tests need deterministic LLM responses. Mocking the fantasy.LanguageModel interface allows tests to control what the LLM "says" without rate limits, latency, or cost.
