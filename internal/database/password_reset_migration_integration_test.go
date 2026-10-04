package database

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestPasswordResetMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "upgrade"}[upgrade], func(t *testing.T) {
			db, err := New(t.Context(), filepath.Join(t.TempDir(), "reset.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			p, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../../migrations"))
			if err != nil {
				t.Fatal(err)
			}
			if upgrade {
				if _, err = p.UpTo(t.Context(), 3); err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec(`INSERT INTO persons(first_name,last_name,email) VALUES('Avant','Migration','avant@example.test'); INSERT INTO users(person_id,username) VALUES(1,'avant');`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = p.Up(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !upgrade {
				if _, err = db.Exec(`INSERT INTO persons(first_name,last_name) VALUES('Avant','Migration'); INSERT INTO users(person_id,username) VALUES(1,'avant');`); err != nil {
					t.Fatal(err)
				}
			}
			hash := sha256.Sum256([]byte("test"))
			insert := `INSERT INTO user_password_reset_requests(user_id,token_hash,expires_at) VALUES(?,?,strftime('%Y-%m-%d %H:%M:%f','now','+30 minutes'))`
			for _, tc := range []struct {
				id   int
				hash []byte
			}{{999, hash[:]}, {1, []byte{1}}} {
				if _, err = db.Exec(insert, tc.id, tc.hash); err == nil {
					t.Fatal("invalid reset accepted")
				}
			}
			if _, err = db.Exec(`INSERT INTO user_password_reset_requests(user_id,token_hash,created_at,expires_at) VALUES(1,?,'2026-01-01','2026-01-01')`, hash[:]); err == nil {
				t.Fatal("invalid expiration accepted")
			}
			if _, err = db.Exec(insert, 1, hash[:]); err != nil {
				t.Fatal(err)
			}
			other := sha256.Sum256([]byte("other"))
			if _, err = db.Exec(insert, 1, other[:]); err == nil {
				t.Fatal("multiple current resets accepted")
			}
			if _, err = db.Exec(`UPDATE user_password_reset_requests SET invalidated_at=strftime('%Y-%m-%d %H:%M:%f','now')`); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(insert, 1, other[:]); err != nil {
				t.Fatal(err)
			}
			if _, err = p.Down(t.Context()); err == nil {
				t.Fatal("rollback lost reset security history")
			}
			var n int
			if err = db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&n); err != nil || n != 0 {
				t.Fatal("foreign keys", n, err)
			}
			if err = db.QueryRow(`SELECT count(*) FROM users WHERE username='avant'`).Scan(&n); err != nil || n != 1 {
				t.Fatal("existing account lost", n, err)
			}
		})
	}
}
