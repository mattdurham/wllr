package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// Server represents a running MCP server process.
type Server struct {
	config ServerConfig
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	cmd     *exec.Cmd
	pending map[int]chan *JSONRPCResponse
	info    ServerInfo
	name    string
	tools   []Tool

	mu     sync.Mutex
	nextID atomic.Int32
}

// NewServer creates a new MCP server instance but does not start it.
func NewServer(name string, config ServerConfig) *Server {
	return &Server{
		name:    name,
		config:  config,
		pending: make(map[int]chan *JSONRPCResponse),
	}
}

// Start spawns the MCP server subprocess and performs the initialize handshake.
func (s *Server) Start(ctx context.Context) error {
	// Build command
	cmd := exec.CommandContext(ctx, s.config.Command, s.config.Args...)

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range s.config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set up stdio pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	s.stdin = stdin

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	s.stdout = stdout

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	s.stderr = stderr

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}
	s.cmd = cmd

	// Start goroutines to handle stdout and stderr
	go s.readLoop()
	go s.logStderr()

	// Perform initialize handshake
	if err := s.initialize(ctx); err != nil {
		_ = s.Close()
		return fmt.Errorf("initialize handshake: %w", err)
	}

	// Discover tools
	if err := s.discoverTools(ctx); err != nil {
		_ = s.Close()
		return fmt.Errorf("discover tools: %w", err)
	}

	slog.Info("mcp: server started", "name", s.name, "tools", len(s.tools))
	return nil
}

// initialize performs the MCP initialize handshake.
func (s *Server) initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo: ClientInfo{
			Name:    "wllr",
			Version: "0.1.0",
		},
	}

	var result InitializeResult
	if err := s.call(ctx, "initialize", params, &result); err != nil {
		return err
	}

	s.info = result.ServerInfo
	slog.Debug(
		"mcp: initialized",
		"name",
		s.name,
		"server",
		result.ServerInfo.Name,
		"version",
		result.ServerInfo.Version,
	)

	// Send initialized notification
	if err := s.notify("notifications/initialized", nil); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	return nil
}

// discoverTools calls tools/list to get the list of available tools.
func (s *Server) discoverTools(ctx context.Context) error {
	var result ListToolsResult
	if err := s.call(ctx, "tools/list", nil, &result); err != nil {
		return err
	}

	s.tools = result.Tools
	return nil
}

// Tools returns the list of tools provided by this server.
func (s *Server) Tools() []Tool {
	return s.tools
}

// CallTool invokes a tool on the MCP server.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error) {
	params := CallToolParams{
		Name:      name,
		Arguments: args,
	}

	var result CallToolResult
	if err := s.call(ctx, "tools/call", params, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// call sends a JSON-RPC request and waits for the response.
func (s *Server) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	id := int(s.nextID.Add(1))

	req := JSONRPCRequest{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Register pending request
	respCh := make(chan *JSONRPCResponse, 1)
	s.mu.Lock()
	s.pending[id] = respCh
	s.mu.Unlock()

	// Clean up on exit
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	err = func() (writeErr error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stdin == nil {
			return fmt.Errorf("server not started")
		}
		_, writeErr = s.stdin.Write(append(data, '\n'))
		return
	}()
	if err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("unmarshal result: %w", err)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify sends a JSON-RPC notification (no response expected).
func (s *Server) notify(method string, params interface{}) error {
	req := JSONRPCRequest{
		JSONRPC: jsonrpcVersion,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.stdin.Write(append(data, '\n'))
	if err != nil {
		return fmt.Errorf("write notification: %w", err)
	}

	return nil
}

// readLoop reads JSON-RPC responses from stdout.
func (s *Server) readLoop() {
	scanner := bufio.NewScanner(s.stdout)
	for scanner.Scan() {
		line := scanner.Bytes()

		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			slog.Warn("mcp: invalid json-rpc response", "name", s.name, "error", err, "line", string(line))
			continue
		}

		// Deliver to pending request
		s.mu.Lock()
		ch, ok := s.pending[resp.ID]
		s.mu.Unlock()

		if ok {
			ch <- &resp
		} else {
			slog.Warn("mcp: unexpected response", "name", s.name, "id", resp.ID)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("mcp: read error", "name", s.name, "error", err)
	}
}

// logStderr logs stderr output from the MCP server.
func (s *Server) logStderr() {
	scanner := bufio.NewScanner(s.stderr)
	for scanner.Scan() {
		slog.Debug("mcp: server stderr", "name", s.name, "line", scanner.Text())
	}
}

// Close terminates the MCP server process.
func (s *Server) Close() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}

	// Close stdin to signal shutdown
	if s.stdin != nil {
		_ = s.stdin.Close()
	}

	// Wait for process to exit (with timeout handled by context)
	if err := s.cmd.Wait(); err != nil {
		// Process may have already exited or been killed
		slog.Debug("mcp: server exit", "name", s.name, "error", err)
	}

	slog.Info("mcp: server stopped", "name", s.name)
	return nil
}
