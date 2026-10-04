package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/demodata"
)

const showcasePassword = "mon-mot-de-passe-de-test"

func TestShowcaseResetGuards(t *testing.T) {
	for _, name := range []string{"clubcore.db", "clubcore_showcase_demo.db.bak", "clubcore_showcase_demo.db/", "file:clubcore_showcase_demo.db", "clubcore_showcase_demo.db?mode=rw"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), strings.TrimSuffix(name, "/"))
			if err := os.WriteFile(path, []byte("must survive"), 0600); err != nil {
				t.Fatal(err)
			}
			target := path
			if strings.HasSuffix(name, "/") {
				target += "/"
			}
			if out, err := runScript(t, "reset-showcase-demo.sh", target, showcasePassword); err == nil {
				t.Fatal("unguarded reset", out)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "must survive" {
				t.Fatal("real file changed", err)
			}
		})
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		t.Run("symlink"+suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "clubcore_showcase_demo.db")
			real := filepath.Join(t.TempDir(), "clubcore.db")
			if err := os.WriteFile(real, []byte("must survive"), 0600); err != nil {
				t.Fatal(err)
			}
			if suffix != "" {
				if err := os.WriteFile(path, []byte("demo must survive"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(real, path+suffix); err != nil {
				t.Fatal(err)
			}
			if out, err := runScript(t, "reset-showcase-demo.sh", path, showcasePassword); err == nil {
				t.Fatal("symlink reset", out)
			}
			if data, err := os.ReadFile(real); err != nil || string(data) != "must survive" {
				t.Fatal("target changed", err)
			}
			if suffix != "" {
				if data, err := os.ReadFile(path); err != nil || string(data) != "demo must survive" {
					t.Fatal("demo changed", err)
				}
			}
		})
	}
	t.Run("parent symlink", func(t *testing.T) {
		dir := t.TempDir()
		link := filepath.Join(t.TempDir(), "linked")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "clubcore_showcase_demo.db")
		if err := os.WriteFile(path, []byte("must survive"), 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := runScript(t, "reset-showcase-demo.sh", filepath.Join(link, filepath.Base(path)), showcasePassword); err == nil {
			t.Fatal("directory symlink accepted", out)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "must survive" {
			t.Fatal("parent symlink changed", err)
		}
	})
	for _, password := range []string{"short", strings.Repeat("x", 73), strings.Repeat("é", 37)} {
		path := filepath.Join(t.TempDir(), "clubcore_showcase_demo.db")
		if err := os.WriteFile(path, []byte("must survive"), 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := runScript(t, "reset-showcase-demo.sh", path, password); err == nil || !strings.Contains(out, "12 à 72 octets") {
			t.Fatal("password validation", out, err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "must survive" {
			t.Fatal("invalid password erased demo", err)
		}
	}
}

func TestShowcaseResetReproducibleAndRunDevPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clubcore_showcase_demo.db")
	real := filepath.Join(filepath.Dir(path), "clubcore.db")
	for _, file := range []string{real, real + "-wal", real + "-shm", path + "-wal", path + "-shm"} {
		if err := os.WriteFile(file, []byte("sentinel"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for iteration := range 2 {
		out, err := runScript(t, "reset-showcase-demo.sh", path, "")
		if err != nil {
			t.Fatal(out, err)
		}
		for _, text := range []string{"member.demo", "parent.demo", "secretary.member.demo", "empty.demo", showcasePassword, "DATABASE_PATH=", "./scripts/run-dev.sh", "integrity_check : ok"} {
			if !strings.Contains(out, text) {
				t.Fatal("missing instructions", text, out)
			}
		}
		db, err := database.New(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		if err := demodata.VerifyShowcase(t.Context(), db, showcasePassword); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if iteration == 0 {
			if _, err := db.ExecContext(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES('Arbitrary','Extra'); INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) SELECT id,(SELECT id FROM activities WHERE name='Jiu-Jitsu Brésilien'),'2026-10-05','registered' FROM persons WHERE last_name='Extra'`); err != nil {
				db.Close()
				t.Fatal(err)
			}
		}
		db.Close()
	}
	// Execute the actual run-dev bootstrap, replacing only the blocking server.
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
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "SERVER_SENTINEL") {
		t.Fatal("run-dev", string(out), err)
	}
	db, err := database.New(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := demodata.VerifyShowcase(t.Context(), db, showcasePassword); err != nil {
		t.Fatal("run-dev altered showcase", err)
	}
	// The independent standard script and its strict empty-business verifier remain valid.
	standard := filepath.Join(t.TempDir(), "standard_demo.db")
	if out, err := runScript(t, "reset-demo.sh", standard, showcasePassword); err != nil {
		t.Fatal(out, err)
	}
	clean, err := database.New(t.Context(), standard)
	if err != nil {
		t.Fatal(err)
	}
	defer clean.Close()
	if err := demodata.VerifyDemo(t.Context(), clean, showcasePassword); err != nil {
		t.Fatal(err)
	}
	assertEmptyBusiness(t, clean)
	for _, file := range []string{real, real + "-wal", real + "-shm"} {
		if data, err := os.ReadFile(file); err != nil || string(data) != "sentinel" {
			t.Fatal("neighbor changed", err)
		}
	}
}
