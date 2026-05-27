package main

// mcpServerState tracks a running MCP server process.
type mcpServerState struct {
	Name   string
	Config mcpServerConfig
	PID    string
	Tools  []mcpTool
}
