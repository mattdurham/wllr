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
