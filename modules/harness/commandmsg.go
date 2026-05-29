package harness

// CommandMsg carries a parsed slash command.
type CommandMsg struct {
	Name string
	Args []string
}
