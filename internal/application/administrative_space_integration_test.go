package application

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/websecurity"
)

func officePerson(id int32) string { return fmt.Sprintf("/persons/%d", id) }
func officeTrial(id int32) string  { return fmt.Sprintf("/trials/%d", id) }
func officePost(t *testing.T, b *browser, path string, form url.Values, want int) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("csrf_token", b.csrf(t, "/persons"))
	r := b.call("POST", path, form)
	if r.Code != want {
		t.Fatalf("%s: status %d want %d: %s", path, r.Code, want, r.Body.String())
	}
	if want == 303 && ((!strings.HasSuffix(r.Header().Get("Location"), "?saved=1") && !strings.HasSuffix(r.Header().Get("Location"), "?notice=requested")) || strings.Contains(r.Header().Get("Location"), "@")) {
		t.Fatal("PRG")
	}
	return r
}
func officeOK(t *testing.T, b *browser, path string, values ...string) string {
	t.Helper()
	r := b.call("GET", path, nil)
	if r.Code != 200 {
		t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
	}
	if r.Header().Get("Cache-Control") != "no-store" || r.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("private headers")
	}
	for _, v := range values {
		if !strings.Contains(r.Body.String(), v) {
			t.Fatalf("%s missing %q", path, v)
		}
	}
	for _, s := range []string{"password_hash", "$2a$", "$2b$", "code_hash", "token_hash", "preserved"} {
		if strings.Contains(r.Body.String(), s) {
			t.Fatal("secret in administrative HTML")
		}
	}
	return r.Body.String()
}
func (f *fixture) officeGroup() (int32, int32) {
	g := f.id("INSERT INTO groups(activity_id,name) VALUES($1,'Groupe adultes') RETURNING id", f.activity)
	slot := f.id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from) VALUES($1,$2,3,'18:30','20:00','Dojo municipal','2026-09-01') RETURNING id", g, f.season)
	return g, slot
}
func TestAdministrativeMultiSessionAndPersons(t *testing.T) {
	f := newFixture(t)
	aID, a := f.personalBrowser(f.person, "member.a")
	b := f.membershipAdminBrowser()
	f.exec("DELETE FROM user_roles WHERE user_id=$1", f.approver)
	for _, role := range []string{"treasurer", "coach", "president"} {
		f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name=$2", f.approver, role)
		want := 403
		if role == "president" {
			want = 200
		}
		for _, path := range []string{"/admin", "/trials", officePerson(f.person)} {
			if r := b.call("GET", path, nil); r.Code != want {
				t.Fatal("role matrix", role, path, r.Code)
			}
		}
		f.exec("DELETE FROM user_roles WHERE user_id=$1", f.approver)
	}
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", f.approver)
	child := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Enfant','Famille','2015-01-01') RETURNING id")
	parent := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Parent','Famille','1980-01-01') RETURNING id")
	cID, c := f.personalBrowser(parent, "guardian.c")
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES($1,$2,'mother',true)", child, parent)
	_, err := f.app.GuardianAccess.Grant(f.authenticatedContext(f.approver), child, parent)
	f.must(err)
	m := f.request()
	for _, normal := range []*browser{a, c} {
		officeOK(t, normal, "/me/account")
		for _, path := range []string{"/admin", "/persons", officePerson(f.person), "/trials", dossierPath(m.ID), officePerson(f.person) + "/memberships/new"} {
			if r := normal.call("GET", path, nil); r.Code != 403 {
				t.Fatalf("normal/guardian %s: %d", path, r.Code)
			}
		}
		token := normal.csrf(t, "/login")
		r := normal.call("POST", officePerson(f.person)+"/notes", url.Values{"csrf_token": {token}, "notes": {"forbidden"}})
		if r.Code != 403 {
			t.Fatal("unauthorized mutation", r.Code)
		}
	}
	f.personalOK(a, personalMembership(m.ID))
	f.personalOK(c, personalChild(child))
	f.personalDenied(b, personalMembership(m.ID))
	f.personalDenied(b, personalChild(child))
	officeOK(t, b, "/me/account?person_id="+fmt.Sprint(f.person), "Admin", "Club")
	for _, path := range []string{"/admin", "/persons", officePerson(f.person), officePerson(child), "/trials", dossierPath(m.ID)} {
		officeOK(t, b, path)
	}
	officeOK(t, b, officePerson(f.person), "member.a", "Compte utilisateur")
	officeOK(t, b, officePerson(child), "Aucun compte utilisateur associé", "Responsable", "Parent", "Contact principal", "ne constituent pas")
	officeOK(t, b, officePerson(parent), "Enfant")
	for _, path := range []string{"/admin", officePerson(f.person), "/trials", officePerson(f.person) + "/trials/new"} {
		r := newBrowser(f.app.Handler).call("GET", path, nil)
		if r.Code != 303 || r.Header().Get("Location") != "/login" {
			t.Fatal("anonymous", path, r.Code)
		}
	}
	for _, path := range []string{"/persons/999999", "/persons/nope", "/trials/999999", "/trials/-1", "/memberships/999999/groups"} {
		if r := b.call("GET", path, nil); r.Code != 404 {
			t.Fatal("absent", path, r.Code)
		}
	}
	f.exec("UPDATE persons SET phone_number='+33612345678' WHERE id=$1", f.person)
	for _, search := range []string{"rémi", "Dupont", "remi@example.test", "06 12 34 56 78"} {
		r := officePost(t, b, "/persons/search", url.Values{"search": {search}}, 200)
		if !strings.Contains(r.Body.String(), officePerson(f.person)) {
			t.Fatal("search missing person", search)
		}
		if r.Header().Get("Location") != "" {
			t.Fatal("search PII redirect")
		}
	}
	officePost(t, b, officePerson(f.person)+"/notes", url.Values{"notes": {"<script>SECRET_PERSON_NOTE</script>"}}, 303)
	officeOK(t, b, officePerson(f.person), "&lt;script&gt;SECRET_PERSON_NOTE&lt;/script&gt;")
	invalidNote := strings.Repeat("n", 10001)
	invalidResponse := officePost(t, b, officePerson(f.person)+"/notes", url.Values{"notes": {invalidNote}}, 422)
	if !strings.Contains(invalidResponse.Body.String(), invalidNote) {
		t.Fatal("invalid note not preserved")
	}

	f.personalOK(a, "/me/account")
	if f.count("SELECT count(*) FROM administrative_events WHERE actor_user_id=$1 AND action='person_notes_updated' AND resource_id=$2", f.approver, f.person) != 1 {
		t.Fatal("audit missing")
	}
	var audit string
	f.must(f.db.QueryRow(t.Context(), "SELECT jsonb_agg(to_jsonb(e))::text FROM administrative_events e").Scan(&audit))
	if strings.Contains(audit, "SECRET_PERSON_NOTE") {
		t.Fatal("sensitive audit")
	}
	// A role and a guardian grant remain independent when held by the same user.
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", cID)
	officeOK(t, c, "/admin")
	f.personalOK(c, personalChild(child))
	f.personalDenied(c, personalMembership(m.ID))
	f.exec("DELETE FROM user_roles WHERE user_id=$1", cID)
	if r := c.call("GET", "/admin", nil); r.Code != 403 {
		t.Fatal("role cache")
	}
	f.personalOK(c, personalChild(child))
	if _, err := f.app.Administration.Person(f.authenticatedContext(aID), f.person); err != authorization.ErrForbidden {
		t.Fatal("service RBAC", err)
	}
	// The list is bounded without a per-person query and search wildcards are literal.
	f.exec("INSERT INTO persons(first_name,last_name) SELECT 'Extra','ZZZ'||n FROM generate_series(1,55) n")
	body := officeOK(t, b, "/persons", "Page suivante")
	if strings.Count(body, "Modifier les informations") != 50 {
		t.Fatal("list bound")
	}
	officeOK(t, b, "/persons?page=1", "Page précédente")
	r := officePost(t, b, "/persons/search", url.Values{"search": {"%"}}, 200)
	if !strings.Contains(r.Body.String(), "Aucune personne") {
		t.Fatal("wildcard search")
	}
}

func TestAdministrativeTrialWorkflowAndConcurrency(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	g, slot := f.officeGroup()
	newPath := officePerson(f.person) + "/trials/new"
	officeOK(t, b, newPath, "Programmer un essai", "Dojo municipal")
	form := url.Values{"trial_date": {"2026-09-16"}, "activity_id": {fmt.Sprint(f.activity)}, "group_id": {fmt.Sprint(g)}, "slot_id": {fmt.Sprint(slot)}, "notes": {"Premier contact"}, "person_id": {"999999"}}
	for _, date := range []string{"2026-09-17", "2027-09-15", "bad"} {
		form.Set("trial_date", date)
		officePost(t, b, newPath, form, 422)
	}
	form.Set("trial_date", "2026-09-16")
	form.Set("group_id", "")
	officePost(t, b, newPath, form, 422)
	form.Set("group_id", fmt.Sprint(g))
	f.exec("UPDATE groups SET is_active=false WHERE id=$1", g)
	officePost(t, b, newPath, form, 422)
	f.exec("UPDATE groups SET is_active=true WHERE id=$1", g)
	f.exec("UPDATE group_slots SET is_active=false WHERE id=$1", slot)
	officePost(t, b, newPath, form, 422)
	f.exec("UPDATE group_slots SET is_active=true WHERE id=$1", slot)
	otherActivity := f.id("INSERT INTO activities(name) VALUES('Other') RETURNING id")
	form.Set("activity_id", fmt.Sprint(otherActivity))
	officePost(t, b, newPath, form, 422)
	form.Set("activity_id", fmt.Sprint(f.activity))
	r := officePost(t, b, newPath, form, 303)
	path := strings.TrimSuffix(r.Header().Get("Location"), "?saved=1")
	id := f.id("SELECT id FROM trial_registrations WHERE person_id=$1", f.person)
	if path != officeTrial(id) {
		t.Fatal("wrong trial person")
	}
	officeOK(t, b, path, "Programmé", "18:30", "20:00", "Dojo municipal")
	officeOK(t, b, "/trials?date=2026-09-16", "Rémi") // name is visible, notes stay in detail
	form.Set("revision", "0")
	form.Set("trial_date", "2026-09-23")
	officePost(t, b, path+"/reschedule", form, 303)
	officePost(t, b, path+"/status", url.Values{"revision": {"0"}, "status": {"attended"}}, 409)
	invalidStatus := officePost(t, b, path+"/status", url.Values{"revision": {"1"}, "status": {"invented"}}, 422)
	if !strings.Contains(invalidStatus.Body.String(), "résultat d’essai valide") {
		t.Fatal("invalid result feedback")
	}
	officePost(t, b, path+"/status", url.Values{"revision": {"1"}, "status": {"attended"}}, 303)
	officePost(t, b, path+"/notes", url.Values{"revision": {"2"}, "notes": {"<img src=x onerror=alert(1)>"}}, 303)
	officeOK(t, b, path, "Présent", "&lt;img")
	// Two independent browser sessions submit the same revision; exactly one wins.
	b2 := f.loginBrowser("admin")
	token1, token2 := b.csrf(t, "/persons"), b2.csrf(t, "/persons")
	done := make(chan int, 2)
	start := make(chan struct{})
	for i, bb := range []*browser{b, b2} {
		go func(bb *browser, token string) {
			<-start
			done <- bb.call("POST", path+"/status", url.Values{"csrf_token": {token}, "revision": {"3"}, "status": {"no_show"}}).Code
		}(bb, []string{token1, token2}[i])
	}
	close(start)
	codes := map[int]int{}
	codes[<-done]++
	codes[<-done]++
	if codes[303] != 1 || codes[409] != 1 {
		t.Fatal("concurrent trial", codes)
	}
	trial, err := dbsqlc.New(f.db).LockAdministrativeTrial(t.Context(), id)
	f.must(err)
	if trial.Revision != 4 || trial.Status != "no_show" {
		t.Fatal("trial concurrency state")
	}
	if f.count("SELECT count(*) FROM administrative_events WHERE resource_type='trial' AND resource_id=$1", id) != 5 {
		t.Fatal("failed writes audited")
	}
	// Audit failure rolls back the business write.
	f.exec("CREATE FUNCTION reject_office_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test audit failure'; END $$")
	f.exec("CREATE TRIGGER reject_office_audit BEFORE INSERT ON administrative_events FOR EACH ROW EXECUTE FUNCTION reject_office_audit()")
	officePost(t, b, path+"/notes", url.Values{"revision": {"4"}, "notes": {"MUST_ROLL_BACK"}}, 503)
	trial, err = dbsqlc.New(f.db).LockAdministrativeTrial(t.Context(), id)
	f.must(err)
	if trial.Revision != 4 || trial.Notes.String == "MUST_ROLL_BACK" {
		t.Fatal("non-atomic audit")
	}
}

func TestAdministrativeAttentionAndFamilyWorkflow(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	group, slot := f.officeGroup()
	f.exec("UPDATE group_slots SET practice_label='Séance découverte' WHERE id=$1", slot)
	today := f.app.Administration.Today().Time
	past, present, future := today.AddDate(0, 0, -1).Format("2006-01-02"), today.Format("2006-01-02"), today.AddDate(0, 0, 1).Format("2006-01-02")
	child := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Lina','Parcours',$1) RETURNING id", today.AddDate(-9, 0, 0))
	guardian := f.id("INSERT INTO persons(first_name,last_name,birth_date,email,phone_number) VALUES('Camille','Parcours',$1,'camille@example.test','0601020304') RETURNING id", today.AddDate(-35, 0, 0))
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES($1,$2,'mother',true)", child, guardian)
	f.exec("UPDATE persons SET phone_number='0605060708' WHERE id=$1", f.person)
	pastTrial := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status,notes) VALUES($1,$2,$3,$4,$5,'registered','Matériel demandé : taille M') RETURNING id", child, f.activity, group, slot, past)
	todayTrial := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) VALUES($1,$2,$3,$4,$5,'registered') RETURNING id", f.person, f.activity, group, slot, present)
	futureTrial := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) VALUES($1,$2,$3,$4,$5,'registered') RETURNING id", f.person, f.activity, group, slot, future)
	f.request()

	home := officeOK(t, b, "/admin", "Essais passés sans résultat", "Aujourd’hui et à venir", "Lina Parcours", "Note à consulter", "Séance découverte")
	if !strings.Contains(home, officeTrial(pastTrial)) || !strings.Contains(home, officeTrial(todayTrial)) || !strings.Contains(home, officeTrial(futureTrial)) {
		t.Fatal("dashboard trial navigation")
	}
	pending := officeOK(t, b, "/trials?pending=1", "Lina Parcours", "Résultat à renseigner")
	if strings.Contains(pending, "Rémi Dupont") {
		t.Fatal("pending list includes upcoming trial")
	}
	officeOK(t, b, "/trials", "Lina Parcours", "Rémi Dupont", "À venir", "Passé", "Programmé")
	childDetail := officeOK(t, b, officeTrial(pastTrial), "Lina Parcours", "Camille Parcours", "camille@example.test", "0601020304", "Contact principal", "Matériel demandé : taille M", "Séance découverte")
	if !strings.Contains(childDetail, officePerson(child)) || !strings.Contains(childDetail, officePerson(guardian)) {
		t.Fatal("family navigation")
	}
	adultDetail := officeOK(t, b, officeTrial(todayTrial), "Rémi Dupont", "0605060708", "Marquer absent")
	if strings.Contains(adultDetail, "<h2>Responsable</h2>") {
		t.Fatal("adult trial has guardian section")
	}
	officeOK(t, b, officePerson(child), "Camille Parcours", "Contact principal", "camille@example.test", "0601020304", "Séance découverte", "Programmé")
	officeOK(t, b, officePerson(guardian), "Enfant", "Lina Parcours")
	people := officeOK(t, b, "/persons", "Prospect après essai", "Adhésion", "Responsable")
	if !strings.Contains(people, officePerson(child)) || !strings.Contains(people, officePerson(guardian)) {
		t.Fatal("person list navigation")
	}
	officePost(t, b, officeTrial(pastTrial)+"/status", url.Values{"revision": {"0"}, "status": {"attended"}}, 303)
	officeOK(t, b, officeTrial(pastTrial), "Présent", "Corriger le statut")
	if strings.Contains(officeOK(t, b, "/admin"), "Lina Parcours") {
		t.Fatal("resolved trial remains in pending dashboard")
	}
	officePost(t, b, officeTrial(todayTrial)+"/status", url.Values{"revision": {"0"}, "status": {"no_show"}}, 303)
	officePost(t, b, officeTrial(futureTrial)+"/status", url.Values{"revision": {"0"}, "status": {"cancelled"}}, 303)
	officeOK(t, b, officeTrial(todayTrial), "Absent")
	officeOK(t, b, officeTrial(futureTrial), "Annulée")
	if f.count("SELECT count(*) FROM trial_registrations WHERE status='registered'") != 0 {
		t.Fatal("trial outcomes not persisted")
	}
	for _, path := range []string{"/admin", "/trials?pending=1", officeTrial(pastTrial), officePerson(child)} {
		if response := newBrowser(f.app.Handler).call("GET", path, nil); response.Code != 303 {
			t.Fatal("anonymous office access", path, response.Code)
		}
	}
	_, member := f.personalBrowser(f.person, "member.p21")
	for _, path := range []string{"/admin", "/trials?pending=1", officeTrial(pastTrial), officePerson(child)} {
		if response := member.call("GET", path, nil); response.Code != 403 {
			t.Fatal("member office access", path, response.Code)
		}
	}
}

func TestAdministrativeMembershipFromTrialAndGroups(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	g, slot := f.officeGroup()
	_, a := f.personalBrowser(f.person, "member")
	trial := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,'2026-09-16','attended') RETURNING id", f.person, f.activity)
	other := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Other','Person','1990-01-01') RETURNING id")
	foreignTrial := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,'2026-09-16','registered') RETURNING id", other, f.activity)
	def := f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES('photo',1,'Photographie','Texte initial') RETURNING id")
	path := officePerson(f.person) + "/memberships/new"
	officeOK(t, b, path+"?trial="+fmt.Sprint(trial), "Décisions recueillies", "Photographie")
	if r := b.call("GET", path+"?trial="+fmt.Sprint(foreignTrial), nil); r.Code != 404 {
		t.Fatal("foreign source GET")
	}
	form := url.Values{"season_id": {fmt.Sprint(f.season)}, "type_id": {fmt.Sprint(f.kind)}, "activities": {fmt.Sprint(f.activity)}, "source_trial": {fmt.Sprint(foreignTrial)}, "definitions": {fmt.Sprint(def)}, "giver_id": {fmt.Sprint(f.person)}, "consent-" + fmt.Sprint(def): {"refused"}}
	officePost(t, b, path, form, 422)
	form.Set("source_trial", fmt.Sprint(trial))
	form.Set("giver_id", fmt.Sprint(other))
	officePost(t, b, path, form, 422)
	form.Set("giver_id", fmt.Sprint(f.person))
	form.Set("consent-"+fmt.Sprint(def), "")
	officePost(t, b, path, form, 422)
	form.Set("consent-"+fmt.Sprint(def), "refused")
	r := officePost(t, b, path, form, 303)
	id := f.id("SELECT id FROM memberships WHERE person_id=$1 AND season_id=$2", f.person, f.season)
	if r.Header().Get("Location") != dossierPath(id)+"?notice=requested" {
		t.Fatal("membership destination")
	}
	if f.count("SELECT count(*) FROM persons") != 3 || f.count("SELECT count(*) FROM trial_registrations") != 2 {
		t.Fatal("conversion duplicated identity")
	}
	officePost(t, b, path, form, 422)
	officeOK(t, b, dossierPath(id), "Refusé", "En attente", "Photographie")
	f.personalOK(a, personalMembership(id), "Refusé", "Dossier complet")
	groups := dossierPath(id) + "/groups"
	groupForm := url.Values{"group_id": {fmt.Sprint(g)}, "joined_at": {"2026-09-01"}}
	groupForm.Set("joined_at", "2026-08-31")
	officePost(t, b, groups, groupForm, 422)
	groupForm.Set("joined_at", "2026-09-01")
	different := f.id("INSERT INTO activities(name) VALUES('Different') RETURNING id")
	wrongGroup := f.id("INSERT INTO groups(activity_id,name) VALUES($1,'Wrong group') RETURNING id", different)
	groupForm.Set("group_id", fmt.Sprint(wrongGroup))
	officePost(t, b, groups, groupForm, 422)
	groupForm.Set("group_id", fmt.Sprint(g))
	officePost(t, b, groups, groupForm, 303)
	officePost(t, b, groups, groupForm, 422)
	assignment := f.id("SELECT id FROM membership_groups WHERE membership_id=$1", id)
	officeOK(t, b, groups, "Groupe adultes", "Actuel")
	f.personalOK(a, personalMembership(id), "Groupe adultes", "18:30", "Dojo municipal")
	foreignMember := f.id("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'pending') RETURNING id", other, f.season, f.kind)
	officePost(t, b, fmt.Sprintf("/memberships/%d/groups/%d/close", foreignMember, assignment), url.Values{"left_at": {"2026-09-10"}}, 404)
	officePost(t, b, fmt.Sprintf("%s/%d/close", groups, assignment), url.Values{"left_at": {"2026-08-31"}}, 422)
	officePost(t, b, fmt.Sprintf("%s/%d/close", groups, assignment), url.Values{"left_at": {"2026-09-10"}}, 303)
	if strings.Contains(f.personalOK(a, personalMembership(id)), "Groupe adultes") {
		t.Fatal("closed group presented as current")
	}
	groupForm.Set("joined_at", "2026-09-09")
	officePost(t, b, groups, groupForm, 422)
	groupForm.Set("joined_at", "2026-09-10")
	officePost(t, b, groups, groupForm, 303)
	if f.count("SELECT count(*) FROM membership_groups WHERE membership_id=$1", id) != 2 {
		t.Fatal("lost group history")
	}
	officePost(t, b, dossierPath(id)+"/notes", url.Values{"notes": {"SECRET_ADMIN_NOTE <script>x</script>"}}, 303)
	officeOK(t, b, dossierPath(id)+"/notes", "SECRET_ADMIN_NOTE &lt;script&gt;")
	f.personalOK(a, personalMembership(id))
	f.exec("UPDATE consent_definitions SET is_active=false WHERE id=$1", def)
	f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES('photo',2,'NEW_NOT_PRESENTED','Changed') RETURNING id")
	if strings.Contains(officeOK(t, b, dossierPath(id)), "NEW_NOT_PRESENTED") {
		t.Fatal("historical consent changed")
	}
	oldSeason := f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES('Historique 2025','2025-09-01','2026-08-31') RETURNING id")
	f.id("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'ended') RETURNING id", f.person, oldSeason, f.kind)
	officeOK(t, b, officePerson(f.person), "Historique 2025", "Terminée")
	_ = slot
}

func TestAdministrativeHTTPSecurityAndGroupConcurrency(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	m := f.request()
	g, _ := f.officeGroup()
	path := officePerson(f.person) + "/notes"
	token := b.csrf(t, "/persons")
	for _, target := range []string{path, officePerson(f.person) + "/trials/new", officePerson(f.person) + "/memberships/new", dossierPath(m.ID) + "/groups", dossierPath(m.ID) + "/notes", "/trials/999/status", "/trials/999/notes", "/trials/999/reschedule", dossierPath(m.ID) + "/groups/999/close", "/persons/search"} {
		if r := b.call("POST", target, url.Values{"csrf_token": {"invalid"}}); r.Code != 403 {
			t.Fatal("CSRF", target, r.Code)
		}
	}
	form := url.Values{"csrf_token": {token}, "notes": {"origin rejected"}}
	req := httptest.NewRequest(http.MethodPost, "https://club.example.test"+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://foreign.example.test")
	for _, cookie := range b.cookies {
		req.AddCookie(cookie)
	}
	r := httptest.NewRecorder()
	f.app.Handler.ServeHTTP(r, req)
	if r.Code != 403 {
		t.Fatal("cross origin", r.Code)
	}
	form.Set("notes", strings.Repeat("x", websecurity.MaxFormBodyBytes))
	if r = b.call("POST", path, form); r.Code != 400 {
		t.Fatal("body limit", r.Code)
	}
	if f.count("SELECT count(*) FROM administrative_events") != 0 {
		t.Fatal("rejected HTTP mutated")
	}
	// Concurrent group additions serialize on the membership and preserve a single open interval.
	path = dossierPath(m.ID) + "/groups"
	b2 := f.loginBrowser("admin")
	token2 := b2.csrf(t, "/persons")
	done := make(chan int, 2)
	start := make(chan struct{})
	for i, bb := range []*browser{b, b2} {
		go func(bb *browser, csrf string) {
			<-start
			done <- bb.call("POST", path, url.Values{"csrf_token": {csrf}, "group_id": {fmt.Sprint(g)}, "joined_at": {"2026-09-01"}}).Code
		}(bb, []string{token, token2}[i])
	}
	close(start)
	codes := map[int]int{}
	codes[<-done]++
	codes[<-done]++
	if codes[303] != 1 || codes[422] != 1 {
		t.Fatal("group race", codes)
	}
	if f.count("SELECT count(*) FROM membership_groups WHERE membership_id=$1", m.ID) != 1 || f.count("SELECT count(*) FROM administrative_events") != 1 {
		t.Fatal("duplicate assignment or audit")
	}
}

func TestAdministrativeMembershipConcurrencyAndRollback(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	b2 := f.loginBrowser("admin")
	trial := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,'2026-09-16','attended') RETURNING id", f.person, f.activity)
	path := officePerson(f.person) + "/memberships/new"
	tokens := []string{b.csrf(t, "/persons"), b2.csrf(t, "/persons")}
	done := make(chan int, 2)
	start := make(chan struct{})
	for i, bb := range []*browser{b, b2} {
		go func(bb *browser, token string) {
			<-start
			done <- bb.call("POST", path, url.Values{"csrf_token": {token}, "season_id": {fmt.Sprint(f.season)}, "type_id": {fmt.Sprint(f.kind)}, "activities": {fmt.Sprint(f.activity)}, "source_trial": {fmt.Sprint(trial)}}).Code
		}(bb, tokens[i])
	}
	close(start)
	codes := map[int]int{}
	codes[<-done]++
	codes[<-done]++
	if codes[303] != 1 || codes[422] != 1 {
		t.Fatal("concurrent conversion", codes)
	}
	if f.count("SELECT count(*) FROM memberships WHERE person_id=$1", f.person) != 1 || f.count("SELECT count(*) FROM administrative_events") != 1 {
		t.Fatal("conversion duplicated")
	}
	other := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Other','Contact','1990-01-01') RETURNING id")
	f.exec("CREATE FUNCTION reject_conversion_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test'; END $$")
	f.exec("CREATE TRIGGER reject_conversion_audit BEFORE INSERT ON administrative_events FOR EACH ROW EXECUTE FUNCTION reject_conversion_audit()")
	officePost(t, b, officePerson(other)+"/memberships/new", url.Values{"season_id": {fmt.Sprint(f.season)}, "type_id": {fmt.Sprint(f.kind)}, "activities": {fmt.Sprint(f.activity)}}, 503)
	if f.count("SELECT count(*) FROM memberships WHERE person_id=$1", other) != 0 || f.count("SELECT count(*) FROM membership_activities") != 1 {
		t.Fatal("conversion not atomic")
	}
	// Authentication precedes parsing and CSRF, as on established administrative routes.
	r := newBrowser(f.app.Handler).call("POST", path, nil)
	if r.Code != 303 || r.Header().Get("Location") != "/login" {
		t.Fatal("anonymous POST")
	}
}
