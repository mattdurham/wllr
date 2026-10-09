# modules/client — SPECS

`client` is a plain HTTP/SSE client for one or more running wllr engines. It
reads (health, agents, history, models), writes (submit, cancel,
UI-callback), and — via `Multiplexer` — follows **N instances at once** and
merges their event streams into one, tagging every event with its source.

It deliberately does **not** embed the engine: it links only `modules/wire` and
`modules/sdk`, so a client binary is small and many can run against many servers.

## Boundary

- **Depends on:** `modules/wire`, `modules/sdk`, the standard library.
- **Never depends on:** `modules/server`, `modules/agent`, `modules/harness`,
  `modules/extension`.
- **Never imported by:** any engine module. The dependency arrow points out of
  the engine, so the client can be omitted from an engine build.

## Why out-of-process

Each instance is a separate `wllr serve` process with its own config, session
directory, and extension host. Merging happens here rather than by co-hosting
sessions in one process, because the engine has process-global singletons (one
extension host, one config file, one per-cwd session dir) that do not co-exist.
The client is the integration point precisely because it is outside all of them.

## Core types

- **`Config`** — one instance: `ID` (unique, required), `BaseURL`, optional
  `Token`, optional `HTTPClient`. The `ID` is the only thing that disambiguates
  two instances that both have an agent named `main`.
- **`Client`** — one instance. Safe for concurrent use. REST helpers plus
  `Follow`.
- **`Multiplexer`** — N instances merged into one `<-chan Tagged`. `Run` follows
  all of them concurrently; `Events()` is the merged stream.
- **`Tagged`** — a `wire.Event` plus its `Instance`, plus a `Gap` flag.
  `AgentKey()` yields the globally unique `"<instance>/<agent>"`.
- **`Backoff`** — reconnect schedule (exponential, capped, jittered).

## Instance declaration

`InstanceFlags` and `BuildInstances` are the single instance-declaration parser
shared by `wllr-multiplex` and `wllr serve`, so the two binaries accept instances
identically:

- **`InstanceFlags`** collects repeated `id=url` command-line declarations and
  implements `flag.Value`, so a binary can bind it with `flag.Var` without this
  package importing `flag`.
- **`BuildInstances(flags, configPath, defaultToken, instanceFlag, configFlag)`**
  merges flag declarations (which take `defaultToken`) with a JSON file of
  `[{"id","url","token"}]` entries; an entry's own `token` wins over the default.
  `instanceFlag`/`configFlag` are the caller's flag names, so an error names a
  flag the caller has (`"instance"`/`"config"` for `wllr-multiplex`,
  `"peer"`/`"peers"` for `wllr serve`). Flag instances come first, file entries
  after. A flag declaration or a file entry missing `id`/`url` is an error (a
  file entry is validated like a flag entry, not silently dropped). Both sources
  whitespace-trim each `id`/`url`, so `-peer " ci = http://x "` and
  `[{"id":" ci ","url":" http://x "}]` yield the same instance. The empty
  case is an error wrapping `ErrNoInstances`, so a caller that may legitimately
  have none (a plain `wllr serve`) recognizes it with `errors.Is` and continues; a caller that
  needs at least one lets it bubble up.

The package still depends only on `wire`, `sdk`, and the standard library.

## Invariants

1. **Identity is always `instance/agent`.** An event's `Seq` is meaningful only
   within one instance; two instances' `Seq=1` are unrelated. `Tagged.Instance`
   (and `AgentKey`) is the only unambiguous key.
2. **`Token`/`AgentID` streams are per-instance.** A consumer must never merge
   sequence numbers or cursors across instances. `Multiplexer` keeps one cursor
   per follower.
3. **Reconnect resumes from the last *delivered* event, never from the `ready`
   frame.** `ready.Seq` is the server's last sequence number, not a delivered
   event; adopting it as the cursor would skip replay on the next reconnect
   (data loss). The resume cursor is the highest `Seq` actually handed to
   `OnEvent`. See `advance` in follow.go.
4. **A sequence discontinuity is surfaced, not hidden.** When `Seq` skips ahead
   of the cursor, `Tagged.Gap` is true so a consumer can re-fetch history to
   resynchronize. A zero `Seq` and the `ready` frame never report a gap.
5. **Reconnect never blocks the caller and never stampedes.** A dropped stream is
   retried with exponential backoff plus jitter, so N instances dropped together
   do not reconnect in lockstep. `Run` returns only on cancellation or an
   explicit `OnConnect` rejection.
6. **Non-2xx is a typed error.** A REST/stream response outside 2xx becomes
   `*HTTPError` with the status and body, so an auth failure is distinguishable
   from a transport fault (`IsUnauthorized`).
7. **The merged channel is closed when `Run` returns**, so a `range` over
   `Events()` terminates.
8. **A slow consumer applies backpressure, not data loss.** `Multiplexer` uses a
   bounded buffered channel; when full, follower goroutines block rather than
   drop. (Contrast the server's `Hub`, which drops to protect the engine — the
   client has no engine to protect.)
9. **Auth is per request.** A configured `Token` is sent as
   `Authorization: Bearer …` on every REST call and on the stream.

## REST surface used

| Method | Path | Helper |
|--------|------|--------|
| GET | `/api/health` | `Health` |
| GET | `/api/agents` | `Agents` |
| GET | `/api/history` | `History` |
| GET | `/api/models` | `Models` |
| POST | `/api/submit` | `Submit` |
| POST | `/api/cancel` | `Cancel` |
| POST | `/api/ui/callback` | `Callback` |
| GET | `/api/stream` | `Follow` / `Multiplexer.Run` |

## `wllr-multiplex`

`cmd/wllr-multiplex` is the reference application: `agents`, `models`, `history`,
`submit`, `cancel`, and `tail`. Instances come from repeatable
`-instance id=url` flags or a JSON `-config` file; `tail` merges every instance
into one prefixed line stream.
