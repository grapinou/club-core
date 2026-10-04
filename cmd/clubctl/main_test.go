package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grapinou/club-core/internal/database"
)

func TestShowcaseCommandsGuardBeforeOpeningDatabase(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	for _, command := range []string{"prepare-showcase-demo", "verify-showcase-demo", "check-demo-reset-path"} {
		t.Run(command, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "clubcore_showcase_demo.db")
			t.Setenv("DATABASE_PATH", path)
			t.Setenv("CLUBCORE_DEMO_PASSWORD", "mon-mot-de-passe-de-test")
			os.Args = []string{"clubctl", command}
			if err := run(); err == nil {
				t.Fatal("confirmation absent")
			}
			os.Args = []string{"clubctl", command, "--confirm-demo"}
			t.Setenv("CLUBCORE_DEMO_PASSWORD", "short")
			if err := run(); err == nil {
				t.Fatal("invalid password")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("guard opened database", err)
			}
			t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "clubcore.db"))
			t.Setenv("CLUBCORE_DEMO_PASSWORD", "mon-mot-de-passe-de-test")
			if err := run(); err == nil {
				t.Fatal("real database accepted")
			}
		})
	}
}

func TestPrepareBudokanDemoWithExistingEmptySQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clubcore_demo.db")
	db, err := database.New(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_PATH", path)
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"clubctl", "prepare-budokan-demo", "--confirm-demo"}
	for range 2 {
		if err = run(); err != nil {
			t.Fatal(err)
		}
	}
	var slots, persons int
	if err = db.QueryRowContext(t.Context(), "SELECT (SELECT count(*) FROM group_slots),(SELECT count(*) FROM persons)").Scan(&slots, &persons); err != nil || slots != 16 || persons != 0 {
		t.Fatal("demo changed after repeated prepare", slots, persons, err)
	}
	// Preparation retains the same explicit confirmation and filename guard.
	os.Args = []string{"clubctl", "prepare-budokan-demo"}
	if err = run(); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "clubcore.db"))
	os.Args = []string{"clubctl", "prepare-budokan-demo", "--confirm-demo"}
	if err = run(); err == nil {
		t.Fatal("non-demo file accepted")
	}
}

func TestOfficeCLIGuardsBeforeOpeningDatabase(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	for _, command := range []string{"prepare-demo-office", "verify-demo"} {
		for _, guard := range []string{"path", "confirmation", "password"} {
			t.Run(command+"/"+guard, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "clubcore_demo.db")
				if guard == "path" {
					path = filepath.Join(t.TempDir(), "clubcore.db")
				}
				t.Setenv("DATABASE_PATH", path)
				t.Setenv("CLUBCORE_DEMO_PASSWORD", "mot-de-passe-demo-tests")
				os.Args = []string{"clubctl", command, "--confirm-demo"}
				if guard == "confirmation" {
					os.Args = os.Args[:2]
				}
				if guard == "password" {
					t.Setenv("CLUBCORE_DEMO_PASSWORD", "")
				}
				if err := run(); err == nil {
					t.Fatal("guard accepted")
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("refusal created/migrated a database", err)
				}
			})
		}
	}
}
