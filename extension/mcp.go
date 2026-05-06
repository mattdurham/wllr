package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/mattdurham/wllr/sdk"
	"github.com/mattdurham/wllr/sdk/mcp"
)

// MCPManager manages MCP server connections and tool registration.
type MCPManager struct {
	host    *Host
	logger  *slog.Logger
	mu      sync.RWMutex
	clients map[string]*mcp.Client // keyed by server name
	toolMap map[string]string      // tool name → server name
}

// MCPServerConfig specifies how to start an MCP server.
type MCPServerConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// NewMCPManager creates an MCP manager and wires it into the host.
func NewMCPManager(host *Host, logger *slog.Logger) *MCPManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &MCPManager{
		host:    host,
		logger:  logger,
		clients: make(map[string]*mcp.Client),
		toolMap: make(map[string]string),
	}
}

// LoadServers starts the specified MCP servers, performs handshake, and registers tools.
func (m *MCPManager) LoadServers(ctx context.Context, configs []MCPServerConfig) error {
	for _, cfg := range configs {
		if err := m.loadServer(ctx, cfg); err != nil {
			m.logger.Error("mcp: load server failed", "server", cfg.Name, "err", err)
			continue
		}
	}
	return nil
}

func (m *MCPManager) loadServer(ctx context.Context, cfg MCPServerConfig) error {
	m.logger.Info("mcp: loading server", "name", cfg.Name, "command", cfg.Command)

	client, err := mcp.NewClient(cfg.Command, cfg.Args...)
	if err != nil {
		return fmt.Errorf("spawn: %w", err)
	}

	serverInfo, err := client.Initialize(mcp.ClientInfo{
		Name:    "wllr",
		Version: "0.1.0",
	})
	if err != nil {
		client.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	m.logger.Info("mcp: server connected", "name", cfg.Name, "server", serverInfo.Name, "version", serverInfo.Version)

	tools, err := client.ListTools()
	if err != nil {
		client.Close()
		return fmt.Errorf("list tools: %w", err)
	}

	m.mu.Lock()
	m.clients[cfg.Name] = client
	for _, tool := range tools {
		toolName := fmt.Sprintf("mcp_%s_%s", cfg.Name, tool.Name)
		m.toolMap[toolName] = cfg.Name

		// Register tool with the host.
		wllrTool := sdk.Tool{
			Name:        toolName,
			Description: fmt.Sprintf("[MCP:%s] %s", cfg.Name, tool.Description),
			InputSchema: tool.InputSchema,
		}
		m.host.mu.Lock()
		m.host.registeredTools[toolName] = wllrTool
		m.host.toolOwners[toolName] = fmt.Sprintf("mcp:%s", cfg.Name)
		m.host.mu.Unlock()

		if m.host.OnRegisterTool != nil {
			if err := m.host.OnRegisterTool(wllrTool); err != nil {
				m.logger.Warn("mcp: register tool callback failed", "tool", toolName, "err", err)
			}
		}

		m.logger.Debug("mcp: registered tool", "server", cfg.Name, "tool", toolName)
	}
	m.mu.Unlock()

	return nil
}

// HandleToolCall executes an MCP tool call if the tool belongs to an MCP server.
// Returns (result, isError, handled). If not handled, returns ("", false, false).
func (m *MCPManager) HandleToolCall(ctx context.Context, toolName string, input json.RawMessage) (string, bool, bool) {
	m.mu.RLock()
	serverName, ok := m.toolMap[toolName]
	client := m.clients[serverName]
	m.mu.RUnlock()

	if !ok || client == nil {
		return "", false, false
	}

	// Extract the original MCP tool name (strip "mcp_<server>_" prefix).
	prefix := fmt.Sprintf("mcp_%s_", serverName)
	mcpToolName := toolName
	if len(toolName) > len(prefix) {
		mcpToolName = toolName[len(prefix):]
	}

	// Parse input as map[string]any for MCP call.
	var arguments map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &arguments); err != nil {
			return fmt.Sprintf("invalid input: %v", err), true, true
		}
	}

	result, err := client.CallTool(mcpToolName, arguments)
	if err != nil {
		return fmt.Sprintf("mcp call error: %v", err), true, true
	}

	if result.IsError {
		return m.formatToolResult(result), true, true
	}

	return m.formatToolResult(result), false, true
}

func (m *MCPManager) formatToolResult(result *mcp.ToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	// Concatenate all text content.
	var out string
	for _, item := range result.Content {
		if item.Type == "text" {
			out += item.Text
		}
	}
	return out
}

// Close shuts down all MCP server connections.
func (m *MCPManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			m.logger.Warn("mcp: close error", "server", name, "err", err)
		}
	}
	m.clients = nil
	m.toolMap = nil
	return nil
}
