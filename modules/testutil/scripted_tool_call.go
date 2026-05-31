package testutil

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "encoding/json"

// ScriptedToolCall describes a single tool call to emit from a ScriptedTurn.
type ScriptedToolCall struct {
	// ID is the tool call identifier (e.g. "tc1").
	ID string
	// Name is the tool function name (e.g. "tasklist_create").
	Name string
	// Input is the JSON-encoded tool input.
	Input json.RawMessage
}
