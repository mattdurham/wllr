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

// LSP Server State
type LSPServer struct {
	Name         string
	Command      string
	Args         []string
	Cmd          *exec.Cmd
	StdinPipe    io.WriteCloser
	StdoutPipe   *bufio.Reader
	Stderr       []byte
	RequestID    int
	Initialized  bool
	WorkspaceURI string
	Mutex        sync.Mutex
}

type LSPServerInfo struct {
	ID           string   `json:"id"`
	Command      string   `json:"command"`
	Args         []string `json:"args,omitempty"`
	Role         string   `json:"role"`
	FilePatterns []string `json:"file_patterns,omitempty"`
	Languages    []string `json:"languages,omitempty"`
}

var LSP_SERVERS = map[string]LSPServerInfo{
	"gopls": {
		ID:           "gopls",
		Command:      "gopls",
		Role:         "primary",
		FilePatterns: []string{"**/*.go", "**/go.mod", "**/go.sum"},
		Languages:    []string{"go"},
	},
}

var fileToServer = map[string][]string{
	".go": {"gopls"},
}

func main() {
	fmt.Println("LSP extension native mode")
}
