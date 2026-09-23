package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TranscriptRebuildCallback is fired when an agent's history is rewritten in
// place (history restore) and the transcript-owning extension must re-render
// it from the agent's current history. Unlike AgentTreeCallback it does not
// switch focus and emits no notification.
const TranscriptRebuildCallback = "agents:transcript_rebuild"

// transcriptRebuildEvent builds the EventOnCommand asking the transcript-owning
// extension to re-render the named agent's transcript from its current history.
// An empty agentID means the root agent.
func transcriptRebuildEvent(agentID string) sdk.Event {
	payload, _ := json.Marshal(sdk.OnCommandPayload{Name: TranscriptRebuildCallback, Args: []string{agentID}})
	return sdk.Event{Type: sdk.EventOnCommand, Payload: payload}
}

// ResetHistoryMsg asks the TUI to replace the main agent's history and
// rebuild the chat view from the supplied messages.
type ResetHistoryMsg struct {
	Messages []sdk.Message
}
