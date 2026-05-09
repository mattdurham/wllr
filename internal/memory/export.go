package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExportOpts controls how engrams are exported.
type ExportOpts struct {
	Path   string
	Format string // "json" | "markdown"
	Repo   string
	Tags   []string
}

// Export writes engrams to disk in the requested format.
func Export(db *sql.DB, opts ExportOpts) (int, error) {
	if opts.Format == "" {
		opts.Format = "json"
	}
	if opts.Path == "" {
		return 0, fmt.Errorf("memory: export path is required")
	}

	engrams, err := queryForExport(db, opts)
	if err != nil {
		return 0, err
	}

	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o755); err != nil {
		return 0, fmt.Errorf("memory: export mkdir: %w", err)
	}

	switch opts.Format {
	case "json":
		if err := exportJSON(opts.Path, engrams); err != nil {
			return 0, err
		}
	case "markdown":
		if err := exportMarkdown(opts.Path, engrams); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("memory: unknown export format %q", opts.Format)
	}
	return len(engrams), nil
}

// ──────────────────────────────────────────────────────────────
// Internal
// ──────────────────────────────────────────────────────────────

// exportEngram is a view of Engram safe for export (no embedding vector).
type exportEngram struct {
	ID          string   `json:"id"`
	Content     string   `json:"content"`
	Summary     string   `json:"summary,omitempty"`
	CWD         string   `json:"cwd,omitempty"`
	Repo        string   `json:"repo,omitempty"`
	User        string   `json:"user,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	Tags        []string `json:"tags"`
	Importance  float64  `json:"importance"`
	AccessCount int      `json:"access_count"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

func toExport(e *Engram) exportEngram {
	return exportEngram{
		ID:          e.ID,
		Content:     e.Content,
		Summary:     e.Summary,
		CWD:         e.CWD,
		Repo:        e.Repo,
		User:        e.User,
		SessionID:   e.SessionID,
		Tags:        e.Tags,
		Importance:  e.Importance,
		AccessCount: e.AccessCount,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

func queryForExport(db *sql.DB, opts ExportOpts) ([]*Engram, error) {
	where := "1=1"
	args := []any{}
	if opts.Repo != "" {
		where += " AND repo = ?"
		args = append(args, opts.Repo)
	}

	rows, err := db.Query(fmt.Sprintf(`
		SELECT id, content, embedding, summary, cwd, repo, user, session_id,
		       tags, importance, access_count, created_at, updated_at
		FROM engrams WHERE %s ORDER BY created_at DESC`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("memory: export query: %w", err)
	}
	defer rows.Close()

	var engrams []*Engram
	for rows.Next() {
		e, err := scanEngramRows(rows)
		if err != nil {
			continue
		}
		// Tag filter.
		if len(opts.Tags) > 0 && !hasAllTags(e.Tags, opts.Tags) {
			continue
		}
		engrams = append(engrams, e)
	}
	return engrams, nil
}

func exportJSON(path string, engrams []*Engram) error {
	out := make([]exportEngram, len(engrams))
	for i, e := range engrams {
		out[i] = toExport(e)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: export json marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func exportMarkdown(path string, engrams []*Engram) error {
	date := time.Now().Format("2006-01-02")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Memory Export — %s\n\n", date))

	for _, e := range engrams {
		// Heading: [repo: name] first line of content
		headline := firstLine(e.Content)
		if e.Repo != "" {
			sb.WriteString(fmt.Sprintf("## [repo: %s] %s\n", e.Repo, headline))
		} else {
			sb.WriteString(fmt.Sprintf("## %s\n", headline))
		}

		// Meta line.
		createdStr := time.Unix(0, e.CreatedAt).Format("2006-01-02 15:04")
		metaParts := []string{}
		if len(e.Tags) > 0 {
			metaParts = append(metaParts, "Tags: "+strings.Join(e.Tags, ", "))
		}
		metaParts = append(metaParts, fmt.Sprintf("Importance: %.1f", e.Importance))
		metaParts = append(metaParts, "Created: "+createdStr)
		sb.WriteString(fmt.Sprintf("*%s*\n\n", strings.Join(metaParts, " | ")))

		// Body.
		sb.WriteString(e.Content)
		sb.WriteString("\n\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func firstLine(s string) string {
	idx := strings.IndexByte(s, '\n')
	if idx < 0 {
		if len(s) > 80 {
			return s[:80] + "…"
		}
		return s
	}
	line := s[:idx]
	if len(line) > 80 {
		return line[:80] + "…"
	}
	return line
}
