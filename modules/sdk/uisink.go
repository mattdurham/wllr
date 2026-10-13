package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UISink is the renderer-neutral seam between the engine and a front end.
//
// The harness and its extension bridges used to reach straight for a
// *tea.Program, so a front end without a terminal could not drive the engine at
// all. Every user-visible effect now flows through this interface instead: the
// interactive TUI supplies a sink that turns effects into bubbletea messages,
// the HTTP/SSE server supplies one that publishes frames to connected clients,
// and an embedder supplies one backed by ordinary callbacks. None of this is
// bubbletea-specific, so a second front end reuses the same engine rather than
// porting widget code.
//
// Implementations MUST be safe for concurrent use: methods are called from
// concurrent WASM dispatch goroutines and from agent streaming goroutines, not
// only from a UI event loop. Implementations MUST NOT block indefinitely — a
// slow or absent front end must not stall a turn.
type UISink interface {
	// Notify surfaces a transient human-readable notice.
	Notify(text string)
	// ShowModal opens a text modal overlay.
	ShowModal(text string)
	// ShowPicker opens an interactive picker. Callback is an opaque id routed
	// back to the requesting extension when an item is chosen.
	ShowPicker(params ShowPickerParams)
	// ShowAgentTree opens the interactive agent-tree overlay; a node selection
	// is routed back as the Callback with the selected agent id.
	ShowAgentTree(params ShowAgentTreeParams)
	// FocusAgent targets user input and the transcript at an agent. An empty id
	// means the root agent.
	FocusAgent(id string)
	// ShowTextInput opens a single-line text prompt. callback is an opaque id
	// routed back with the entered value.
	ShowTextInput(title, placeholder, initialValue, callback string)
	// Abort stops rendering the in-progress stream. It does not cancel the turn.
	Abort()
	// SetStatus sets a status-line key; an empty value clears it.
	SetStatus(key, value string)
	// StatusInfo returns the current snapshot a statusline renders.
	StatusInfo() StatusInfo
	// Submit enters content as a user turn. display, when non-empty, is shown in
	// place of content (for example a skill-invocation label).
	Submit(content, display string)
	// SetModel switches the active model and/or thinking level. value may be a
	// model id or a configured tier name. An empty thinking leaves it unchanged.
	SetModel(value, thinking string) error
	// RegisterCommand registers a slash command with the front end. A front end
	// with no command surface returns nil.
	RegisterCommand(name, desc string, instant bool) error
	// Console appends a line to the console pane, or clears it when clear is true.
	Console(line string, clear bool)
	// ResetHistory replaces the front end's transcript with messages.
	ResetHistory(messages []Message)
	// Token streams assistant text for the root transcript.
	Token(text string)
	// ToolCallStart announces a tool invocation beginning.
	ToolCallStart(agentID, id, toolName, input string)
	// ToolCallDone reports a tool completion with its output.
	ToolCallDone(agentID, id, toolName, output string, isError bool)
	// StreamDone signals the end of the root agent's turn.
	StreamDone(err error)
	// AgentWakeup signals that an agent has begun working off the UI loop, so a
	// front end can show a "working" indicator.
	AgentWakeup(agentID string)
	// SceneCreate registers a scene-graph area.
	SceneCreate(area UIArea) error
	// ScenePatch applies a batch of scene-graph ops to an area atomically.
	ScenePatch(params UIPatchParams) error
	// SceneUpdate updates an area's sizing constraints and/or weight.
	SceneUpdate(params UIUpdateAreaParams) error
	// SceneRemove removes an area and its scene graph.
	SceneRemove(id string)
}
