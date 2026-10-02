package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestFamilyUXMigrationPreservesAdministrativeAudit(t *testing.T) {
	db, err := New(t.Context(), filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES('Ancien','Bureau');
 INSERT INTO users(person_id,username) VALUES(1,'ancien');
 INSERT INTO administrative_events(id,actor_user_id,action,resource_type,resource_id,created_at) VALUES(42,1,'person_notes_updated','person',1,'2026-09-30 12:00:00');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM administrative_events WHERE id=42 AND actor_user_id=1 AND action='person_notes_updated' AND resource_id=1 AND created_at='2026-09-30 12:00:00'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("audit changed", n, err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO administrative_events(actor_user_id,action,resource_type,resource_id) VALUES(1,'family_relation_saved','person',1)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Down(t.Context()); err == nil {
		t.Fatal("new audit silently discarded on rollback")
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM administrative_events`).Scan(&n); err != nil || n != 2 {
		t.Fatal("rollback damaged audit", n, err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&n); err != nil || n != 0 {
		t.Fatal("invalid migration FK", n, err)
	}
}
