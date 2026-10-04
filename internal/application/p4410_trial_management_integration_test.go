package application

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

func managementTrialIDs(t *testing.T, body string) []int32 {
	t.Helper()
	main := pagePart(t, body, `<main id="main-content"`, `</main>`)
	var ids []int32
	for _, match := range regexp.MustCompile(`<h3><a href="/trials/(\d+)">`).FindAllStringSubmatch(main, -1) {
		var id int32
		if _, err := fmt.Sscan(match[1], &id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestP4410MainTrialWeekAndPolicy(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	f.exec("INSERT INTO organizations(name) VALUES('Club')")
	id := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-10-03','registered') RETURNING id", f.person, f.activity)
	for _, tt := range []struct {
		limit any
		text  string
	}{
		{3, "Chaque personne peut effectuer jusqu’à 3 essais par saison."},
		{1, "Chaque personne peut effectuer jusqu’à 1 essai par saison."},
		{nil, "Aucune limite d’essais n’est configurée."},
	} {
		f.exec("UPDATE organizations SET max_trials_per_person_per_season=?1", tt.limit)
		body := officeOK(t, b, "/trials", "Programmer un essai", tt.text, "Gestion des essais", "Semaine courante / aujourd’hui", "Lundi", "Dimanche")
		header := pagePart(t, body, `<header class="page-header">`, `</header>`)
		if !strings.Contains(header, tt.text) || strings.Contains(header, "<details") || strings.Index(header, "Programmer un essai") > strings.Index(header, tt.text) {
			t.Fatal("policy must be directly below scheduling action")
		}
		for _, absent := range []string{"Rechercher une date et consulter la politique des essais", "Passés sans résultat", "Tous les essais", "pending=1", `name="date"`, `name="search"`} {
			if strings.Contains(body, absent) {
				t.Fatal("obsolete or management-only interface", absent)
			}
		}
	}
	body := officeOK(t, b, "/trials?week=2026-09-30", `href="/trials?week=2026-09-21"`, `href="/trials?week=2026-10-05"`, "28/09/2026 – 04/10/2026")
	week := pagePart(t, body, `<div class="trial-week">`, `</main>`)
	if strings.Count(week, `<section class="section-panel">`) != 7 || !strings.Contains(week, `href="`+officeTrial(id)+`">Rémi</a>`) {
		t.Fatal("week days or detail navigation changed")
	}
	officeOK(t, b, "/trials?week=2026-09-21", `href="/trials?week=2026-09-28"`)
	officeOK(t, b, "/trials?week=2026-10-05", `href="/trials?week=2026-09-28"`)
}

func TestP4410TrialManagementFiltersAndOrder(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	group, slot := f.officeGroup()
	elodie := f.id("INSERT INTO persons(first_name,last_name) VALUES('Élodie','Ancienne') RETURNING id")
	create := func(person int32, date, status string) int32 {
		return f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) VALUES(?1,?2,?3,?4,?5,?6) RETURNING id", person, f.activity, group, slot, date, status)
	}
	// IDs deliberately disagree with chronology, including a future result.
	newest := create(f.person, "2027-03-03", "registered")
	oldest := create(elodie, "2026-09-03", "cancelled")
	current := create(elodie, "2026-10-03", "attended")
	absent := create(f.person, "2026-09-10", "no_show")
	for _, tt := range []struct {
		name, date, search string
		want               []int32
	}{
		{"all", "", "", []int32{newest, current, absent, oldest}},
		{"date", "2026-10-03", "", []int32{current}},
		{"first name unicode and case", "", "  ÉLODIE  ", []int32{current, oldest}},
		{"last name", "", "ancienne", []int32{current, oldest}},
		{"full name", "", "élodie ancienne", []int32{current, oldest}},
		{"combined", "2026-09-03", "ancienne", []int32{oldest}},
		{"no match", "2026-10-03", "Dupont", nil},
		{"unknown person", "", "Introuvable", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := "/trials?" + url.Values{"all": {"1"}, "date": {tt.date}, "search": {tt.search}}.Encode()
			body := officeOK(t, b, path, "Gestion des essais", `name="all" value="1"`, "Prénom ou nom", "Rechercher", "Réinitialiser les filtres")
			if got := managementTrialIDs(t, body); !slices.Equal(got, tt.want) {
				t.Fatalf("results %v want %v", got, tt.want)
			}
			if len(tt.want) == 0 {
				if !strings.Contains(body, "Aucun essai à afficher.") {
					t.Fatal("empty state missing")
				}
			} else {
				for _, text := range []string{"Practice", "Groupe adultes", `value="` + tt.date + `"`, `value="` + strings.TrimSpace(tt.search) + `"`} {
					if !strings.Contains(body, text) {
						t.Fatal("result or retained filter missing", text)
					}
				}
			}
			if strings.Contains(body, `class="trial-week"`) {
				t.Fatal("management must be separate from week")
			}
		})
	}
	body := officeOK(t, b, "/trials?all=1", "Programmé", "Présent", "Absent", "Annulée", "03/03/2027", "03/10/2026", "Rémi Dupont", "Élodie Ancienne")
	if strings.Contains(body, "Page suivante") {
		t.Fatal("unexpected pagination")
	}
	// Existing direct date links lead to the same visible management form.
	officeOK(t, b, "/trials?date=2026-10-03", "Gestion des essais", "Élodie Ancienne")
	for _, query := range []string{"date=invalid", "page=-1", "page=10001", "page=bad", "search=" + strings.Repeat("x", 255)} {
		if b.call("GET", "/trials?all=1&"+query, nil).Code != 422 {
			t.Fatal("invalid management filter", query)
		}
	}
}

func TestP4410TrialManagementPaginationAndPermissions(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	person := f.id("INSERT INTO persons(first_name,last_name) VALUES('Pagination','A & B') RETURNING id")
	var ids []int32
	for i := 0; i < 103; i++ {
		ids = append(ids, f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-10-03','attended') RETURNING id", person, f.activity))
	}
	for _, filters := range []url.Values{
		{"all": {"1"}},
		{"all": {"1"}, "date": {"2026-10-03"}, "search": {"A & B"}},
	} {
		path := "/trials?" + filters.Encode()
		body := officeOK(t, b, path, "Page suivante")
		if got := managementTrialIDs(t, body); !slices.Equal(got, ids[:100]) {
			t.Fatal("first page incomplete or unstable")
		}
		match := regexp.MustCompile(`href="([^"]+)">Page suivante`).FindStringSubmatch(body)
		if len(match) != 2 {
			t.Fatal("next link missing")
		}
		next := html.UnescapeString(match[1])
		u, err := url.Parse(next)
		f.must(err)
		for key := range filters {
			if u.Query().Get(key) != filters.Get(key) {
				t.Fatal("pagination lost filter", key)
			}
		}
		body = officeOK(t, b, next, "Page précédente")
		if got := managementTrialIDs(t, body); !slices.Equal(got, ids[100:]) || strings.Contains(body, "Page suivante") {
			t.Fatal("second page incomplete or duplicated")
		}
		previous := regexp.MustCompile(`href="([^"]+)">Page précédente`).FindStringSubmatch(body)
		body = officeOK(t, b, html.UnescapeString(previous[1]))
		if got := managementTrialIDs(t, body); !slices.Equal(got, ids[:100]) {
			t.Fatal("previous page navigation")
		}
	}
	if newBrowser(f.app.Handler).call("GET", "/trials?all=1", nil).Code != 303 {
		t.Fatal("anonymous management access")
	}
	_, member := f.personalBrowser(f.person, "management.member")
	if member.call("GET", "/trials?all=1&search=Pagination", nil).Code != 403 {
		t.Fatal("member management access")
	}
	ctx := f.authenticatedContext(f.id("SELECT id FROM users WHERE username='management.member'"))
	if _, err := f.app.Administration.SearchTrials(ctx, dbtypes.Date{}, "", 0); err != authorization.ErrForbidden {
		t.Fatal("service management access", err)
	}
	// Other existing roles retain their current lack of trial access.
	for _, role := range []string{"coach", "treasurer"} {
		f.exec("INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name=?2", f.id("SELECT id FROM users WHERE username='management.member'"), role)
		if member.call("GET", "/trials?all=1", nil).Code != 403 {
			t.Fatal("non-secretariat role management access", role)
		}
	}
}
