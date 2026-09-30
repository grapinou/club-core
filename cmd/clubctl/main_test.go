package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grapinou/club-core/internal/database"
)

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
