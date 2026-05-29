package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Registry holds registered slash commands and dispatches them.
type Registry struct {
	commands map[string]Command
}
