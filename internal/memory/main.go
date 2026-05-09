// Package memory implements the Engram persistent memory extension for wllr.
// It registers six native Go tools on the extension host:
//
//   - memory_store   — store a new engram
//   - memory_search  — semantic + attribute search
//   - memory_get     — retrieve a single engram by ID
//   - memory_update  — update content/tags/importance
//   - memory_delete  — delete by ID
//   - memory_export  — export to JSON or Markdown
//
// Call Register(h) once during startup (e.g. from cmd/main.go) to activate
// all tools. The SQLite database lives at ~/.wllr/memory.db and persists
// across sessions.
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// ──────────────────────────────────────────────────────────────
// Context capture — updated before each agent turn
// ──────────────────────────────────────────────────────────────

var (
	ctxMu      sync.RWMutex
	ctxCWD     string
	ctxRepo    string
	ctxUser    string
	ctxSession string
)

// SetContext stores the current environment context. Call this from a
// before_agent_start hook so every memory_store picks up the right cwd/repo/user.
func SetContext(cwd, repo, user, sessionID string) {
	ctxMu.Lock()
	defer ctxMu.Unlock()
	ctxCWD = cwd
	ctxRepo = repo
	ctxUser = user
	ctxSession = sessionID
}

func getContext() (cwd, repo, user, sessionID string) {
	ctxMu.RLock()
	defer ctxMu.RUnlock()
	return ctxCWD, ctxRepo, ctxUser, ctxSession
}

// ──────────────────────────────────────────────────────────────
// Register — call once from cmd/main.go
// ──────────────────────────────────────────────────────────────

// Register opens the SQLite database and registers all memory tools on h.
// Returns an error only if the database cannot be opened; tool handler
// errors are returned as tool results rather than panicking.
func Register(h *extension.Host) error {
	db, err := OpenDB()
	if err != nil {
		return fmt.Errorf("memory: open db: %w", err)
	}
	// db is intentionally never closed — it lives for the process lifetime.

	registerStore(h, db)
	registerSearch(h, db)
	registerGet(h, db)
	registerUpdate(h, db)
	registerDelete(h, db)
	registerExport(h, db)
	return nil
}

// ──────────────────────────────────────────────────────────────
// Tool: memory_store
// ──────────────────────────────────────────────────────────────

func registerStore(h *extension.Host, db *sql.DB) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "memory_store",
		Description: "Store a new engram (memory unit). Embedding is generated automatically. Context (cwd, repo, user) is captured from the environment.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "content":    { "type": "string",  "description": "The memory to store" },
    "summary":    { "type": "string",  "description": "Optional short summary for display" },
    "tags":       { "type": "array",   "items": { "type": "string" }, "description": "Optional tags" },
    "importance": { "type": "number",  "description": "0.0–1.0 importance weight (default 0.5)" }
  },
  "required": ["content"]
}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Content    string   `json:"content"`
			Summary    string   `json:"summary"`
			Tags       []string `json:"tags"`
			Importance float64  `json:"importance"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Content == "" {
			return `{"error":"content is required"}`, true
		}
		cwd, repo, user, sessionID := getContext()
		id, err := Store(db, StoreOpts{
			Content:    in.Content,
			Summary:    in.Summary,
			Tags:       in.Tags,
			Importance: in.Importance,
			CWD:        cwd,
			Repo:       repo,
			User:       user,
			SessionID:  sessionID,
		})
		if err != nil {
			return jsonError(err), true
		}
		out, _ := json.Marshal(map[string]any{"id": id, "stored": true})
		return string(out), false
	})
}

// ──────────────────────────────────────────────────────────────
// Tool: memory_search
// ──────────────────────────────────────────────────────────────

func registerSearch(h *extension.Host, db *sql.DB) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "memory_search",
		Description: "Search engrams by semantic similarity and/or attributes. Returns top-k results ranked by similarity × importance × recency.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query":     { "type": "string",  "description": "Semantic search query" },
    "repo":      { "type": "string",  "description": "Filter by repo name" },
    "cwd":       { "type": "string",  "description": "Filter by working directory" },
    "user":      { "type": "string",  "description": "Filter by user" },
    "tags":      { "type": "array",   "items": { "type": "string" }, "description": "Filter: must match all tags" },
    "limit":     { "type": "integer", "description": "Max results (default 10)" },
    "min_score": { "type": "number",  "description": "Minimum score 0–1 (default 0.3)" }
  }
}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Query    string   `json:"query"`
			Repo     string   `json:"repo"`
			CWD      string   `json:"cwd"`
			User     string   `json:"user"`
			Tags     []string `json:"tags"`
			Limit    int      `json:"limit"`
			MinScore float64  `json:"min_score"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return jsonError(err), true
		}
		results, err := Search(db, SearchOpts{
			Query:    in.Query,
			Repo:     in.Repo,
			CWD:      in.CWD,
			User:     in.User,
			Tags:     in.Tags,
			Limit:    in.Limit,
			MinScore: in.MinScore,
		})
		if err != nil {
			return jsonError(err), true
		}
		out, _ := json.Marshal(results)
		return string(out), false
	})
}

// ──────────────────────────────────────────────────────────────
// Tool: memory_get
// ──────────────────────────────────────────────────────────────

func registerGet(h *extension.Host, db *sql.DB) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "memory_get",
		Description: "Retrieve a single engram by ID. Increments its access count.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": { "type": "string", "description": "Engram UUID" }
  },
  "required": ["id"]
}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.ID == "" {
			return `{"error":"id is required"}`, true
		}
		e, err := Get(db, in.ID)
		if err != nil {
			return jsonError(err), true
		}
		out, _ := json.Marshal(e)
		return string(out), false
	})
}

// ──────────────────────────────────────────────────────────────
// Tool: memory_update
// ──────────────────────────────────────────────────────────────

func registerUpdate(h *extension.Host, db *sql.DB) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "memory_update",
		Description: "Update content, tags, or importance of an existing engram. Re-generates embedding if content changes.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id":         { "type": "string" },
    "content":    { "type": "string" },
    "tags":       { "type": "array", "items": { "type": "string" } },
    "importance": { "type": "number" }
  },
  "required": ["id"]
}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			ID         string   `json:"id"`
			Content    *string  `json:"content"`
			Tags       []string `json:"tags"`
			Importance *float64 `json:"importance"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.ID == "" {
			return `{"error":"id is required"}`, true
		}
		if err := Update(db, UpdateOpts{
			ID:         in.ID,
			Content:    in.Content,
			Tags:       in.Tags,
			Importance: in.Importance,
		}); err != nil {
			return jsonError(err), true
		}
		out, _ := json.Marshal(map[string]any{"id": in.ID, "updated": true})
		return string(out), false
	})
}

// ──────────────────────────────────────────────────────────────
// Tool: memory_delete
// ──────────────────────────────────────────────────────────────

func registerDelete(h *extension.Host, db *sql.DB) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "memory_delete",
		Description: "Delete an engram by ID.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": { "type": "string" }
  },
  "required": ["id"]
}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.ID == "" {
			return `{"error":"id is required"}`, true
		}
		if err := Delete(db, in.ID); err != nil {
			return jsonError(err), true
		}
		out, _ := json.Marshal(map[string]any{"id": in.ID, "deleted": true})
		return string(out), false
	})
}

// ──────────────────────────────────────────────────────────────
// Tool: memory_export
// ──────────────────────────────────────────────────────────────

func registerExport(h *extension.Host, db *sql.DB) {
	h.RegisterNativeTool(sdk.Tool{
		Name:        "memory_export",
		Description: "Export engrams to a JSON or Markdown file.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":   { "type": "string", "description": "Output file path" },
    "format": { "type": "string", "enum": ["json","markdown"], "description": "Default: json" },
    "repo":   { "type": "string", "description": "Filter by repo (optional)" },
    "tags":   { "type": "array",  "items": { "type": "string" }, "description": "Filter by tags (optional)" }
  },
  "required": ["path"]
}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Path   string   `json:"path"`
			Format string   `json:"format"`
			Repo   string   `json:"repo"`
			Tags   []string `json:"tags"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return `{"error":"path is required"}`, true
		}
		n, err := Export(db, ExportOpts{
			Path:   in.Path,
			Format: in.Format,
			Repo:   in.Repo,
			Tags:   in.Tags,
		})
		if err != nil {
			return jsonError(err), true
		}
		out, _ := json.Marshal(map[string]any{
			"path":     in.Path,
			"exported": n,
			"format":   in.Format,
		})
		return string(out), false
	})
}

// ──────────────────────────────────────────────────────────────
// Git context helpers
// ──────────────────────────────────────────────────────────────

// GitRepoName returns the basename of the git remote origin URL, or "" if
// the working directory is not inside a git repository.
func GitRepoName() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// GitUserName returns git config user.name.
func GitUserName() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	return ""
}

// ──────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────

func jsonError(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}
