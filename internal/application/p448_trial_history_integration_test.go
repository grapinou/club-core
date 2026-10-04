package application

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

func p448TrialLinks(t *testing.T, body string, want []int32) {
	t.Helper()
	last := -1
	for _, id := range want {
		link := `href="` + officeTrial(id) + `"`
		pos := strings.Index(body, link)
		if pos <= last || strings.Count(body, link) != 1 {
			t.Fatalf("trial %d absent, duplicated or out of order; want %v", id, want)
		}
		last = pos
	}
	if strings.Count(body, `href="/trials/`) != len(want) {
		t.Fatal("unexpected trials in history")
	}
}

func TestP448TrialHistoryAndDirectoryOrder(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	f.exec("UPDATE seasons SET name='2026/2027' WHERE id=?1", f.season)
	group, slot := f.officeGroup()
	create := func(date, status string, withSlot bool) int32 {
		if withSlot {
			return f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) VALUES(?1,?2,?3,?4,?5,?6) RETURNING id", f.person, f.activity, group, slot, date, status)
		}
		return f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,?3,?4) RETURNING id", f.person, f.activity, date, status)
	}
	// Insertion order deliberately differs from date order, with both future and past trials.
	newest := create("2027-03-03", "registered", true)
	current := create("2026-10-03", "attended", true)
	oldest := create("2026-09-03", "cancelled", false)
	absent := create("2026-09-10", "no_show", false)
	sameDate := create("2026-09-10", "registered", false)
	previousSeason := f.id("INSERT INTO seasons(name,starts_at,ends_at,is_active) VALUES('2025/2026','2025-09-01','2026-08-31',false) RETURNING id")
	previous := create("2026-06-03", "attended", false)
	otherPerson := f.id("INSERT INTO persons(first_name,last_name) VALUES('Autre','Personne') RETURNING id")
	f.exec("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-10-03','attended')", otherPerson, f.activity)

	ctx := f.authenticatedContext(f.approver)
	rows, err := f.app.Administration.PersonTrials(ctx, f.person)
	f.must(err)
	want := []int32{newest, current, sameDate, absent, oldest, previous}
	var ids []int32
	for _, row := range rows {
		ids = append(ids, row.TrialID)
	}
	if !slices.Equal(ids, want) {
		t.Fatalf("query order %v want %v", ids, want)
	}
	// The history projection must match existing quota season resolution, including inactive seasons.
	q := dbsqlc.New(f.db)
	for _, row := range rows {
		seasons, err := q.ResolveTrialQuotaSeason(ctx, dbsqlc.ResolveTrialQuotaSeasonParams{SlotID: row.GroupSlotID, TrialDate: row.TrialDate})
		f.must(err)
		if len(seasons) != 1 || !slices.Equal(row.SeasonIds, []int32{seasons[0].ID}) {
			t.Fatal("season projection differs from quota resolution", row)
		}
	}
	if !slices.Equal(rows[len(rows)-1].SeasonIds, []int32{previousSeason}) {
		t.Fatal("inactive season lost")
	}

	body := officeOK(t, b, officeTrial(current), "Essais — 2026/2027", "Rémi Dupont")
	header := pagePart(t, body, `<header class="page-header trial-detail-header">`, `</header>`)
	if !strings.Contains(header, "Rémi Dupont") || !strings.Contains(header, `href="`+officePerson(f.person)+fmt.Sprintf(`/memberships/new?trial=%d"`, current)) || !strings.Contains(header, "Préparer une demande d’adhésion") {
		t.Fatal("membership action absent from header")
	}
	if strings.Contains(body, "Après l’essai") {
		t.Fatal("obsolete panel present")
	}
	history := pagePart(t, body, `<h2>Essais — 2026/2027</h2>`, `</section>`)
	p448TrialLinks(t, history, want[:5])
	for _, text := range []string{"03/03/2027", "03/10/2026", "Practice", "Groupe adultes", "Programmé", "Présent", "Annulée", "Absent"} {
		if !strings.Contains(history, text) {
			t.Fatal("history missing", text)
		}
	}
	trialDetailLayout(t, body, false)
	body = officeOK(t, b, officePerson(f.person))
	p448TrialLinks(t, pagePart(t, body, `<section id="essais"`, `</section>`), want)
	body = officeOK(t, b, officeTrial(previous), "Essais — 2025/2026")
	p448TrialLinks(t, pagePart(t, body, `<h2>Essais — 2025/2026</h2>`, `</section>`), []int32{previous})
}

func TestP448HistoryIsCompleteBeyondCockpitLimit(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	var want []int32
	for i := 0; i < 103; i++ {
		want = append(want, f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-09-01','cancelled') RETURNING id", f.person, f.activity))
	}
	slices.Reverse(want)
	body := officeOK(t, b, officeTrial(want[0]))
	p448TrialLinks(t, pagePart(t, body, `<h2>Essais — 2026</h2>`, `</section>`), want)
	body = officeOK(t, b, officePerson(f.person))
	p448TrialLinks(t, pagePart(t, body, `<section id="essais"`, `</section>`), want)
}

func TestP448IndeterminateSeasonHistory(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprintf("ambiguous=%t", ambiguous), func(t *testing.T) {
			f := newFixture(t)
			b := p43Secretary(f)
			if ambiguous {
				f.exec("INSERT INTO seasons(name,starts_at,ends_at) VALUES('Chevauchement','2026-09-01','2027-08-31')")
			} else {
				f.exec("DELETE FROM seasons WHERE id=?1", f.season)
			}
			first := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-09-01','attended') RETURNING id", f.person, f.activity)
			last := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,'2026-10-01','no_show') RETURNING id", f.person, f.activity)
			body := officeOK(t, b, officeTrial(first), "Comptage à vérifier")
			p448TrialLinks(t, pagePart(t, body, `<h2>Essais — Saison indéterminée</h2>`, `</section>`), []int32{last, first})
			// A precise slot remains assigned even when calendar seasons overlap.
			if ambiguous {
				group, slot := f.officeGroup()
				precise := f.id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) VALUES(?1,?2,?3,?4,'2026-10-03','registered') RETURNING id", f.person, f.activity, group, slot)
				body = officeOK(t, b, officeTrial(precise))
				p448TrialLinks(t, pagePart(t, body, `<h2>Essais — 2026</h2>`, `</section>`), []int32{precise})
			}
		})
	}
}
