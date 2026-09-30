package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const DefaultPath = "data/clubcore.db"

// New opens a file-backed SQLite database. Every connection receives the PRAGMAs
// through the driver DSN. IMMEDIATE transactions serialize validation and writes
// before taking a snapshot; WAL lets other connections continue reading.
func New(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultPath
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("chemin SQLite : %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("créer le dossier SQLite %q : %w", filepath.Dir(path), err)
	}
	// Create private files even when an operator chooses an existing shared folder.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("ouvrir le fichier SQLite %q : %w", path, err)
	}
	if err = file.Close(); err != nil {
		return nil, fmt.Errorf("fermer le fichier SQLite %q : %w", path, err)
	}

	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("ouvrir SQLite %q : %w", path, err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ouvrir SQLite %q : %w", path, err)
	}
	return db, nil
}

// Path is used by local tooling and the guarded demo seed, never as credentials.
func Path(ctx context.Context, db *sql.DB) (string, error) {
	var seq int
	var name, path string
	err := db.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &name, &path)
	return path, err
}
