package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func newTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	return newTestDatabaseNamed(t, "club_manager_test")
}
func newTestDatabaseNamed(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := New(t.Context(), filepath.Join(t.TempDir(), name+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func testDatabasePath(t *testing.T, db *sql.DB) string {
	t.Helper()
	p, err := Path(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func openTestConnection(t *testing.T, db *sql.DB) (*sql.DB, error) {
	t.Helper()
	return New(t.Context(), testDatabasePath(t, db))
}
