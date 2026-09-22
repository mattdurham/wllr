package main

// skillMeta holds the parsed frontmatter metadata for a skill.
type skillMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	// Model is the skill's requested model: either a configured model-tier name
	// (e.g. "high") or an exact model ID. Empty means the skill does not change
	// the active model. Applied when the skill is activated.
	Model string `json:"model,omitempty"`
	// Thinking is the optional reasoning level applied with Model (e.g. "high").
	Thinking string `json:"thinking,omitempty"`
}
