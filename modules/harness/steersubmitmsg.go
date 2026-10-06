package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// steerSubmitMsg carries /steer guidance from the command handler to the
// update loop, which routes it into the focused agent via submitSteer.
type steerSubmitMsg struct {
	Content string
}
