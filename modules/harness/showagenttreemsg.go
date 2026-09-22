package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// ShowAgentTreeMsg asks the TUI to open the interactive agent tree overlay.
// Emitted by the agents extension via the show_agent_tree host call.
type ShowAgentTreeMsg struct {
	Title    string
	Callback string
	Nodes    []sdk.AgentTreeNode
}

// FocusAgentMsg asks the TUI to change which agent receives user input and owns
// the transcript. Emitted by set_focused_agent. An empty AgentID means the root.
type FocusAgentMsg struct {
	AgentID string
}
