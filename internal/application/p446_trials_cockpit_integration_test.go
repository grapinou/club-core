package application

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/authorization"
)

func TestP446TodayTrialCounter(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	today := f.app.Administration.Today().Time
	for _, tt := range []struct {
		name, status string
		offset       int
		want         int
	}{
		{"none", "", 0, 0}, {"one", "registered", 0, 1}, {"several", "registered", 0, 2},
		{"tomorrow", "registered", 1, 2}, {"past", "registered", -1, 2},
		{"attended", "attended", 0, 2}, {"cancelled", "cancelled", 0, 2}, {"no show", "no_show", 0, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.status != "" {
				f.exec("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,?3,?4)", f.person, f.activity, today.AddDate(0, 0, tt.offset).Format("2006-01-02"), tt.status)
			}
			body := officeOK(t, b, "/trials", fmt.Sprintf("Essais (%d)", tt.want))
			nav := pagePart(t, body, `<nav class="admin-nav"`, `</nav>`)
			if !strings.Contains(nav, fmt.Sprintf("Essais (%d)", tt.want)) {
				t.Fatal("count missing from office nav")
			}
		})
	}
	_, personal := f.personalBrowser(f.person, "counter.member")
	body := officeOK(t, personal, "/me/account")
	if strings.Contains(body, `href="/trials"`) || strings.Contains(body, "Essais (2)") {
		t.Fatal("count leaked")
	}
	if _, err := f.app.Administration.Counts(f.authenticatedContext(f.id("SELECT id FROM users WHERE username='counter.member'"))); err != authorization.ErrForbidden {
		t.Fatal("counter service permission", err)
	}
	// Outcome changes are reflected by the next request; no cross-request cache.
	f.exec("UPDATE trial_registrations SET status='attended' WHERE trial_date=?1", today.Format("2006-01-02"))
	officeOK(t, b, "/trials", "Essais (0)")
}

func TestP446RepeatTrialPeople(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	p := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Élodie','Ancienne','1990-01-01') RETURNING id")
	archived := f.id("INSERT INTO persons(first_name,last_name,archived_at) VALUES('Archive','Invisible',CURRENT_TIMESTAMP) RETURNING id")
	trial := func(person int32, date string) {
		f.exec("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,?3,'attended')", person, f.activity, date)
	}
	trial(p, "2026-09-01")
	trial(p, "2026-09-15")
	trial(archived, "2026-09-20")
	// A member with an older trial is included, just like a prospect.
	m := f.request()
	f.exec("UPDATE memberships SET status='active' WHERE id=?1", m.ID)
	trial(f.person, "2026-09-02")
	path := "/trials/new/repeat"
	body := officeOK(t, b, path, "Nom ou prénom", "Élodie Ancienne", "Rémi Dupont", "15/09/2026", officePerson(p)+"/trials/new")
	main := pagePart(t, body, `<main id="main-content"`, `</main>`)
	if strings.Count(main, `class="repeat-trial-person"`) != 2 || strings.Contains(main, "Archive Invisible") || strings.Contains(main, "Admin Club") {
		t.Fatal("repeat eligibility or duplicates")
	}
	if strings.Index(main, "Élodie Ancienne") > strings.Index(main, "Rémi Dupont") {
		t.Fatal("latest trial order")
	}
	for _, search := range []string{"élodie", "Ancienne"} {
		body = officeOK(t, b, path+"?search="+url.QueryEscape(search), "Élodie Ancienne")
		if strings.Contains(pagePart(t, body, `<main id="main-content"`, `</main>`), "Rémi Dupont") {
			t.Fatal("search not filtered")
		}
	}
	normalPerson := f.id("INSERT INTO persons(first_name,last_name) VALUES('Normal','SansDroit') RETURNING id")
	_, normal := f.personalBrowser(normalPerson, "repeat.member")
	if normal.call("GET", path, nil).Code != 403 {
		t.Fatal("repeat permissions")
	}
	if _, err := f.app.Administration.RepeatTrialPersons(f.authenticatedContext(f.id("SELECT id FROM users WHERE username='repeat.member'")), "", 0); err != authorization.ErrForbidden {
		t.Fatal("repeat service permissions", err)
	}
	for i := 0; i < 53; i++ {
		person := f.id("INSERT INTO persons(first_name,last_name) VALUES(?1,'Pages') RETURNING id", fmt.Sprintf("Person%02d", i))
		trial(person, "2026-10-01")
	}
	body = officeOK(t, b, path+"?search=Pages", "Page suivante")
	if strings.Count(body, `class="repeat-trial-person"`) != 50 || strings.Contains(body, "Person50 Pages") {
		t.Fatal("page limit")
	}
	body = officeOK(t, b, path+"?search=Pages&page=1", "Page précédente", "Person50 Pages")
	if strings.Count(body, `class="repeat-trial-person"`) != 3 || strings.Contains(body, "Person00 Pages") || strings.Contains(body, "Page suivante") {
		t.Fatal("second page")
	}
	for _, raw := range []string{"-1", "10001", "bad"} {
		if b.call("GET", path+"?page="+raw, nil).Code != 422 {
			t.Fatal("invalid page")
		}
	}
	officeOK(t, b, "/trials/new", "Nouvelle personne", "Première venue au club", `href="/persons/new?after=trial"`, `href="/trials/new/repeat"`)
}

func TestP446TrialDetailAndCompactWeek(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	group, slot := f.officeGroup()
	f.exec("UPDATE group_slots SET practice_label='Découverte' WHERE id=?1", slot)
	child := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Léa','Semainier','2014-10-10') RETURNING id")
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES(?1,?2,'mother',true)", child, f.person)
	f.exec("UPDATE persons SET phone_number='0601020304' WHERE id=?1", f.person)
	id := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status,notes) VALUES(?1,?2,?3,?4,'2026-10-03','registered','Note lisible') RETURNING id", child, f.activity, group, slot)
	body := officeOK(t, b, officeTrial(id), "Découverte", "Dojo municipal", "Léa Semainier", "Note lisible", `href="tel:0601020304"`, `href="mailto:remi@example.test"`)
	context := pagePart(t, body, `<div class="trial-context">`, `<section class="trial-follow-up"`)
	follow := pagePart(t, body, `<section class="trial-follow-up"`, `</main>`)
	for _, value := range []string{"Séance", "Pratiquant", "Responsable", "Contact principal", officePerson(child), officePerson(f.person)} {
		if !strings.Contains(context, value) {
			t.Fatal("context missing", value)
		}
	}
	for _, value := range []string{"Résultat de l’essai", "Programmé", "Note lisible", officeTrial(id) + "/notes", officeTrial(id) + "/status", officeTrial(id) + "/reschedule"} {
		if !strings.Contains(follow, value) {
			t.Fatal("follow-up missing", value)
		}
	}
	if strings.Contains(context, "/status") || strings.Contains(context, "/notes") {
		t.Fatal("actions outside follow-up")
	}
	week := officeOK(t, b, "/trials?week=2026-09-28")
	week = pagePart(t, week, `<div class="trial-week">`, `</main>`)
	for _, v := range []string{"18:30", `href="` + officeTrial(id) + `">Léa</a>, 11 ans`} {
		if !strings.Contains(week, v) {
			t.Fatal("compact week missing", v)
		}
	}
	for _, v := range []string{"Semainier", "Practice", "Groupe adultes", "Programmé", "remi@example.test", "Note lisible", "Responsable"} {
		if strings.Contains(week, v) {
			t.Fatal("superfluous week data", v)
		}
	}
	officePost(t, b, officeTrial(id)+"/notes", url.Values{"revision": {"0"}, "notes": {"Notes modifiées"}}, 303)
	officeOK(t, b, officeTrial(id), "Notes modifiées")
	officePost(t, b, officeTrial(id)+"/reschedule", url.Values{"revision": {"1"}, "trial_date": {"2026-10-07"}, "activity_id": {fmt.Sprint(f.activity)}, "group_id": {fmt.Sprint(group)}, "slot_id": {fmt.Sprint(slot)}}, 303)
	officeOK(t, b, officeTrial(id), "07/10/2026")
	officePost(t, b, officeTrial(id)+"/status", url.Values{"revision": {"2"}, "status": {"attended"}}, 303)
	officeOK(t, b, officeTrial(id), "Présent", "Après l’essai", officePerson(child)+"/memberships/new?trial="+fmt.Sprint(id))
	adult := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-09-16','attended') RETURNING id", f.person, f.activity)
	body = officeOK(t, b, officeTrial(adult), "Préparer une demande d’adhésion")
	if strings.Contains(body, "<h2>Responsable</h2>") {
		t.Fatal("adult guardian")
	}
	officePost(t, b, officePerson(f.person)+"/memberships/new", url.Values{"season_id": {fmt.Sprint(f.season)}, "type_id": {fmt.Sprint(f.kind)}, "activities": {fmt.Sprint(f.activity)}, "source_trial": {fmt.Sprint(adult)}}, 303)
	m := f.id("SELECT id FROM memberships WHERE source_trial_id=?1", adult)
	officeOK(t, b, officeTrial(adult), "Voir le dossier d’adhésion", dossierPath(m))
}

func TestP446NewPersonContinuesToExistingTrialForm(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	officeOK(t, b, "/persons/new?after=trial", `name="after" value="trial"`)
	form := url.Values{"csrf_token": {b.csrf(t, "/persons/new?after=trial")}, "FirstName": {"  Nouvelle  "}, "LastName": {"  Venue  "}, "Birthdate": {"10/10/1990"}, "Email": {"nouvelle@example.test"}, "after": {"trial"}}
	r := b.call("POST", "/persons", form)
	if r.Code != 303 {
		t.Fatal("person creation", r.Code, r.Body.String())
	}
	id := f.id("SELECT id FROM persons WHERE first_name='Nouvelle' AND last_name='Venue' AND birth_date='1990-10-10'")
	path := officePerson(id) + "/trials/new"
	if r.Header().Get("Location") != path {
		t.Fatal("trial continuation", r.Header().Get("Location"))
	}
	officeOK(t, b, path, "Nouvelle Venue", "Programmer un essai")
	officePost(t, b, path, url.Values{"activity_id": {fmt.Sprint(f.activity)}, "trial_date": {"2026-10-03"}}, 303)
	trial := f.id("SELECT id FROM trial_registrations WHERE person_id=?1", id)
	officeOK(t, b, officeTrial(trial), "Nouvelle Venue", "Programmé")
}
