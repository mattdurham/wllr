package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

const (
	commandHelp   = "help"
	commandModel  = "model"
	commandModels = "models"
	keyEsc        = "esc"
	keyEnter      = "enter"
	// keyKillAgent hard-kills the highlighted agent from the /agents tree.
	// `x` reads as an eXterminate/cut affordance and does not collide with any
	// other tree key (q/esc close, enter focus, space/arrows fold and move).
	keyKillAgent = "x"
	// keyClearQueue discards the main agent's queued messages (issue #48).
	// ctrl+x is deliberately chosen: not a zellij default binding, not a
	// terminal signal or flow-control key, and not a readline editing key.
	keyClearQueue = "ctrl+x"
	borderRounded = "rounded"
)
