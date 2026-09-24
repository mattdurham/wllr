//go:build wasip1

package main

import (
	"encoding/json"
	"path/filepath"
)

var config Config

func init() {
	// Native path-rule logic calls this indirection; point it at the host's
	// get_env before anything reads config.
	getEnv = GetEnv

	// Load configuration from the host.
	if err := loadConfig(); err != nil {
		Logf("error", "permissions: failed to load config: %v", err)
		// Default to permissive mode if config fails to load.
		config = Config{
			Read:  PathRules{Allow: []string{"*"}},
			Write: PathRules{Allow: []string{"*"}},
			Exec:  ExecRules{},
		}
	}

	// Set the event handler.
	OnEvent = handleEvent

	// Subscribe to before_tool_call to intercept file and configured exec rules.
	Subscribe("before_tool_call")

	// The summary prints bare patterns; rules now carry an optional message
	// that would bloat the line.
	denyCmds := make([]string, 0, len(config.Exec.DenyCommands))
	for _, r := range config.Exec.DenyCommands {
		denyCmds = append(denyCmds, r.Command)
	}
	denyVars := make([]string, 0, len(config.Exec.DenyEnvVars))
	for _, r := range config.Exec.DenyEnvVars {
		denyVars = append(denyVars, r.EnvVar)
	}
	denyRead := make([]string, 0, len(config.Read.Deny))
	for _, r := range config.Read.Deny {
		denyRead = append(denyRead, r.Path)
	}
	denyWrite := make([]string, 0, len(config.Write.Deny))
	for _, r := range config.Write.Deny {
		denyWrite = append(denyWrite, r.Path)
	}
	SetStatus("permissions", "active")
	Logf(
		"info",
		"permissions: initialized (read allow=%v deny=%v, write allow=%v deny=%v, exec allow=%v deny=%v deny_env_vars=%v)",
		config.Read.Allow,
		denyRead,
		config.Write.Allow,
		denyWrite,
		config.Exec.AllowCommands,
		denyCmds,
		denyVars,
	)
}

// loadConfig reads the extension configuration from the host.
func loadConfig() error {
	data, err := ConfigRead()
	if err != nil {
		return err
	}
	if len(data) == 0 || string(data) == "{}" {
		// No config provided; use defaults.
		config = Config{
			Read:  PathRules{Allow: []string{"*"}},
			Write: PathRules{Allow: []string{"*"}},
			Exec:  ExecRules{},
		}
		return nil
	}
	return json.Unmarshal(data, &config)
}

// handleEvent is called by the host for subscribed events.
func handleEvent(evt Event) *EventResponse {
	if evt.Type != "before_tool_call" {
		return nil
	}

	var payload struct {
		ToolCallID string          `json:"tool_call_id"`
		ToolName   string          `json:"tool_name"`
		Input      json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		Logf("error", "permissions: unmarshal before_tool_call: %v", err)
		return nil
	}

	// Intercept file tools and exec when command rules are configured.
	if payload.ToolName == "exec" {
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(payload.Input, &input); err != nil {
			Logf("error", "permissions: unmarshal exec input: %v", err)
			return nil
		}
		allowed, reason := checkCommandPermission(input.Command, config.Exec)
		if !allowed {
			Logf("warn", "permissions: blocked exec command %q: %s", input.Command, reason)
			ToolResult(payload.ToolCallID, "Permission denied: "+reason, true)
			return &EventResponse{Block: true}
		}
		Logf("debug", "permissions: allowed exec command %q", input.Command)
		return nil
	}

	var rules PathRules
	switch payload.ToolName {
	case "read_file":
		rules = config.Read
	case "write_file":
		rules = config.Write
	default:
		// Not a file operation — allow.
		return nil
	}

	// Extract path from input.
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(payload.Input, &input); err != nil {
		Logf("error", "permissions: unmarshal %s input: %v", payload.ToolName, err)
		return nil
	}

	path := input.Path
	// Clean and resolve the path.
	path = filepath.Clean(path)

	// Check permission.
	allowed, denyMessage := checkPermission(path, rules)
	if !allowed {
		if denyMessage != "" {
			Logf("warn", "permissions: blocked %s to %s: %s", payload.ToolName, path, denyMessage)
			ToolResult(payload.ToolCallID, "Permission denied: "+denyMessage, true)
		} else {
			Logf("warn", "permissions: blocked %s to %s", payload.ToolName, path)
			// Return a tool_result error immediately to block the operation.
			ToolResult(payload.ToolCallID, "Permission denied: "+path, true)
		}
		// Return a response that blocks the event from proceeding.
		return &EventResponse{Block: true}
	}

	Logf("debug", "permissions: allowed %s to %s", payload.ToolName, path)
	return nil
}

func main() {}
