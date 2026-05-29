# testutil — Test Specifications

The testutil package is itself a test helper — it does not have its own test suite beyond compilation verification.

## Invariants verified by consumers

- FakeLM streams preset responses correctly (verified by agent_test.go)
- RecordedCall captures all invocations (verified by harness integration tests)
