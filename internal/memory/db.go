package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS engrams (
    id           TEXT PRIMARY KEY,
    content      TEXT NOT NULL,
    embedding    TEXT NOT NULL DEFAULT '[]',
    summary      TEXT,
    cwd          TEXT,
    repo         TEXT,
    user         TEXT,
    session_id   TEXT,
    tags         TEXT NOT NULL DEFAULT '[]',
    importance   REAL NOT NULL DEFAULT 0.5,
    access_count INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_engrams_repo ON engrams(repo);
CREATE INDEX IF NOT EXISTS idx_engrams_cwd  ON engrams(cwd);
CREATE INDEX IF NOT EXISTS idx_engrams_user ON engrams(user);
`

// OpenDB opens (or creates) the SQLite database at ~/.wllr/memory.db and
// runs the schema migration. The returned *sql.DB is safe for concurrent use.
func OpenDB() (*sql.DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("memory: home dir: %w", err)
	}
	dir := filepath.Join(home, ".wllr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory: mkdir %s: %w", dir, err)
	}
	dbPath := filepath.Join(dir, "memory.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("memory: open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: single writer
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("memory: migrate: %w", err)
	}
	return nil
}
