package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// CommandListerFunc adapts a function to CommandLister.
type CommandListerFunc func() []CommandInfo

// ListCommands implements CommandLister.
func (f CommandListerFunc) ListCommands() []CommandInfo { return f() }
