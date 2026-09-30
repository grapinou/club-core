package application

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"html"
	"regexp"
)

// These helpers read the controls and links rendered by the server, rather than
// manufacturing a source_trial field which could conceal a broken UI handoff.
func recipeLink(t *testing.T, body, label string) string {
	t.Helper()
	for _, m := range regexp.MustCompile(`<a\b([^>]*)>(.*?)</a>`).FindAllStringSubmatch(body, -1) {
		if html.UnescapeString(strings.TrimSpace(m[2])) == label {
			return recipeAttribute(m[1], "href")
		}
	}
	t.Fatalf("link %q missing", label)
	return ""
}
func recipeAttribute(tag, key string) string {
	m := regexp.MustCompile(`\b` + key + `="([^"]*)"`).FindStringSubmatch(tag)
	if len(m) > 1 {
		return html.UnescapeString(m[1])
	}
	return ""
}
func recipeForm(t *testing.T, body, action string) url.Values {
	t.Helper()
	for _, m := range regexp.MustCompile(`(?s)<form\b([^>]*)>(.*?)</form>`).FindAllStringSubmatch(body, -1) {
		if recipeAttribute(m[1], "action") != action {
			continue
		}
		v := url.Values{}
		for _, tag := range regexp.MustCompile(`<input\b[^>]*>`).FindAllString(m[2], -1) {
			name, kind := recipeAttribute(tag, "name"), recipeAttribute(tag, "type")
			if name != "" && ((kind != "checkbox" && kind != "radio") || strings.Contains(tag, "checked")) {
				v.Add(name, recipeAttribute(tag, "value"))
			}
		}
		for _, sel := range regexp.MustCompile(`(?s)<select\b([^>]*)>(.*?)</select>`).FindAllStringSubmatch(m[2], -1) {
			for _, opt := range regexp.MustCompile(`<option\b[^>]*>`).FindAllString(sel[2], -1) {
				if strings.Contains(opt, "selected") {
					v.Add(recipeAttribute(sel[1], "name"), recipeAttribute(opt, "value"))
				}
			}
		}
		return v
	}
	t.Fatalf("form %s missing", action)
	return nil
}
func TestP431TrialHTTPDiagnostic(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	g, _ := f.officeGroup()
	for _, fromTrial := range []bool{true, false} {
		p := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Parcours','Recette','1990-01-01') RETURNING id")
		tr := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,trial_date,status) VALUES(?1,?2,?3,'2026-09-20','attended') RETURNING id", p, f.activity, g)
		path := officePerson(p) + "/memberships/new"
		entry := path
		if fromTrial {
			body := officeOK(t, b, officeTrial(tr))
			entry = recipeLink(t, body, "Préparer une demande d’adhésion")
		} else {
			entry = path // P4.3.2: creation is entered from Adhésions, not the Person header.
		}
		body := officeOK(t, b, entry)
		form := recipeForm(t, body, path)
		if fromTrial && (form.Get("source_trial") != fmt.Sprint(tr) || form.Get("activities") != fmt.Sprint(f.activity)) {
			t.Fatal("source/default activity lost", form)
		}
		if !fromTrial && form.Get("source_trial") != "" {
			t.Fatal("unexpected implicit source")
		}
		form.Set("season_id", fmt.Sprint(f.season))
		form.Set("type_id", fmt.Sprint(f.kind))
		if !fromTrial {
			form.Set("activities", fmt.Sprint(f.activity))
		}
		r := b.call("POST", path, form)
		if r.Code != 303 {
			t.Fatal(r.Code, r.Body.String())
		}
		id := f.id("SELECT id FROM memberships WHERE person_id=?1", p)
		expected := 0
		if fromTrial {
			expected = 1
		}
		if f.count("SELECT count(*) FROM memberships WHERE id=?1 AND source_trial_id=?2", id, tr) != expected || f.count("SELECT count(*) FROM membership_groups WHERE membership_id=?1 AND group_id=?2", id, g) != 1 || f.count("SELECT count(*) FROM administrative_events WHERE resource_id=?1 AND action='membership_trial_group_assigned'", id) != expected {
			t.Fatal("HTTP conversion result", fromTrial)
		}
		t.Logf("fromTrial=%v: source/audit count=%d, group count=1 (visible unique default for direct request), redirect=%s", fromTrial, expected, r.Header().Get("Location"))
	}
}

func TestP431NavigationAndCreation(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	body := officeOK(t, b, "/memberships", "Créer une adhésion", "personne pour une saison")
	nav := regexp.MustCompile(`(?s)<nav class="admin-nav".*?</nav>`).FindString(body)
	last := -1
	for _, path := range []string{"/admin", "/memberships", "/registration-reviews", "/trials", "/members", "/guardians", "/prospects", "/persons"} {
		pos := strings.Index(nav, `href="`+path+`"`)
		if pos <= last {
			t.Fatal("navigation order", path)
		}
		last = pos
	}
	if strings.Contains(nav, "Autres outils") || !strings.Contains(nav, "Inscriptions à vérifier (0)") || strings.Contains(nav, "/admin/config") {
		t.Fatal("navigation")
	}
	for _, path := range []string{"/members", "/guardians", "/prospects", "/persons"} {
		page := officeOK(t, b, path)
		for _, removed := range []string{"Annuaire complet", "Gérer les adhésions", "Gérer les essais"} {
			if strings.Contains(page, removed) {
				t.Fatal("redundant", path, removed)
			}
		}
		if path == "/persons" {
			recipeLink(t, page, "Ajouter une personne")
			recipeLink(t, page, "Personnes archivées")
		}
	}
	entry := recipeLink(t, body, "Créer une adhésion")
	picker := officeOK(t, b, entry, "Recherchez d’abord")
	if b.call("POST", entry, url.Values{"search": {"Dupont"}}).Code != 403 {
		t.Fatal("picker CSRF")
	}
	v := recipeForm(t, picker, entry)
	v.Set("search", "remi@example.test")
	before := f.count("SELECT count(*) FROM persons")
	r := b.call("POST", entry, v)
	if r.Code != 200 {
		t.Fatal(r.Code)
	}
	path := recipeLink(t, r.Body.String(), "Créer une adhésion pour Rémi Dupont")
	if f.count("SELECT count(*) FROM persons") != before {
		t.Fatal("search created person")
	}
	def := f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES('image',1,'Image','Décision réelle') RETURNING id")
	form := recipeForm(t, officeOK(t, b, path, "Créer une adhésion directe"), path)
	form.Set("season_id", fmt.Sprint(f.season))
	form.Set("type_id", fmt.Sprint(f.kind))
	form.Set("activities", fmt.Sprint(f.activity))
	form.Set("giver_id", fmt.Sprint(f.person))
	if b.call("POST", path, form).Code != 422 || f.count("SELECT count(*) FROM memberships") != 0 {
		t.Fatal("missing consent accepted")
	}
	form.Set("consent-"+fmt.Sprint(def), "refused")
	r = b.call("POST", path, form)
	if r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	m := f.id("SELECT id FROM memberships WHERE person_id=?1 AND status='pending' AND source_trial_id IS NULL", f.person)
	if r.Header().Get("Location") != dossierPath(m)+"?notice=requested" || f.count("SELECT count(*) FROM membership_groups") != 0 {
		t.Fatal("direct creation")
	}
	body = officeOK(t, b, path, "Dossiers existants", "Aucune saison disponible")
	if b.call("POST", path, form).Code != 422 || f.count("SELECT count(*) FROM memberships") != 1 {
		t.Fatal("occupied season accepted")
	}
	// New Person is created by the existing form, with a closed continuation marker.
	personPath := recipeLink(t, picker, "Créer une nouvelle personne")
	personForm := recipeForm(t, officeOK(t, b, personPath), "/persons")
	personForm.Set("FirstName", "Visite")
	personForm.Set("LastName", "Bureau")
	personForm.Set("Birthdate", "1990-01-02")
	bad := url.Values{"after": {"membership"}, "FirstName": {"Sans"}, "LastName": {"CSRF"}}
	if b.call("POST", "/persons", bad).Code != 403 {
		t.Fatal("person CSRF")
	}
	r = b.call("POST", "/persons", personForm)
	if r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	p := f.id("SELECT id FROM persons WHERE first_name='Visite'")
	path = officePerson(p) + "/memberships/new"
	if r.Header().Get("Location") != path {
		t.Fatal("continuation lost", r.Header())
	}
	form = recipeForm(t, officeOK(t, b, path), path)
	form.Set("season_id", fmt.Sprint(f.season))
	form.Set("type_id", fmt.Sprint(f.kind))
	form.Set("activities", fmt.Sprint(f.activity))
	form.Set("giver_id", fmt.Sprint(p))
	form.Set("consent-"+fmt.Sprint(def), "granted")
	if b.call("POST", path, form).Code != 303 {
		t.Fatal("new person membership")
	}
	if f.count("SELECT count(*) FROM memberships WHERE person_id=?1 AND status='pending' AND source_trial_id IS NULL", p) != 1 {
		t.Fatal("new membership")
	}
	personForm.Set("after", "https://outside.example/")
	personForm.Set("next", "https://outside.example/")
	if r = b.call("POST", "/persons", personForm); r.Code != 303 || r.Header().Get("Location") != "/persons" {
		t.Fatal("open redirect", r.Header())
	}
	_, ordinary := f.personalBrowser(f.person, "ordinary.recipe")
	for _, path := range []string{entry, personPath, officePerson(p) + "/memberships/new"} {
		if ordinary.call("GET", path, nil).Code != 403 {
			t.Fatal("RBAC", path)
		}
	}
	for _, path := range []string{entry, "/persons", officePerson(p) + "/memberships/new"} {
		form.Set("csrf_token", ordinary.csrf(t, "/dashboard"))
		if ordinary.call("POST", path, form).Code != 403 {
			t.Fatal("POST RBAC", path)
		}
	}
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name='president'", f.approver)
	nav = regexp.MustCompile(`(?s)<nav class="admin-nav".*?</nav>`).FindString(officeOK(t, b, "/memberships"))
	if strings.Index(nav, `href="/admin/users"`) < strings.Index(nav, `href="/persons"`) || strings.Index(nav, `href="/admin/config"`) < strings.Index(nav, `href="/admin/users"`) {
		t.Fatal("president ordering")
	}
}

func TestP431EffectiveFamilySummary(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	child, parent := f.guardianPair()
	f.id("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES(?1,?2,?3,'pending') RETURNING id", child, f.season, f.kind)
	user := f.id("INSERT INTO users(person_id,username) VALUES(?1,'parent.recipe') RETURNING id", parent)
	f.id("INSERT INTO guardian_access_grants(child_person_id,guardian_person_id,granted_by_user_id) VALUES(?1,?2,?3) RETURNING id", child, parent, f.approver)
	assertAccess := func(want bool) {
		t.Helper()
		body := officeOK(t, b, "/memberships")
		if strings.Contains(body, "Accès via responsable") != want {
			t.Fatal("incorrect family access", want)
		}
		if !want && !strings.Contains(body, "Aucun accès familial actif") {
			t.Fatal("missing honest status")
		}
		detail := officeOK(t, b, officePerson(child))
		effective := detail[strings.Index(detail, "<h3>Accès actuellement utilisables</h3>"):]
		effective = effective[:strings.Index(effective, "</section>")]
		if strings.Contains(effective, "Compte activé") != want {
			t.Fatal("summary/detail diverge")
		}
	}
	assertAccess(false)
	f.exec("UPDATE users SET activated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=?1", user)
	assertAccess(false)
	f.exec("UPDATE users SET password_hash='test-secret-never-rendered' WHERE id=?1", user)
	assertAccess(true)
	for _, q := range []string{"UPDATE users SET is_active=false WHERE id=?1", "UPDATE persons SET archived_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=?1", "UPDATE guardian_access_grants SET revoked_at=strftime('%Y-%m-%d %H:%M:%f','now'),revoked_by_user_id=?2 WHERE guardian_person_id=?1"} {
		target := parent
		if strings.Contains(q, "UPDATE users") {
			target = user
		}
		if strings.Contains(q, "?2") {
			f.exec(q, target, f.approver)
		} else {
			f.exec(q, target)
		}
		assertAccess(false)
		f.exec("UPDATE users SET is_active=true WHERE id=?1", user)
		f.exec("UPDATE persons SET archived_at=NULL WHERE id=?1", parent)
	}
}

func TestP431DiscoverAndChooseTrial(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	g, _ := f.officeGroup()
	trial := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,trial_date,status) VALUES(?1,?2,?3,'2026-09-20','attended') RETURNING id", f.person, f.activity, g)
	another := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-09-21','attended') RETURNING id", f.person, f.activity)
	f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-09-22','registered') RETURNING id", f.person, f.activity)
	path := officePerson(f.person) + "/memberships/new"
	body := officeOK(t, b, path, "Créer depuis un essai", "Groupe adultes", "Créer une adhésion directe", `href="/memberships" aria-current="page"`)
	link := recipeLink(t, body, "Créer depuis cet essai du 20/09/2026 · Practice")
	recipeLink(t, body, "Créer depuis cet essai du 21/09/2026 · Practice")
	if strings.Contains(body, "Créer depuis cet essai du 22/09/2026") || recipeForm(t, body, path).Get("source_trial") != "" {
		t.Fatal("implicit or non-attended source")
	}
	form := recipeForm(t, officeOK(t, b, link), path)
	if form.Get("source_trial") != fmt.Sprint(trial) || form.Get("activities") != fmt.Sprint(f.activity) {
		t.Fatal("selected context lost")
	}
	form.Set("type_id", fmt.Sprint(f.kind))
	// A validation error must preserve the selected source for a subsequent retry.
	form.Set("season_id", "")
	r := b.call("POST", path, form)
	if r.Code != 422 || !strings.Contains(r.Body.String(), "Depuis l’essai") || strings.Contains(r.Body.String(), "Cette demande ne sera rattachée à aucun essai") {
		t.Fatal("expected validation error with source context", r.Code)
	}
	form = recipeForm(t, r.Body.String(), path)
	if form.Get("source_trial") != fmt.Sprint(trial) {
		t.Fatal("retry source lost")
	}
	form.Set("season_id", fmt.Sprint(f.season))
	r = b.call("POST", path, form)
	if r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	m := f.id("SELECT id FROM memberships WHERE source_trial_id=?1", trial)
	officeOK(t, b, r.Header().Get("Location"), "Groupe repris de l’essai : Groupe adultes")
	if f.count("SELECT count(*) FROM membership_groups WHERE membership_id=?1 AND group_id=?2", m, g) != 1 {
		t.Fatal("discovery conversion")
	}
	eligible, err := f.app.Administration.EligibleSourceTrials(f.authenticatedContext(f.approver), f.person)
	f.must(err)
	if len(eligible) != 1 || eligible[0].ID != another {
		t.Fatal("used trial offered", eligible)
	}
}
