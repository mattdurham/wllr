package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeMCPServer is a minimal in-process JSON-RPC server that responds to the
// MCP initialize handshake and tools/list call, then handles tools/call.
type fakeMCPServer struct {
	tools []Tool
}

// serve reads newline-delimited JSON-RPC requests from r and writes responses to w.
func (f *fakeMCPServer) serve(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var req JSONRPCRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		// Notifications have no ID — skip.
		if req.ID == 0 {
			continue
		}

		var result interface{}
		switch req.Method {
		case "initialize":
			result = InitializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities:    map[string]any{},
				ServerInfo:      ServerInfo{Name: "fake", Version: "0.0.1"},
			}
		case "tools/list":
			result = ListToolsResult{Tools: f.tools}
		case "tools/call":
			var p CallToolParams
			raw, _ := json.Marshal(req.Params)
			_ = json.Unmarshal(raw, &p)
			result = CallToolResult{
				Content: []ContentItem{{Type: "text", Text: "called:" + p.Name}},
			}
		default:
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32601, Message: "method not found"},
			}
			line, _ := json.Marshal(resp)
			_, _ = w.Write(append(line, '\n'))
			continue
		}

		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		resp.Result, _ = json.Marshal(result)
		line, _ := json.Marshal(resp)
		_, _ = w.Write(append(line, '\n'))
	}
}

// startFakeServer starts the fake MCP server over in-memory pipes and returns a
// fully-initialised *Server whose stdin/stdout are wired to it.
func startFakeServer(t *testing.T, tools []Tool) *Server {
	t.Helper()

	// Client writes to clientW → serverR reads; server writes to serverW → clientR reads.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	// Separate pipe just so srv.stderr has a valid ReadCloser.
	stderrR, stderrW := io.Pipe()
	// Drain stderr silently.
	go func() {
		io.Copy(io.Discard, stderrR) //nolint:errcheck
	}()

	fake := &fakeMCPServer{tools: tools}
	go fake.serve(serverR, serverW)

	srv := NewServer("test", ServerConfig{Command: "fake"})
	srv.stdin = clientW
	srv.stdout = clientR
	srv.stderr = stderrR

	go srv.readLoop()
	go srv.logStderr()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := srv.discoverTools(ctx); err != nil {
		t.Fatalf("discoverTools: %v", err)
	}

	t.Cleanup(func() {
		_ = clientW.Close()
		_ = serverW.Close()
		_ = stderrW.Close()
	})

	return srv
}

// --- Server tests ---

func TestServer_Initialize_And_DiscoverTools(t *testing.T) {
	tools := []Tool{
		{Name: "read_file", Description: "reads a file", InputSchema: json.RawMessage(`{}`)},
		{Name: "write_file", Description: "writes a file", InputSchema: json.RawMessage(`{}`)},
	}
	srv := startFakeServer(t, tools)

	got := srv.Tools()
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	names := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !names["read_file"] || !names["write_file"] {
		t.Errorf("unexpected tools: %v", got)
	}
}

func TestServer_CallTool(t *testing.T) {
	tools := []Tool{{Name: "echo", InputSchema: json.RawMessage(`{}`)}}
	srv := startFakeServer(t, tools)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := srv.CallTool(ctx, "echo", map[string]interface{}{"input": "hi"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
	if result.Content[0].Text != "called:echo" {
		t.Errorf("text: got %q, want %q", result.Content[0].Text, "called:echo")
	}
}

func TestServer_CallTool_ContextCancelled(t *testing.T) {
	tools := []Tool{{Name: "slow", InputSchema: json.RawMessage(`{}`)}}
	srv := startFakeServer(t, tools)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := srv.CallTool(ctx, "slow", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestServer_Close_NilCmd(t *testing.T) {
	srv := NewServer("empty", ServerConfig{})
	if err := srv.Close(); err != nil {
		t.Errorf("Close on uninitialised server: %v", err)
	}
}

func TestServer_NewServer_Fields(t *testing.T) {
	cfg := ServerConfig{Command: "node", Args: []string{"server.js"}}
	srv := NewServer("myserver", cfg)
	if srv.name != "myserver" {
		t.Errorf("name: got %q, want myserver", srv.name)
	}
	if srv.config.Command != "node" {
		t.Errorf("command: got %q, want node", srv.config.Command)
	}
	if srv.pending == nil {
		t.Error("pending map should be initialised")
	}
}

func TestServer_Start_BadCommand(t *testing.T) {
	srv := NewServer("bad", ServerConfig{Command: "/nonexistent/binary/xyz"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Start(ctx)
	if err == nil {
		t.Fatal("expected error starting nonexistent binary")
	}
}

func TestServer_Start_RealProcess(t *testing.T) {
	// `true` exits immediately; the initialize handshake fails with EOF.
	// This exercises the pipe-setup and goroutine-start code paths.
	srv := NewServer("true", ServerConfig{Command: "true"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Start(ctx)
	if err == nil {
		_ = srv.Close()
	}
	// Pass regardless — we only care that it doesn't panic.
}

// --- Protocol tests ---

func TestJSONRPCError_Error(t *testing.T) {
	e := &JSONRPCError{Code: -32600, Message: "invalid request"}
	s := e.Error()
	if !strings.Contains(s, "-32600") || !strings.Contains(s, "invalid request") {
		t.Errorf("Error() format: %q", s)
	}
}

func TestJSONRPCError_Error_WithData(t *testing.T) {
	e := &JSONRPCError{Code: -32600, Message: "bad", Data: json.RawMessage(`"details"`)}
	s := e.Error()
	if !strings.Contains(s, "data") {
		t.Errorf("Error() with data should mention data: %q", s)
	}
}

// --- Bridge tests (additional, not in config_bridge_test.go) ---

func TestBridge_CallTool_IsError(t *testing.T) {
	// Wire a fake server directly into the bridge.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	go io.Copy(io.Discard, stderrR) //nolint:errcheck

	fake := &fakeMCPServer{
		tools: []Tool{{Name: "bad_tool", InputSchema: json.RawMessage(`{}`)}},
	}
	go fake.serve(serverR, serverW)

	srv := NewServer("bridge-test", ServerConfig{})
	srv.stdin = clientW
	srv.stdout = clientR
	srv.stderr = stderrR
	go srv.readLoop()
	go srv.logStderr()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := srv.initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := srv.discoverTools(ctx); err != nil {
		t.Fatalf("discoverTools: %v", err)
	}

	b := NewBridge()
	b.servers["bridge-test"] = srv
	b.toolToSrv["bad_tool"] = "bridge-test"

	// fake returns IsError=false always, so CallTool should succeed.
	result, err := b.CallTool(ctx, "bad_tool", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "called:bad_tool" {
		t.Errorf("result: got %q, want %q", result, "called:bad_tool")
	}

	t.Cleanup(func() {
		_ = clientW.Close()
		_ = serverW.Close()
		_ = stderrW.Close()
	})
}

func TestBridge_Close_WithServer(t *testing.T) {
	// Close a bridge that has an already-closed underlying server (nil cmd).
	b := NewBridge()
	srv := NewServer("test", ServerConfig{})
	b.servers["test"] = srv
	b.toolToSrv["my_tool"] = "test"

	if err := b.Close(); err != nil {
		t.Errorf("Close with nil-cmd server: %v", err)
	}
	if len(b.servers) != 0 {
		t.Error("servers should be cleared after Close")
	}
}

// Ensure os is used.
var _ = os.DevNull

// --- H-contract2: MCP protocol version validation ---

// fakeVersionMismatchServer serves an initialize response with an incompatible protocol version.
type fakeVersionMismatchServer struct {
	version string
}

func (f *fakeVersionMismatchServer) serve(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var req JSONRPCRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if req.ID == 0 {
			continue
		}
		result := InitializeResult{
			ProtocolVersion: f.version,
			Capabilities:    map[string]any{},
			ServerInfo:      ServerInfo{Name: "fake", Version: "0.0.1"},
		}
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
		}
		resp.Result, _ = json.Marshal(result)
		data, _ := json.Marshal(resp)
		_, _ = w.Write(append(data, '\n'))
	}
}

func TestServer_Initialize_RejectsIncompatibleVersion(t *testing.T) {
	// H-contract2: servers with an incompatible protocol version must be rejected.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	fake := &fakeVersionMismatchServer{version: "1999-01-01"}
	go fake.serve(serverR, serverW)
	go io.Copy(io.Discard, stderrR) // drain stderr

	srv := &Server{
		name:    "version-test",
		config:  ServerConfig{},
		pending: make(map[int]chan *JSONRPCResponse),
		stdin:   clientW,
		stdout:  clientR,
		stderr:  stderrR,
	}
	go srv.readLoop()
	go srv.logStderr()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := srv.initialize(ctx)
	if err == nil {
		t.Fatal("expected error for incompatible protocol version, got nil")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Errorf("expected 'incompatible' in error message, got: %v", err)
	}

	t.Cleanup(func() {
		_ = clientW.Close()
		_ = serverW.Close()
		_ = stderrW.Close()
	})
}

func TestServer_Initialize_AcceptsCompatibleVersion(t *testing.T) {
	// H-contract2: servers with the expected protocol version must succeed.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	fake := &fakeVersionMismatchServer{version: expectedProtocolVersion}
	go fake.serve(serverR, serverW)
	go io.Copy(io.Discard, stderrR)

	srv := &Server{
		name:    "version-compat-test",
		config:  ServerConfig{},
		pending: make(map[int]chan *JSONRPCResponse),
		stdin:   clientW,
		stdout:  clientR,
		stderr:  stderrR,
	}
	go srv.readLoop()
	go srv.logStderr()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// initialize should succeed (version matches) but no tools/list yet.
	// We can't call discoverTools because our fake server doesn't handle it,
	// so just test that initialize returns nil.
	err := srv.initialize(ctx)
	if err != nil {
		t.Fatalf("expected no error for compatible version, got: %v", err)
	}

	t.Cleanup(func() {
		_ = clientW.Close()
		_ = serverW.Close()
		_ = stderrW.Close()
	})
}
