package harness

// Registry holds registered slash commands and dispatches them.
type Registry struct {
	commands map[string]Command
}
