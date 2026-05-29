package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// dispatchOnCommandMsg is sent when an extension-registered slash command is invoked.
// The harness dispatches EventOnCommand to all subscribed extensions.
type dispatchOnCommandMsg struct {
	Name string
	Args []string
}
