package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"fmt"
)

const (
	lifecycleEventIdle     = "agent_idle"
	lifecycleEventFailed   = "agent_failed"
	lifecycleEventShutdown = "AGENT_SHUTDOWN"
)

type lifecycleNotification struct {
	Event     string `json:"event"`
	AgentID   string `json:"agent_id"`
	CreatorID string `json:"creator_id,omitempty"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
}

func (a *Agent) lifecycleTarget() string {
	if a.creatorID != "" {
		return a.creatorID
	}
	return MainAgentID
}

func (a *Agent) lifecycleMessage(event, message string, err error) (string, error) {
	n := lifecycleNotification{Event: event, AgentID: a.id, CreatorID: a.creatorID, Message: message}
	if err != nil {
		n.Error = err.Error()
	}
	b, marshalErr := json.Marshal(n)
	if marshalErr != nil {
		return "", fmt.Errorf("marshal agent lifecycle notification: %w", marshalErr)
	}
	return string(b), nil
}
