package main

// shouldNotify reports whether a status transition from oldStatus to newStatus
// should trigger a notification to the task list owner.
// Notifications fire when a task reaches a terminal or notable state:
// "completed" or "blocked". Transitions within non-terminal states (e.g.
// pending → in_progress) do not fire.
func shouldNotify(oldStatus, newStatus string) bool {
	if newStatus != "completed" && newStatus != "blocked" {
		return false
	}
	// Suppress no-op transitions (completed → completed).
	if oldStatus == newStatus {
		return false
	}
	return true
}
