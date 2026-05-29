package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// CommandMsg carries a parsed slash command.
type CommandMsg struct {
	Name string
	Args []string
}
