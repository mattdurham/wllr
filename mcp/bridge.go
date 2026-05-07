package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/mattdurham/wllr/sdk"
)

// Bridge manages multiple MCP servers and registers their tools with wllr.
type Bridge struct {
	servers   map[string]*Server
	toolToSrv map[string]string // tool name -> server name
	mu        sync.RWMutex
}

// NewBridge creates a new MCP bridge.
func NewBridge() *Bridge {
	return &Bridge{
		servers:   make(map[string]*Server),
		toolToSrv: make(map[string]string),
	}
}

// Start loads config and starts all configured MCP servers.
func (b *Bridge) Start(ctx context.Context) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.MCPServers) == 0 {
		slog.Info("mcp: no servers configured")
		return nil
	}

	// Start all servers
	var wg sync.WaitGroup
	errCh := make(chan error, len(cfg.MCPServers))

	for name, srvCfg := range cfg.MCPServers {
		wg.Add(1)
		go func(n string, c ServerConfig) {
			defer wg.Done()
			
			srv := NewServer(n, c)
			if err := srv.Start(ctx); err != nil {
				errCh <- fmt.Errorf("start server %q: %w", n, err)
				return
			}

			b.mu.Lock()
			b.servers[n] = srv
			
			// Build tool mapping
			for _, tool := range srv.Tools() {
				if existingSrv, exists := b.toolToSrv[tool.Name]; exists {
					slog.Warn("mcp: tool name collision", 
						"tool", tool.Name, 
						"server", n, 
						"existing_server", existingSrv)
				} else {
					b.toolToSrv[tool.Name] = n
				}
			}
			b.mu.Unlock()
		}(name, srvCfg)
	}

	wg.Wait()
	close(errCh)

	// Collect any errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// Return first error, but all servers that started successfully are running
		return errs[0]
	}

	b.mu.RLock()
	totalTools := len(b.toolToSrv)
	b.mu.RUnlock()

	slog.Info("mcp: bridge started", "servers", len(b.servers), "tools", totalTools)
	return nil
}

// RegisterTools registers all discovered MCP tools with wllr's extension host.
// Built-in tools take precedence over MCP tools.
func (b *Bridge) RegisterTools() []sdk.Tool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var tools []sdk.Tool
	
	for _, srv := range b.servers {
		for _, mcpTool := range srv.Tools() {
			// Convert MCP tool to sdk.Tool
			tool := sdk.Tool{
				Name:        mcpTool.Name,
				Description: mcpTool.Description,
				InputSchema: mcpTool.InputSchema,
			}
			tools = append(tools, tool)
		}
	}

	return tools
}

// CallTool routes a tool call to the appropriate MCP server.
func (b *Bridge) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	b.mu.RLock()
	srvName, ok := b.toolToSrv[name]
	if !ok {
		b.mu.RUnlock()
		return "", fmt.Errorf("tool %q not found", name)
	}
	
	srv, ok := b.servers[srvName]
	if !ok {
		b.mu.RUnlock()
		return "", fmt.Errorf("server %q not found for tool %q", srvName, name)
	}
	b.mu.RUnlock()

	// Call the tool
	result, err := srv.CallTool(ctx, name, args)
	if err != nil {
		return "", fmt.Errorf("call tool %q on server %q: %w", name, srvName, err)
	}

	if result.IsError {
		// Tool execution resulted in an error
		return formatToolResult(result), fmt.Errorf("tool error")
	}

	return formatToolResult(result), nil
}

// formatToolResult formats the tool result as a string for wllr.
func formatToolResult(result *CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}

	// For now, concatenate all text content
	// TODO: handle other content types (images, resources)
	var out string
	for _, item := range result.Content {
		if item.Type == "text" {
			out += item.Text
		}
	}
	return out
}

// Close stops all MCP servers.
func (b *Bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var errs []error
	for name, srv := range b.servers {
		if err := srv.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close server %q: %w", name, err))
		}
	}

	b.servers = make(map[string]*Server)
	b.toolToSrv = make(map[string]string)

	if len(errs) > 0 {
		return errs[0]
	}

	return nil
}

// ToolHandler returns a function that can be used as an extension tool handler.
func (b *Bridge) ToolHandler() func(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		var argsMap map[string]interface{}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &argsMap); err != nil {
				return "", fmt.Errorf("unmarshal args: %w", err)
			}
		}
		return b.CallTool(ctx, name, argsMap)
	}
}
