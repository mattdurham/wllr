//go:build wasip1

package main

// compactionEntry is one compaction-metadata record appended to a session
// JSONL file by recordCompaction. Loaders skip non-"message" entries, so
// these lines persist for inspection without affecting message loading.
type compactionEntry struct {
	Type              string `json:"type"`
	ID                string `json:"id"`
	Timestamp         string `json:"timestamp"`
	Trigger           string `json:"trigger"`
	MessagesCompacted int    `json:"messages_compacted"`
}
