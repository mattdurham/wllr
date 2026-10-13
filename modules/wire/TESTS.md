# modules/wire — TESTS

`wire` is a set of type and constant declarations, but the declarations *are* the
contract, so they have a direct test: the stream contract suite in
`wire_test.go`.

## Stream contract (`wire_test.go`)

The suite parses `wire.go` with `go/parser` to discover every constant declared
with type `EventKind`. Reading the source (rather than a hand-maintained list in
the test) means a newly added kind is caught automatically instead of silently
skipping coverage.

- `TestEveryDeclaredKindHasASample` — every declared kind has a sample `Event`
  and every sample has a declared kind (catches an un-sampled new kind and a
  stale sample).
- `TestEventKindValuesUniqueAndWellFormed` — values are unique and
  lower_snake_case, so a copy-paste collision cannot merge two kinds into one
  client switch arm.
- `TestEveryKindRoundTripsJSON` — each sample marshals, unmarshals back, and
  re-marshals byte-identically with the same `Kind`/`Seq`. This is the concrete
  contract: what the server writes, a client can read. Samples populate every
  field a kind can carry, so a dropped or renamed JSON tag fails here.
- `TestEveryKindDocumentedInSpecs` — each kind value appears in `SPECS.md`'s
  event-kinds table, so the protocol doc cannot drift from the code.
- `TestProtocolVersionDeclared` — the version a client gates on is `>= 1` and
  matches what the `ready` sample advertises.
- `TestKindConstantsSortedStable` — sanity guard that the parser walk found the
  full vocabulary (not an empty or truncated set).

## Covered indirectly

- Shape/serialization end-to-end is also exercised by `modules/server` (which
  marshals these frames) and `modules/client`
  (`TestStreamSkipsHeartbeatsAndComments`,
  `TestStreamRecordsProtocolFromReady`, `TestStreamRejectsBadJSON`).
- The alias identity (that `server.Event` and `wire.Event` are one type) is
  enforced by the compiler: `modules/server` declares the aliases, so any drift
  fails to build.

If a non-declaration helper is ever added here (for example a frame constructor),
it should get its own direct unit test in this package alongside the suite.
