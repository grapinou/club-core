package application

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/organization"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/grapinou/club-core/internal/websecurity"
)

func p447Selected(t *testing.T, body, field string, id int32) bool {
	t.Helper()
	selectHTML := pagePart(t, body, `id="`+field+`"`, `</select>`)
	return strings.Contains(selectHTML, fmt.Sprintf(`value="%d" selected`, id))
}

func TestP447RepeatPrefillAndCurrentReschedule(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	group, slot := f.officeGroup()
	old := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status,notes) VALUES(?1,?2,?3,?4,'2026-09-16','attended','Ancien matériel') RETURNING id", f.person, f.activity, group, slot)
	latest := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status,notes) VALUES(?1,?2,?3,?4,'2026-09-23','no_show','Autre matériel') RETURNING id", f.person, f.activity, group, slot)
	path := officePerson(f.person) + "/trials/new"
	source := path + "?from_trial=" + fmt.Sprint(latest)
	body := officeOK(t, b, "/trials/new/repeat", source)
	if strings.Contains(body, path+"?from_trial="+fmt.Sprint(old)+`"`) {
		t.Fatal("list did not select latest trial")
	}
	body = officeOK(t, b, source, `name="trial_date" type="date" value="`+f.app.Administration.Today().Time.Format("2006-01-02")+`"`)
	for field, id := range map[string]int32{"activity_id": f.activity, "group_id": group, "slot_id": slot} {
		if !p447Selected(t, body, field, id) {
			t.Fatal("missing prefill", field)
		}
	}
	for _, value := range []string{"Ancien matériel", "Autre matériel", `name="status"`, `name="revision"`, `value="2026-09-23"`} {
		if strings.Contains(body, value) {
			t.Fatal("old trial state copied", value)
		}
	}
	// The selected source is explicit, including when opened directly from a link.
	officeOK(t, b, path+"?from_trial="+fmt.Sprint(old))
	other := f.id("INSERT INTO persons(first_name,last_name) VALUES('Autre','Personne') RETURNING id")
	foreign := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-09-23','registered') RETURNING id", other, f.activity)
	for _, method := range []string{"GET", "POST"} {
		form := url.Values{"csrf_token": {b.csrf(t, "/persons")}}
		if b.call(method, path+"?from_trial="+fmt.Sprint(foreign), form).Code != 404 {
			t.Fatal("foreign source", method)
		}
	}
	for _, raw := range []string{"bad", "0", "-1", "999999999999", "1&from_trial=2"} {
		if b.call("GET", path+"?from_trial="+raw, nil).Code != 422 {
			t.Fatal("invalid source", raw)
		}
	}
	for _, tt := range []struct {
		name, change, restore string
		activity, group, slot bool
	}{
		{"inactive group", "UPDATE groups SET is_active=false", "UPDATE groups SET is_active=true", true, false, false},
		{"inactive slot", "UPDATE group_slots SET is_active=false", "UPDATE group_slots SET is_active=true", true, true, false},
		{"inactive activity", "UPDATE activities SET is_active=false", "UPDATE activities SET is_active=true", false, false, false},
		{"ended season", "UPDATE seasons SET ends_at='2026-09-30'", "UPDATE seasons SET ends_at='2027-08-31'", true, true, false},
		{"inactive season", "UPDATE seasons SET is_active=false", "UPDATE seasons SET is_active=true", true, true, false},
		{"expired slot", "UPDATE group_slots SET valid_until='2026-09-30'", "UPDATE group_slots SET valid_until=NULL", true, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f.exec(tt.change)
			body := officeOK(t, b, source)
			for field, want := range map[string]bool{"activity_id": tt.activity, "group_id": tt.group, "slot_id": tt.slot} {
				id := map[string]int32{"activity_id": f.activity, "group_id": group, "slot_id": slot}[field]
				if p447Selected(t, body, field, id) != want {
					t.Fatal("invalid prefill", field)
				}
			}
			f.exec(tt.restore)
		})
	}
	// Reprogramming still uses fillTrial, with the current trial's values.
	body = officeOK(t, b, officeTrial(latest))
	current := pagePart(t, body, `<details id="reprogrammer"`, `</details>`)
	for field, id := range map[string]int32{"activity_id": f.activity, "group_id": group, "slot_id": slot} {
		if !p447Selected(t, current, field, id) {
			t.Fatal("reschedule prefill", field)
		}
	}
	if !strings.Contains(current, `value="2026-09-23"`) || !strings.Contains(current, `name="revision" value="0"`) {
		t.Fatal("current date or revision missing")
	}
	if !strings.Contains(body, `>Autre matériel</textarea>`) || !strings.Contains(body, `value="no_show" selected`) {
		t.Fatal("current notes/status not filled")
	}
	// Scheduling creates a distinct trial and leaves its source unchanged.
	r := officePost(t, b, path, url.Values{"activity_id": {fmt.Sprint(f.activity)}, "group_id": {fmt.Sprint(group)}, "slot_id": {fmt.Sprint(slot)}, "trial_date": {"2026-10-07"}}, 303)
	newID := f.id("SELECT max(id) FROM trial_registrations WHERE person_id=?1", f.person)
	if newID == latest || !strings.Contains(r.Header().Get("Location"), officeTrial(newID)) {
		t.Fatal("did not create distinct trial")
	}
	trial, err := dbsqlc.New(f.db).LockAdministrativeTrial(t.Context(), newID)
	f.must(err)
	if trial.Status != "registered" || trial.Notes.Valid || trial.TrialDate.Time.Format("2006-01-02") != "2026-10-07" {
		t.Fatal("new trial inherited source state")
	}
	if f.count("SELECT count(*) FROM trial_registrations WHERE id=?1 AND status='no_show' AND notes='Autre matériel' AND trial_date='2026-09-23' AND revision=0", latest) != 1 {
		t.Fatal("source modified")
	}
}

func TestP447PersonFrenchBirthDatesAndErrors(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	form := url.Values{"csrf_token": {b.csrf(t, "/persons/new?after=trial")}, "FirstName": {"  Alice  "}, "LastName": {"Naissance"}, "Birthdate": {"31/02/2012"}, "Email": {"alice@example.test"}, "after": {"trial"}}
	r := b.call("POST", "/persons", form)
	if r.Code != 400 || !strings.Contains(r.Body.String(), `value="31/02/2012"`) || !strings.Contains(r.Body.String(), `name="after" value="trial"`) || !strings.Contains(r.Body.String(), "JJ/MM/AAAA") {
		t.Fatal("create validation/value", r.Code)
	}
	if f.count("SELECT count(*) FROM persons WHERE last_name='Naissance'") != 0 {
		t.Fatal("invalid birth persisted")
	}
	form.Set("Birthdate", "17/04/2012")
	r = b.call("POST", "/persons", form)
	if r.Code != 303 {
		t.Fatal("create French birth", r.Code)
	}
	id := f.id("SELECT id FROM persons WHERE first_name='Alice' AND birth_date='2012-04-17'")
	edit := officePerson(id) + "/edit"
	body := officeOK(t, b, edit, `value="17/04/2012"`, `placeholder="JJ/MM/AAAA"`, `autocomplete="bday"`)
	if strings.Contains(body, `type="date"`) {
		t.Fatal("birth uses calendar")
	}
	form.Set("Birthdate", "2012-04-17")
	form.Set("FirstName", "  Conservée  ")
	r = b.call("POST", edit, form)
	if r.Code != 400 || !strings.Contains(r.Body.String(), `value="2012-04-17"`) || !strings.Contains(r.Body.String(), `value="  Conservée  "`) {
		t.Fatal("edit error did not retain exact input")
	}
	form.Set("Birthdate", "29/02/2012")
	r = b.call("POST", edit, form)
	if r.Code != 303 || f.count("SELECT count(*) FROM persons WHERE id=?1 AND birth_date='2012-02-29'", id) != 1 {
		t.Fatal("edited civil date")
	}
}

func TestP447PublicMembershipBirthDates(t *testing.T) {
	t.Run("adult", func(t *testing.T) {
		f := newFixture(t)
		f.consentDefinition()
		b := newBrowser(f.app.Handler)
		v := f.joinForm(b)
		v.Set("birth_date", "31/04/1990")
		r := b.call("POST", "/join", v)
		if r.Code != 422 || !strings.Contains(r.Body.String(), `value="31/04/1990"`) || !strings.Contains(r.Body.String(), "JJ/MM/AAAA") {
			t.Fatal("adult validation")
		}
		v.Set("birth_date", "17/04/1990")
		r = b.call("POST", "/join", v)
		if r.Code != 303 || f.count("SELECT count(*) FROM registration_submissions WHERE birth_date='1990-04-17'") != 1 {
			t.Fatal("adult persistence", r.Code)
		}
	})
	t.Run("child and guardian", func(t *testing.T) {
		f := newFixture(t)
		f.consentDefinition()
		b := newBrowser(f.app.Handler)
		v := f.childForm(b)
		v.Set("birth_date", "17/04/2012")
		v.Set("guardian_birth_date", "1980-04-17")
		r := b.call("POST", "/join/child", v)
		if r.Code != 422 || !strings.Contains(r.Body.String(), `value="1980-04-17"`) || !strings.Contains(r.Body.String(), "JJ/MM/AAAA") {
			t.Fatal("guardian format/error")
		}
		v.Set("guardian_birth_date", "17/04/1980")
		r = b.call("POST", "/join/child", v)
		if r.Code != 303 || f.count("SELECT count(*) FROM registration_submissions WHERE birth_date='2012-04-17'") != 1 || f.count("SELECT count(*) FROM guardian_identity_claims WHERE birth_date='1980-04-17'") != 1 {
			t.Fatal("child/guardian persistence", r.Code)
		}
	})
	t.Run("optional guardian empty", func(t *testing.T) {
		f := newFixture(t)
		f.consentDefinition()
		b := newBrowser(f.app.Handler)
		v := f.childForm(b)
		v.Set("guardian_birth_date", "")
		if r := b.call("POST", "/join/child", v); r.Code != 303 {
			t.Fatal("optional empty birth", r.Code)
		}
	})
}

func TestP447IdentityFrenchBirthDate(t *testing.T) {
	f := newFixture(t)
	_, b := f.personalBrowser(f.person, "french.identity")
	officeOK(t, b, "/me/account/identity", `value="01/01/1990"`)
	v := url.Values{"first_name": {"Rémi"}, "last_name": {"Dupont"}, "birth_date": {"29/02/2025"}, "csrf_token": {b.csrf(t, "/me/account/identity")}}
	r := b.call("POST", "/me/account/identity", v)
	if r.Code != 422 || !strings.Contains(r.Body.String(), `value="29/02/2025"`) {
		t.Fatal("identity validation")
	}
	v.Set("birth_date", "17/04/1990")
	r = b.call("POST", "/me/account/identity", v)
	if r.Code != 303 || f.count("SELECT count(*) FROM identity_correction_requests WHERE proposed_birth_date='1990-04-17'") != 1 {
		t.Fatal("identity proposal persistence")
	}
}

func TestP447TrialContextStructureAndSeparator(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	group, slot := f.officeGroup()
	child, parent := f.guardianPair()
	id := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) VALUES(?1,?2,?3,?4,'2026-10-03','registered') RETURNING id", child, f.activity, group, slot)
	body := officeOK(t, b, officeTrial(id), `class="trial-detail-grid"`, `class="trial-context"`, `class="trial-follow-up"`, officePerson(parent))
	personPanel := pagePart(t, body, `<h2>Pratiquant</h2>`, `</section>`)
	if strings.Contains(personPanel, `class="section-panel"`) || strings.Contains(personPanel, "Essais —") || strings.Contains(personPanel, "Responsable") {
		t.Fatal("nested quota/responsible")
	}
	context := pagePart(t, body, `<div class="trial-context">`, `<section class="trial-follow-up"`)
	if strings.Index(context, "Responsable") > strings.Index(context, "Essais —") || !strings.Contains(context, "Essais —") {
		t.Fatal("quota order")
	}
	if strings.Count(body, `class="trial-context"`) != 1 || strings.Count(body, `class="trial-follow-up"`) != 1 || !strings.Contains(body, `</div>`+"\n"+`<section class="trial-follow-up"`) {
		t.Fatal("two primary children")
	}
	week := officeOK(t, b, "/trials?week=2026-09-28")
	if !regexp.MustCompile(`<time>18:30</time>, <span><a href="/trials/\d+">`).MatchString(week) {
		t.Fatal("explicit separator missing")
	}
}

func TestP447PublicTrialFrenchBirthDate(t *testing.T) {
	f := newFixture(t)
	_, slot := p444Calendar(f)
	loc := p444Paris(t)
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, loc)
	h := handlers.NewPublicHandler(organization.New(f.db), loc, "", trials.NewPublic(f.db, loc), nil, f.mail, "club@example.test", handlers.WithPublicClock(func() time.Time { return now }))
	b := newBrowser(websecurity.NewCSRF(false).Protect(h))
	path := fmt.Sprintf("/essai?activity=%d&slot=%d", f.activity, slot)
	v := url.Values{"csrf_token": {b.csrf(t, path)}, "activity": {fmt.Sprint(f.activity)}, "slot": {fmt.Sprint(slot)}, "date": {"2026-10-02"}, "first_name": {"Léa"}, "last_name": {"Française"}, "birth_date": {"31/02/2012"}, "equipment_needed": {"no"}, "step": {"contacts"}}
	r := b.call("POST", "/essai", v)
	if r.Code != 422 || !strings.Contains(r.Body.String(), `value="31/02/2012"`) || !strings.Contains(r.Body.String(), "JJ/MM/AAAA") {
		t.Fatal("public birth validation", r.Code)
	}
	v.Set("birth_date", "17/04/2012")
	v.Set("guardian_first_name", "Claire")
	v.Set("guardian_last_name", "Française")
	v.Set("guardian_email", "claire@example.test")
	v.Set("guardian_phone", "0601020304")
	v.Set("relationship", "mother")
	v.Set("step", "book")
	r = b.call("POST", "/essai", v)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Votre demande d’essai est enregistrée") || f.count("SELECT count(*) FROM persons WHERE first_name='Léa' AND birth_date='2012-04-17'") != 1 {
		t.Fatal("public birth persistence", r.Code)
	}
}
