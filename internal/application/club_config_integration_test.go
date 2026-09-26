package application

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/clubconfig"
	"golang.org/x/crypto/bcrypt"
)

func TestBlankAssociationConfiguredInBrowser(t *testing.T) {
	db, app, setup := newSetupApplication(t)
	secret, err := setup.IssueSecret(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	browser := newBrowser(app.Handler)
	if got := browser.call("POST", "/setup", setupForm(browser.csrf(t, "/setup"), secret, "chess.admin")); got.Code != 303 {
		t.Fatalf("setup: %d", got.Code)
	}
	if got := browser.call("POST", "/login", url.Values{"csrf_token": {browser.csrf(t, "/login")}, "username": {"chess.admin"}, "password": {"a secure password"}}); got.Code != 303 {
		t.Fatalf("login: %d", got.Code)
	}
	if page := browser.call("GET", "/admin/config", nil); page.Code != 200 || !strings.Contains(page.Body.String(), "Configurer votre association") || !strings.Contains(page.Body.String(), "À faire") {
		t.Fatalf("config start: %d", page.Code)
	}
	anonymous := newBrowser(app.Handler)
	if got := anonymous.call("GET", "/admin/config", nil); got.Code != 303 {
		t.Fatalf("anonymous: %d", got.Code)
	}
	if got := browser.call("POST", "/admin/config/association/new", url.Values{"name": {"Pirate"}}); got.Code != 403 {
		t.Fatalf("csrf: %d", got.Code)
	}
	post := func(section, record string, form url.Values) {
		t.Helper()
		p := "/admin/config/" + section + "/" + record
		form.Set("csrf_token", browser.csrf(t, p))
		got := browser.call("POST", p, form)
		if got.Code != 303 {
			t.Fatalf("%s save: %d %s", p, got.Code, got.Body.String())
		}
	}
	post("association", "new", url.Values{"name": {"Club d’échecs de Senlis"}, "short_name": {"Échecs Senlis"}, "description": {"Parties et rencontres pour tous."}, "public_email": {"bonjour@echecs.example"}, "public_phone": {"01 23 45 67 89"}})
	post("saisons", "new", url.Values{"name": {"2026/2027"}, "starts_at": {"2026-09-01"}, "ends_at": {"2027-08-31"}, "is_active": {"yes"}})
	post("lieux", "new", url.Values{"name": {"Salle municipale"}, "address": {"12 rue du Jeu, Senlis"}, "is_active": {"yes"}})
	post("activites", "new", url.Values{"name": {"Échecs"}, "is_active": {"yes"}})
	var season, location, activity int32
	if err := db.QueryRow(t.Context(), `SELECT (SELECT id FROM seasons WHERE name='2026/2027'),(SELECT id FROM locations WHERE name='Salle municipale'),(SELECT id FROM activities WHERE name='Échecs')`).Scan(&season, &location, &activity); err != nil {
		t.Fatal(err)
	}
	post("groupes", "new", url.Values{"activity_id": {fmt.Sprint(activity)}, "name": {"Jeunes"}, "description": {"Découverte et parties."}, "show_name_publicly": {"yes"}, "is_active": {"yes"}})
	var group int32
	if err := db.QueryRow(t.Context(), `SELECT id FROM groups WHERE name='Jeunes'`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	slot := url.Values{"group_id": {fmt.Sprint(group)}, "season_id": {fmt.Sprint(season)}, "location_id": {fmt.Sprint(location)}, "weekday": {"3"}, "start_time": {"14:00"}, "end_time": {"15:30"}, "valid_from": {"2026-09-01"}, "is_active": {"yes"}}
	bad := url.Values{}
	for k, v := range slot {
		bad[k] = append([]string{}, v...)
	}
	bad.Set("end_time", "13:00")
	bad.Set("csrf_token", browser.csrf(t, "/admin/config/horaires/new"))
	if got := browser.call("POST", "/admin/config/horaires/new", bad); got.Code != 422 {
		t.Fatalf("invalid slot: %d", got.Code)
	}
	post("horaires", "new", slot)
	post("tarifs", "new", url.Values{"name": {"Jeunes"}, "amount": {"85,50"}, "currency": {"EUR"}, "public_note": {"Cotisation annuelle"}, "is_active": {"yes"}})
	var org int32
	if err := db.QueryRow(t.Context(), `SELECT id FROM organizations WHERE is_active`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	post("contenu", fmt.Sprint(org), url.Values{"trial_session_description": {"Venez découvrir les échecs."}, "trial_items_to_bring": {"Curiosité\nBonne humeur"}, "public_rules_description": {"Le règlement est remis lors de la première visite."}})
	post("liens", "new", url.Values{"kind": {"site"}, "label": {"Notre blog"}, "url": {"https://echecs.example/blog"}, "position": {"0"}, "is_active": {"yes"}})
	for path, want := range map[string]string{"/": "Club d’échecs de Senlis", "/horaires": "Salle municipale", "/tarifs": "85,50 EUR", "/contact": "bonjour@echecs.example", "/essai": "Venez découvrir les échecs.", "/rules": "Le règlement est remis lors de la première visite."} {
		page := anonymous.call("GET", path, nil)
		if page.Code != 200 || !strings.Contains(page.Body.String(), want) {
			t.Fatalf("public %s: %d missing %q", path, page.Code, want)
		}
	}
	if page := anonymous.call("GET", "/essai?activity="+fmt.Sprint(activity), nil); !strings.Contains(page.Body.String(), "Échecs") || !strings.Contains(page.Body.String(), "Jeunes") {
		t.Fatal("new schedule is absent from trial flow")
	}
	if page := browser.call("GET", "/admin/config", nil); page.Code != 200 || !strings.Contains(page.Body.String(), "Les informations essentielles sont prêtes") {
		t.Fatal("configuration progress")
	}
	var audits int
	if err := db.QueryRow(t.Context(), `SELECT count(*) FROM administrative_events WHERE action='club_configuration_saved'`).Scan(&audits); err != nil || audits != 9 {
		t.Fatalf("audit: %d %v", audits, err)
	}
	// Disabling a referenced location keeps the existing slot and its history.
	post("lieux", fmt.Sprint(location), url.Values{"name": {"Salle municipale"}, "address": {"12 rue du Jeu, Senlis"}})
	var slots int
	if err := db.QueryRow(t.Context(), `SELECT count(*) FROM group_slots WHERE location_id=$1`, location).Scan(&slots); err != nil || slots != 1 {
		t.Fatal("slot history lost", err)
	}
	// A group with a schedule cannot be silently moved to another activity.
	post("activites", "new", url.Values{"name": {"Chorale"}, "is_active": {"yes"}})
	var otherActivity int32
	if err := db.QueryRow(t.Context(), `SELECT id FROM activities WHERE name='Chorale'`).Scan(&otherActivity); err != nil {
		t.Fatal(err)
	}
	move := url.Values{"activity_id": {fmt.Sprint(otherActivity)}, "name": {"Jeunes"}, "is_active": {"yes"}, "csrf_token": {browser.csrf(t, "/admin/config/groupes/"+fmt.Sprint(group))}}
	if got := browser.call("POST", "/admin/config/groupes/"+fmt.Sprint(group), move); got.Code != 422 || !strings.Contains(got.Body.String(), "préserver l’historique") {
		t.Fatalf("historical group move: %d", got.Code)
	}
	// An optional editorial update must also work with no items-to-bring list.
	post("contenu", fmt.Sprint(org), url.Values{"trial_session_description": {"Séance ouverte à tous."}})
	if page := anonymous.call("GET", "/essai", nil); !strings.Contains(page.Body.String(), "Séance ouverte à tous.") {
		t.Fatal("editorial update without items")
	}
	// A signed-in account with no role cannot open or mutate club configuration.
	person := int32(0)
	if err := db.QueryRow(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES('Nora','Visiteuse') RETURNING id`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), `INSERT INTO users(person_id,username,password_hash,activated_at) VALUES($1,'nora', $2,now())`, person, string(hash)); err != nil {
		t.Fatal(err)
	}
	ordinary := newBrowser(app.Handler)
	if got := ordinary.call("POST", "/login", url.Values{"csrf_token": {ordinary.csrf(t, "/login")}, "username": {"nora"}, "password": {"a secure password"}}); got.Code != 303 {
		t.Fatal("ordinary login")
	}
	if got := ordinary.call("GET", "/admin/config", nil); got.Code != 403 {
		t.Fatalf("ordinary read: %d", got.Code)
	}
	if got := ordinary.call("POST", "/admin/config/activites/new", url.Values{"csrf_token": {ordinary.csrf(t, "/dashboard")}, "name": {"Forbidden"}, "is_active": {"yes"}}); got.Code != 403 {
		t.Fatalf("ordinary mutation: %d", got.Code)
	}
	// Service authorization is independent of HTTP middleware.
	if _, err := clubconfig.New(db).Save(t.Context(), "activites", 0, url.Values{"name": {"Hors droit"}, "is_active": {"yes"}}); err == nil {
		t.Fatal("service accepted unauthenticated mutation")
	}
}
