package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// recall.go exposes the canonical transcript (transcript.go) to the model as a
// tool. This is the second half of the hybrid compaction model in issue #42:
// compaction may replace old turns with a summary, but the model can always ask
// for the exact material back instead of guessing from the summary.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// RecallToolName is the model-facing name of the transcript search tool. It is
// exported so the harness and tests can refer to it without duplicating the
// literal.
const RecallToolName = "recall"

// recallDescription tells the model what the tool is for and, importantly, when
// it should reach for it: after compaction, when the summary it holds is not
// precise enough. Without that cue a model tends to answer from a lossy summary
// rather than retrieving the exact record.
const recallDescription = "Search this session's canonical transcript for the " +
	"exact, verbatim record of earlier messages, tool calls, and tool output " +
	"(including command results and error text). Use this AFTER context " +
	"compaction has summarized older turns, when you need precise detail the " +
	"summary omitted or may have distorted: exact commands, file paths, error " +
	"messages, or the precise wording of an earlier instruction. Filter by a " +
	"tool name or a file path, or pull a contiguous range of entries by their " +
	"sequence numbers. Results are bounded; if a search is too broad, narrow it."

// recallTool adapts an agent's canonical transcript to the fantasy.AgentTool
// interface. It holds the agent rather than a transcript pointer so it always
// reads the agent's current transcript, including one created lazily after the
// tool was built.
type recallTool struct {
	agent           *Agent
	providerOptions fantasy.ProviderOptions
}

var _ fantasy.AgentTool = (*recallTool)(nil)

// RecallTool returns a tool that searches this agent's canonical transcript.
// The tool is cheap to construct and reads the transcript at call time.
func (a *Agent) RecallTool() fantasy.AgentTool {
	return &recallTool{agent: a}
}

// recallInput is the tool's argument object. Every field is optional, but at
// least one must be supplied (validated in Run) — an unfiltered recall of a
// whole session would return nothing useful and waste the context it is meant
// to protect.
type recallInput struct {
	// Query is a case-insensitive substring to find in entry content.
	Query string `json:"query"`
	// Tool restricts results to one tool's calls and results.
	Tool string `json:"tool"`
	// Path restricts results to entries whose content contains this substring,
	// typically a file path.
	Path string `json:"path"`
	// From and To bound an inclusive sequence range. Zero means unbounded.
	From int `json:"from"`
	To   int `json:"to"`
	// Limit caps the number of entries returned. Zero uses the default.
	Limit int `json:"limit"`
}

// JSON Schema keys repeated by every parameter. Hoisted so the field names are
// spelled once; a typo in one of these would silently drop a parameter from the
// model-facing schema.
const (
	schemaKeyType        = "type"
	schemaKeyDescription = "description"
)

// Info implements fantasy.AgentTool.
func (r *recallTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{
		Name:        RecallToolName,
		Description: recallDescription,
		Parameters: map[string]any{
			"query": map[string]any{
				schemaKeyType: "string",
				schemaKeyDescription: "Text to find in the transcript, matched " +
					"case-insensitively as a substring. Omit to search by " +
					"tool, path, or range only.",
			},
			"tool": map[string]any{
				schemaKeyType: "string",
				schemaKeyDescription: "Only return entries produced by this tool " +
					"(e.g. \"exec\" or \"edit_file\").",
			},
			"path": map[string]any{
				schemaKeyType: "string",
				schemaKeyDescription: "Only return entries whose content contains " +
					"this substring, e.g. a file path.",
			},
			"from": map[string]any{
				schemaKeyType: "integer",
				schemaKeyDescription: "Start of an inclusive sequence range. " +
					"Omit for the beginning.",
			},
			"to": map[string]any{
				schemaKeyType: "integer",
				schemaKeyDescription: "End of an inclusive sequence range. " +
					"Omit for the latest entry.",
			},
			"limit": map[string]any{
				schemaKeyType: "integer",
				schemaKeyDescription: "Maximum entries to return. " +
					"Omit for the default.",
			},
		},
		Required: []string{},
	}
}

// Run implements fantasy.AgentTool. It never returns a Go error: a bad query is
// reported as a tool error response so the model can correct itself, which is
// the convention this codebase's other tools follow.
func (r *recallTool) Run(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var in recallInput
	if s := strings.TrimSpace(call.Input); s != "" {
		if err := json.Unmarshal([]byte(s), &in); err != nil {
			return fantasy.NewTextErrorResponse(
				fmt.Sprintf("recall: invalid input JSON: %v", err),
			), nil
		}
	}

	query := strings.TrimSpace(in.Query)
	tool := strings.TrimSpace(in.Tool)
	path := strings.TrimSpace(in.Path)
	if query == "" && tool == "" && path == "" && in.From <= 0 && in.To <= 0 {
		return fantasy.NewTextErrorResponse(
			"recall: provide at least one of query, tool, path, or a from/to range",
		), nil
	}
	if in.From > 0 && in.To > 0 && in.From > in.To {
		return fantasy.NewTextErrorResponse(
			fmt.Sprintf("recall: from (%d) must not exceed to (%d)", in.From, in.To),
		), nil
	}
	if in.From < 0 || in.To < 0 {
		return fantasy.NewTextErrorResponse("recall: from and to must not be negative"), nil
	}

	if r.agent == nil {
		return fantasy.NewTextErrorResponse("recall: no agent bound to this tool"), nil
	}
	transcript := r.agent.CanonicalTranscript()
	matches, total := transcript.Search(RecallQuery{
		Text:  query,
		Tool:  tool,
		Path:  path,
		From:  in.From,
		To:    in.To,
		Limit: in.Limit,
	})

	budget := recallBudgetForWindow(r.agent.ContextWindow())
	return fantasy.NewTextResponse(RenderRecall(matches, total, budget)), nil
}

// ProviderOptions implements fantasy.AgentTool.
func (r *recallTool) ProviderOptions() fantasy.ProviderOptions { return r.providerOptions }

// SetProviderOptions implements fantasy.AgentTool.
func (r *recallTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	r.providerOptions = opts
}
