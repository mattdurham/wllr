# Logging Extension

Bundled (built-in) WASM extension that writes wllr's log file. It is the log
sink that replaced the file-writing half of the old Go-core `teeHandler`.

## What it does

- Subscribes to the `log` event (`OnLog`).
- On each batch of records, formats them in slog's text style
  (`time=… level=… msg=… key=value …`) and appends them to a per-run file
  `~/.wllr/logs/<timestamp>.log` via the `append_file` host call.
- One timestamped file per process run, Debug level — identical to the
  behaviour of the core handler it replaced.

## Why it's an extension

Moving log-file writing into a WASM component makes the sink **swappable** and,
more importantly, makes logs **hookable**: any extension can subscribe to `log`
and ship records elsewhere (e.g. the `otel-traces` extension, a remote
collector, a filter) without touching the core.

## How logs reach it

The Go core's slog handler (`cmd/loghandler.go`) batches records (~30ms) and
dispatches them as the `log` event. Records emitted before this extension
subscribes are held in a bootstrap ring buffer and flushed once it loads, so
startup logs are not lost. The dispatch path is reentrancy-guarded: logs emitted
*while* dispatching `log` are dropped, preventing a log→dispatch→log loop.

## Permissions

Requires `file_write` (for `append_file`). As a built-in it is trusted and
receives all permissions automatically.

## Writing your own log sink

```go
//go:build wasip1
package main

func init() {
    OnLog(func(records []LogRecord) {
        for _, r := range records {
            // ship r.Level / r.Message / r.Attrs somewhere
        }
    })
}
func main() {}
```

Do **not** call `Log`/`Logf` from inside `OnLog` — the host's reentrancy guard
drops those records.
