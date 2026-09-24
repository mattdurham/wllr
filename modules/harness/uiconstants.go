package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

const (
	commandHelp   = "help"
	commandModel  = "model"
	commandModels = "models"
	keyEsc        = "esc"
	keyEnter      = "enter"
	// keyClearQueue discards the main agent's queued messages (issue #48).
	// ctrl+x is deliberately chosen: not a zellij default binding, not a
	// terminal signal or flow-control key, and not a readline editing key.
	keyClearQueue = "ctrl+x"
	borderRounded = "rounded"
)
