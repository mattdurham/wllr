package testutil

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ScriptedTurn is one complete LLM response: optional text and/or zero or more
// tool calls. When a scripted turn is consumed by FakeLM.Stream, text is emitted
// as text start/delta/end parts, then each tool call is emitted as a single
// StreamPartTypeToolCall part, then a finish part.
type ScriptedTurn struct {
	// Text is optional freeform text emitted before any tool calls.
	Text string
	// ToolCalls are the tool calls to emit in order.
	ToolCalls []ScriptedToolCall
}
