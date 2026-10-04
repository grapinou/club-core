package passwordreset

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/pressly/goose/v3"
)

func TestPublicSubmissionDoesNotWaitForLookupAndIsBounded(t *testing.T) {
	db, err := database.New(t.Context(), filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	connection, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	s := New(db, mailer.Disabled{}, auth.NewSessions(false), "club@example.test", "http://localhost", 30*time.Minute)
	// Hold the sole DB connection: account existence cannot affect Submit latency.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.Submit("unknown")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("public response waited for database")
	}
	if len(s.publicSlots) != 16 {
		t.Fatal("public work is not bounded", len(s.publicSlots))
	}
	connection.Close()
	deadline := time.Now().Add(2 * time.Second)
	for len(s.publicSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(s.publicSlots) != 0 {
		t.Fatal("public workers did not release capacity")
	}
}
