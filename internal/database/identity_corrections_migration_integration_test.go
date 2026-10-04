package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestIdentityCorrectionMigrationPreservesAuditAndEvidence(t *testing.T) {
	db, err := New(t.Context(), filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES('Avant','Migration');
 INSERT INTO users(person_id,username) VALUES(1,'compte');
 INSERT INTO administrative_events(id,actor_user_id,action,resource_type,resource_id,created_at) VALUES(42,1,'family_relation_saved','person',1,'2026-10-01 12:00:00');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(t.Context(), 3); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM administrative_events WHERE id=42 AND action='family_relation_saved' AND created_at='2026-10-01 12:00:00'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("historical audit", n, err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO identity_correction_requests(person_id,requesting_user_id,original_first_name,original_last_name,proposed_first_name,proposed_last_name,proposed_birth_date) VALUES(1,1,'Avant','Migration','Après','Migration','1990-01-01');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Down(t.Context()); err == nil {
		t.Fatal("rollback discarded identity evidence")
	}
	if err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM identity_correction_requests").Scan(&n); err != nil || n != 1 {
		t.Fatal("request lost", n, err)
	}
	if err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM pragma_foreign_key_check").Scan(&n); err != nil || n != 0 {
		t.Fatal("migration foreign keys", n, err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO administrative_events(actor_user_id,action,resource_type,resource_id) VALUES(1,'person_identity_corrected','person',1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM administrative_events WHERE id>42 AND action='person_identity_corrected'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("audit sequence", n, err)
	}
}
