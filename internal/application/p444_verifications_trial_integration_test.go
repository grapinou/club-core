package application

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/organization"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/grapinou/club-core/internal/verifications"
	"github.com/grapinou/club-core/internal/websecurity"
)

func TestP444VerificationCounter(t *testing.T) {
	f := newFixture(t)
	office := p43Secretary(f)
	user, member := f.personalBrowser(f.person, "member")
	counter := verifications.New(f.app.Reviews, dbsqlc.New(f.db))
	check := func(want int) {
		t.Helper()
		body := officeOK(t, office, "/registration-reviews")
		nav := pagePart(t, body, `<header class="site-header">`, `</header>`)
		if !strings.Contains(nav, fmt.Sprintf("Inscriptions à vérifier (%d)", want)) {
			t.Fatal("verification count", want, nav)
		}
	}
	// Resolved dossiers and live email verification are not interventions.
	f.exec("INSERT INTO registration_submissions(first_name,last_name,status,resolved_person_id,resolution_type,resolved_at) VALUES('Terminé','Dossier','resolved',?1,'existing_person',CURRENT_TIMESTAMP)", f.person)
	email := f.id("INSERT INTO registration_submissions(first_name,last_name,status) VALUES('Email','EnCours','awaiting_email_verification') RETURNING id")
	f.exec("INSERT INTO registration_submission_candidates(submission_id,person_id,confidence,matched_name,matched_birth_date,matched_email,matched_phone) VALUES(?1,?2,'strong',1,1,1,0)", email, f.person)
	f.exec("INSERT INTO registration_email_verifications(submission_id,person_id,public_reference,code_hash,recipient_hash,expires_at) VALUES(?1,?2,?3,zeroblob(32),zeroblob(32),datetime('now','+1 hour'))", email, f.person, strings.Repeat("a", 64))
	check(0)
	f.accountPost(member, "/me/account/identity", identityProposal(), 303)
	check(1)
	id := f.correctionID()
	f.must(f.app.IdentityCorrections.Review(f.authenticatedContext(f.approver), id, "approved"))
	check(0)
	f.exec("INSERT INTO registration_submissions(first_name,last_name,status) VALUES('Intervention','Club','awaiting_identity_review')")
	check(1)
	proposal := identityProposal()
	proposal.Set("first_name", "Deuxième")
	f.accountPost(member, "/me/account/identity", proposal, 303)
	check(2)
	dashboard := officeOK(t, office, "/admin")
	main := pagePart(t, dashboard, `<main id="main-content"`, `</main>`)
	if !strings.Contains(main, "1 demande(s) d’inscription à vérifier") || !strings.Contains(main, "1 correction d’identité à vérifier") {
		t.Fatal("dashboard mixed categories")
	}
	if _, err := counter.CountOpen(f.authenticatedContext(user), user); err == nil {
		t.Fatal("unauthorized count")
	}
	memberPage := f.personalOK(member, "/dashboard")
	if strings.Contains(memberPage, "Inscriptions à vérifier") || member.call("GET", "/identity-corrections", nil).Code != 403 {
		t.Fatal("verification permission")
	}
	id = f.correctionID()
	f.must(f.app.IdentityCorrections.Review(f.authenticatedContext(f.approver), id, "rejected"))
	check(1)
}

func p444Paris(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
func p444Calendar(f *fixture) (int32, int32) {
	f.exec("INSERT INTO organizations(name,trial_equipment_offer,trial_equipment_detail_prompt) VALUES('Club recette','Prêt de matériel','Précisez votre taille.')")
	group, slot := f.officeGroup()
	f.exec("UPDATE group_slots SET weekday=5,start_time='19:00:00',end_time='20:00:00' WHERE id=?1", slot)
	return group, slot
}

func TestP444SameDayPublicCalendarAndBooking(t *testing.T) {
	f := newFixture(t)
	group, future := p444Calendar(f)
	loc := p444Paris(t)
	past := f.id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from) VALUES(?1,?2,5,'10:00:00','11:00:00','Dojo','2026-09-01') RETURNING id", group, f.season)
	tomorrow := f.id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from) VALUES(?1,?2,6,'10:00:00','11:00:00','Dojo','2026-09-01') RETURNING id", group, f.season)
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, loc)
	svc := trials.NewPublic(f.db, loc)
	offers, err := svc.Offerings(t.Context(), now)
	f.must(err)
	dates := map[int32][]string{}
	chosen := trials.PublicOffering{}
	for _, o := range offers {
		dates[o.SlotID] = o.Dates
		if o.SlotID == future {
			chosen = o
		}
	}
	contains := func(slot int32, date string) bool {
		for _, d := range dates[slot] {
			if d == date {
				return true
			}
		}
		return false
	}
	if !contains(future, "2026-10-02") || contains(past, "2026-10-02") || !contains(tomorrow, "2026-10-03") || !contains(future, "2026-10-23") || contains(tomorrow, "2026-10-24") {
		t.Fatal("civil public calendar", dates)
	}
	for _, o := range offers {
		for _, d := range o.Dates {
			if d < "2026-10-02" || d > "2026-10-23" {
				t.Fatal("window", d)
			}
		}
	}
	booking := trials.PublicBooking{Offering: chosen, Date: "2026-10-02", FirstName: "Aujourd’hui", LastName: "Visiteur", BirthDate: "1990-01-01", Email: "visiteur@example.test", Phone: "0612345678"}
	before := f.count("SELECT count(*) FROM persons")
	stale := booking
	stale.Offering.SlotID = past
	if _, err = svc.Book(t.Context(), stale, now); !errors.Is(err, trials.ErrInvalidPublicBooking) || f.count("SELECT count(*) FROM persons") != before {
		t.Fatal("past hour booked", err)
	}
	if _, err = svc.Book(t.Context(), booking, now.Add(6*time.Hour)); !errors.Is(err, trials.ErrInvalidPublicBooking) {
		t.Fatal("stale future selection accepted", err)
	}
	if _, err = svc.Book(t.Context(), booking, now); err != nil {
		t.Fatal("same day future rejected", err)
	}
	booking.Date = "2026-10-23"
	if _, err = svc.Book(t.Context(), booking, now); err != nil {
		t.Fatal("J+21 rejected", err)
	}
	booking.Date = "2026-10-24"
	booking.Offering.SlotID = tomorrow
	if _, err = svc.Book(t.Context(), booking, now); !errors.Is(err, trials.ErrInvalidPublicBooking) {
		t.Fatal("J+22 booked", err)
	}
	if f.count("SELECT count(*) FROM trial_registrations") != 2 {
		t.Fatal("invalid calendar writes")
	}
}

func TestP444ProgressiveTrialForm(t *testing.T) {
	f := newFixture(t)
	_, slot := p444Calendar(f)
	loc := p444Paris(t)
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, loc)
	handler := handlers.NewPublicHandler(organization.New(f.db), loc, "", trials.NewPublic(f.db, loc), nil, f.mail, "club@example.test", handlers.WithPublicClock(func() time.Time { return now }))
	b := newBrowser(websecurity.NewCSRF(false).Protect(handler))
	path := fmt.Sprintf("/essai?activity=%d&slot=%d", f.activity, slot)
	response := b.call("GET", path, nil)
	body := response.Body.String()
	for _, want := range []string{`id="equipment_details"`, `formaction="/essai#coordonnees"`, "dès aujourd’hui"} {
		if !strings.Contains(body, want) {
			t.Fatal("initial form", want)
		}
	}
	form := url.Values{"csrf_token": {hiddenValue(t, body, "csrf_token")}, "activity": {fmt.Sprint(f.activity)}, "slot": {fmt.Sprint(slot)}, "date": {"2026-10-02"}, "first_name": {"Alice"}, "last_name": {"Recette"}, "birth_date": {"01/01/1990"}, "equipment_needed": {"yes"}, "equipment_details": {"1,72 m"}, "step": {"contacts"}}
	continued := b.call("POST", "/essai", form)
	if continued.Code != 200 || continued.Header().Get("Cache-Control") != "no-store" || f.count("SELECT count(*) FROM trial_registrations") != 0 {
		t.Fatal("continue state", continued.Code)
	}
	body = continued.Body.String()
	for _, want := range []string{`id="coordonnees"`, `value="Alice"`, `value="Recette"`, `value="01/01/1990"`, `value="1,72 m"`, `value="2026-10-02" selected`, `name="email"`, `name="phone"`} {
		if !strings.Contains(body, want) {
			t.Fatal("continue preservation", want)
		}
	}
	if strings.Contains(body, "Actualiser") || strings.Count(body, "Enregistrer mon essai") != 1 || strings.Contains(body, "Continuer vers les coordonnées") {
		t.Fatal("final actions")
	}
	// Conditional obligation is enforced server-side even for an intermediate POST.
	missing := maps.Clone(form)
	missing.Del("equipment_details")
	if r := b.call("POST", "/essai", missing); r.Code != 422 || !strings.Contains(r.Body.String(), "Ajoutez la précision") {
		t.Fatal("missing equipment allowed")
	}
	form.Set("step", "book")
	form.Set("email", "alice@example.test")
	form.Set("phone", "0612345678")
	// Changing birth requires guardian coordinates, ignoring the obsolete adult path.
	form.Set("birth_date", "01/01/2015")
	switched := b.call("POST", "/essai", form)
	if switched.Code != 422 || !strings.Contains(switched.Body.String(), `name="guardian_email"`) || f.count("SELECT count(*) FROM persons WHERE first_name='Alice'") != 0 {
		t.Fatal("minor switch saved wrong contacts")
	}
	form.Set("guardian_first_name", "Parent")
	form.Set("guardian_last_name", "Recette")
	form.Set("guardian_email", "parent@example.test")
	form.Set("guardian_phone", "0611223344")
	form.Set("relationship", "mother")
	invalid := maps.Clone(form)
	invalid.Set("guardian_email", "wrong")
	if r := b.call("POST", "/essai", invalid); r.Code != 422 || !strings.Contains(r.Body.String(), `value="1,72 m"`) || !strings.Contains(r.Body.String(), `value="wrong"`) {
		t.Fatal("validation lost values")
	}
	result := b.call("POST", "/essai", form)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "Votre demande d’essai est enregistrée") || f.count("SELECT count(*) FROM persons WHERE first_name='Alice' AND email IS NULL AND phone_number IS NULL") != 1 || f.count("SELECT count(*) FROM person_guardians") != 1 {
		t.Fatal("minor booking", result.Code, result.Body.String())
	}
	if len(f.mail.messages) != 1 || f.mail.messages[0].To != "parent@example.test" {
		t.Fatal("minor confirmation recipient")
	}
	// Reverse the path: guardian values must not substitute adult coordinates.
	form.Set("first_name", "Bruno")
	form.Set("birth_date", "01/01/1990")
	form.Del("email")
	form.Del("phone")
	form.Set("equipment_needed", "no")
	form.Set("equipment_details", strings.Repeat("parasite", 100))
	switched = b.call("POST", "/essai", form)
	if switched.Code != 422 || !strings.Contains(switched.Body.String(), `name="email"`) || strings.Contains(switched.Body.String(), "parasite") {
		t.Fatal("adult switch or equipment cleanup")
	}
	form.Set("email", "bruno@example.test")
	form.Set("phone", "0612345678")
	result = b.call("POST", "/essai", form)
	if result.Code != 200 || f.count("SELECT count(*) FROM trial_registrations WHERE notes IS NULL") != 1 || f.count("SELECT count(*) FROM person_guardians") != 1 {
		t.Fatal("no equipment or wrong guardian saved", result.Code, result.Body.String())
	}
	if len(f.mail.messages) != 2 || f.mail.messages[1].To != "bruno@example.test" {
		t.Fatal("adult confirmation recipient")
	}
}

func TestP444EquipmentDetailRequiredOnlyWithPrompt(t *testing.T) {
	f := newFixture(t)
	_, slot := p444Calendar(f)
	loc := p444Paris(t)
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, loc)
	service := trials.NewPublic(f.db, loc)
	offerings, err := service.Offerings(t.Context(), now)
	f.must(err)
	var offer trials.PublicOffering
	for _, o := range offerings {
		if o.SlotID == slot {
			offer = o
		}
	}
	booking := trials.PublicBooking{Offering: offer, Date: "2026-10-02", FirstName: "Matériel", LastName: "Visiteur", BirthDate: "1990-01-01", Email: "visiteur@example.test", Phone: "0612345678", EquipmentNeeded: true}
	before := f.count("SELECT count(*) FROM persons")
	if _, err := service.Book(t.Context(), booking, now); !errors.Is(err, trials.ErrInvalidPublicBooking) || f.count("SELECT count(*) FROM persons") != before {
		t.Fatal("required detail omitted", err)
	}
	for _, prompt := range []any{"", nil} {
		f.exec("UPDATE organizations SET trial_equipment_detail_prompt=?1", prompt)
		if _, err := service.Book(t.Context(), booking, now); err != nil {
			t.Fatal("optional detail rejected", err)
		}
	}
}
