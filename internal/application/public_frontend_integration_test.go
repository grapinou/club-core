package application

import (
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/demodata"
	"github.com/grapinou/club-core/internal/organization"
)

func TestPublicFrontendPostgres(t *testing.T) {
	db := newApplicationDatabase(t, "public_demo")
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(demodata.SeedBudokan(ctx, db, true))
	// Keep the HTTP test independent of the wall-clock year, without changing
	// seed data or historical fixtures. Season selection itself is tested below.
	exec := func(sql string, args ...any) { t.Helper(); _, err := db.Exec(ctx, sql, args...); must(err) }
	exec("UPDATE seasons SET starts_at=CURRENT_DATE-1,ends_at=CURRENT_DATE+365")
	exec("UPDATE group_slots SET valid_from=CURRENT_DATE-1,valid_until=CURRENT_DATE+365")
	cfg := config.Config{SiteName: "TCR SENTINEL", Home: config.PageConfig{Description: "JSON HOME SENTINEL"}, Contact: config.ContactConfig{EmailAddress: "legacy@example.test"}, Rules: config.PageConfig{Description: "Texte éditorial du règlement"}}
	app, err := NewWithMailer(cfg, config.Runtime{Location: time.UTC, ActivationValidity: time.Hour, RegistrationVerificationTTL: time.Hour}, db, &fakeMailer{})
	must(err)
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	body := func(path string) string {
		t.Helper()
		w := get(path)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		b := html.UnescapeString(w.Body.String())
		if strings.Count(b, "<h1>") != 1 {
			t.Fatal("h1", path)
		}
		for _, bad := range []string{"TCR SENTINEL", "JSON HOME SENTINEL", "legacy@example.test", "JJB pratiques spécifiques", "Rattachement technique", "280 €", "260 €", "250 €"} {
			if strings.Contains(b, bad) {
				t.Fatalf("public leak: %s on %s", bad, path)
			}
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("headers", path)
		}
		return b
	}
	home := body("/")
	for _, want := range []string{"Budokan Sud Oise", "Jiu-Jitsu Brésilien", "Jiu-Jitsu Traditionnel / Combat", "Préparation physique", "Gymnase La Mardelle", "Rue des Marais, 60260 Lamorlaye", "budokansud.oise@gmail.com", "06 21 03 21 61"} {
		if !strings.Contains(home, want) {
			t.Fatal("home", want)
		}
	}
	for _, want := range []string{`src="/static/images/budokan/hero/hero-bureau-mascots.png"`, `hero-bureau-mascots-800.webp 800w`, `src="/static/images/budokan/illustrations/training-jjb.png"`, `training-jjb-800.webp 800w`, `src="/static/images/budokan/illustrations/club-spirit.png"`, `club-spirit-800.webp 800w`} {
		if !strings.Contains(home, want) {
			t.Fatal("home visual", want)
		}
	}
	schedule := body("/horaires")
	for _, want := range []string{`src="/static/images/budokan/illustrations/schedule-jjb.png"`, `schedule-jjb-800.webp 800w`, `alt="" loading="lazy"`} {
		if !strings.Contains(schedule, want) {
			t.Fatal("schedule visual", want)
		}
	}
	if strings.Count(schedule, `class="public-slot"`) != 16 {
		t.Fatal("slot count")
	}
	for _, want := range []string{"2026/2027", "Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche", "18:15", "22:00", "10:00", "12:00", "16:00", "18:00", "JJB No-Gi", "JJB libre", "Préparation physique / Jiu-Jitsu Brésilien", "Gymnase La Mardelle", "JJB Adolescents et Adultes"} {
		if !strings.Contains(schedule, want) {
			t.Fatal("schedule", want)
		}
	}
	for _, path := range []string{"/tarifs", "/contact", "/essai", "/rules"} {
		body(path)
	}
	if trial := body("/essai"); !strings.Contains(trial, `src="/static/images/budokan/illustrations/trial-jjb.png"`) || !strings.Contains(trial, `trial-jjb-800.webp 800w`) || strings.Contains(trial, "schedule-jjb.png") {
		t.Fatal("trial visual")
	}
	contact := body("/contact")
	if !strings.Contains(contact, "instagram.com/budokan_sud_oise/") {
		t.Fatal("social link")
	}
	if !strings.Contains(body("/tarifs"), "Les montants ne sont pas encore affichés") {
		t.Fatal("pricing limitation")
	}
	exec("UPDATE groups SET name='Regroupement interne renommé' WHERE NOT show_name_publicly")
	if strings.Contains(body("/horaires"), "Regroupement interne renommé") {
		t.Fatal("hidden name rule must be generic")
	}
	exec("UPDATE seasons SET is_active=false")
	if !strings.Contains(body("/horaires"), "Aucun planning de saison courante") {
		t.Fatal("missing current season HTTP")
	}
	exec("UPDATE seasons SET is_active=true")
	for from, to := range map[string]string{"/club": "/", "/where": "/contact#lieux", "/when": "/horaires"} {
		w := get(from)
		if w.Code != 308 || w.Header().Get("Location") != to {
			t.Fatal("alias", from)
		}
	}
	w := httptest.NewRecorder()
	app.Handler.ServeHTTP(w, httptest.NewRequest("POST", "/horaires", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("public write")
	}
	// Every request re-reads the DB, with no restart or config update.
	exec("UPDATE organizations SET name='Association nouvelle',public_email='public@example.test'")
	exec("UPDATE locations SET name='Salle nouvelle',address='Adresse nouvelle'")
	if h := body("/"); !strings.Contains(h, "Association nouvelle") || strings.Contains(h, "Budokan Sud Oise") || !strings.Contains(h, "public@example.test") {
		t.Fatal("identity not refreshed")
	}
	if h := body("/horaires"); !strings.Contains(h, "Salle nouvelle") || !strings.Contains(h, "Adresse nouvelle") || strings.Contains(h, "Gymnase La Mardelle") {
		t.Fatal("location not refreshed")
	}
	exec("UPDATE organization_links SET is_active=false")
	if strings.Contains(body("/contact"), "instagram.com") {
		t.Fatal("inactive link")
	}
	exec("INSERT INTO organization_links(organization_id,kind,label,url) SELECT id,'external','Club partenaire','https://example.org/' FROM organizations")
	exec("INSERT INTO organization_links(organization_id,kind,label,url) SELECT id,'external','Autre lien','https://example.net/' FROM organizations")
	if c := body("/contact"); !strings.Contains(c, "Club partenaire") || !strings.Contains(c, "Autre lien") {
		t.Fatal("multiple links")
	}
	exec("UPDATE group_slots SET is_active=false WHERE weekday=7")
	if strings.Contains(body("/horaires"), "JJB libre") {
		t.Fatal("inactive slot")
	}
	exec("UPDATE groups SET is_active=false WHERE name='JJB Adolescents et Adultes'")
	if strings.Contains(body("/horaires"), "JJB No-Gi") {
		t.Fatal("inactive group")
	}
	exec("UPDATE activities SET is_active=false WHERE name='Jiu-Jitsu Traditionnel / Combat'")
	if strings.Contains(body("/"), "Jiu-Jitsu Traditionnel / Combat") || strings.Contains(body("/horaires"), "Jiu-Jitsu Traditionnel / Combat") {
		t.Fatal("inactive activity")
	}
	exec("UPDATE locations SET is_active=false")
	if strings.Contains(body("/contact"), "Salle nouvelle") || strings.Contains(body("/horaires"), `class="public-slot"`) {
		t.Fatal("inactive location")
	}
	exec("UPDATE membership_types SET is_active=false")
	if !strings.Contains(body("/tarifs"), "Aucun type d’adhésion") {
		t.Fatal("empty types")
	}
	exec("UPDATE organizations SET name='<script>alert(1)</script>',description='<b>Texte inerte</b>'")
	raw := get("/").Body.String()
	if strings.Contains(raw, "<script>alert(1)</script>") || !strings.Contains(raw, "&lt;script&gt;") {
		t.Fatal("escaping")
	}
	exec("UPDATE organizations SET is_active=false")
	for _, path := range []string{"/", "/horaires", "/tarifs", "/contact", "/essai", "/rules"} {
		if !strings.Contains(body(path), "Les informations du club seront bientôt disponibles") {
			t.Fatal("inactive org", path)
		}
	}
}

func TestPublicSeasonSelection(t *testing.T) {
	db := newApplicationDatabase(t, "season_demo")
	ctx := t.Context()
	if err := demodata.SeedBudokan(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	s := organization.New(db)
	day := func(date string) time.Time {
		v, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, date := range []string{"2026-09-01", "2026-09-14", "2027-08-31"} {
		v, err := s.PublicSchedule(ctx, day(date))
		if err != nil || v.Season != "2026/2027" || len(v.Slots) != 16 {
			t.Fatalf("current %s: %+v %v", date, v, err)
		}
	}
	if _, err := db.Exec(ctx, "INSERT INTO seasons(name,starts_at,ends_at) VALUES ('Historique','2025-09-01','2026-08-31')"); err != nil {
		t.Fatal(err)
	}
	for date, name := range map[string]string{"2026-08-31": "Historique", "2026-09-14": "2026/2027", "2027-09-01": ""} {
		v, err := s.PublicSchedule(ctx, day(date))
		if err != nil || v.Season != name {
			t.Fatalf("season %s: %+v %v", date, v, err)
		}
	}
	if _, err := db.Exec(ctx, "UPDATE seasons SET is_active=false WHERE name='2026/2027'"); err != nil {
		t.Fatal(err)
	}
	v, err := s.PublicSchedule(ctx, day("2026-09-14"))
	if err != nil || v.Season != "" {
		t.Fatal("inactive season")
	}
	if _, err := db.Exec(ctx, "UPDATE seasons SET is_active=true; INSERT INTO seasons(name,starts_at,ends_at) VALUES ('Overlap','2026-09-01','2027-08-31')"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicSchedule(ctx, day("2026-09-14")); !errors.Is(err, organization.ErrAmbiguousSeason) {
		t.Fatal("overlap must not choose arbitrarily", err)
	}
}
