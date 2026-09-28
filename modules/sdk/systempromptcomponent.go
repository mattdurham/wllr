package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// SystemPromptComponent is one labeled slice of the base system prompt. The
// prompt extension reports the decomposition of the prompt it sets (built-in
// rules, tools list, prompt files, AGENTS.md, cwd note), and the pool appends
// a component for every labeled append_system_prompt. Chars is the raw
// character count of the slice; consumers estimate tokens with chars/4, the
// same heuristic compaction uses.
//
// The list is display-only observability for the /context breakdown: nothing
// rebuilds the prompt from it, so a stale or partial list degrades the modal,
// never the agent.
type SystemPromptComponent struct {
	// Source names where the slice comes from — e.g. "built-in rules",
	// "file:~/.wllr/AGENTS.md", "file:<project>/AGENTS.md", or the extension
	// name for labeled appends ("skills", "agents").
	Source string `json:"source"`
	// Chars is the slice's character count.
	Chars int `json:"chars"`
}
