# modules/client — TESTS

Tests use `httptest` with a small `fakeInstance` (client_test.go) that serves the
REST endpoints and a scripted SSE `/api/stream`. Stream behaviour is scripted
**per connection**, so a test can make a connection drop and then observe
reconnection and the resume cursor. No real engine or socket is required.

## client_test.go

| Test | Intent |
|------|--------|
| `TestNewValidatesConfig` | Missing ID/BaseURL rejected; trailing slash trimmed |
| `TestClientHealth` | `/api/health` decodes, protocol included |
| `TestClientAgentsModelsHistory` | Agents/models/history decode, including rendered `HTML` |
| `TestClientSubmitCancelCallback` | POST paths reach the server with the right bodies |
| `TestClientSendsBearerToken` | `Authorization: Bearer …` is sent when configured |
| `TestClientUnauthorizedSurfacesAsHTTPError` | A 401 is a typed error, and `IsUnauthorized` is true |
| `TestFollowCollectsUntilCancel` | Ready + scripted events arrive, in order, tagged with the instance |
| `TestFollowReconnectsAndResumesFromCursor` | A drop reconnects with `since` = last delivered seq; no token delivered twice |
| `TestFollowFlagsGapOnSequenceDiscontinuity` | A seq skip sets `Tagged.Gap` |
| `TestMultiplexerRejectsDuplicateAndEmpty` | No instances, or a duplicate ID, is an error |
| `TestMultiplexerMergesInstances` | **Core acceptance:** two instances merge; `alpha/main` and `beta/main` are unambiguous |
| `TestMultiplexerEventsClosedAfterRun` | The merged channel closes when `Run` returns |
| `TestMultiplexerReturnsOnFollowerRejection` | **One follower's `OnConnect` rejection returns `Run`** without an external cancel; the other follower unwinds |
| `TestMultiplexerStopUnblocksSaturatedConsumer` | **`-race`:** with a saturated, unread merged buffer, `Stop` unblocks the follower so `Run` returns |
| `TestMultiplexerInstancesSorted` | Instance IDs are returned sorted |
| `TestAgentKeyAndParse` | `AgentKey`/`ParseAgentRef`/`AgentRef.String` round-trip |

## stream_test.go

| Test | Intent |
|------|--------|
| `TestStreamSkipsHeartbeatsAndComments` | `: keep-alive` and blank separators are skipped; data frames decode |
| `TestStreamRecordsProtocolFromReady` | The `ready` frame's protocol is captured |
| `TestStreamRejectsBadJSON` | A malformed frame is an error, not a silent skip |
| `TestOpenStreamMapsNon200ToHTTPError` | A 401 stream open is a typed `*HTTPError` |

## backoff_test.go

| Test | Intent |
|------|--------|
| `TestBackoffGrowsExponentiallyAndCaps` | Delays follow base·2^n and cap at Max |
| `TestBackoffResetReturnsToBase` | `Reset` returns the schedule to Base |
| `TestBackoffDefaults` | Zero-value `Backoff` starts at `DefaultBackoffBase` |
| `TestBackoffJitterIsBounded` | Jitter stays non-negative and is deterministic for a fixed seed |
| `TestJitterZeroReturnsUnchanged` | `frac == 0` disables jitter |

## Live verification

Beyond unit tests, the multi-instance path was verified against two real
`wllr serve` instances: `wllr-multiplex agents` listed both, `submit` to each was
accepted, and `tail` merged both streams (`alpha/main`, `beta/main`) with a
completed turn visible in each instance's history.
