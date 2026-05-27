package main

import "sync"

// TaskList represents a collection of tasks.
type TaskList struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Tasks       map[string]*Task `json:"tasks"`
	mu          sync.RWMutex
}
