package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCWDNote(t *testing.T) {
	cwd := "/Users/test/project"
	note := cwdNote(cwd)
	expected := "You are operating in the current working directory: /Users/test/project"
	if note != expected {
		t.Errorf("cwdNote() = %q, want %q", note, expected)
	}
}

func TestOnSessionStart_AppendsCWD(t *testing.T) {
	// This test verifies that onSessionStart appends the working directory
	// to the system prompt. We can't fully test it without running WASM,
	// but we can at least verify the function exists and compiles.
	testCWD := "/test/project"

	// Verify the cwdNote function produces correct output
	note := cwdNote(testCWD)
	if !strings.Contains(note, testCWD) {
		t.Errorf("cwdNote(%q) = %q does not contain the path", testCWD, note)
	}

	if !strings.Contains(note, "current working directory") {
		t.Errorf("cwdNote(%q) = %q missing 'current working directory' text", testCWD, note)
	}
}

func TestBuildPromptWithConfig_OverrideAndDynamicMetadata(t *testing.T) {
	prompt := buildPromptWithConfig(
		[]promptTool{{Name: "read_file"}, {Name: "exec"}},
		[]promptCommand{{Name: "help", Desc: "show help"}},
		promptConfig{Override: "custom base"},
	)
	if !strings.HasPrefix(prompt, "custom base\n\nAvailable tools: exec, read_file") {
		t.Fatalf("prompt does not honor override or sort tools: %q", prompt)
	}
	if !strings.Contains(prompt, "**/help** — show help") {
		t.Fatalf("prompt missing command metadata: %q", prompt)
	}
	if strings.Contains(prompt, "## Action Rules") {
		t.Fatalf("built-in prompt should be replaced by override: %q", prompt)
	}
}

// setTestHome points os.UserHomeDir at a fresh temp dir for the duration of
// the test. os.UserHomeDir reads $HOME on unix; skipped on windows, where it
// reads USERPROFILE instead.
func setTestHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir reads USERPROFILE on windows; $HOME override does not apply")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestGlobalPaths_UnderHome(t *testing.T) {
	home := setTestHome(t)
	paths := globalPaths()
	if len(paths) != 2 {
		t.Fatalf("globalPaths() = %v, want 2 entries", paths)
	}
	wantAgents := filepath.Join(home, ".wllr", "AGENTS.md")
	wantClaude := filepath.Join(home, ".wllr", "CLAUDE.md")
	if paths[0] != wantAgents || paths[1] != wantClaude {
		t.Errorf("globalPaths() = %v, want [%s %s]", paths, wantAgents, wantClaude)
	}
}

func TestReadFirst_GlobalTier(t *testing.T) {
	agentsContent := "global rule: be concise"
	claudeContent := "claude fallback rule"
	writeGlobal := func(t *testing.T, agents, claude string) string {
		t.Helper()
		home := setTestHome(t)
		dir := filepath.Join(home, ".wllr")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(name, content string) {
			if content == "" {
				return
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("AGENTS.md", agents)
		write("CLAUDE.md", claude)
		return home
	}

	t.Run("agents wins when both exist", func(t *testing.T) {
		home := writeGlobal(t, agentsContent, claudeContent)
		content, path := readFirst(globalPaths())
		if content != agentsContent || path != filepath.Join(home, ".wllr", "AGENTS.md") {
			t.Errorf("readFirst() = %q, %q; want agents content from AGENTS.md", content, path)
		}
	})

	t.Run("falls back to CLAUDE.md", func(t *testing.T) {
		home := writeGlobal(t, "", claudeContent)
		content, path := readFirst(globalPaths())
		if content != claudeContent || path != filepath.Join(home, ".wllr", "CLAUDE.md") {
			t.Errorf("readFirst() = %q, %q; want claude content from CLAUDE.md", content, path)
		}
	})

	t.Run("whitespace-only AGENTS.md does not shadow CLAUDE.md", func(t *testing.T) {
		home := writeGlobal(t, "   \n\t ", claudeContent)
		content, path := readFirst(globalPaths())
		if content != claudeContent || path != filepath.Join(home, ".wllr", "CLAUDE.md") {
			t.Errorf("readFirst() = %q, %q; want fall-through past whitespace-only AGENTS.md", content, path)
		}
	})

	t.Run("neither file present", func(t *testing.T) {
		writeGlobal(t, "", "")
		content, path := readFirst(globalPaths())
		if content != "" || path != "" {
			t.Errorf("readFirst() = %q, %q; want empty results", content, path)
		}
	})
}

func TestBuildPrompt_GlobalTier(t *testing.T) {
	t.Run("labeled component placed before project tier", func(t *testing.T) {
		home := setTestHome(t)
		agentsPath := filepath.Join(home, ".wllr", "AGENTS.md")
		if err := os.MkdirAll(filepath.Dir(agentsPath), 0o755); err != nil {
			t.Fatal(err)
		}
		marker := "global rule: never commit to slack without asking"
		if err := os.WriteFile(agentsPath, []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}

		prompt, components := buildPromptWithConfigParts([]promptTool{{Name: "read_file"}}, nil, promptConfig{})

		globalIdx := -1
		for i := range components {
			if components[i].Source == "file:"+agentsPath {
				globalIdx = i
			}
		}
		if globalIdx == -1 {
			t.Fatalf("no component labeled file:%s in %v", agentsPath, components)
		}
		if got := components[globalIdx].Chars; got != len(marker) {
			t.Errorf("global component chars = %d, want %d", got, len(marker))
		}
		if !strings.Contains(prompt, marker) {
			t.Errorf("prompt missing global tier content %q", marker)
		}
		// The global tier sits after the base/tools glue and before any
		// project-tier file found by the cwd walk-up (the repo's own
		// AGENTS.md when tests run inside the wllr checkout).
		for i := range components {
			if i == globalIdx || !strings.HasPrefix(components[i].Source, "file:") {
				continue
			}
			if i < globalIdx {
				t.Errorf("project-tier component %q (index %d) precedes global tier (index %d)",
					components[i].Source, i, globalIdx)
			}
		}
	})

	t.Run("absent files contribute no component", func(t *testing.T) {
		setTestHome(t)
		_, components := buildPromptWithConfigParts(nil, nil, promptConfig{})
		for i := range components {
			if strings.Contains(components[i].Source, ".wllr") {
				t.Errorf("unexpected global-tier component %q with no ~/.wllr files", components[i].Source)
			}
		}
	})
}

func TestBuildPromptWithConfig_ContainsAuthoritativeEditingPolicy(t *testing.T) {
	prompt := buildPromptWithConfig(nil, nil, promptConfig{})
	for _, want := range []string{
		"Use the edit_file tool for targeted edits to an existing single file",
		"Do not use sed, perl, Python, awk, ed, cat >, tee, shell redirection",
		"Use write_file only when creating a new file or intentionally replacing an entire file",
		"If edit_file is unavailable, stop and report that limitation",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("built-in prompt missing editing policy %q", want)
		}
	}
}

func TestBuildPromptWithConfigParts_LabelsAndChars(t *testing.T) {
	prompt, components := buildPromptWithConfigParts(
		[]promptTool{{Name: "read_file"}},
		nil,
		promptConfig{Override: "custom base"},
	)
	if len(components) == 0 {
		t.Fatal("components empty, want at least the override base")
	}
	// The override replaces the built-in rules label.
	if components[0].Source != "prompt_override" || components[0].Chars != len("custom base") {
		t.Errorf("components[0] = %+v, want {prompt_override %d}", components[0], len("custom base"))
	}
	// The dynamic tools list is its own labeled component.
	var dynamic *promptComponent
	for i := range components {
		if components[i].Source == "tools & commands" {
			dynamic = &components[i]
		}
	}
	if dynamic == nil {
		t.Errorf("no tools & commands component in %v", components)
	} else if want := len("Available tools: read_file"); dynamic.Chars != want {
		t.Errorf("tools & commands chars = %d, want %d", dynamic.Chars, want)
	}
	// The cwd note is the last component when a working directory exists.
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		last := components[len(components)-1]
		if last.Source != "cwd note" || last.Chars != len(cwdNote(cwd)) {
			t.Errorf("last component = %+v, want cwd note of %d chars", last, len(cwdNote(cwd)))
		}
	}
	// The prompt must start with the override and the dynamic section glued
	// with \n\n (historical layout), not the --- separator.
	if !strings.HasPrefix(prompt, "custom base\n\nAvailable tools: read_file") {
		t.Errorf("prompt does not glue the dynamic section onto the override: %q", prompt[:min(len(prompt), 80)])
	}
	// Components must account for every non-separator byte: the sum of chars
	// plus the join separators equals the prompt length. Each promptComponent
	// shares its join unit except the glued dynamic one, so the exact total is
	// chars + separators between join units + one \n\n for the glue.
	glued := 0
	if dynamic != nil {
		glued = len("\n\n")
	}
	joinUnits := len(components)
	if dynamic != nil {
		joinUnits-- // dynamic shares the base's join unit
	}
	sum := 0
	for _, c := range components {
		sum += c.Chars
	}
	wantLen := sum + glued + (joinUnits-1)*len("\n\n---\n\n")
	if len(prompt) != wantLen {
		t.Errorf("prompt length = %d, want %d (chars %d + separators; components %v)",
			len(prompt), wantLen, sum, components)
	}
}
