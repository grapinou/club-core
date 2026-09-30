package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/application"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/mailer"
)

func TestPrivateFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	original := map[string]string{"A": "local-placeholder"}
	if err := privateJSON(p, original); err != nil {
		t.Fatal(err)
	}
	if err := privateJSON(p, map[string]string{"A": "replacement"}); err == nil {
		t.Fatal("overwrote private state")
	}
	var got map[string]string
	if err := readPrivate(p, &got); err != nil || got["A"] != original["A"] {
		t.Fatal("private state changed or unreadable")
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if readPrivate(p, &got) == nil {
		t.Fatal("accepted public file")
	}
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if readPrivate(link, &got) == nil {
		t.Fatal("accepted symlink")
	}
}
func TestCampaignPortsStayLocal(t *testing.T) {
	args := mailpitArgs()
	ports := 0
	for i, arg := range args {
		if arg == "-p" {
			ports++
			if !strings.HasPrefix(args[i+1], "127.0.0.1::") {
				t.Fatal("port exposed outside loopback", args[i+1])
			}
		}
	}
	if ports != 2 {
		t.Fatal("missing mail ports")
	}
}

func TestCampaignSQLiteSeed(t *testing.T) {
	db, err := database.New(t.Context(), filepath.Join(t.TempDir(), "club_campaign.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.Runtime{BaseURL: "http://localhost:8090", Location: loc, ActivationValidity: time.Hour, RegistrationVerificationTTL: time.Hour, EmailChangeTTL: time.Hour}
	app, err := application.NewWithMailer(config.Config{SiteName: "Test campaign"}, runtime, db, mailer.Disabled{})
	if err != nil {
		t.Fatal(err)
	}
	s := state{Accounts: map[string]account{}}
	ids := identities{Emails: map[string]string{"A": "a@example.test", "B": "b@example.test", "C": "c@example.test"}}
	if err = seed(t.Context(), db, app, loc, ids, &s); err != nil {
		t.Fatal(err)
	}
	if s.Trial == 0 || s.MembershipA == 0 || s.MembershipC == 0 || s.MembershipChild == 0 || len(s.Accounts) != 3 {
		t.Fatal("incomplete campaign fixture")
	}
	var childUsers int
	if err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM users WHERE person_id=?1", s.Child).Scan(&childUsers); err != nil || childUsers != 0 {
		t.Fatal("child account behavior changed", childUsers, err)
	}
}
