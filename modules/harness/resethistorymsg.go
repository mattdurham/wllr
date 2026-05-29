package harness

import "github.com/mattdurham/wllr/modules/sdk"

// ResetHistoryMsg asks the TUI to replace the main agent's history and
// rebuild the chat view from the supplied messages.
type ResetHistoryMsg struct {
	Messages []sdk.Message
}
