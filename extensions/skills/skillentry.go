package main

// skillEntry holds both metadata and body for a loaded skill.
type skillEntry struct {
	meta     skillMeta
	body     string
	filePath string
}
