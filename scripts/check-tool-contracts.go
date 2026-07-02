//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	registerToolRe       = regexp.MustCompile(`(?s)RegisterTool\(\s*"([^"]+)"`)
	registerNativeToolRe = regexp.MustCompile(`(?s)RegisterNativeTool\(\s*sdk\.Tool\{[^}]*Name:\s*"([^"]+)"`)
	contractHeadingRe    = regexp.MustCompile("(?m)^### `([^`]+)`$")
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}

	registered, err := registeredTools(root)
	if err != nil {
		fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "docs/tool-contracts.md"))
	if err != nil {
		fatal(err)
	}
	documented := map[string]bool{}
	for _, match := range contractHeadingRe.FindAllStringSubmatch(string(data), -1) {
		documented[match[1]] = true
	}

	var missing []string
	for name, file := range registered {
		if !documented[name] {
			missing = append(missing, fmt.Sprintf("%s (%s)", name, file))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "missing tool contract docs:\n  %s\n", strings.Join(missing, "\n  "))
		os.Exit(1)
	}
}

func registeredTools(root string) (map[string]string, error) {
	registered := map[string]string{}
	for _, dir := range []string{"cmd", "extensions"} {
		if err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "wllrsdk.go" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			file, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			text := string(data)
			for _, match := range registerToolRe.FindAllStringSubmatch(text, -1) {
				registered[match[1]] = file
			}
			for _, match := range registerNativeToolRe.FindAllStringSubmatch(text, -1) {
				registered[match[1]] = file
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return registered, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
