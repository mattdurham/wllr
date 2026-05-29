package harness

// ToolLogEntry records one tool call during the current agent turn.
type ToolLogEntry struct {
	Name    string
	Preview string
	Done    bool
	IsError bool
}
