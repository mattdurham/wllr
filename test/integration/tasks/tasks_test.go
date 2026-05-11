//go:build integration

package tasks_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattdurham/wllr/extension"
)

func setupTasksExtension(t *testing.T) (*extension.Host, func()) {
	t.Helper()

	host := extension.NewHost(nil)

	// Load tasks extension
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	wasmPath := filepath.Join(home, ".wllr", "extensions", "tasks", "tasks.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skip("tasks.wasm not found - run 'make extensions' first")
	}

	ctx := context.Background()
	if err := host.Load(ctx, wasmPath); err != nil {
		t.Fatalf("Failed to load tasks extension: %v", err)
	}

	cleanup := func() {
		host.Close(ctx)
	}

	return host, cleanup
}

func executeToolJSON(t *testing.T, host *extension.Host, toolName string, params interface{}) map[string]interface{} {
	t.Helper()

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal params: %v", err)
	}

	ctx := context.Background()
	result, err := host.ExecuteTool(ctx, "test-agent", "call-1", toolName, paramsJSON)
	if err != nil {
		t.Fatalf("ExecuteTool %s failed: %v", toolName, err)
	}

	if result.IsError {
		t.Fatalf("Tool %s returned error: %s", toolName, result.Result)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result.Result), &resultMap); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	return resultMap
}

func TestTasklistCreate(t *testing.T) {
	host, cleanup := setupTasksExtension(t)
	defer cleanup()

	result := executeToolJSON(t, host, "tasklist_create", map[string]interface{}{
		"name":        "My Tasks",
		"description": "Test task list",
	})

	listID, ok := result["list_id"].(string)
	if !ok || listID == "" {
		t.Error("Expected list_id in response")
	}

	if listID != "list-1" {
		t.Errorf("Expected list-1, got %s", listID)
	}
}

func TestTasksCreate(t *testing.T) {
	host, cleanup := setupTasksExtension(t)
	defer cleanup()

	// Create list
	listResult := executeToolJSON(t, host, "tasklist_create", map[string]interface{}{
		"name": "My Tasks",
	})
	listID := listResult["list_id"].(string)

	// Create task
	taskResult := executeToolJSON(t, host, "tasks_create", map[string]interface{}{
		"list_id":     listID,
		"title":       "Test Task",
		"description": "A test task",
		"priority":    "high",
		"tags":        []string{"test", "integration"},
	})

	taskID, ok := taskResult["task_id"].(string)
	if !ok || taskID == "" {
		t.Error("Expected task_id in response")
	}

	if taskID != "task-1" {
		t.Errorf("Expected task-1, got %s", taskID)
	}
}

func TestTasksUpdate(t *testing.T) {
	host, cleanup := setupTasksExtension(t)
	defer cleanup()

	// Create list and task
	listResult := executeToolJSON(t, host, "tasklist_create", map[string]interface{}{
		"name": "My Tasks",
	})
	listID := listResult["list_id"].(string)

	taskResult := executeToolJSON(t, host, "tasks_create", map[string]interface{}{
		"list_id": listID,
		"title":   "Test Task",
	})
	taskID := taskResult["task_id"].(string)

	// Update task
	updateResult := executeToolJSON(t, host, "tasks_update", map[string]interface{}{
		"list_id": listID,
		"task_id": taskID,
		"status":  "in_progress",
	})

	success, ok := updateResult["success"].(bool)
	if !ok || !success {
		t.Error("Expected success: true")
	}
}

func TestTasksGet(t *testing.T) {
	host, cleanup := setupTasksExtension(t)
	defer cleanup()

	// Create list and task
	listResult := executeToolJSON(t, host, "tasklist_create", map[string]interface{}{
		"name": "My Tasks",
	})
	listID := listResult["list_id"].(string)

	taskResult := executeToolJSON(t, host, "tasks_create", map[string]interface{}{
		"list_id":     listID,
		"title":       "Test Task",
		"description": "Test description",
		"priority":    "high",
		"tags":        []string{"test"},
	})
	taskID := taskResult["task_id"].(string)

	// Get task
	getResult := executeToolJSON(t, host, "tasks_get", map[string]interface{}{
		"list_id": listID,
		"task_id": taskID,
	})

	if getResult["id"] != taskID {
		t.Errorf("Expected id %s, got %v", taskID, getResult["id"])
	}
	if getResult["title"] != "Test Task" {
		t.Errorf("Expected title 'Test Task', got %v", getResult["title"])
	}
	if getResult["priority"] != "high" {
		t.Errorf("Expected priority 'high', got %v", getResult["priority"])
	}
}

func TestTasksList(t *testing.T) {
	host, cleanup := setupTasksExtension(t)
	defer cleanup()

	// Create list
	listResult := executeToolJSON(t, host, "tasklist_create", map[string]interface{}{
		"name": "My Tasks",
	})
	listID := listResult["list_id"].(string)

	// Create multiple tasks
	for i := 1; i <= 3; i++ {
		executeToolJSON(t, host, "tasks_create", map[string]interface{}{
			"list_id": listID,
			"title":   "Task " + string(rune('0'+i)),
		})
	}

	// List tasks
	listTasksResult := executeToolJSON(t, host, "tasks_list", map[string]interface{}{
		"list_id": listID,
	})

	tasks, ok := listTasksResult["tasks"].([]interface{})
	if !ok {
		t.Fatal("Expected tasks array in response")
	}

	if len(tasks) != 3 {
		t.Errorf("Expected 3 tasks, got %d", len(tasks))
	}
}

func TestTasksWorkflow(t *testing.T) {
	host, cleanup := setupTasksExtension(t)
	defer cleanup()

	// Create list
	listResult := executeToolJSON(t, host, "tasklist_create", map[string]interface{}{
		"name":        "Workflow Test",
		"description": "Complete workflow test",
	})
	listID := listResult["list_id"].(string)

	// Create task
	taskResult := executeToolJSON(t, host, "tasks_create", map[string]interface{}{
		"list_id":     listID,
		"title":       "Implement feature",
		"description": "Add new functionality",
		"priority":    "high",
		"tags":        []string{"feature", "backend"},
	})
	taskID := taskResult["task_id"].(string)

	// Update to in_progress
	executeToolJSON(t, host, "tasks_update", map[string]interface{}{
		"list_id": listID,
		"task_id": taskID,
		"status":  "in_progress",
	})

	// Get and verify
	getResult := executeToolJSON(t, host, "tasks_get", map[string]interface{}{
		"list_id": listID,
		"task_id": taskID,
	})

	if getResult["status"] != "in_progress" {
		t.Errorf("Expected status in_progress, got %v", getResult["status"])
	}

	// Update to completed
	executeToolJSON(t, host, "tasks_update", map[string]interface{}{
		"list_id": listID,
		"task_id": taskID,
		"status":  "completed",
	})

	// Verify final state
	finalResult := executeToolJSON(t, host, "tasks_get", map[string]interface{}{
		"list_id": listID,
		"task_id": taskID,
	})

	if finalResult["status"] != "completed" {
		t.Errorf("Expected status completed, got %v", finalResult["status"])
	}
}

func TestTasksDependencies(t *testing.T) {
	host, cleanup := setupTasksExtension(t)
	defer cleanup()

	// Create list
	listResult := executeToolJSON(t, host, "tasklist_create", map[string]interface{}{
		"name": "Dependencies Test",
	})
	listID := listResult["list_id"].(string)

	// Create task 1
	task1Result := executeToolJSON(t, host, "tasks_create", map[string]interface{}{
		"list_id": listID,
		"title":   "Task 1",
	})
	task1ID := task1Result["task_id"].(string)

	// Create task 2 with dependency on task 1
	task2Result := executeToolJSON(t, host, "tasks_create", map[string]interface{}{
		"list_id":      listID,
		"title":        "Task 2",
		"dependencies": []string{task1ID},
	})
	task2ID := task2Result["task_id"].(string)

	// Verify task 2 has dependency
	getResult := executeToolJSON(t, host, "tasks_get", map[string]interface{}{
		"list_id": listID,
		"task_id": task2ID,
	})

	deps, ok := getResult["dependencies"].([]interface{})
	if !ok || len(deps) != 1 {
		t.Error("Expected task 2 to have 1 dependency")
	}
	if deps[0] != task1ID {
		t.Errorf("Expected dependency %s, got %v", task1ID, deps[0])
	}
}
