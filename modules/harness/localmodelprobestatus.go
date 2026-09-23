package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// LocalModelProbeStatus classifies the outcome of probing a base URL, so the
// setup flow can distinguish a wrong/unreachable endpoint (which should be
// re-prompted, not silently downgraded) from one that responded with nothing
// usable (which falls back to manual entry).
type LocalModelProbeStatus int

const (
	// LocalModelProbeOK means the endpoint returned at least one usable model.
	LocalModelProbeOK LocalModelProbeStatus = iota
	// LocalModelProbeUnreachable means the request itself failed to complete
	// (bad URL, connection refused, timeout, DNS failure) — the endpoint is
	// likely misconfigured, so the user should be asked to re-enter it.
	LocalModelProbeUnreachable
	// LocalModelProbeEmpty means the endpoint was reached but returned no
	// usable models (empty list, unexpected shape) — falls back to manual entry.
	LocalModelProbeEmpty
)
