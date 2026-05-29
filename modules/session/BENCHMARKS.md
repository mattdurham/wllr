# session — Benchmark Specifications

## Metric Targets

| Benchmark | Target | Notes |
|-----------|--------|-------|
| `BenchmarkWire_Start` | < 5ms with no WASM extensions | session_start dispatch overhead |
| `BenchmarkConversationSession_Submit` | < 100μs | pool.Send is non-blocking |

## Existing Benchmarks

None yet.

## Recommended Benchmarks

- `BenchmarkWire_StatusCallback` — cost of OnGetStatusInfo under concurrent reads (liveState.mu.RLock)
