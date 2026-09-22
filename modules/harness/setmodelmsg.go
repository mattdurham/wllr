package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Internal message types used by built-in command handlers.
type setModelMsg struct {
	Model string
	// Thinking, when set, is a provider-agnostic reasoning level (e.g. "high")
	// applied after the model switch. Used by skills whose frontmatter declares
	// both model and thinking.
	Thinking string
}
