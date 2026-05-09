package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Engram is a single persistent memory unit.
type Engram struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`
	Embedding   []float64 `json:"-"`           // not serialised in search results
	Summary     string    `json:"summary,omitempty"`
	CWD         string    `json:"cwd,omitempty"`
	Repo        string    `json:"repo,omitempty"`
	User        string    `json:"user,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Tags        []string  `json:"tags"`
	Importance  float64   `json:"importance"`
	AccessCount int       `json:"access_count"`
	CreatedAt   int64     `json:"created_at"` // Unix nanoseconds
	UpdatedAt   int64     `json:"updated_at"`
}

// SearchResult is a ranked engram returned by Search.
type SearchResult struct {
	Engram
	Score float64 `json:"score"`
}

// ──────────────────────────────────────────────────────────────
// Store
// ──────────────────────────────────────────────────────────────

// StoreOpts are options for storing an engram.
type StoreOpts struct {
	Content   string
	Tags      []string
	Importance float64
	Summary   string
	CWD       string
	Repo      string
	User      string
	SessionID string
}

// Store inserts a new engram into the database. Returns the new engram ID.
func Store(db *sql.DB, opts StoreOpts) (string, error) {
	if opts.Importance == 0 {
		opts.Importance = 0.5
	}
	if opts.Tags == nil {
		opts.Tags = []string{}
	}

	embedding, err := EmbedText(opts.Content)
	if err != nil {
		// Non-fatal: store with empty embedding, TF-IDF will still work at search time.
		embedding = tfidfEmbed(opts.Content)
	}

	embJSON, err := EncodeEmbedding(embedding)
	if err != nil {
		return "", fmt.Errorf("memory: encode embedding: %w", err)
	}
	tagsJSON, err := json.Marshal(opts.Tags)
	if err != nil {
		return "", fmt.Errorf("memory: encode tags: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UnixNano()

	_, err = db.Exec(`
		INSERT INTO engrams
			(id, content, embedding, summary, cwd, repo, user, session_id,
			 tags, importance, access_count, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,0,?,?)`,
		id, opts.Content, embJSON, opts.Summary,
		opts.CWD, opts.Repo, opts.User, opts.SessionID,
		string(tagsJSON), opts.Importance,
		now, now,
	)
	if err != nil {
		return "", fmt.Errorf("memory: store engram: %w", err)
	}
	return id, nil
}

// ──────────────────────────────────────────────────────────────
// Get
// ──────────────────────────────────────────────────────────────

// Get retrieves a single engram by ID and increments its access_count.
func Get(db *sql.DB, id string) (*Engram, error) {
	row := db.QueryRow(`
		SELECT id, content, embedding, summary, cwd, repo, user, session_id,
		       tags, importance, access_count, created_at, updated_at
		FROM engrams WHERE id = ?`, id)

	e, err := scanEngram(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("memory: engram %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("memory: get engram: %w", err)
	}

	// Increment access count asynchronously — ignore errors.
	_, _ = db.Exec(`UPDATE engrams SET access_count = access_count + 1 WHERE id = ?`, id)
	return e, nil
}

// ──────────────────────────────────────────────────────────────
// Search
// ──────────────────────────────────────────────────────────────

// SearchOpts are options for searching engrams.
type SearchOpts struct {
	Query    string
	Repo     string
	CWD      string
	User     string
	Tags     []string
	Limit    int
	MinScore float64
}

const (
	wSimilarity = 0.60
	wImportance = 0.25
	wRecency    = 0.15
)

// Search returns engrams ranked by combined similarity + importance + recency.
func Search(db *sql.DB, opts SearchOpts) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.MinScore <= 0 {
		opts.MinScore = 0.3
	}

	// Build WHERE clause for attribute filters.
	where := "1=1"
	args := []any{}
	if opts.Repo != "" {
		where += " AND repo = ?"
		args = append(args, opts.Repo)
	}
	if opts.CWD != "" {
		where += " AND cwd = ?"
		args = append(args, opts.CWD)
	}
	if opts.User != "" {
		where += " AND user = ?"
		args = append(args, opts.User)
	}

	rows, err := db.Query(fmt.Sprintf(`
		SELECT id, content, embedding, summary, cwd, repo, user, session_id,
		       tags, importance, access_count, created_at, updated_at
		FROM engrams WHERE %s`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("memory: search query: %w", err)
	}
	defer rows.Close()

	var queryVec []float64
	if opts.Query != "" {
		queryVec, _ = EmbedText(opts.Query)
	}
	if queryVec == nil {
		queryVec = tfidfEmbed(opts.Query)
	}

	now := float64(time.Now().UnixNano())
	const dayNs = float64(24 * 60 * 60 * 1e9)

	var results []SearchResult
	for rows.Next() {
		e, err := scanEngramRows(rows)
		if err != nil {
			continue
		}

		// Tag filter: must match ALL requested tags.
		if len(opts.Tags) > 0 && !hasAllTags(e.Tags, opts.Tags) {
			continue
		}

		// Cosine similarity.
		sim := CosineSimilarity(queryVec, e.Embedding)

		// Recency decay: exponential with half-life = 30 days.
		ageDays := (now - float64(e.CreatedAt)) / dayNs
		recency := math.Exp(-0.693 * ageDays / 30)

		score := wSimilarity*sim + wImportance*e.Importance + wRecency*recency

		if score < opts.MinScore {
			continue
		}
		results = append(results, SearchResult{Engram: *e, Score: score})
	}

	// Sort descending by score.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results, nil
}

func hasAllTags(have, want []string) bool {
	set := make(map[string]struct{}, len(have))
	for _, t := range have {
		set[t] = struct{}{}
	}
	for _, t := range want {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// ──────────────────────────────────────────────────────────────
// Update
// ──────────────────────────────────────────────────────────────

// UpdateOpts contains fields that may be updated on an existing engram.
// Zero-value fields are not updated (except Importance and Tags which use pointers).
type UpdateOpts struct {
	ID         string
	Content    *string
	Tags       []string
	Importance *float64
}

// Update modifies an existing engram. Re-embeds if Content changes.
func Update(db *sql.DB, opts UpdateOpts) error {
	e, err := Get(db, opts.ID)
	if err != nil {
		return err
	}

	content := e.Content
	embJSON := ""
	if opts.Content != nil && *opts.Content != "" && *opts.Content != e.Content {
		content = *opts.Content
		vec, _ := EmbedText(content)
		if vec == nil {
			vec = tfidfEmbed(content)
		}
		embJSON, err = EncodeEmbedding(vec)
		if err != nil {
			return fmt.Errorf("memory: update embed: %w", err)
		}
	}

	tags := e.Tags
	if opts.Tags != nil {
		tags = opts.Tags
	}
	tagsJSON, _ := json.Marshal(tags)

	importance := e.Importance
	if opts.Importance != nil {
		importance = *opts.Importance
	}

	now := time.Now().UnixNano()
	if embJSON != "" {
		_, err = db.Exec(`
			UPDATE engrams SET content=?, embedding=?, tags=?, importance=?, updated_at=?
			WHERE id=?`,
			content, embJSON, string(tagsJSON), importance, now, opts.ID)
	} else {
		_, err = db.Exec(`
			UPDATE engrams SET content=?, tags=?, importance=?, updated_at=?
			WHERE id=?`,
			content, string(tagsJSON), importance, now, opts.ID)
	}
	if err != nil {
		return fmt.Errorf("memory: update engram: %w", err)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────
// Delete
// ──────────────────────────────────────────────────────────────

// Delete removes an engram by ID.
func Delete(db *sql.DB, id string) error {
	res, err := db.Exec(`DELETE FROM engrams WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("memory: delete engram: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory: engram %q not found", id)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────
// Scan helpers
// ──────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanEngram(s scanner) (*Engram, error) {
	var e Engram
	var embStr, tagsStr string
	var summary, cwd, repo, user, sessionID sql.NullString
	if err := s.Scan(
		&e.ID, &e.Content, &embStr, &summary,
		&cwd, &repo, &user, &sessionID,
		&tagsStr, &e.Importance, &e.AccessCount,
		&e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		return nil, err
	}
	e.Summary = summary.String
	e.CWD = cwd.String
	e.Repo = repo.String
	e.User = user.String
	e.SessionID = sessionID.String

	vec, _ := DecodeEmbedding(embStr)
	e.Embedding = vec

	if tagsStr != "" && tagsStr != "[]" {
		_ = json.Unmarshal([]byte(tagsStr), &e.Tags)
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	return &e, nil
}

func scanEngramRows(rows *sql.Rows) (*Engram, error) {
	var e Engram
	var embStr, tagsStr string
	var summary, cwd, repo, user, sessionID sql.NullString
	if err := rows.Scan(
		&e.ID, &e.Content, &embStr, &summary,
		&cwd, &repo, &user, &sessionID,
		&tagsStr, &e.Importance, &e.AccessCount,
		&e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		return nil, err
	}
	e.Summary = summary.String
	e.CWD = cwd.String
	e.Repo = repo.String
	e.User = user.String
	e.SessionID = sessionID.String

	vec, _ := DecodeEmbedding(embStr)
	e.Embedding = vec

	if tagsStr != "" && tagsStr != "[]" {
		_ = json.Unmarshal([]byte(tagsStr), &e.Tags)
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	return &e, nil
}
