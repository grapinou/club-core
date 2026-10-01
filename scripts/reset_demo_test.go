package scripts_test

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/demodata"
)

func runScript(t *testing.T, script, path, password string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", script)
	cmd.Env = append(os.Environ(), "DATABASE_PATH="+path, "CLUBCORE_DEMO_PASSWORD="+password)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestResetGuardDoesNotDeleteRealOrAmbiguousFiles(t *testing.T) {
	for _, name := range []string{"clubcore.db", "clubcore_demo.db.bak", "clubcore_demo.db/"} {
		path := filepath.Join(t.TempDir(), strings.TrimSuffix(name, "/"))
		if err := os.WriteFile(path, []byte("must survive"), 0600); err != nil {
			t.Fatal(err)
		}
		target := path
		if strings.HasSuffix(name, "/") {
			target += "/"
		}
		if out, err := runScript(t, "reset-demo.sh", target, "mon-mot-de-passe-de-test"); err == nil || !strings.Contains(out, "refusé") {
			t.Fatalf("guard %s: %s %v", name, out, err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "must survive" {
			t.Fatal("non-demo file changed", err)
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := filepath.Join(t.TempDir(), "clubcore_demo.db")
		real := filepath.Join(t.TempDir(), "clubcore.db")
		if err := os.WriteFile(real, []byte("must survive"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, path+suffix); err != nil {
			t.Fatal(err)
		}
		if out, err := runScript(t, "reset-demo.sh", path, "mon-mot-de-passe-de-test"); err == nil || !strings.Contains(out, "refusé") {
			t.Fatalf("symlink: %s %v", out, err)
		}
		if data, err := os.ReadFile(real); err != nil || string(data) != "must survive" {
			t.Fatal("symlink target changed", err)
		}
	}
}

func TestInvalidPasswordPreservesDemo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clubcore_demo.db")
	if err := os.WriteFile(path, []byte("existing demo sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, "reset-demo.sh", path, "short"); err == nil || !strings.Contains(out, "12 à 72 octets") {
		t.Fatal("password guard", out, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "existing demo sentinel" {
		t.Fatal("invalid password erased demo", err)
	}
}

func TestResetDemoAfterBusinessUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clubcore_demo.db")
	real := filepath.Join(filepath.Dir(path), "clubcore.db")
	for _, file := range []string{real, real + "-wal", real + "-shm"} {
		if err := os.WriteFile(file, []byte("real database sentinel"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate stale SQLite sidecars, which must be removed before migration.
	for _, file := range []string{path + "-wal", path + "-shm"} {
		if err := os.WriteFile(file, []byte("stale demo sidecar"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, password := range []string{"", "surcharge-mot-de-passe-demo"} {
		out, err := runScript(t, "reset-demo.sh", path, password)
		if err != nil {
			t.Fatalf("reset: %s %v", out, err)
		}
		expected := password
		if expected == "" {
			expected = "mon-mot-de-passe-de-test"
		}
		if !strings.Contains(out, expected) || !strings.Contains(out, "2 activités") {
			t.Fatal("missing summary", out)
		}
		db, err := database.New(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		if err := demodata.VerifyDemo(t.Context(), db, expected); err != nil {
			t.Fatal(err)
		}
		assertEmptyBusiness(t, db)
		// Seed representative linked data: the second reset must erase it all.
		if _, err := db.ExecContext(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES('Personne','Recette');
 INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status)
 SELECT p.id,g.activity_id,g.id,gs.id,'2026-10-05','registered' FROM persons p CROSS JOIN group_slots gs JOIN groups g ON g.id=gs.group_id WHERE p.last_name='Recette' LIMIT 1;
 INSERT INTO memberships(person_id,season_id,membership_type_id,status)
 SELECT p.id,s.id,mt.id,'pending' FROM persons p CROSS JOIN seasons s CROSS JOIN membership_types mt WHERE p.last_name='Recette' AND mt.name='Adulte';
 INSERT INTO membership_activities(membership_id,activity_id) SELECT m.id,a.id FROM memberships m CROSS JOIN activities a WHERE a.name='Jiu-Jitsu Brésilien';
 INSERT INTO membership_groups(membership_id,group_id,joined_at) SELECT m.id,g.id,'2026-10-05' FROM memberships m CROSS JOIN groups g WHERE g.name='JJB Adolescents et Adultes';`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runScript(t, "reset-demo.sh", path, "")
	if err != nil {
		t.Fatal(out, err)
	}
	db, err := database.New(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := demodata.VerifyDemo(t.Context(), db, "mon-mot-de-passe-de-test"); err != nil {
		t.Fatal(err)
	}
	assertEmptyBusiness(t, db)
	for _, file := range []string{real, real + "-wal", real + "-shm"} {
		if data, err := os.ReadFile(file); err != nil || string(data) != "real database sentinel" {
			t.Fatal("real database or sidecar changed", err)
		}
	}
}

func assertEmptyBusiness(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"trial_registrations", "memberships", "membership_groups", "membership_activities", "person_guardians", "person_emergency_contacts", "registration_application_emergency_contacts", "guardian_access_grants", "registration_applications", "child_registration_applications", "registration_submissions"} {
		var count int
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s: %d %v", table, count, err)
		}
	}
}

func TestRunDevPreservesExistingBusinessData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clubcore_demo.db")
	db, err := database.New(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := demodata.SeedBudokan(t.Context(), db, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO persons(first_name,last_name) VALUES('Personne','Conservée')"); err != nil {
		t.Fatal(err)
	}
	// Execute the real prepare command but replace the long-lived server with
	// a sentinel, so this test never takes the application's listening port.
	bin := t.TempDir()
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/usr/bin/env bash\nif [[ ${2:-} == './cmd/server' ]]; then echo SERVER_SENTINEL; exit 0; fi\nexec \"$REAL_GO\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "run-dev.sh")
	cmd.Env = append(os.Environ(), "DATABASE_PATH="+path, "PATH="+bin+":"+os.Getenv("PATH"), "REAL_GO="+goPath)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SERVER_SENTINEL") {
		t.Fatalf("run-dev: %s %v", out, err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM persons WHERE last_name='Conservée'").Scan(&count); err != nil || count != 1 {
		t.Fatal("run-dev destroyed business data", count, err)
	}
}
