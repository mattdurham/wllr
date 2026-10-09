package wire

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ProtocolVersion is the API/stream wire version. Clients that need a
// compatibility gate can compare it against GET /api/health.
const ProtocolVersion = 1

// EventKind identifies the type of a streamed Event. The set is intentionally
// small: it covers what a chat client needs (tokens, tool activity, turns,
// context, and agent membership) and maps 1:1 onto engine callbacks that
// already exist, so no field is synthesized.
type EventKind string

const (
	// KindReady is sent to a freshly connected client once, before any replay,
	// carrying the protocol version and the last sequence number.
	KindReady EventKind = "ready"
	// KindUserMessage marks a turn starting with the given prompt. Inbox is the
	// list of messages that were queued and drained into this turn.
	KindUserMessage EventKind = "user_message"
	// KindToken is one streamed text delta (Text).
	KindToken EventKind = "token"
	// KindTurnStarted marks a turn beginning before any output.
	KindTurnStarted EventKind = "turn_started"
	// KindTurnDone marks a turn completing; Err is non-empty on failure.
	KindTurnDone EventKind = "turn_done"
	// KindAssistantMessage carries a completed assistant message: Text is the raw
	// markdown and HTML is that content rendered for display (mdhtml.go). It
	// lets a client replace an in-progress streamed bubble with the rendered
	// form when the turn ends, exactly as the terminal transcript does — raw
	// while streaming, rendered once final.
	KindAssistantMessage EventKind = "assistant_message"
	// KindToolCall marks a tool dispatch (ToolCallID, ToolName, Input).
	KindToolCall EventKind = "tool_call"
	// KindToolResult marks a tool completion (ToolCallID, ToolName, Output, IsError).
	KindToolResult EventKind = "tool_result"
	// KindContextUsage reports the main agent's context-window usage.
	KindContextUsage EventKind = "context_usage"
	// KindNotify is a human-readable notice (for example a context compaction).
	KindNotify EventKind = "notify"
	// KindAgentSpawned marks an agent being added to the pool.
	KindAgentSpawned EventKind = "agent_spawned"
	// KindAgentClosed marks an agent being removed from the pool.
	KindAgentClosed EventKind = "agent_closed"
	// KindError reports a server-side problem that did not end a turn.
	KindError EventKind = "error"

	// The kinds below are produced by the renderer-neutral front-end sink: they
	// are the presentational half of what an extension can ask the front end to
	// draw. Serve mode installs the harness UIBridge with a HubSink (hubsink.go)
	// that translates each sink method into one of these frames, so a web client
	// sees the same UI requests the TUI would have rendered.

	// KindModal is a modal overlay with Text (UIBridge.ShowModal).
	KindModal EventKind = "modal"
	// KindPicker is an interactive picker overlay (Picker).
	KindPicker EventKind = "picker"
	// KindAgentTree is an agent-tree overlay (AgentTree).
	KindAgentTree EventKind = "agent_tree"
	// KindFocus sets the agent that owns input/transcript; Text is the agent ID
	// (empty means the root agent) (UIBridge.SetFocusedAgent).
	KindFocus EventKind = "focus"
	// KindTextInput is a free-form text prompt overlay (TextInput).
	KindTextInput EventKind = "text_input"
	// KindAbort tells the front end to stop showing an in-progress stream.
	KindAbort EventKind = "abort"
	// KindStatus reports a status-line key/value pair (Key, Value).
	KindStatus EventKind = "status"
	// KindConsole appends a line to the console pane (Text).
	KindConsole EventKind = "console"
	// KindConsoleClear clears the console pane.
	KindConsoleClear EventKind = "console_clear"
	// KindResetHistory replaces the transcript with Messages.
	KindResetHistory EventKind = "reset_history"

	// KindUIArea reports an area being registered (Area).
	KindUIArea EventKind = "ui_area"
	// KindUIPatch applies a batch of scene-graph ops to an area (Patch).
	KindUIPatch EventKind = "ui_patch"
	// KindUIAreaUpdate updates an area's sizing constraints (AreaUpdate).
	KindUIAreaUpdate EventKind = "ui_area_update"
	// KindUIAreaRemove removes an area and its scene graph (Text is the area ID).
	KindUIAreaRemove EventKind = "ui_area_remove"

	// KindUIAreaHTML carries a fully rendered area as HTML. It accompanies every
	// change that is not a pure text append: area creation, structural patches,
	// and constraint updates. A client replaces the region for Area.ID with HTML.
	// Pre-rendering server-side keeps one renderer (modules/server/htmlscene.go)
	// authoritative, so the browser does not reimplement the scene vocabulary.
	KindUIAreaHTML EventKind = "ui_area_html"
	// KindUITextAppend is the streaming fast path: Text is a delta appended to the
	// text node NodeID inside AreaID, so token-by-token growth does not re-send
	// the whole area. A client appends to the element matching the node ID.
	KindUITextAppend EventKind = "ui_text_append"
)
