package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// agentWakeupMsg is sent when OnAgentRun triggers a main-agent turn (e.g.
// a sub-agent called send_message). It sets m.streaming=true so the TUI
// shows the "working." indicator during the agent-triggered turn.
type agentWakeupMsg struct {
}
