package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattdurham/wllr/modules/extension"
)

// wllrExtensionsDir returns the default user extensions directory.
func wllrExtensionsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".wllr", "extensions")
}

// loadExtensionsFromSubdirs scans dir for subdirectories, loading the first
// *.wasm file found in each subdirectory (alongside an optional <name>.json manifest).
func loadExtensionsFromSubdirs(ctx context.Context, h *extension.Host, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var loaded []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subDir := filepath.Join(dir, e.Name())
		subs, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			if sub.IsDir() || filepath.Ext(sub.Name()) != ".wasm" {
				continue
			}
			path := filepath.Join(subDir, sub.Name())
			if loadErr := h.Load(ctx, path); loadErr != nil {
				fmt.Fprintf(os.Stderr, "wllr: load extension %q: %v\n", sub.Name(), loadErr)
				continue
			}
			loaded = append(loaded, path)
			break // one WASM per subdirectory
		}
	}
	return loaded
}

// loadExtensionsFlat scans dir for *.wasm files directly (flat layout).
func loadExtensionsFlat(ctx context.Context, h *extension.Host, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wllr: extensions dir %q not found, skipping\n", dir)
		return nil
	}
	var loaded []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".wasm" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if loadErr := h.Load(ctx, path); loadErr != nil {
			fmt.Fprintf(os.Stderr, "wllr: load extension %q: %v\n", e.Name(), loadErr)
			continue
		}
		loaded = append(loaded, path)
	}
	return loaded
}
