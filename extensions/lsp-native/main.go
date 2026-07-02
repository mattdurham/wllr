// Package main is the native LSP extension for wllr.
// It bridges to WASM-based lsp-bridge for process management and stdio handling.

package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// LSPServer tracks native LSP process state.
type LSPServer struct {
	StdinPipe    io.WriteCloser
	Cmd          *exec.Cmd
	StdoutPipe   *bufio.Reader
	Name         string
	Command      string
	WorkspaceURI string
	Args         []string
	Stderr       []byte
	RequestID    int
	Mutex        sync.Mutex
	Initialized  bool
}

var lspServers = map[string]LSPServerInfo{
	"gopls": {
		ID:           "gopls",
		Command:      "gopls",
		Role:         "primary",
		FilePatterns: []string{"**/*.go", "**/go.mod", "**/go.sum"},
		Languages:    []string{"go"},
	},
}

func main() {
	_ = lspServers
	fmt.Println("LSP extension native mode")
}
