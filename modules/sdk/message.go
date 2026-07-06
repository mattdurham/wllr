package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// MessageType classifies the routing and visibility of a Message.
// An empty value (zero) is equivalent to MessageTypeNormal and is omitted from JSON
// for backward compatibility with existing serialized messages.
type MessageType string

const (
	// MessageTypeNormal is a regular user/assistant message visible to the LLM.
	MessageTypeNormal MessageType = "normal"
	// MessageTypeSteering is a guidance message injected by the orchestrator;
	// visible in history but filtered from the LLM context slice.
	MessageTypeSteering MessageType = "steering"
	// MessageTypeSystem is a Go-level control message (e.g. shutdown_request,
	// AGENT_SHUTDOWN). Never sent to the LLM; not recorded in history.
	MessageTypeSystem MessageType = "system"
)

// Message is a chat message.
type Message struct {
	ID      string      `json:"id,omitempty"`
	Role    Role        `json:"role"`
	Content string      `json:"content"`
	Type    MessageType `json:"type,omitempty"`
}
