package demodata_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/demodata"
	"golang.org/x/crypto/bcrypt"
)

const demoPassword = "mot-de-passe-demo-tests"

func demoDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.New(t.Context(), filepath.Join(t.TempDir(), "clubcore_demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err = demodata.SeedBudokan(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestOfficeBootstrapAuthenticationAndRepeat(t *testing.T) {
	db := demoDB(t)
	ctx := t.Context()
	if err := demodata.PrepareDemoOffice(ctx, db, demoPassword); err != nil {
		t.Fatal(err)
	}
	if err := demodata.VerifyDemo(ctx, db, demoPassword); err != nil {
		t.Fatal(err)
	}
	service, err := auth.New(dbsqlc.New(db))
	if err != nil {
		t.Fatal(err)
	}
	for username, role := range map[string]string{"president.demo": "president", "secretary.demo": "secretary", "treasurer.demo": "treasurer"} {
		user, err := dbsqlc.New(db).GetUserByUsername(ctx, username)
		if err != nil || !user.IsActive || !user.ActivatedAt.Valid || !user.PasswordHash.Valid {
			t.Fatalf("account %s: %+v %v", username, user, err)
		}
		if cost, err := bcrypt.Cost([]byte(user.PasswordHash.String)); err != nil || cost != bcrypt.DefaultCost {
			t.Fatalf("bcrypt %s: %d %v", username, cost, err)
		}
		if id, err := service.Authenticate(ctx, username, demoPassword); err != nil || id != user.ID {
			t.Fatalf("login %s: %d %v", username, id, err)
		}
		if _, err := service.Authenticate(ctx, username, "wrong-password"); !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("wrong password %s: %v", username, err)
		}
		var actual string
		if err := db.QueryRowContext(ctx, "SELECT r.name FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=?1", user.ID).Scan(&actual); err != nil || actual != role {
			t.Fatalf("role %s: %s %v", username, actual, err)
		}
	}
	if err := demodata.PrepareDemoOffice(ctx, db, "different-demo-password"); !errors.Is(err, demodata.ErrOfficeNotEmpty) {
		t.Fatalf("repeat: %v", err)
	}
	if err := demodata.VerifyDemo(ctx, db, demoPassword); err != nil {
		t.Fatal("repeat mutated accounts", err)
	}
}

func TestOfficeRollbackAndDirtyDatabase(t *testing.T) {
	for _, failure := range []string{
		"CREATE TRIGGER reject_treasurer BEFORE INSERT ON user_roles WHEN NEW.role_id=(SELECT id FROM roles WHERE name='treasurer') BEGIN SELECT RAISE(ABORT,'injected failure'); END",
		"DELETE FROM roles WHERE name='treasurer'",
		"INSERT INTO persons(first_name,last_name) VALUES('Unexpected','Person')",
		"INSERT INTO registration_submissions(first_name,last_name,email,birth_date,status) VALUES('Unexpected','Submission','demo@clubcore.invalid','2000-01-01','received')",
	} {
		t.Run(failure, func(t *testing.T) {
			db := demoDB(t)
			if _, err := db.ExecContext(t.Context(), failure); err != nil {
				t.Fatal(err)
			}
			var before int
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM persons").Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err := demodata.PrepareDemoOffice(t.Context(), db, demoPassword); err == nil {
				t.Fatal("non-conforming bootstrap accepted")
			}
			var persons, users, roles, initialized int
			err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM persons),(SELECT count(*) FROM users),
 (SELECT count(*) FROM user_roles),(SELECT count(*) FROM installation_setup WHERE initialized_at IS NOT NULL)`).Scan(&persons, &users, &roles, &initialized)
			if err != nil || persons != before || users != 0 || roles != 0 || initialized != 0 {
				t.Fatalf("rollback: %d/%d/%d/%d %v", persons, users, roles, initialized, err)
			}
		})
	}
	for _, password := range []string{"", "short"} {
		db := demoDB(t)
		if err := demodata.PrepareDemoOffice(t.Context(), db, password); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
}

func TestDemoPathGuard(t *testing.T) {
	for _, name := range []string{"", "clubcore.db", "demo.db", "clubcore_demo.db.bak", "clubcore_demo/clubcore.db", "file:clubcore_demo.db?mode=rw", "clubcore_demo.db/"} {
		if err := demodata.CheckDemoPath(name); !errors.Is(err, demodata.ErrGuard) {
			t.Errorf("accepted %q: %v", name, err)
		}
	}
	path := filepath.Join(t.TempDir(), "linked_demo.db")
	if err := os.Symlink(filepath.Join(t.TempDir(), "real.db"), path); err != nil {
		t.Fatal(err)
	}
	if err := demodata.CheckDemoPath(path); !errors.Is(err, demodata.ErrGuard) {
		t.Fatal("symlink accepted", err)
	}
	for _, ext := range []string{"", ".db", ".sqlite", ".sqlite3"} {
		if err := demodata.CheckDemoPath(filepath.Join(t.TempDir(), "clubcore_demo"+ext)); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.New(t.Context(), filepath.Join(t.TempDir(), "real.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := demodata.PrepareDemoOffice(t.Context(), db, demoPassword); !errors.Is(err, demodata.ErrGuard) {
		t.Fatal("non-demo DB accepted", err)
	}
}

func TestBudokanExplicitMembershipGroupMapping(t *testing.T) {
	db := demoDB(t)
	rows, err := db.QueryContext(t.Context(), `SELECT mt.name,g.name FROM membership_type_groups c JOIN membership_types mt ON mt.id=c.membership_type_id JOIN groups g ON g.id=c.group_id ORDER BY mt.name,g.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	expected := map[string]bool{
		"Adulte/JJB Adolescents et Adultes":                        true,
		"Adolescent/JJB Adolescents et Adultes":                    true,
		"Enfant/JJB enfants 7–10 ans":                              true,
		"Enfant/JJB enfants 10–14 ans":                             true,
		"Enfant/Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans":  true,
		"Enfant/Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans": true,
	}
	for rows.Next() {
		var kind, group string
		if err = rows.Scan(&kind, &group); err != nil {
			t.Fatal(err)
		}
		key := kind + "/" + group
		if !expected[key] {
			t.Fatal("unexpected compatibility", key)
		}
		delete(expected, key)
	}
	if err = rows.Err(); err != nil || len(expected) > 0 {
		t.Fatal("missing compatibility", expected, err)
	}
}
