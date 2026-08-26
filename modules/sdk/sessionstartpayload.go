package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// SessionStartPayload is the payload for EventSessionStart.
//
// CWD and StartedAt carry host ground truth to extensions: the WASM sandbox
// has no working directory (guest os.Getwd returns "/") and its clock may be
// stale, so extensions must prefer these fields over their own os calls when
// writing pathed or timestamped artifacts.
type SessionStartPayload struct {
	Reason    string          `json:"reason"`
	Tools     []PromptTool    `json:"tools,omitempty"`
	Commands  []PromptCommand `json:"commands,omitempty"`
	CWD       string          `json:"cwd,omitempty"`
	StartedAt string          `json:"started_at,omitempty"` // RFC3339Nano
}

// PromptTool is the prompt-relevant subset of a registered tool.
type PromptTool struct {
	Name string `json:"name"`
}

// PromptCommand is the prompt-relevant subset of a registered slash command.
type PromptCommand struct {
	Name string `json:"name"`
	Desc string `json:"description,omitempty"`
}
