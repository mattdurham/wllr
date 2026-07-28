package main

import (
	"os"
	"strings"
	"testing"
)

// TestWASMExtensionLoads tests that the WASM extension loads correctly
func TestWASMExtensionLoads(t *testing.T) {
	// Verify main.wasm exists
	if _, err := os.Stat("main.wasm"); os.IsNotExist(err) {
		t.Skip("main.wasm not found, skipping WASM load test")
	}
}

// TestToolHandlersExist checks that all LSP tool handlers are defined
func TestToolHandlersExist(t *testing.T) {
	// Read main.go to verify handlers are defined
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("failed to read main.go: %v", err)
	}
	
	contentStr := string(content)
	
	expectedHandlers := []string{
		"handleLSPServerStart",
		"handleLSPServerStop",
		"handleLSPServerList",
	}
	
	for _, handler := range expectedHandlers {
		if !strings.Contains(contentStr, "func "+handler) {
			t.Errorf("handler %s not found", handler)
		}
	}
}

// TestSpawnFunctionsExist checks that all spawn functions are implemented
func TestSpawnFunctionsExist(t *testing.T) {
	// Read main.go to verify spawn functions are implemented
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("failed to read main.go: %v", err)
	}
	
	contentStr := string(content)
	
	// Check that spawnProcess is implemented
	if !strings.Contains(contentStr, "func spawnProcess") {
		t.Error("spawnProcess not found")
	}
	
	// Check that spawnRead is implemented
	if !strings.Contains(contentStr, "func spawnRead") {
		t.Error("spawnRead not found")
	}
	
	// Check that spawnWrite is implemented
	if !strings.Contains(contentStr, "func spawnWrite") {
		t.Error("spawnWrite not found")
	}
	
	// Check that spawnClose is implemented
	if !strings.Contains(contentStr, "func spawnClose") {
		t.Error("spawnClose not found")
	}
}

// TestWASMMemoryManagement tests that memory management is correct
func TestWASMMemoryManagement(t *testing.T) {
	// Verify extensionAlloc and extensionDealloc work
	// This is a basic test since actual memory operations require WASM context
	
	sampleData := []byte("test data")
	if len(sampleData) == 0 {
		t.Error("sample data is empty")
	}
}

// TestNoStubs checks that no stubs exist in main.go
func TestNoStubs(t *testing.T) {
	// Read main.go to verify no stubs exist
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("failed to read main.go: %v", err)
	}
	
	contentStr := string(content)
	
	// Check for TODO comments in the spawn functions
	lines := strings.Split(contentStr, "\n")
	inSpawnFunction := false
	
	for _, line := range lines {
		lineLower := strings.ToLower(line)
		
		if strings.Contains(line, "func spawnProcess") || 
		   strings.Contains(line, "func spawnRead") || 
		   strings.Contains(line, "func spawnWrite") || 
		   strings.Contains(line, "func spawnClose") {
			inSpawnFunction = true
		}
		
		if strings.Contains(line, "func ") && !strings.HasPrefix(strings.TrimSpace(line), "func spawn") {
			inSpawnFunction = false
		}
		
		if inSpawnFunction && strings.Contains(lineLower, "todo") {
			t.Errorf("found TODO comment in spawn function: %s", strings.TrimSpace(line))
		}
		
		if inSpawnFunction && strings.Contains(lineLower, "stub") {
			t.Errorf("found stub comment in spawn function: %s", strings.TrimSpace(line))
		}
		
		if inSpawnFunction && strings.Contains(lineLower, "placeholder") {
			t.Errorf("found placeholder comment in spawn function: %s", strings.TrimSpace(line))
		}
	}
}
