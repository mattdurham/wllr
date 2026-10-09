package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// TokenSink is the subset of UISink the streaming path needs. Keeping it narrow
// lets the token batcher and the main-agent callbacks be driven by the full sink
// or by a small test double.
type TokenSink interface {
	Token(text string)
}
