package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// ResetHistoryMsg asks the TUI to replace the main agent's history and
// rebuild the chat view from the supplied messages.
type ResetHistoryMsg struct {
	Messages []sdk.Message
}
