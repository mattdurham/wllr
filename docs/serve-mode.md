# `wllr serve` — the HTTP/SSE mode

`wllr serve [addr]` runs the same engine the TUI runs, with the renderer off and
an HTTP API (plus a self-contained HTML front end) on instead of the interactive
program.

```sh
wllr serve                       # binds 127.0.0.1:8765 (loopback only)
wllr serve 127.0.0.1:9000        # explicit address
```

It is deliberately **not** a lighter-weight code path. `main()` performs the full
wiring (provider, pool, extension host, built-in extensions, MCP bridge, native
tools) before reaching serve mode, exactly as `--exec` does. The only difference
is what drives the engine: a network API rather than a terminal.

## Binding and auth

- **Address.** Defaults to `127.0.0.1:8765` (loopback only), so the API is not
  exposed on the network unless the operator asks for it.
- **Auth.** Set `WLLR_API_TOKEN` to require a bearer token on every route,
  including the event stream. Clients may present it as
  `Authorization: Bearer <token>` or `?token=<token>`. An empty token disables
  auth. Exposing the API on a non-loopback address should always be paired with
  a token.

## What the engine exposes

Serve mode installs the harness's *real* extension bridges backed by the
server's renderer-neutral `HubSink`. Extension UI (notifications, tool results,
scene-graph patches) is published to the stream instead of dropped, and — because
the agent bridge carries the sub-agent spawner — `create_agent` and the team
tools work headlessly. Only the sink differs from the TUI's `teaSink`, so the
browser sees the same UI requests the terminal would render.

The session-boot dispatches (`session_start`, `model_changed`) that the TUI's
`Init` performs are run explicitly, since a headless front end has no tea
program. A browser `/model` selection applies through the same `apply*` methods
the terminal uses, and dispatches `model_changed` so extensions such as the
bundled `statusline` report the new provider/model.

## HTTP surface

| Method | Path                 | Purpose |
|--------|----------------------|---------|
| GET    | `/api/health`        | Liveness, `protocol`, `last_seq`, subscriber and drop counts |
| GET    | `/api/agents`        | Agent tree: id, name, running, inbox, history length, tools, compactions |
| GET    | `/api/history`       | Transcript for `?agent=` with `?limit=`/`?offset=` |
| GET    | `/api/context`       | The `/context` breakdown for `?agent=` |
| POST   | `/api/submit`        | `{"agent","content"}` → starts a turn (`202`) |
| POST   | `/api/cancel`        | `{"agent"}` → cancels the active turn |
| GET    | `/api/inbox`         | Snapshot of `?agent=`'s queued messages |
| POST   | `/api/inbox/clear`   | `{"agent"}` → discard queued messages |
| POST   | `/api/inbox/edit`    | `{"agent","index"|"message_id","content"}` → edit one queued message |
| GET    | `/api/stream`        | Server-Sent Events stream (see below) |
| GET    | `/api/scene`         | Every scene area with its rendered HTML |
| GET    | `/api/models`        | Selectable models (provider-qualified keys), the selection `callback`, and the live `current` provider/model |
| GET    | `/api/commands`      | Registered slash commands for autocomplete and help |
| POST   | `/api/ui/callback`   | `{"callback","value"}` → routes a UI selection back to the engine |
| GET    | `/`                  | Self-contained HTML front end; any other non-API path → `404` |

An omitted `agent` resolves to the root agent (`main`); an unknown agent yields
`404`. Every handler that touches the pool answers `503` when no pool is
configured, rather than dereferencing a nil pool.

### Event stream (`GET /api/stream`)

One frame = one JSON object on a `data:` line. The envelope and kind vocabulary
are defined in `modules/wire` and shared with `modules/client`. A `since` query
parameter replays retained events after that sequence number, so a reconnecting
client resumes without gaps (subject to the server's backlog limit). Each event
carries a monotonic `Seq`, so a client that observes a gap can re-sync from
`GET /api/history`.

A slow client never stalls the engine: publishing is non-blocking, a full
subscriber buffer drops the event, and the drop is counted (`/api/health`).

### Browser front end (`GET /`)

The server serves a self-contained HTML page that consumes the REST/SSE surface
above: transcript, queued-messages and tool-activity panes, slash-command
autocomplete, model/thinking/provider-routing pickers, and the scene-graph
areas. Message bodies and scene text are rendered server-side (markdown →
ANSI → escaped HTML) and inserted as pre-escaped fragments, so the browser shows
exactly what the terminal shows.

## Multi-instance client

`wllr-multiplex` (see `cmd/wllr-multiplex`) connects to N running `wllr serve`
instances, merges their event streams, and reads/writes through one tool. It is a
plain HTTP/SSE client — it does not embed the engine — so it is safe to run many
of them against many servers.

```sh
wllr-multiplex -instance local=http://127.0.0.1:8765 -instance ci=http://127.0.0.1:8766 tail
wllr-multiplex -config instances.json agents
wllr-multiplex -config instances.json submit local/main "summarize README.md"
```

Instances are declared with repeatable `-instance id=url` flags or a JSON config
file (`[{"id","url","token"}]`). Because two instances can both have an agent
named `main`, every merged event is tagged with its instance ID and addressed as
`instance/agent`.

## See also

- [docs/exec-mode.md](exec-mode.md) — the other non-interactive mode, a single
  turn to stdout.
- `modules/server/SPECS.md` — the package's invariants and full API contract.
- `modules/client/SPECS.md` — the client and multiplexer contract.
