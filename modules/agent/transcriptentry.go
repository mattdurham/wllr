package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// transcriptentry.go defines one record in an agent's canonical transcript.
// The Transcript container lives in transcript.go; entry search and rendering
// live there too.

import "time"

// Transcript entry kinds. Kind distinguishes a conversational message from a
// tool call and its result, which are the three things recall must be able to
// retrieve independently (issue #42: "messages, tool calls, command output").
const (
	// TranscriptKindMessage is a user or assistant conversational message.
	TranscriptKindMessage = "message"
	// TranscriptKindToolCall is a tool invocation and its input.
	TranscriptKindToolCall = "tool_call"
	// TranscriptKindToolResult is a tool's output, including command output.
	TranscriptKindToolResult = "tool_result"
)

// TranscriptEntry is one immutable record in an agent's canonical transcript.
type TranscriptEntry struct {
	// Timestamp is when the entry was recorded.
	Timestamp time.Time
	// ID is the stable source pointer for this entry ("u1", "a2", "t3", ...).
	// A recalled entry cites its ID so the model can refer to exact material
	// and correlate it with the persisted session file.
	ID string
	// Role is the conversational role for message entries ("user" or
	// "assistant"). It is empty for tool entries, whose subject is ToolName.
	Role string
	// Kind is one of the TranscriptKind* constants.
	Kind string
	// ToolName is the tool that produced a tool_call or tool_result entry.
	ToolName string
	// Content is the entry's verbatim text.
	Content string
	// Seq is the monotonic sequence number, also encoded in ID. Recall ranges
	// (from/to) are expressed in these terms.
	Seq int
}
