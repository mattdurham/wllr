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
	// MessageTypeProtocol is an internal protocol message (e.g. the agent_idle
	// or agent_failed lifecycle notifications the host sends to a parent). It
	// IS model-visible — the orchestrator reads it to learn that a child
	// finished — but it is not conversation: it must not be rendered as a chat
	// bubble, and it is not recorded as a user prompt.
	MessageTypeProtocol MessageType = "protocol"
	// MessageTypeSteer is a mid-turn steering message submitted with /steer.
	// It is fully model-visible and recorded in history — unlike
	// MessageTypeSteering, which the Go runtime consumes — but it is delivered
	// at a step boundary of the running turn (via the agent's PrepareStep
	// hook) instead of waiting for the next turn. When the agent is idle it is
	// consumed as the next turn's message like any other inbox message.
	MessageTypeSteer MessageType = "steer"
)

// Message is a chat message.
type Message struct {
	ID      string      `json:"id,omitempty"`
	Role    Role        `json:"role"`
	Content string      `json:"content"`
	Type    MessageType `json:"type,omitempty"`
}
