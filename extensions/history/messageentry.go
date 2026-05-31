//go:build wasip1

package main

type messageEntry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Role      string `json:"role"`
	Content   string `json:"content"`
}
