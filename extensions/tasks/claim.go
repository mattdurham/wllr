package main

// dependenciesSatisfied reports whether all of task's dependencies are present
// in the completed set. A task with unmet dependencies is not claimable.
func dependenciesSatisfied(task *Task, completed map[string]bool) bool {
	for _, dep := range task.Dependencies {
		if !completed[dep] {
			return false
		}
	}
	return true
}

// sortTaskIDs orders task IDs of the form "task-N" by their numeric suffix so
// claiming is deterministic (lowest N first). IDs without a parseable suffix
// sort after the numeric ones, in lexical order.
func sortTaskIDs(ids []string) {
	// Insertion sort — task lists are small; avoids pulling in sort for one site.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && taskIDLess(ids[j], ids[j-1]); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

// taskIDLess compares two "task-N" IDs by numeric suffix, falling back to
// lexical comparison when either lacks a parseable suffix.
func taskIDLess(a, b string) bool {
	an, aok := taskIDNum(a)
	bn, bok := taskIDNum(b)
	if aok && bok {
		return an < bn
	}
	if aok != bok {
		return aok // numeric IDs sort before unparseable ones
	}
	return a < b
}

// taskIDNum extracts the integer suffix from a "task-N" ID.
func taskIDNum(id string) (int, bool) {
	const prefix = "task-"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return 0, false
	}
	n := 0
	for _, ch := range id[len(prefix):] {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return n, true
}
