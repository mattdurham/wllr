package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ToolLogEntry records one tool call during the current agent turn.
type ToolLogEntry struct {
	Name    string
	Preview string
	Done    bool
	IsError bool
}
