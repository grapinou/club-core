package database

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbtypes"
)

func TestSQLiteEveryConnectionEnforcesForeignKeys(t *testing.T) {
	db := newTestDatabase(t)
	// Holding all eight connections forces verification of each physical connection.
	for range 8 {
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		var foreignKeys, busyTimeout, synchronous int
		var mode string
		for pragma, dst := range map[string]any{"foreign_keys": &foreignKeys, "busy_timeout": &busyTimeout, "synchronous": &synchronous, "journal_mode": &mode} {
			if err := conn.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(dst); err != nil {
				t.Fatal(err)
			}
		}
		if foreignKeys != 1 || busyTimeout != 10000 || synchronous != 2 || mode != "wal" {
			t.Fatalf("connection pragmas: foreign_keys=%d busy_timeout=%d synchronous=%d mode=%s", foreignKeys, busyTimeout, synchronous, mode)
		}
		if _, err := conn.ExecContext(t.Context(), "INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES(999,998,'guardian')"); err == nil {
			t.Fatal("foreign key violation accepted")
		}
	}
}

func TestSQLiteCreatesFolderAndRejectsUnusablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "instance.db")
	db, err := New(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = New(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var roles int
	if err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM roles").Scan(&roles); err != nil || roles != 4 {
		t.Fatal("reopen schema", roles, err)
	}
	if broken, err := New(t.Context(), filepath.Join(path, "blocked.db")); err == nil {
		broken.Close()
		t.Fatal("file used as a folder")
	}
	if broken, err := New(t.Context(), t.TempDir()); err == nil {
		broken.Close()
		t.Fatal("directory used as a database file")
	}
}

func TestSQLiteCivilDatesAndTimes(t *testing.T) {
	db := newTestDatabase(t)
	for _, bad := range []string{"2026-02-29", "2026-02-30", "2026-1-02", "2026", "0000-01-01"} {
		if _, err := db.ExecContext(t.Context(), "INSERT INTO persons(first_name,last_name,birth_date) VALUES('Date','Invalid',?1)", bad); err == nil {
			t.Fatal("invalid date accepted", bad)
		}
	}
	date := dbtypes.Date{Time: time.Date(2024, 2, 29, 0, 0, 0, 0, time.FixedZone("civil", 3600)), Valid: true}
	var id int32
	if err := db.QueryRowContext(t.Context(), "INSERT INTO persons(first_name,last_name,birth_date) VALUES('Leap','Valid',?1) RETURNING id", date).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var got dbtypes.Date
	if err := db.QueryRowContext(t.Context(), "SELECT birth_date FROM persons WHERE id=?1", id).Scan(&got); err != nil || got.Time.Format("2006-01-02") != "2024-02-29" {
		t.Fatal(got, err)
	}
	// The age boundary uses calendar years, including February 29 -> February 28.
	var eighteenYearsAgo string
	if err := db.QueryRowContext(t.Context(), "SELECT date('2024-02-29','-18 years','floor')").Scan(&eighteenYearsAgo); err != nil || eighteenYearsAgo != "2006-02-28" {
		t.Fatal(eighteenYearsAgo, err)
	}
	var clock dbtypes.Time
	wantClock := dbtypes.Time{Microseconds: (18*3600+30*60)*1000000 + 123456, Valid: true}
	if err := db.QueryRowContext(t.Context(), "SELECT ?1", wantClock).Scan(&clock); err != nil || clock != wantClock {
		t.Fatal(clock, err)
	}
	var stamp dbtypes.Timestamp
	wantStamp := dbtypes.Timestamp{Time: time.Date(2026, 9, 30, 12, 34, 56, 123000000, time.FixedZone("Paris", 7200)), Valid: true}
	if err := db.QueryRowContext(t.Context(), "SELECT ?1", wantStamp).Scan(&stamp); err != nil || !stamp.Time.Equal(wantStamp.Time) || stamp.Time.Location() != time.UTC {
		t.Fatal(stamp, err)
	}
}

func TestSQLiteRequiresApplicationActivityAtCommit(t *testing.T) {
	db := newTestDatabase(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(query string) {
		t.Helper()
		if _, err := tx.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO registration_submissions(id,first_name,last_name) VALUES(1,'Test','Activity')")
	exec("INSERT INTO seasons(id,name,starts_at,ends_at) VALUES(1,'Season','2026-01-01','2026-12-31')")
	exec("INSERT INTO membership_types(id,name) VALUES(1,'Member')")
	exec("INSERT INTO activities(id,name) VALUES(1,'Activity')")
	exec("INSERT INTO registration_applications(id,required_activity_id,submission_id,request_key,season_id,membership_type_id) VALUES(1,1,1,zeroblob(32),1,1)")
	if err := tx.Commit(); err == nil {
		t.Fatal("application without activities committed")
	}
	// database/sql has completed the transaction; the driver must have rolled it back.
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM registration_applications").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed commit persisted data", count, err)
	}
}

func TestSQLiteWALReaderDuringWriter(t *testing.T) {
	db := newTestDatabase(t)
	other, err := openTestConnection(t, db)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(t.Context(), "INSERT INTO persons(first_name,last_name) VALUES('Pending','Write')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = other.QueryRowContext(t.Context(), "SELECT count(*) FROM persons").Scan(&count); err != nil || count != 0 {
		t.Fatal("WAL reader blocked or uncommitted data visible", count, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = other.QueryRowContext(t.Context(), "SELECT count(*) FROM persons").Scan(&count); err != nil || count != 1 {
		t.Fatal("committed data missing", count, err)
	}
	var integrity string
	if err = db.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
	rows, err := db.QueryContext(t.Context(), "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key integrity failure")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
