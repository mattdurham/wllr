package wire

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// Event is one frame on the stream. It is a flat envelope rather than a tagged
// union so an HTML client can switch on Kind and read the relevant fields
// without a nested decoder. Unused fields are omitted.
type Event struct {
	// Usage is the context-window snapshot for KindContextUsage.
	Usage *sdk.ContextUsage `json:"usage,omitempty"`

	// TextInput accompanies KindTextInput.
	TextInput *TextInputParams `json:"text_input,omitempty"`
	// Picker accompanies KindPicker.
	Picker *sdk.ShowPickerParams `json:"picker,omitempty"`
	// AgentTree accompanies KindAgentTree.
	AgentTree *sdk.ShowAgentTreeParams `json:"agent_tree,omitempty"`
	// Area accompanies KindUIArea.
	Area *sdk.UIArea `json:"area,omitempty"`
	// Patch accompanies KindUIPatch.
	Patch *sdk.UIPatchParams `json:"patch,omitempty"`
	// AreaUpdate accompanies KindUIAreaUpdate.
	AreaUpdate *sdk.UIUpdateAreaParams `json:"area_update,omitempty"`
	// Kind selects the meaning of the remaining fields.
	Kind EventKind `json:"kind"`
	// AgentID is the agent the event concerns ("main" for the root agent).
	AgentID string `json:"agent_id,omitempty"`

	// Text is the primary text payload for the token, notify, error,
	// assistant-message, focus (agent ID), console (line), modal, and
	// ui_area_remove (area ID) kinds; see each kind's own documentation for the
	// exact meaning.
	Text string `json:"text,omitempty"`
	// Role accompanies KindUserMessage.
	Role sdk.Role `json:"role,omitempty"`

	// ToolCallID/ToolName/Input accompany KindToolCall and KindToolResult.
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Input      string `json:"input,omitempty"`
	Output     string `json:"output,omitempty"`

	// Err is the failure text for KindTurnDone and KindError.
	Err string `json:"err,omitempty"`

	// Key/Value accompany KindStatus.
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
	// HTML accompanies KindUIAreaHTML: the rendered area fragment.
	HTML string `json:"html,omitempty"`
	// AreaID and NodeID address an incremental scene update (KindUITextAppend).
	AreaID string `json:"area_id,omitempty"`
	NodeID string `json:"node_id,omitempty"`
	// Name is the agent's display name (KindAgentSpawned).
	Name string `json:"name,omitempty"`
	// Inbox carries the messages drained into the turn (KindUserMessage).
	Inbox []sdk.Message `json:"inbox,omitempty"`

	// Messages accompanies KindResetHistory.
	Messages []sdk.Message `json:"messages,omitempty"`

	// Seq is the hub's monotonic sequence number, assigned on Publish. A client
	// that sees a gap knows it missed events and can re-sync.
	Seq int64 `json:"seq"`

	// Live is the pool size after a KindAgentSpawned/KindAgentClosed change.
	Live int64 `json:"live,omitempty"`

	// Protocol is the wire version, set only on KindReady.
	Protocol int  `json:"protocol,omitempty"`
	IsError  bool `json:"is_error,omitempty"`

	// Main reports whether the agent is the root agent.
	Main bool `json:"main,omitempty"`
}
