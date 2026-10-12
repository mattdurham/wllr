//go:build wasip1

package main

// compactionEntry records compaction metadata in a session JSONL file.
// Session replay loads only message entries, so this is not replayed.
type compactionEntry struct {
	Type              string `json:"type"`
	ID                string `json:"id"`
	Timestamp         string `json:"timestamp"`
	Trigger           string `json:"trigger"`
	MessagesCompacted int    `json:"messages_compacted"`
}
