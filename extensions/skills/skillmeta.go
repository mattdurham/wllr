package main

// skillMeta holds the parsed frontmatter metadata for a skill.
type skillMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}
