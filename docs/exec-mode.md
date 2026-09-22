# `--exec` — non-interactive runs

`wllr --exec "<prompt>"` runs a single agent turn and exits.

```sh
wllr --exec "summarise the failing tests in this repo"
```

The assistant's final response is written to stdout; logs go to stderr. The
process exits as soon as the turn finishes, so it is safe to use in scripts and
CI:

```sh
out=$(wllr --exec "what changed in the last commit?")
```

## It is the same run as the interactive TUI

`--exec` is deliberately **not** a lightweight execution path. It builds the same
harness model, spawns the main agent in the same pool, loads the same extensions,
and calls the same `SetProgram` wiring as the TUI. The only differences are:

| | Interactive | `--exec` |
|---|---|---|
| Renderer | full TUI | disabled |
| Input | stdin | none |
| Session | recorded | recorded |
| Lifetime | until you quit | until the turn completes |

Because the wiring is shared, everything that applies interactively also
applies here:

- extension lifecycle and tool events
- the tool permission chain
- the `before_provider_request` chain — PII redaction, request blocking, and
  model rerouting all take effect
- session recording under `~/.wllr/sessions/`, so an `--exec` run is resumable
  with `/history`
- token and usage metrics

That matters for enforcement: a run that skipped the provider-request chain
would silently bypass whatever policy extensions had installed, so `--exec` is
not a way around them.

## Differences from the TUI worth knowing

- **No transcript on screen.** Only the final response text goes to stdout; tool
  activity, notifications, and streamed partials are not printed. Inspect the
  session file under `~/.wllr/sessions/` for the full record.
- **No interactive approvals.** A tool that would prompt for permission cannot
  ask, so configure permissions ahead of time (see `extensions/permissions`).
- **One turn.** Sub-agents spawned during the run are awaited as part of the
  turn; there is no follow-up prompt afterwards.
