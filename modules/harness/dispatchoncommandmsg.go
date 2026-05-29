package harness

// dispatchOnCommandMsg is sent when an extension-registered slash command is invoked.
// The harness dispatches EventOnCommand to all subscribed extensions.
type dispatchOnCommandMsg struct {
	Name string
	Args []string
}
