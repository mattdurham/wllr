//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	engramVersion = "1.15.10"
	engramBaseURL = "https://github.com/Gentleman-Programming/engram/releases/download/v" + engramVersion
	engramBinDir  = "~/.wllr/bin"
	engramBin     = "~/.wllr/bin/engram"

	// storeKeyInstalled records the installed version so we can detect upgrades.
	storeKeyInstalled = "engram_installed_version"
)

// ─── Asset URL resolution ─────────────────────────────────────────────────────

// assetName returns the release asset filename for the given OS and arch.
// Returns ("", false) if the platform is not supported.
func assetName(goos, goarch string) (string, bool) {
	switch {
	case goos == "darwin" && goarch == "amd64":
		return fmt.Sprintf("engram_%s_darwin_amd64.tar.gz", engramVersion), true
	case goos == "darwin" && goarch == "arm64":
		return fmt.Sprintf("engram_%s_darwin_arm64.tar.gz", engramVersion), true
	case goos == "linux" && goarch == "amd64":
		return fmt.Sprintf("engram_%s_linux_amd64.tar.gz", engramVersion), true
	case goos == "linux" && goarch == "arm64":
		return fmt.Sprintf("engram_%s_linux_arm64.tar.gz", engramVersion), true
	case goos == "windows" && goarch == "amd64":
		return fmt.Sprintf("engram_%s_windows_amd64.zip", engramVersion), true
	case goos == "windows" && goarch == "arm64":
		return fmt.Sprintf("engram_%s_windows_arm64.zip", engramVersion), true
	default:
		return "", false
	}
}

// binName returns the engram binary name for the OS (engram vs engram.exe).
func binName(goos string) string {
	if goos == "windows" {
		return "engram.exe"
	}
	return "engram"
}

// ─── Install logic ────────────────────────────────────────────────────────────

// isInstalled returns true if the engram binary already exists at engramBin.
func isInstalled(goos string) bool {
	bin := expandHome(engramBin)
	if goos == "windows" {
		bin += "e" // engram.exe
	}
	out, err := Exec(fmt.Sprintf("test -f %s && echo yes || echo no", shellQuote(bin)), "")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "yes"
}

// install downloads and installs the engram binary for the given OS/arch.
// It returns an error message on failure, or "" on success.
func install(goos, goarch string) string {
	asset, ok := assetName(goos, goarch)
	if !ok {
		return fmt.Sprintf("unsupported platform: %s/%s", goos, goarch)
	}

	url := engramBaseURL + "/" + asset
	dir := expandHome(engramBinDir)
	bin := expandHome(engramBin)
	if goos == "windows" {
		bin += ".exe"
	}

	SetStatus("engram", "installing…")

	// Ensure the bin directory exists.
	if _, err := Exec(fmt.Sprintf("mkdir -p %s", shellQuote(dir)), ""); err != nil {
		return "failed to create bin dir: " + err.Error()
	}

	tmpDir := dir + "/.engram-install-tmp"
	if _, err := Exec(fmt.Sprintf("mkdir -p %s", shellQuote(tmpDir)), ""); err != nil {
		return "failed to create tmp dir: " + err.Error()
	}

	// Download the archive.
	archivePath := tmpDir + "/" + asset
	_, err := Exec(fmt.Sprintf("curl -fsSL -o %s %s", shellQuote(archivePath), shellQuote(url)), "")
	if err != nil {
		cleanup(tmpDir)
		return "download failed: " + err.Error()
	}

	// Extract.
	if strings.HasSuffix(asset, ".tar.gz") {
		_, err = Exec(fmt.Sprintf("tar -xzf %s -C %s", shellQuote(archivePath), shellQuote(tmpDir)), "")
	} else {
		// .zip (Windows)
		_, err = Exec(fmt.Sprintf("unzip -o %s -d %s", shellQuote(archivePath), shellQuote(tmpDir)), "")
	}
	if err != nil {
		cleanup(tmpDir)
		return "extraction failed: " + err.Error()
	}

	// Move the binary into place.
	extracted := tmpDir + "/" + binName(goos)
	_, err = Exec(fmt.Sprintf("mv %s %s && chmod +x %s",
		shellQuote(extracted), shellQuote(bin), shellQuote(bin)), "")
	if err != nil {
		cleanup(tmpDir)
		return "install failed: " + err.Error()
	}

	cleanup(tmpDir)
	StoreSet(storeKeyInstalled, engramVersion)
	return ""
}

func cleanup(dir string) {
	Exec(fmt.Sprintf("rm -rf %s", shellQuote(dir)), "") //nolint
}

// ─── System prompt ────────────────────────────────────────────────────────────

const engramSystemPrompt = `
## Persistent Memory (Engram)

You have access to a persistent memory store via the Engram MCP tools. Use them
proactively to save important context and retrieve relevant memories at the start
of each session.

### Key tools

| Tool | Purpose |
|------|---------|
| mem_session_start | Call at the start of EVERY session to load relevant context |
| mem_save | Save a significant decision, insight, or finding |
| mem_search | Search memories by keyword or topic |
| mem_context | Get memories relevant to the current working directory / project |
| mem_session_end | Summarise and close the session |
| mem_update | Correct or enrich an existing memory |
| mem_delete | Remove a memory that is no longer accurate |
| mem_stats | Overview of stored memories |

### When to save

- Architecture decisions and the reasoning behind them
- Bug root causes and fixes
- Learnings about the codebase that took time to discover
- User preferences and working style
- Important file locations, patterns, and conventions

### Session lifecycle

1. Always call mem_session_start at the beginning of each conversation.
2. Save noteworthy findings with mem_save as they occur.
3. Call mem_session_end before finishing, with a concise summary.
`

// ─── Init ─────────────────────────────────────────────────────────────────────

func init() {
	// Register the manual install/upgrade tool.
	RegisterTool(
		"memory_install",
		"Install or upgrade the Engram memory binary. Run this if Engram is missing or outdated.",
		json.RawMessage(`{"type":"object","properties":{}}`),
	)

	// Register a /memory slash command for convenience.
	RegisterCommand("memory", "Show Engram memory status and install info")

	// On session start: ensure engram is installed, then inject the system prompt.
	OnSessionStart(func() {
		ensureEngram()
	})

	// Handle the memory_install tool call.
	OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
		if name != "memory_install" {
			return "", false
		}
		goos, goarch, err := GetOS()
		if err != nil {
			return fmt.Sprintf(`{"error":%q}`, err.Error()), true
		}
		if errMsg := install(goos, goarch); errMsg != "" {
			return fmt.Sprintf(`{"error":%q}`, errMsg), true
		}
		bin := expandHome(engramBin)
		if goos == "windows" {
			bin += ".exe"
		}
		SetStatus("engram", "v"+engramVersion)
		Notify(fmt.Sprintf("✅ Engram v%s installed at %s", engramVersion, bin))
		return fmt.Sprintf(`{"installed":true,"version":%q,"path":%q}`, engramVersion, bin), false
	})

	// Handle the /memory command.
	OnCommand("memory", func(args []string) {
		goos, _, err := GetOS()
		if err != nil {
			Modal("Engram: could not detect OS — " + err.Error())
			return
		}
		installed := isInstalled(goos)
		ver, _ := StoreGet(storeKeyInstalled)
		bin := expandHome(engramBin)
		if goos == "windows" {
			bin += ".exe"
		}
		status := "not installed"
		if installed {
			status = "installed"
		}
		lines := []string{
			"# Engram Memory Status",
			"",
			fmt.Sprintf("**Status:** %s", status),
			fmt.Sprintf("**Version:** %s", ver),
			fmt.Sprintf("**Binary:** %s", bin),
			fmt.Sprintf("**Release:** v%s", engramVersion),
			"",
			"Use the `memory_install` tool to install or upgrade.",
		}
		Modal(strings.Join(lines, "\n"))
	})
}

// ensureEngram checks whether engram is installed and installs it if not.
// It also appends the system prompt so the LLM knows how to use engram.
func ensureEngram() {
	goos, goarch, err := GetOS()
	if err != nil {
		Logf(2, "engram: GetOS failed: %v", err)
		SetStatus("engram", "error")
		return
	}

	alreadyInstalled := isInstalled(goos)

	// Check if we need to upgrade (stored version differs from target).
	storedVersion, hasStored := StoreGet(storeKeyInstalled)
	needsUpgrade := hasStored && storedVersion != engramVersion

	if !alreadyInstalled || needsUpgrade {
		action := "installing"
		if needsUpgrade {
			action = "upgrading"
		}
		// Use status bar only — do not write to the chat window during background install.
		Logf(1, "engram: %s v%s for %s/%s", action, engramVersion, goos, goarch)
		SetStatus("engram", action+"…")

		if errMsg := install(goos, goarch); errMsg != "" {
			Logf(3, "engram: install failed: %s", errMsg)
			SetStatus("engram", "install failed")
			Notify("❌ Engram install failed: " + errMsg)
			return
		}

		bin := expandHome(engramBin)
		if goos == "windows" {
			bin += ".exe"
		}
		Notify(fmt.Sprintf("✅ Engram v%s ready at %s", engramVersion, bin))
	}

	SetStatus("engram", "v"+engramVersion)
	AppendSystemPrompt(engramSystemPrompt)
}

// ─── Utilities ────────────────────────────────────────────────────────────────

// expandHome replaces a leading ~ with the value of $HOME.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := GetEnv("HOME")
	if err != nil || home == "" {
		// Windows fallback
		home, _ = GetEnv("USERPROFILE")
	}
	if home == "" {
		return path
	}
	return home + path[1:]
}

// shellQuote wraps a string in single quotes, escaping any single quotes within.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func main() {}
