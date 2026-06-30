package main

// Task represents a task in a task list.
type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	// Assignee is the agent ID that claimed this task via tasks_claim. Empty when
	// unclaimed. Set atomically as part of the claim (find first pending + flip to
	// in_progress) so two workers can never own the same task.
	Assignee     string   `json:"assignee,omitempty"`
	Tags         []string `json:"tags"`
	Dependencies []string `json:"dependencies"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}
