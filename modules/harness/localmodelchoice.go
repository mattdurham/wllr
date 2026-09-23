package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// LocalModelChoice is one model discovered by probing a local OpenAI-compatible
// endpoint's /models listing.
type LocalModelChoice struct {
	ID            string
	Name          string
	ContextWindow int64
}
