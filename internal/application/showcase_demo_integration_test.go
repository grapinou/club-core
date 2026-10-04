package application

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/demodata"
)

func TestShowcasePersonalHTTPScenarios(t *testing.T) {
	const password = "mon-mot-de-passe-de-test"
	db := newApplicationDatabase(t, "showcase_demo")
	for _, prepare := range []func() error{
		func() error { return demodata.SeedBudokan(t.Context(), db, true) },
		func() error { return demodata.PrepareDemoOffice(t.Context(), db, password) },
		func() error { return demodata.PrepareShowcase(t.Context(), db, password) },
	} {
		if err := prepare(); err != nil {
			t.Fatal(err)
		}
	}
	runtime := config.Runtime{BaseURL: "https://club.example.test", SecureCookies: true, ActivationValidity: time.Hour, RegistrationVerificationTTL: time.Hour, Location: time.UTC}
	app, err := NewWithMailer(config.Config{SiteName: "Club Core"}, runtime, db, &fakeMailer{})
	if err != nil {
		t.Fatal(err)
	}
	login := func(username string) *browser {
		b := newBrowser(app.Handler)
		token := b.csrf(t, "/login")
		r := b.call("POST", "/login", url.Values{"csrf_token": {token}, "username": {username}, "password": {password}})
		if r.Code != 303 || r.Header().Get("Location") != "/dashboard" {
			t.Fatal("showcase login", username, r.Code)
		}
		return b
	}
	page := func(b *browser, path string, want ...string) string {
		r := b.call("GET", path, nil)
		if r.Code != 200 {
			t.Fatal("showcase page", path, r.Code)
		}
		body := personalContent(t, r.Body.String())
		for _, text := range want {
			if !strings.Contains(body, text) {
				t.Fatal("scenario missing", path, text)
			}
		}
		return body
	}
	member := login("member.demo")
	body := page(member, "/dashboard", "Mon dossier", "Historique de mes adhésions", "Contacts d’urgence : 2 renseignés", "2026/2027", "2025/2026", "JJB Adolescents et Adultes")
	if strings.Contains(body, "de La Roche-Saint-Clair") {
		t.Fatal("dashboard contains contact details")
	}
	page(member, "/me/account", "Marc Membre", "member.demo")
	page(member, "/me/emergency", "Jean-Paul Membre", "Anne-Sophie de La Roche-Saint-Clair")
	var current, arthur, louise, hugo, emma, arthurMembership, louiseMembership int32
	if err := db.QueryRowContext(t.Context(), `SELECT m.id FROM memberships m JOIN users u ON u.person_id=m.person_id WHERE u.username='member.demo' AND m.status='active'`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	page(member, personalMembership(current), "Pratique", "Gymnase La Mardelle", "Accordé")
	parent := login("parent.demo")
	page(parent, "/dashboard", "À suivre", "Ma famille", "Arthur", "Louise", "Hugo", "Emma-Lou", "Demander une adhésion")
	for name, dst := range map[string]*int32{"Arthur": &arthur, "Louise": &louise, "Hugo": &hugo, "Emma-Lou": &emma} {
		if err := db.QueryRowContext(t.Context(), "SELECT id FROM persons WHERE first_name=? AND last_name='Famille'", name).Scan(dst); err != nil {
			t.Fatal(err)
		}
		page(parent, personalChild(*dst), name, "Contacts d’urgence", "Adhésions")
	}
	page(parent, personalChild(hugo), "Aucune adhésion enregistrée pour cet enfant.")
	page(parent, personalChild(emma), "2025/2026", "Terminée")
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM memberships WHERE person_id=? AND status='active'`, arthur).Scan(&arthurMembership); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM memberships WHERE person_id=? AND status='pending'`, louise).Scan(&louiseMembership); err != nil {
		t.Fatal(err)
	}
	page(parent, familyMembership(arthur, arthurMembership), "Pratique", "JJB enfants 10–14 ans", "Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans", "Accordé")
	page(parent, familyMembership(louise, louiseMembership), "En attente", "Un contact d’urgence doit encore être enregistré.")
	secretary := login("secretary.member.demo")
	r := secretary.call("GET", "/dashboard", nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Mon espace") || !strings.Contains(r.Body.String(), "Mon tableau de bord") {
		t.Fatal("secretary navigation")
	}
	for _, child := range []int32{arthur, louise, hugo, emma} {
		if r := secretary.call("GET", personalChild(child), nil); r.Code != 404 {
			t.Fatal("secretary inherited family access", child, r.Code)
		}
	}
	if r := member.call("GET", personalChild(arthur), nil); r.Code != 404 {
		t.Fatal("member inherited family access")
	}
	var secretaryMembership int32
	if err := db.QueryRowContext(t.Context(), `SELECT m.id FROM memberships m JOIN users u ON u.person_id=m.person_id WHERE u.username='secretary.member.demo'`).Scan(&secretaryMembership); err != nil {
		t.Fatal(err)
	}
	page(secretary, personalMembership(secretaryMembership), "Aucun groupe n’est actuellement associé à cette adhésion.", "Refusé")
	empty := login("empty.demo")
	body = page(empty, "/dashboard", "Mon dossier", "Aucune adhésion enregistrée.", "Mon compte", "Aucun contact d’urgence renseigné")
	if strings.Contains(body, "À suivre") || strings.Contains(body, "Ma famille") {
		t.Fatal("empty account unexpectedly populated")
	}
}
