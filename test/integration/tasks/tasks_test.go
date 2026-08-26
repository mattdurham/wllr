//go:build integration

package tasks_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattdurham/wllr/modules/extension"
)

func setupTasksExtension(t *testing.T) (*extension.Host, context.Context) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil { t.Fatal(err) }
	wasmPath := filepath.Join(home, ".wllr", "extensions", "tasks", "tasks.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) { t.Skip("tasks.wasm not found - run 'make extensions' first") }
	host, err := extension.NewHostWithTaskLedger(nil, t.TempDir())
	if err != nil { t.Fatal(err) }
	ctx := context.Background()
	if err := host.Load(ctx, wasmPath); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = host.Close(ctx) })
	return host, ctx
}

func call(t *testing.T, ctx context.Context, host *extension.Host, name string, params any) map[string]any {
	t.Helper(); raw, _ := json.Marshal(params)
	result, err := host.ExecuteTool(ctx, "test-agent", name+"-call", name, raw)
	if err != nil { t.Fatal(err) }; if result.IsError { t.Fatalf("%s: %s", name, result.Result) }
	var out map[string]any; if err := json.Unmarshal([]byte(result.Result), &out); err != nil { t.Fatal(err) }; return out
}

func TestDurableTaskLifecycleAndReplay(t *testing.T) {
	host, ctx := setupTasksExtension(t)
	list := call(t, ctx, host, "tasklist_create", map[string]any{"name":"workflow", "owner_agent_id":"parent"})["list"].(map[string]any)
	listID := list["list_id"].(string)
	created := call(t, ctx, host, "tasks_create", map[string]any{"list_id":listID, "title":"build", "workspace_mode":"readonly"})["task"].(map[string]any)
	taskID, version := created["task_id"].(string), int64(created["version"].(float64))
	claimed := call(t, ctx, host, "tasks_claim", map[string]any{"list_id":listID, "agent_id":"worker"})["task"].(map[string]any)
	attemptID := claimed["attempt_id"].(string)
	if int64(claimed["version"].(float64)) <= version { t.Fatal("claim did not advance version") }
	reported := call(t, ctx, host, "tasks_report", map[string]any{"list_id":listID, "task_id":taskID, "attempt_id":attemptID, "agent_id":"worker", "status":"completed", "result":map[string]any{"ok":true}})["task"].(map[string]any)
	if reported["status"] != "completed" { t.Fatalf("status = %v", reported["status"]) }
	events := call(t, ctx, host, "tasks_events_after", map[string]any{"list_id":listID, "cursor":0, "limit":100})["events"].([]any)
	if len(events) < 4 { t.Fatalf("got %d events, want list/create/claim/report", len(events)) }
	seen := map[string]bool{}; for _, raw := range events { id := raw.(map[string]any)["event_id"].(string); if seen[id] { t.Fatalf("duplicate event %s", id) }; seen[id] = true }
}

func TestTaskCASAndReportValidation(t *testing.T) {
	host, ctx := setupTasksExtension(t)
	listID := call(t, ctx, host, "tasklist_create", map[string]any{"name":"cas"})["list"].(map[string]any)["list_id"].(string)
	task := call(t, ctx, host, "tasks_create", map[string]any{"list_id":listID, "title":"one"})["task"].(map[string]any)
	_, _ = task["task_id"].(string), task["version"].(float64)
	raw, _ := json.Marshal(map[string]any{"list_id":listID, "task_id":task["task_id"], "expected_version":999, "title":"stale"})
	result, err := host.ExecuteTool(ctx, "test-agent", "stale", "tasks_update", raw)
	if err != nil { t.Fatal(err) }; if !result.IsError { t.Fatal("stale update unexpectedly succeeded") }
}
