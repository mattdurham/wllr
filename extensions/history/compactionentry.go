//go:build wasip1

package main

// compactionEntry is one compaction-metadata record in a session JSONL file.
// Written when the engine folds history into a summary; the field tags mirror
// the other session entries so the file stays uniformly parsable.
type compactionEntry struct {
	Type              string `json:"type"`
	ID                string `json:"id"`
	Timestamp         string `json:"timestamp"`
	Trigger           string `json:"trigger"`
	MessagesCompacted int    `json:"messages_compacted"`
}
