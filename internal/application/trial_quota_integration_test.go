package application

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/clubconfig"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/trials"
)

func TestP31QuotaConfigurationAndPublic(t *testing.T) {
	f := newFixture(t)
	admin := f.membershipAdminBrowser()
	const field = "max_trials_per_person_per_season"
	path := "/admin/config/association/new"
	form := url.Values{"name": {"Cercle culturel"}, field: {""}}
	if r := newBrowser(f.app.Handler).call("POST", path, form); r.Code != 303 {
		t.Fatal("anonymous config", r.Code)
	}
	if r := admin.call("POST", path, form); r.Code != 403 {
		t.Fatal("config CSRF", r.Code)
	}
	user, member := f.personalBrowser(f.person, "ordinary.quota")
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", user)
	if r := member.call("POST", path, form); r.Code != 403 {
		t.Fatal("club.configure bypass", r.Code)
	}
	if _, err := clubconfig.New(f.db).Save(f.authenticatedContext(user), "association", 0, form); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("service permission", err)
	}
	save := func(value string, want int) {
		t.Helper()
		form.Set(field, value)
		form.Set("csrf_token", admin.csrf(t, path))
		if r := admin.call("POST", path, form); r.Code != want {
			t.Fatalf("quota=%q: %d want %d", value, r.Code, want)
		}
	}
	save("", 303)
	org, err := dbsqlc.New(f.db).GetActiveOrganization(t.Context())
	f.must(err)
	if org.MaxTrialsPerPersonPerSeason.Valid {
		t.Fatal("new organization default quota")
	}
	path = fmt.Sprintf("/admin/config/association/%d", org.ID)
	if page := newBrowser(f.app.Handler).call("GET", "/essai", nil); strings.Contains(page.Body.String(), "jusqu’à") {
		t.Fatal("unlimited public policy")
	}
	for _, value := range []string{"1", "3"} {
		save(value, 303)
		org, err = dbsqlc.New(f.db).GetActiveOrganization(t.Context())
		f.must(err)
		if fmt.Sprint(org.MaxTrialsPerPersonPerSeason.Int32) != value || !org.MaxTrialsPerPersonPerSeason.Valid {
			t.Fatal("not persisted")
		}
		text := "jusqu’à " + value + " séance"
		if value != "1" {
			text += "s"
		}
		text += " d’essai par saison"
		if page := newBrowser(f.app.Handler).call("GET", "/essai", nil); page.Code != 200 || !strings.Contains(page.Body.String(), text) {
			t.Fatal("public policy", value)
		}
	}
	before := f.count("SELECT count(*) FROM administrative_events WHERE action='club_configuration_saved'")
	for _, value := range []string{"0", "-1", "1.5", "2147483648"} {
		save(value, 422)
	}
	if f.count("SELECT count(*) FROM administrative_events WHERE action='club_configuration_saved'") != before || before != 3 {
		t.Fatal("configuration audit")
	}
	for _, value := range []int{-1, 0} {
		if _, err = f.db.Exec(t.Context(), "UPDATE organizations SET max_trials_per_person_per_season=$1", value); err == nil {
			t.Fatal("DB check")
		}
	}
	save("", 303)
	org, err = dbsqlc.New(f.db).GetActiveOrganization(t.Context())
	f.must(err)
	if org.MaxTrialsPerPersonPerSeason.Valid {
		t.Fatal("clear quota")
	}
	save("1", 303)
	// Repeated anonymous details still create distinct prospects. There is no proof
	// of identity here and quota enforcement must never merge them automatically.
	f.exec("UPDATE seasons SET starts_at=CURRENT_DATE-1,ends_at=CURRENT_DATE+365 WHERE id=$1", f.season)
	group := f.id("INSERT INTO groups(activity_id,name) VALUES($1,'Découverte') RETURNING id", f.activity)
	f.id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,valid_from) VALUES($1,$2,3,'14:00','16:00',CURRENT_DATE-1) RETURNING id", group, f.season)
	offers, err := trials.NewPublic(f.db, time.UTC).Offerings(t.Context(), time.Now().UTC())
	f.must(err)
	if len(offers) != 1 || len(offers[0].Dates) == 0 {
		t.Fatal("public fixture offerings")
	}
	offer := offers[0]
	people := f.count("SELECT count(*) FROM persons")
	for range 2 {
		public := newBrowser(f.app.Handler)
		bookingPath := fmt.Sprintf("/essai?activity=%d&slot=%d", f.activity, offer.SlotID)
		booking := url.Values{"csrf_token": {public.csrf(t, bookingPath)}, "activity": {fmt.Sprint(f.activity)}, "slot": {fmt.Sprint(offer.SlotID)}, "date": {offer.Dates[0]}, "first_name": {"Alice"}, "last_name": {"Public"}, "birth_date": {"1990-01-01"}, "email": {"alice@example.test"}, "phone": {"0601020304"}, "minor": {"no"}}
		if r := public.call("POST", "/essai", booking); r.Code != 200 || !strings.Contains(r.Body.String(), "Votre demande d’essai est enregistrée") {
			t.Fatalf("public booking %d %s", r.Code, r.Body.String())
		}
	}
	if f.count("SELECT count(*) FROM persons") != people+2 || f.count("SELECT count(DISTINCT person_id) FROM trial_registrations") != 2 {
		t.Fatal("public identity merged")
	}
}

func TestP31QuotaAdministrativeHTTP(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	f.id("INSERT INTO organizations(name,max_trials_per_person_per_season) VALUES('Cercle culturel',3) RETURNING id")
	path := officePerson(f.person) + "/trials/new"
	form := url.Values{"activity_id": {fmt.Sprint(f.activity)}, "trial_date": {"2026-09-16"}}
	officeOK(t, b, path, "0 / 3", "3 séances")
	for range 3 {
		officePost(t, b, path, form, 303)
	}
	officeOK(t, b, officePerson(f.person), "3 / 3", "Quota atteint")
	officeOK(t, b, path, "Quota atteint")
	trial := f.id("SELECT min(id) FROM trial_registrations WHERE person_id=$1", f.person)
	officeOK(t, b, officeTrial(trial), "3 / 3")
	before := f.count("SELECT count(*) FROM administrative_events")
	r := officePost(t, b, path, form, 422)
	if !strings.Contains(r.Body.String(), "nombre maximal") || !strings.Contains(r.Body.String(), "3 sur 3") {
		t.Fatal("quota feedback")
	}
	if f.count("SELECT count(*) FROM administrative_events") != before {
		t.Fatal("rejected creation audited")
	}
	officePost(t, b, officeTrial(trial)+"/status", url.Values{"revision": {"0"}, "status": {"cancelled"}}, 303)
	officeOK(t, b, officePerson(f.person), "2 / 3", "1 séance", "Annulée")
	officePost(t, b, path, form, 303)
	officePost(t, b, officeTrial(trial)+"/status", url.Values{"revision": {"1"}, "status": {"attended"}}, 422)
	f.exec("UPDATE seasons SET starts_at='2027-01-01' WHERE id=$1", f.season)
	officePost(t, b, path, form, 422)
	if strings.Contains(officeOK(t, b, officePerson(f.person)), "<pre>") {
		t.Fatal("SQL error exposed")
	}
	// No quota influences a direct P3 request or its approval.
	m := f.request()
	a, err := f.app.Accounts.ApproveMembership(t.Context(), m.ID, f.approver, nil)
	f.must(err)
	if a.Membership.SourceTrialID.Valid || a.Membership.Status != "active" {
		t.Fatal("direct P3 regression")
	}
}
