package application

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/demodata"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/trials"
)

func TestPublicTrialBooking(t *testing.T) {
	db := newApplicationDatabase(t, "public_trial_demo")
	ctx := t.Context()
	if err := demodata.SeedBudokan(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE seasons SET starts_at=CURRENT_DATE-1,ends_at=CURRENT_DATE+365")
	exec("UPDATE group_slots SET valid_from=CURRENT_DATE-1,valid_until=CURRENT_DATE+365")
	now := time.Now().UTC()
	service := trials.NewPublic(db, time.UTC)
	offers, err := service.Offerings(ctx, now)
	if err != nil || len(offers) == 0 {
		t.Fatalf("offerings: %v, %d", err, len(offers))
	}
	var mondayFound bool
	for _, o := range offers {
		if o.Weekday == 1 && o.Start == "20:15" {
			mondayFound = true
			if o.Group != "JJB Adolescents et Adultes" || o.Practice != "Préparation physique / Jiu-Jitsu Brésilien" {
				t.Fatal("Monday session attached to wrong group", o)
			}
		}
		if o.Group == "JJB pratiques spécifiques" {
			t.Fatal("technical group offered")
		}
	}
	if !mondayFound {
		t.Fatal("Monday session not offered")
	}
	var chosen trials.PublicOffering
	for _, o := range offers {
		if strings.Contains(o.Group, "Adultes") {
			chosen = o
			break
		}
	}
	if chosen.SlotID == 0 {
		t.Fatal("adult offering absent")
	}
	birth := now.AddDate(-30, 0, 0).Format("2006-01-02")
	adult := trials.PublicBooking{Offering: chosen, Date: chosen.Dates[0], FirstName: "Alice", LastName: "Public", BirthDate: birth, Email: "alice@example.test", Phone: "0601020304"}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	beforeP, beforeT := count("persons"), count("trial_registrations")
	confirm, err := service.Book(ctx, adult, now)
	if err != nil {
		t.Fatal(err)
	}
	if confirm.Offering.Start == "" || confirm.Offering.Location == "" || count("persons") != beforeP+1 || count("trial_registrations") != beforeT+1 {
		t.Fatal("adult booking incomplete")
	}
	var status string
	var personID, groupID, slotID int32
	if err := db.QueryRow(ctx, "SELECT status,person_id,group_id,group_slot_id FROM trial_registrations WHERE id=$1", confirm.TrialID).Scan(&status, &personID, &groupID, &slotID); err != nil {
		t.Fatal(err)
	}
	if status != "registered" || groupID != chosen.GroupID || slotID != chosen.SlotID {
		t.Fatal("trial values")
	}
	rows, err := dbsqlc.New(db).AdministrativeTrials(ctx, dbsqlc.AdministrativeTrialsParams{})
	if err != nil || len(rows) != 1 || rows[0].FirstName != "Alice" {
		t.Fatalf("administration: %v %#v", err, rows)
	}
	child := adult
	child.FirstName = "Noé"
	child.BirthDate = now.AddDate(-10, 0, 0).Format("2006-01-02")
	child.Email = ""
	child.Phone = ""
	child.Minor = true
	child.GuardianFirstName = "Camille"
	child.GuardianLastName = "Public"
	child.GuardianEmail = "parent@example.test"
	child.GuardianPhone = "0605060708"
	child.Relationship = "mother"
	child.EquipmentNeeded = true
	child.EquipmentDetails = "1,42 m"
	minorConfirm, err := service.Book(ctx, child, now)
	if err != nil {
		t.Fatal(err)
	}
	if count("persons") != beforeP+3 || count("person_guardians") != 1 || count("trial_registrations") != beforeT+2 {
		t.Fatal("minor booking incomplete")
	}
	var notes string
	if err := db.QueryRow(ctx, "SELECT notes FROM trial_registrations WHERE id=$1", minorConfirm.TrialID).Scan(&notes); err != nil || !strings.Contains(notes, "1,42 m") {
		t.Fatalf("equipment note: %q %v", notes, err)
	}
	var guardianEmail string
	if err := db.QueryRow(ctx, `SELECT p.email FROM trial_registrations t JOIN person_guardians g ON g.child_person_id=t.person_id JOIN persons p ON p.id=g.guardian_person_id WHERE t.id=$1`, minorConfirm.TrialID).Scan(&guardianEmail); err != nil || guardianEmail != "parent@example.test" {
		t.Fatalf("guardian: %v %s", err, guardianEmail)
	}
	invalidChild := child
	invalidChild.GuardianEmail = "invalid"
	if _, err := service.Book(ctx, invalidChild, now); err == nil || count("persons") != beforeP+3 || count("person_guardians") != 1 {
		t.Fatal("invalid guardian created partial data")
	}
	cases := []struct {
		name   string
		change func(*trials.PublicBooking)
		sql    string
	}{
		{"bad activity", func(b *trials.PublicBooking) { b.Offering.ActivityID = 999999 }, ""},
		{"bad group", func(b *trials.PublicBooking) { b.Offering.GroupID = 999999 }, ""},
		{"bad slot", func(b *trials.PublicBooking) { b.Offering.SlotID = 999999 }, ""},
		{"wrong weekday", func(b *trials.PublicBooking) {
			d, _ := time.Parse("2006-01-02", b.Date)
			b.Date = d.AddDate(0, 0, 1).Format("2006-01-02")
		}, ""},
		{"invalid form", func(b *trials.PublicBooking) { b.Email = "bad" }, ""},
		{"inactive slot", nil, "UPDATE group_slots SET is_active=false WHERE id=$1"},
		{"inactive group", nil, "UPDATE groups SET is_active=false WHERE id=$1"},
		{"inactive activity", nil, "UPDATE activities SET is_active=false WHERE id=$1"},
		{"inactive season", nil, "UPDATE seasons SET is_active=false"},
		{"outside period", nil, "UPDATE group_slots SET valid_until=CURRENT_DATE WHERE id=$1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := adult
			if tc.change != nil {
				tc.change(&b)
			}
			if tc.sql != "" {
				if strings.Contains(tc.sql, "seasons") {
					exec(tc.sql)
				} else if strings.Contains(tc.sql, "groups") {
					exec(tc.sql, chosen.GroupID)
				} else if strings.Contains(tc.sql, "activities") {
					exec(tc.sql, chosen.ActivityID)
				} else {
					exec(tc.sql, chosen.SlotID)
				}
			}
			p0, t0, g0 := count("persons"), count("trial_registrations"), count("person_guardians")
			if _, err := service.Book(ctx, b, now); err == nil {
				t.Fatal("booking accepted")
			}
			if count("persons") != p0 || count("trial_registrations") != t0 || count("person_guardians") != g0 {
				t.Fatal("partial creation")
			}
			exec("UPDATE group_slots SET is_active=true,valid_until=CURRENT_DATE+365 WHERE id=$1", chosen.SlotID)
			exec("UPDATE groups SET is_active=true WHERE id=$1", chosen.GroupID)
			exec("UPDATE activities SET is_active=true WHERE id=$1", chosen.ActivityID)
			exec("UPDATE seasons SET is_active=true")
		})
	}
	mail := &fakeMailer{}
	app, err := NewWithMailer(config.Config{SiteName: "Club Core"}, config.Runtime{Location: time.UTC, ActivationValidity: time.Hour, RegistrationVerificationTTL: time.Hour, SMTP: mailer.SMTPConfig{From: "club@example.test"}}, db, mail)
	if err != nil {
		t.Fatal(err)
	}
	activityPage := httptest.NewRecorder()
	app.Handler.ServeHTTP(activityPage, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/essai?activity=%d", chosen.ActivityID), nil))
	if activityPage.Code != 200 || !strings.Contains(activityPage.Body.String(), `action="/essai#choix-seance"`) || !strings.Contains(activityPage.Body.String(), `id="choix-seance"`) || !strings.Contains(activityPage.Body.String(), `action="/essai#choix-date"`) {
		t.Fatal("activity selection does not lead to the session step")
	}
	get := httptest.NewRecorder()
	app.Handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/essai?activity=%d&slot=%d", chosen.ActivityID, chosen.SlotID), nil))
	if get.Code != 200 || !strings.Contains(get.Body.String(), "Enregistrer mon essai") || !strings.Contains(get.Body.String(), `id="choix-date"`) {
		t.Fatalf("form: %d %s", get.Code, get.Body.String())
	}
	values := url.Values{"activity": {fmt.Sprint(chosen.ActivityID)}, "slot": {fmt.Sprint(chosen.SlotID)}, "date": {chosen.Dates[0]}, "first_name": {"Bruno"}, "last_name": {"Visiteur"}, "birth_date": {birth}, "email": {"bruno@example.test"}, "phone": {"0612345678"}, "minor": {"no"}, "equipment_needed": {"no"}}
	var cookie *http.Cookie
	for _, c := range get.Result().Cookies() {
		if c.Name == "club_csrf" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("csrf cookie absent")
	}
	values.Set("csrf_token", cookie.Value)
	req := httptest.NewRequest(http.MethodPost, "/essai", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	post := httptest.NewRecorder()
	app.Handler.ServeHTTP(post, req)
	if post.Code != 200 || !strings.Contains(post.Body.String(), "Votre demande d’essai est enregistrée") || !strings.Contains(post.Body.String(), chosen.Location) {
		t.Fatalf("confirmation: %d %s", post.Code, post.Body.String())
	}
	if count("trial_registrations") != beforeT+3 {
		t.Fatal("HTTP booking not saved")
	}
	if len(mail.messages) != 1 || mail.messages[0].To != "bruno@example.test" || !strings.Contains(mail.messages[0].Text, chosen.Location) {
		t.Fatal("adult confirmation email")
	}
	for _, want := range []string{chosen.Activity, chosen.Dates[0], chosen.Start, chosen.End, chosen.Location, "À apporter", "Confirmation de votre essai"} {
		if !strings.Contains(mail.messages[0].Text+mail.messages[0].Subject, want) {
			t.Fatalf("adult email missing %q", want)
		}
	}
	var adultNotes *string
	if err := db.QueryRow(ctx, "SELECT notes FROM trial_registrations WHERE person_id=(SELECT id FROM persons WHERE email='bruno@example.test')").Scan(&adultNotes); err != nil || adultNotes != nil {
		t.Fatalf("no equipment should leave notes empty: %v %v", adultNotes, err)
	}
	mail.err = errors.New("relay unavailable")
	minorValues := url.Values{"activity": {fmt.Sprint(chosen.ActivityID)}, "slot": {fmt.Sprint(chosen.SlotID)}, "date": {chosen.Dates[0]}, "first_name": {"Lina"}, "last_name": {"Visiteur"}, "birth_date": {now.AddDate(-9, 0, 0).Format("2006-01-02")}, "minor": {"yes"}, "guardian_first_name": {"Nora"}, "guardian_last_name": {"Visiteur"}, "guardian_email": {"nora@example.test"}, "guardian_phone": {"0600000000"}, "relationship": {"mother"}, "equipment_needed": {"yes"}, "equipment_details": {"1,40 m"}, "csrf_token": {cookie.Value}}
	minorRequest := httptest.NewRequest(http.MethodPost, "/essai", strings.NewReader(minorValues.Encode()))
	minorRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	minorRequest.AddCookie(cookie)
	minorPost := httptest.NewRecorder()
	app.Handler.ServeHTTP(minorPost, minorRequest)
	if minorPost.Code != 200 || !strings.Contains(minorPost.Body.String(), "Nora Visiteur") || !strings.Contains(minorPost.Body.String(), "nora@example.test") || !strings.Contains(minorPost.Body.String(), "1,40 m") {
		t.Fatalf("minor confirmation: %d %s", minorPost.Code, minorPost.Body.String())
	}
	if count("trial_registrations") != beforeT+4 || len(mail.messages) != 2 || mail.messages[1].To != "nora@example.test" {
		t.Fatal("email failure affected booking")
	}
	for _, want := range []string{chosen.Activity, chosen.Dates[0], chosen.Start, chosen.End, chosen.Location, "Matériel demandé : oui", "1,40 m"} {
		if !strings.Contains(mail.messages[1].Text, want) {
			t.Fatalf("minor email missing %q", want)
		}
	}
}

func TestPublicTrialVisibleInOffice(t *testing.T) {
	f := newFixture(t)
	f.exec("INSERT INTO organizations(name) VALUES('Club de test')")
	group, slot := f.officeGroup()
	service := trials.NewPublic(f.db, time.UTC)
	now := time.Now().UTC()
	offers, err := service.Offerings(t.Context(), now)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offerings %v %d", err, len(offers))
	}
	booking := trials.PublicBooking{Offering: offers[0], Date: offers[0].Dates[0], FirstName: "Visiteur", LastName: "Essai", BirthDate: now.AddDate(-25, 0, 0).Format("2006-01-02"), Email: "visiteur@example.test", Phone: "0611223344", EquipmentNeeded: true, EquipmentDetails: "taille M"}
	confirmation, err := service.Book(t.Context(), booking, now)
	if err != nil {
		t.Fatal(err)
	}
	if confirmation.Offering.GroupID != group || confirmation.Offering.SlotID != slot {
		t.Fatal("wrong office target")
	}
	var personID int32
	if err := f.db.QueryRow(t.Context(), "SELECT person_id FROM trial_registrations WHERE id=$1", confirmation.TrialID).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	b := f.membershipAdminBrowser()
	officeOK(t, b, "/trials?date="+booking.Date, "Visiteur Essai", "Groupe adultes", "18:30", "Dojo municipal", "Programmé")
	officeOK(t, b, fmt.Sprintf("/trials/%d", confirmation.TrialID), "Visiteur Essai", "Dojo municipal", "Programmé", "taille M")
	officeOK(t, b, fmt.Sprintf("/persons/%d", personID), "visiteur@example.test", "0611223344")
}
