# modules/wire — BENCHMARKS

## Binary size (the reason this package exists)

The envelope is deliberately a leaf package so a client does not link the engine.
Approximate stripped binary sizes on the development host:

| Binary | Imports | Size |
|--------|---------|------|
| `wllr` (engine) | `cmd` → agent, extension, harness, server … | ~93 MB |
| `wllr-multiplex` (client) | `modules/client` → `modules/wire`, `modules/sdk` | ~11 MB |

If the envelope lived in `modules/server`, the client would link `modules/agent`
and the provider SDKs, converging on the engine's size. This gap is the concrete
justification for `modules/wire` (see NOTES.md).

## Runtime

None. Decoding a frame is a single `encoding/json.Unmarshal` into a flat struct;
the envelope adds no measurable overhead over the JSON itself.
