//go:build wasip1

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// planSnapshotDir is the per-user directory holding the durable plan snapshot.
// Plans survive compaction and process restart by writing a single JSON file
// here via WASI (the host mounts "/" as "/"), mirroring how the history and
// logging extensions persist to ~/.wllr. A missing/unreadable snapshot fails
// closed to an empty plan set (never an implicit grant of stale state).
func planSnapshotDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".wllr", "plans")
}

func planSnapshotPath() string {
	return filepath.Join(planSnapshotDir(), "plan_state.json")
}

// loadPlanStateFromDisk reads the durable snapshot into memory. It is called on
// session_start. On any error it resets to an empty plan set so a corrupt file
// never resurrects stale plans.
func loadPlanStateFromDisk() {
	raw, err := os.ReadFile(planSnapshotPath())
	if err != nil {
		// Not found or unreadable: start empty.
		planMu.Lock()
		state = emptyPlanState()
		stateReady = true
		planMu.Unlock()
		return
	}
	var saved planState
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Plans == nil {
		planMu.Lock()
		state = emptyPlanState()
		stateReady = true
		planMu.Unlock()
		return
	}
	planMu.Lock()
	state = saved
	stateReady = true
	planMu.Unlock()
}

// savePlanStateToDisk writes the in-memory plan state to the durable snapshot.
// It is called after every mutation so the on-disk state tracks memory.
// Caller must hold planMu (RLock or Lock).
func savePlanStateToDisk() error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return persistSnapshot(data)
}

// persistSnapshot writes pre-marshaled plan state to the durable snapshot.
// Write is direct (os.WriteFile), matching the pattern the TinyGo-built history
// extension already uses. NOTE: not atomic — a crash mid-write can leave a torn
// snapshot; loadPlanStateFromDisk fails closed on a malformed snapshot so a torn
// file resets to empty rather than resurrecting stale plans.
func persistSnapshot(data []byte) error {
	if err := os.MkdirAll(planSnapshotDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(planSnapshotPath(), data, 0o600)
}
