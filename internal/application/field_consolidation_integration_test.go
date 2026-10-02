package application

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/trials"
)

func TestP432PublicWindowAndProgressiveForm(t *testing.T) {
	f := newFixture(t)
	f.exec("INSERT INTO organizations(name,trial_equipment_offer,trial_equipment_detail_prompt) VALUES('Cercle','Prêt disponible','Taille')")
	f.exec("UPDATE seasons SET starts_at='2026-01-01',ends_at='2027-12-31'")
	_, slot := f.officeGroup()
	f.exec("UPDATE group_slots SET weekday=1,valid_from='2026-01-01'")
	loc, _ := time.LoadLocation("Europe/Paris")
	svc := trials.NewPublic(f.db, loc)
	for _, at := range []string{"2026-09-28T12:00:00Z", "2026-10-02T12:00:00Z", "2026-10-04T12:00:00Z", "2026-10-04T22:30:00Z"} {
		now, _ := time.Parse(time.RFC3339, at)
		first, last := trials.PublicWindow(now, loc)
		offers, err := svc.Offerings(t.Context(), now)
		f.must(err)
		if len(offers) != 1 || offers[0].Dates[0] != first.Format("2006-01-02") {
			t.Fatal("next Monday", offers)
		}
		for _, raw := range offers[0].Dates {
			d, _ := time.Parse("2006-01-02", raw)
			if d.Before(first) || d.After(last) {
				t.Fatal("outside window", raw)
			}
		}
		b := trials.PublicBooking{Offering: offers[0], Date: first.AddDate(0, 0, -7).Format("2006-01-02"), FirstName: "Date", LastName: "Essai", BirthDate: "1990-01-01", Email: "date@example.test", Phone: "0601020304"}
		if _, err = svc.Book(t.Context(), b, now); err == nil {
			t.Fatal("current week POST accepted")
		}
		b.Date = offers[0].Dates[0]
		b.Minor = true
		if _, err = svc.Book(t.Context(), b, now); err != nil {
			t.Fatal(err)
		}
	}
	b := newBrowser(f.app.Handler)
	path := fmt.Sprintf("/essai?activity=%d&slot=%d", f.activity, slot)
	body := officeOK(t, b, path, "* champs obligatoires", "Continuer vers les coordonnées")
	if strings.Contains(body, "name=\"minor\"") || strings.Contains(body, "name=\"email\"") {
		t.Fatal("redundant age or premature contacts")
	}
	offers, err := svc.Offerings(t.Context(), time.Now())
	f.must(err)
	form := url.Values{"csrf_token": {hiddenValue(t, body, "csrf_token")}, "activity": {fmt.Sprint(f.activity)}, "slot": {fmt.Sprint(slot)}, "date": {offers[0].Dates[0]}, "first_name": {"Enfant"}, "last_name": {"Essai"}, "birth_date": {"2015-01-01"}, "equipment_needed": {"yes"}, "step": {"contacts"}}
	before := f.count("SELECT count(*) FROM persons")
	r := b.call("POST", "/essai", form)
	if r.Code != 200 || f.count("SELECT count(*) FROM persons") != before {
		t.Fatal("intermediate step writes", r.Code)
	}
	for _, field := range []string{"guardian_first_name", "guardian_last_name", "guardian_email", "guardian_phone", "relationship", "equipment_details"} {
		body = r.Body.String()
		tag := regexp.MustCompile("<(?:input|select)[^>]*name=\"" + field + "\"[^>]*>").FindString(body)
		if !strings.Contains(tag, "required") {
			t.Fatal("missing required", field)
		}
	}

	form.Set("birth_date", "1990-01-01")
	adultStep := b.call("POST", "/essai", form)
	if adultStep.Code != 200 || strings.Contains(adultStep.Body.String(), "name=\"guardian_email\"") {
		t.Fatal("birth correction did not switch to adult")
	}
	for _, field := range []string{"email", "phone"} {
		tag := regexp.MustCompile("<input[^>]*name=\"" + field + "\"[^>]*>").FindString(adultStep.Body.String())
		if !strings.Contains(tag, "required") {
			t.Fatal("adult required contact", field)
		}
	}
	form.Set("birth_date", "2015-01-01")
	form.Set("step", "book")
	form.Set("guardian_first_name", "Parent")
	form.Set("guardian_last_name", "Essai")
	form.Set("guardian_email", "parent@example.test")
	form.Set("guardian_phone", "0601020304")
	form.Set("relationship", "mother")
	form.Set("equipment_details", "140 cm")
	r = b.call("POST", "/essai", form)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Votre demande d’essai est enregistrée") {
		t.Fatal("minor booking", r.Code, r.Body.String())
	}
	form.Set("date", time.Now().Format("2006-01-02"))
	if b.call("POST", "/essai", form).Code != 422 {
		t.Fatal("forged date")
	}
}

func TestP432DashboardDirectoryAndPersonalContext(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	for i, name := range []string{"HierSansResultat", "Aujourdhui", "Demain", "PlusDeux"} {
		p := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES(?1,'Recette','1990-01-01') RETURNING id", name)
		f.exec("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,date(CURRENT_DATE,?3||' days'),'registered')", p, f.activity, i-1)
	}
	body := officeOK(t, b, "/admin", "Aujourdhui", "Demain", "HierSansResultat")
	if strings.Contains(body, "PlusDeux") || strings.Contains(body, "href=\"/trials?pending=1\"") || strings.Count(body, "class=\"personal-space-link\"") != 1 {
		t.Fatal("dashboard scope/duplicate/personal link")
	}
	body = officeOK(t, b, "/trials", "Semaine précédente", "Lundi", "Dimanche")
	if !strings.Contains(body, "PlusDeux") {
		t.Fatal("week missing J+2")
	}
	body = officeOK(t, b, officePerson(f.person), "Identité et coordonnées", ">Modifier</a>")
	if strings.Contains(body, "Programmer un essai") || strings.Contains(body, "Créer une demande d’adhésion") {
		t.Fatal("person shortcuts")
	}
	child, parent := f.guardianPair()
	f.exec("INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES(?1,?2,1)", child, parent)
	body = officeOK(t, b, officePerson(child), "id=\"urgence-personne\"", "Être enregistré comme responsable")
	family := body[strings.Index(body, "id=\"famille\""):strings.Index(body, "id=\"urgence-personne\"")]
	if strings.Contains(family, "urgence") {
		t.Fatal("emergency mixed with family")
	}
	adminPerson := f.id("SELECT person_id AS id FROM users WHERE id=?1", f.approver)
	f.exec("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES(?1,?2,?3,'active')", adminPerson, f.season, f.kind)
	body = officeOK(t, b, "/admin")
	if !strings.Contains(body, "class=\"personal-space-link\"") {
		t.Fatal("member secretary personal context")
	}
	body = officeOK(t, b, "/dashboard")
	main := body[strings.Index(body, "<main"):]
	if strings.Contains(main, "Ouvrir mon tableau de bord") || strings.Contains(main, "Configurer mon association") {
		t.Fatal("administrative block")
	}
	f.exec("UPDATE persons SET email=NULL WHERE id=?1", f.person)
	approved := f.approved()
	body = officeOK(t, b, dossierPath(approved.Membership.ID), "aucun email n’est renseigné", "non renseigné")
	if strings.Contains(body, "resend-activation") {
		t.Fatal("false resend")
	}
	officeOK(t, b, "/admin", "aucun email n’est renseigné")
	n := f.count("SELECT count(*) FROM user_activation_codes WHERE user_id=?1", approved.UserID)
	r := b.call("POST", resendPath(approved.UserID), url.Values{"csrf_token": {b.csrf(t, "/persons")}, "membership_id": {fmt.Sprint(approved.Membership.ID)}})
	if r.Code != 303 || !strings.Contains(r.Header().Get("Location"), "resend_no_channel") || f.count("SELECT count(*) FROM user_activation_codes WHERE user_id=?1", approved.UserID) != n {
		t.Fatal("manual resend without email")
	}
}

func TestP432GroupChoicesAndFutureSeason(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	g, _ := f.officeGroup()
	other := f.id("INSERT INTO activities(name) VALUES('Autre activité') RETURNING id")
	foreign := f.id("INSERT INTO groups(activity_id,name) VALUES(?1,'Hors activité') RETURNING id", other)
	path := officePerson(f.person) + "/memberships/new"
	initial := recipeForm(t, officeOK(t, b, path), path)
	initial.Set("type_id", fmt.Sprint(f.kind))
	initial.Set("action", "choices")
	refreshed := b.call("POST", path, initial)
	if refreshed.Code != 200 {
		t.Fatal("group refresh", refreshed.Code)
	}
	body := refreshed.Body.String()
	if !strings.Contains(body, "Seul groupe compatible") {
		t.Fatal("missing compatible prefill")
	}
	form := recipeForm(t, body, path)
	if form.Get(fmt.Sprintf("group-%d", f.activity)) != fmt.Sprint(g) {
		t.Fatal("unique default", form)
	}
	secondGroup := f.id("INSERT INTO groups(activity_id,name) VALUES(?1,'Second groupe') RETURNING id", f.activity)
	f.exec("INSERT INTO membership_type_groups(membership_type_id,group_id) VALUES(?1,?2),(?1,?3)", f.kind, secondGroup, foreign)
	refreshed = b.call("POST", path, initial)
	body = refreshed.Body.String()
	if !strings.Contains(body, "choisir explicitement") {
		t.Fatal("multiple groups")
	}
	form = recipeForm(t, body, path)
	if form.Get(fmt.Sprintf("group-%d", f.activity)) != "" {
		t.Fatal("arbitrary choice")
	}
	future := f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES('Future','2030-09-01','2031-08-31') RETURNING id")
	form.Set("season_id", fmt.Sprint(future))
	form.Set("type_id", fmt.Sprint(f.kind))
	form.Set("activities", fmt.Sprint(f.activity))
	form.Set(fmt.Sprintf("group-%d", f.activity), fmt.Sprint(foreign))
	if b.call("POST", path, form).Code != 422 {
		t.Fatal("foreign group accepted")
	}
	form.Set(fmt.Sprintf("group-%d", f.activity), fmt.Sprint(g))
	r := b.call("POST", path, form)
	if r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	m := f.id("SELECT id FROM memberships WHERE person_id=?1", f.person)
	if f.count("SELECT count(*) FROM membership_groups WHERE membership_id=?1 AND joined_at='2030-09-01'", m) != 1 {
		t.Fatal("future start")
	}
	body = officeOK(t, b, dossierPath(m)+"/groups", "value=\"2030-09-01\"")
	if strings.Contains(body, "Hors activité") {
		t.Fatal("foreign choice in groups")
	}
	p := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Sans','Groupe','1990-01-01') RETURNING id")
	path = officePerson(p) + "/memberships/new"
	form = recipeForm(t, officeOK(t, b, path), path)
	form.Set("season_id", fmt.Sprint(f.season))
	form.Set("type_id", fmt.Sprint(f.kind))
	form.Set("activities", fmt.Sprint(other))
	form.Set(fmt.Sprintf("group-%d", other), "")
	if b.call("POST", path, form).Code != 303 {
		t.Fatal("without group")
	}
	if f.count("SELECT count(*) FROM membership_groups mg JOIN memberships m ON m.id=mg.membership_id WHERE m.person_id=?1", p) != 0 {
		t.Fatal("implicit group")
	}
	// Multiple activities remain independent and are assigned in the same request.
	multi := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Deux','Activités','1990-01-01') RETURNING id")
	path = officePerson(multi) + "/memberships/new"
	form = recipeForm(t, officeOK(t, b, path), path)
	form.Set("season_id", fmt.Sprint(f.season))
	form.Set("type_id", fmt.Sprint(f.kind))
	form["activities"] = []string{fmt.Sprint(f.activity), fmt.Sprint(other)}
	form.Set(fmt.Sprintf("group-%d", f.activity), fmt.Sprint(g))
	form.Set(fmt.Sprintf("group-%d", other), fmt.Sprint(foreign))
	if b.call("POST", path, form).Code != 303 {
		t.Fatal("multiple activities")
	}
	if f.count("SELECT count(*) FROM membership_groups mg JOIN memberships m ON m.id=mg.membership_id WHERE m.person_id=?1", multi) != 2 {
		t.Fatal("multiple assignments")
	}
	// An activity with no active group remains selectable and requires no assignment.
	empty := f.id("INSERT INTO activities(name) VALUES('Sans groupe disponible') RETURNING id")
	p = f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Aucun','Groupe','1990-01-01') RETURNING id")
	path = officePerson(p) + "/memberships/new"
	form = recipeForm(t, officeOK(t, b, path, "Aucun groupe compatible"), path)
	form.Set("season_id", fmt.Sprint(f.season))
	form.Set("type_id", fmt.Sprint(f.kind))
	form.Set("activities", fmt.Sprint(empty))
	if b.call("POST", path, form).Code != 303 {
		t.Fatal("activity without group")
	}

}

func TestP432ConsentChangesAndAuthorization(t *testing.T) {
	f := newFixture(t)
	office := p43Secretary(f)
	definition := f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES('image',1,'Droit à l’image','Autorisation photographique') RETURNING id")
	m, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: f.person, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}, Consents: []memberships.Decision{{ConsentDefinitionID: definition, GivenByPersonID: f.person, Decision: "granted"}}})
	f.must(err)
	_, adult := f.personalBrowser(f.person, "adult")
	personalPath := fmt.Sprintf("/me/memberships/%d/consents/%d", m.ID, definition)
	post := func(b *browser, path, decision string, giver int32, want int) {
		t.Helper()
		token := b.csrf(t, path)
		form := url.Values{"csrf_token": {token}, "decision": {decision}, "giver_id": {fmt.Sprint(giver)}}
		r := b.call("POST", path, form)
		if r.Code != want {
			t.Fatal("consent", r.Code, r.Body.String())
		}
	}
	if adult.call("POST", personalPath, url.Values{"decision": {"refused"}}).Code != 403 {
		t.Fatal("consent CSRF")
	}
	post(adult, personalPath, "withdrawn", 999999, 303)
	if f.count("SELECT count(*) FROM membership_consents WHERE membership_id=?1", m.ID) != 2 {
		t.Fatal("history overwritten")
	}
	officePath := fmt.Sprintf("/memberships/%d/consents/%d", m.ID, definition)
	post(office, officePath, "granted", 999999, 422)
	post(office, officePath, "refused", f.person, 303)
	if f.count("SELECT count(*) FROM administrative_events WHERE resource_id=?1 AND action='membership_consent_recorded'", m.ID) != 1 {
		t.Fatal("operator audit")
	}
	child, parent := f.guardianPair()
	_, guardian := f.personalBrowser(parent, "parent")
	cm, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: child, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}, Consents: []memberships.Decision{{ConsentDefinitionID: definition, GivenByPersonID: parent, Decision: "refused"}}})
	f.must(err)
	childPath := fmt.Sprintf("/me/children/%d/memberships/%d/consents/%d", child, cm.ID, definition)
	if guardian.call("GET", childPath, nil).Code != 404 {
		t.Fatal("relation alone allowed")
	}
	f.exec("INSERT INTO guardian_access_grants(child_person_id,guardian_person_id,granted_by_user_id) VALUES(?1,?2,?3)", child, parent, f.approver)
	if !strings.Contains(officeOK(t, guardian, "/dashboard"), "class=\"personal-space-link\"") {
		t.Fatal("guardian personal context")
	}
	post(guardian, childPath, "granted", f.person, 303)
	post(guardian, childPath, "withdrawn", parent, 303)
	if adult.call("GET", childPath, nil).Code != 404 || adult.call("GET", officePath, nil).Code != 403 {
		t.Fatal("cross-resource access")
	}
	token := guardian.csrf(t, childPath)
	if adult.call("POST", childPath, url.Values{"csrf_token": {adult.csrf(t, "/dashboard")}, "decision": {"granted"}}).Code != 404 {
		t.Fatal("unauthorized child POST")
	}
	if adult.call("POST", officePath, url.Values{"csrf_token": {adult.csrf(t, "/dashboard")}, "decision": {"granted"}, "giver_id": {fmt.Sprint(f.person)}}).Code != 403 {
		t.Fatal("unauthorized secretary POST")
	}
	f.exec("UPDATE guardian_access_grants SET revoked_at=strftime('%Y-%m-%d %H:%M:%f','now'),revoked_by_user_id=?1 WHERE child_person_id=?2", f.approver, child)
	if guardian.call("POST", childPath, url.Values{"csrf_token": {token}, "decision": {"granted"}}).Code != 404 {
		t.Fatal("revoked grant")
	}
	body := officeOK(t, guardian, "/dashboard")
	if !strings.Contains(body, "class=\"personal-space-link\"") || strings.Contains(body, personalChild(child)) {
		t.Fatal("revoked child must disappear while personal navigation remains")
	}
	if f.count("SELECT count(*) FROM membership_consents WHERE membership_id=?1", cm.ID) != 3 {
		t.Fatal("child history")
	}
	if _, err = f.db.ExecContext(t.Context(), "UPDATE membership_consents SET decision='refused' WHERE membership_id=?1", m.ID); err == nil {
		t.Fatal("destructive UPDATE allowed")
	}
}

func TestP432RegistrationReviewWithoutGuardianEmail(t *testing.T) {
	f := newFixture(t)
	public := newBrowser(f.app.Handler)
	id := f.childSubmit(public, f.childForm(public))
	f.must(f.app.RegistrationApplications.ConfirmGuardian(f.authenticatedContext(f.approver), id))
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	guardian := d.Child.Guardian.ResolvedPersonID.Int32
	f.exec("UPDATE persons SET email=NULL WHERE id=?1", guardian)
	b := p43Secretary(f)
	path := fmt.Sprintf("/registration-reviews/%d", id)
	body := officeOK(t, b, path, "aucun email n’est renseigné")
	if strings.Contains(body, "action=\""+path+"/guardian-activation\"") {
		t.Fatal("review advertises impossible activation")
	}
	before := f.count("SELECT count(*) FROM user_activation_codes")
	r := b.call("POST", path+"/guardian-activation", url.Values{"csrf_token": {hiddenValue(t, body, "csrf_token")}})
	if r.Code != 303 || !strings.Contains(r.Header().Get("Location"), "activation_no_channel") || f.count("SELECT count(*) FROM user_activation_codes") != before {
		t.Fatal("manual review activation claims success or adds a code", r.Code, r.Header().Get("Location"))
	}
}

func TestP432TrialWeekBoundaries(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	for _, row := range []struct{ date, name string }{
		{"2030-01-06", "AvantSemaine"}, {"2030-01-07", "LundiSemaine"},
		{"2030-01-13", "DimancheSemaine"}, {"2030-01-14", "ApresSemaine"},
	} {
		person := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES(?1,'Calendrier','1990-01-01') RETURNING id", row.name)
		f.exec("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,?3,'registered')", person, f.activity, row.date)
	}
	start, _ := time.Parse("2006-01-02", "2030-01-07")
	rows, err := f.app.Administration.TrialsInWeek(f.authenticatedContext(f.approver), start)
	f.must(err)
	if len(rows) != 2 {
		t.Fatal("SQL week bounds must exclude adjacent Sundays/Mondays", rows)
	}
	body := officeOK(t, b, "/trials?week=2030-01-10", "LundiSemaine", "DimancheSemaine", "week=2029-12-31", "week=2030-01-14")
	if strings.Contains(body, "AvantSemaine") || strings.Contains(body, "ApresSemaine") {
		t.Fatal("week leaks adjacent dates")
	}
	monday := body[strings.Index(body, "<h2>Lundi "):strings.Index(body, "<h2>Mardi ")]
	sunday := body[strings.Index(body, "<h2>Dimanche "):]
	if !strings.Contains(monday, "LundiSemaine") || strings.Contains(monday, "DimancheSemaine") || !strings.Contains(sunday, "DimancheSemaine") {
		t.Fatal("trial in wrong day")
	}
	if b.call("GET", "/trials?week=invalid", nil).Code != 422 {
		t.Fatal("invalid week accepted")
	}
}
