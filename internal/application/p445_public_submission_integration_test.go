package application

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/organization"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/grapinou/club-core/internal/websecurity"
)

func TestP445TrialPreparationsAndPlainConfirmation(t *testing.T) {
	f := newFixture(t)
	_, slot := p444Calendar(f)
	loc := p444Paris(t)
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, loc)
	handler := handlers.NewPublicHandler(organization.New(f.db), loc, "", trials.NewPublic(f.db, loc), handlers.NewRegistrationSubmissionLimiter(), f.mail, "club@example.test", handlers.WithPublicClock(func() time.Time { return now }))
	b := newBrowser(websecurity.NewCSRF(false).Protect(handler))
	path := fmt.Sprintf("/essai?activity=%d&slot=%d", f.activity, slot)
	form := url.Values{"csrf_token": {b.csrf(t, path)}, "activity": {fmt.Sprint(f.activity)}, "slot": {fmt.Sprint(slot)}, "date": {"2026-10-02"}, "first_name": {"Alice"}, "last_name": {"Recette"}, "birth_date": {"01/01/2015"}, "equipment_needed": {"no"}, "guardian_first_name": {"Parent"}, "guardian_last_name": {"Recette"}, "guardian_email": {"parent@example.test"}, "guardian_phone": {"0612345678"}, "relationship": {"mother"}}
	beforePeople := f.count("SELECT count(*) FROM persons")
	for _, step := range [][]string{nil, {"unknown"}, {"contacts", "book"}, {"book", "contacts"}} {
		bad := cloneForm(form)
		bad["step"] = step
		r := b.call("POST", "/essai?step=book", bad)
		if r.Code != 400 || f.count("SELECT count(*) FROM persons") != beforePeople {
			t.Fatal("ambiguous intention reached booking", step, r.Code)
		}
	}
	for i := 0; i < 5; i++ {
		form.Set("first_name", fmt.Sprintf("Visiteur%d", i))
		form.Set("step", "contacts")
		before := f.count("SELECT count(*) FROM persons")
		for range 3 {
			response := b.call("POST", "/essai?step=book", form)
			if response.Code != 200 || f.count("SELECT count(*) FROM persons") != before {
				t.Fatal("preparation consumed budget or wrote", i, response.Code)
			}
			body := response.Body.String()
			if !strings.Contains(body, `<h3 id="coordonnees" class="h5">Le parent ou responsable à contacter</h3>`) || strings.Contains(body, `tabindex="-1"`) && strings.Contains(body, `<div id="coordonnees"`) {
				t.Fatal("coordinates anchor is not the heading")
			}
		}
		form.Set("step", "book")
		response := b.call("POST", "/essai?step=contacts", form)
		if response.Code != 200 || !strings.Contains(response.Body.String(), "Votre demande d’essai est enregistrée") {
			t.Fatal("complete trial", i, response.Code, response.Body.String())
		}
		summary := pagePart(t, response.Body.String(), `<section class="section-panel booking-confirmation"`, `</section>`)
		for _, value := range []string{"Parent Recette", "0612345678", "parent@example.test"} {
			if !strings.Contains(summary, value) {
				t.Fatal("missing coordinates", value)
			}
		}
		if strings.Contains(summary, "tel:") || strings.Contains(summary, "mailto:") {
			t.Fatal("personal confirmation links")
		}
	}
	before := f.count("SELECT count(*) FROM persons")
	if r := b.call("POST", "/essai", form); r.Code != 429 || r.Header().Get("Retry-After") != "900" || f.count("SELECT count(*) FROM persons") != before {
		t.Fatal("sixth trial", r.Code)
	}
	if f.count("SELECT count(*) FROM trial_registrations") != 5 || len(f.mail.messages) != 5 {
		t.Fatal("durable trial count")
	}
	form.Set("step", "contacts")
	if r := b.call("POST", "/essai", form); r.Code != 200 {
		t.Fatal("preparation after quota", r.Code)
	}
	if b.call("GET", path, nil).Code != 200 {
		t.Fatal("GET after quota")
	}
}

func TestP445PublicMembershipPreparationsDoNotSpendBudget(t *testing.T) {
	for _, path := range []string{"/join", "/join/child"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t)
			b := newBrowser(f.app.Handler)
			makeForm := func() url.Values {
				if path == "/join/child" {
					return f.childForm(b)
				}
				return f.joinForm(b)
			}
			form := makeForm()
			before := f.count("SELECT count(*) FROM persons")
			for i := 0; i < 5; i++ {
				for _, action := range []string{"review", "edit"} {
					form.Set("action", action)
					response := b.call("POST", path+"?action=submit", form)
					if response.Code != 200 || f.count("SELECT count(*) FROM registration_submissions") != 0 || f.count("SELECT count(*) FROM persons") != before {
						t.Fatal("preparation", action, i, response.Code, response.Body.String())
					}
				}
			}
			// Unknown or duplicate actions cannot use preparation as a write bypass.
			for _, actions := range [][]string{{"review", "submit"}, {"edit", "submit"}, {"unknown"}, nil} {
				bad := cloneForm(form)
				bad["action"] = actions
				if r := b.call("POST", path, bad); r.Code != 422 || f.count("SELECT count(*) FROM registration_submissions") != 0 {
					t.Fatal("action bypass", actions, r.Code)
				}
			}
			for i := 0; i < 5; i++ {
				form = makeForm()
				form.Set("first_name", fmt.Sprintf("Visiteur%d", i))
				form.Set("action", "submit")
				response := b.call("POST", path+"?action=review", form)
				if response.Code != 303 {
					t.Fatal("submit", i, response.Code, response.Body.String())
				}
			}
			count := f.count("SELECT count(*) FROM registration_submissions")
			if r := b.call("POST", path, form); r.Code != 429 || r.Header().Get("Retry-After") != "900" || r.Header().Get("Cache-Control") != "no-store" || f.count("SELECT count(*) FROM registration_submissions") != count {
				t.Fatal("sixth submit", r.Code)
			}
			form = makeForm()
			form.Set("action", "review")
			if r := b.call("POST", path, form); r.Code != 200 {
				t.Fatal("review after quota", r.Code)
			}
		})
	}
}
