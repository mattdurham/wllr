# agent — Benchmark Specifications

## Metric Targets

| Benchmark | Target | Notes |
|-----------|--------|-------|
| `BenchmarkAgent_Submit_Throughput` | < 1ms overhead per turn (excluding LLM) | Measure pool.Send → goroutine launch |
| `BenchmarkPool_Spawn_1000` | < 10ms for 1000 sequential spawns | Pool lock contention |
| `BenchmarkInbox_DrainUnderContention` | No measurable overhead vs uncontended | inboxMu lock cost |

## Existing Benchmarks

None yet.

## Recommended Benchmarks

- `BenchmarkPool_Send_Concurrent` — N goroutines calling Send simultaneously; measures isRunning CAS contention
- `BenchmarkSpawner_Spawn` — full Spawn call with fake LM; baseline for sub-agent creation overhead
