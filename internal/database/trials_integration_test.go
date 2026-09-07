package database

import (
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestTrialsIntegration(t *testing.T) {
	db := newTestDatabase(t)
	ctx := t.Context()
	q := dbsqlc.New(db)
	svc := trials.New(db)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) int32 {
		t.Helper()
		var n int32
		must(db.QueryRow(ctx, sql, args...).Scan(&n))
		return n
	}
	exec := func(sql string, args ...any) { t.Helper(); _, err := db.Exec(ctx, sql, args...); must(err) }
	date := func(s string) pgtype.Date {
		d, err := time.Parse("2006-01-02", s)
		must(err)
		return pgtype.Date{Time: d, Valid: true}
	}
	nullable := func(n int32) pgtype.Int4 { return pgtype.Int4{Int32: n, Valid: true} }
	note := pgtype.Text{String: "Essai précis", Valid: true}
	person := id("INSERT INTO persons(first_name,last_name,birth_date,phone_number,notes) VALUES ('Arthur','Dupont','2016-01-02','0612345678','Durable') RETURNING id")
	a := id("INSERT INTO activities(name) VALUES ('JJB') RETURNING id")
	a2 := id("INSERT INTO activities(name) VALUES ('Judo') RETURNING id")
	g := id("INSERT INTO groups(activity_id,name) VALUES ($1,'Enfants') RETURNING id", a)
	g2 := id("INSERT INTO groups(activity_id,name) VALUES ($1,'Autre') RETURNING id", a2)
	season := id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2026','2026-09-01','2027-06-30') RETURNING id")
	slot := id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from,valid_until) VALUES ($1,$2,2,'18:00','19:00','Dojo municipal','2026-09-08','2027-06-22') RETURNING id", g, season)
	slot2 := id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,valid_from) VALUES ($1,$2,2,'18:00','19:00','2026-09-01') RETURNING id", g2, season)
	base := dbsqlc.CreateTrialParams{PersonID: person, ActivityID: a, TrialDate: date("2026-09-15"), Notes: note}
	full := base
	full.GroupID = nullable(g)
	full.GroupSlotID = nullable(slot)
	var historical int32
	t.Run("valid precision levels and multiple trials", func(t *testing.T) {
		grouped := base
		grouped.GroupID = nullable(g)
		other := base
		other.ActivityID = a2
		repeated := base
		repeated.TrialDate = date("2026-09-08")
		for _, p := range []dbsqlc.CreateTrialParams{base, grouped, full, repeated, other} {
			got, err := svc.Schedule(ctx, p)
			must(err)
			summary, err := q.GetTrial(ctx, got.ID)
			must(err)
			if summary.PersonID != person || summary.FirstName != "Arthur" || summary.LastName != "Dupont" || summary.BirthDate != date("2016-01-02") || summary.PhoneNumber.String != "0612345678" || summary.ActivityID != p.ActivityID || summary.GroupID != p.GroupID || summary.GroupSlotID != p.GroupSlotID || summary.Notes != note || summary.Status != "registered" {
				t.Fatalf("summary: %+v", summary)
			}
			if p.GroupID.Valid && summary.GroupName.String != "Enfants" {
				t.Fatal(summary)
			}
			if !p.GroupID.Valid && summary.GroupName.Valid {
				t.Fatal(summary)
			}
			if p.GroupSlotID.Valid {
				historical = got.ID
				if summary.ActivityName != "JJB" || summary.Location.String != "Dojo municipal" || summary.Weekday.Int16 != 2 || summary.StartTime.Microseconds != 18*int64(time.Hour/time.Microsecond) || summary.EndTime.Microseconds != 19*int64(time.Hour/time.Microsecond) {
					t.Fatal(summary)
				}
			} else if summary.Weekday.Valid || summary.StartTime.Valid || summary.EndTime.Valid || summary.Location.Valid {
				t.Fatal(summary)
			}
		}
		all, err := q.ListPersonTrials(ctx, person)
		must(err)
		if len(all) != 5 {
			t.Fatal(all)
		}
		daily, err := q.ListTrialsByDate(ctx, base.TrialDate)
		must(err)
		if len(daily) != 4 || daily[0].TrialID != historical || daily[0].Location.String != "Dojo municipal" {
			t.Fatal(daily)
		}
		upcoming, err := q.ListUpcomingTrials(ctx, base.TrialDate)
		must(err)
		if len(upcoming) != 4 {
			t.Fatal(upcoming)
		}
	})
	t.Run("structure enforced even for direct writes", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			group, slot pgtype.Int4
			code        string
		}{
			{"wrong activity", nullable(g2), pgtype.Int4{}, "23503"},
			{"wrong group", nullable(g), nullable(slot2), "23503"},
			{"missing group", pgtype.Int4{}, nullable(slot), "23514"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := base
				p.GroupID = tc.group
				p.GroupSlotID = tc.slot
				_, err := q.CreateTrial(ctx, p)
				requirePostgresCode(t, err, tc.code)
				if _, err = svc.Schedule(ctx, p); err == nil {
					t.Fatal("service accepted invalid target")
				}
			})
		}
		for _, statement := range []string{"DELETE FROM group_slots WHERE id=$1", "DELETE FROM groups WHERE id=$1", "DELETE FROM activities WHERE id=$1"} {
			n := slot
			if statement == "DELETE FROM groups WHERE id=$1" {
				n = g
			}
			if statement == "DELETE FROM activities WHERE id=$1" {
				n = a
			}
			_, err := db.Exec(ctx, statement, n)
			requirePostgresCode(t, err, "23503")
		}
	})
	t.Run("calendar creation and rescheduling", func(t *testing.T) {
		for _, d := range []string{"2026-09-16", "2026-09-01", "2027-06-29"} {
			p := full
			p.TrialDate = date(d)
			if _, err := svc.Schedule(ctx, p); err == nil {
				t.Fatalf("accepted %s", d)
			}
			if _, err := svc.Reschedule(ctx, dbsqlc.RescheduleTrialParams{ID: historical, ActivityID: a, GroupID: p.GroupID, GroupSlotID: p.GroupSlotID, TrialDate: p.TrialDate}); err == nil {
				t.Fatalf("rescheduled %s", d)
			}
		}
		exec("UPDATE group_slots SET valid_from='2026-08-01',valid_until=NULL WHERE id=$1", slot)
		for _, d := range []string{"2026-08-25", "2027-07-06"} {
			p := full
			p.TrialDate = date(d)
			if _, err := svc.Schedule(ctx, p); err == nil {
				t.Fatalf("outside season: %s", d)
			}
		}
		exec("UPDATE group_slots SET valid_from='2026-09-08',valid_until='2027-06-22' WHERE id=$1", slot)
		for _, d := range []string{"2026-09-08", "2027-06-22"} {
			p := full
			p.TrialDate = date(d)
			_, err := svc.Schedule(ctx, p)
			must(err)
		}
		exec("UPDATE seasons SET starts_at='2026-09-15',ends_at='2026-09-15' WHERE id=$1", season)
		_, err := svc.Schedule(ctx, full)
		must(err)
		exec("UPDATE seasons SET starts_at='2026-09-01',ends_at='2027-06-30' WHERE id=$1", season)
		p := full
		p.TrialDate = pgtype.Date{}
		if _, err := svc.Schedule(ctx, p); err == nil {
			t.Fatal("missing date accepted")
		}
		for _, change := range []func(*dbsqlc.CreateTrialParams){func(p *dbsqlc.CreateTrialParams) { p.ActivityID = -1 }, func(p *dbsqlc.CreateTrialParams) { p.GroupID = nullable(-1) }, func(p *dbsqlc.CreateTrialParams) { p.GroupSlotID = nullable(-1) }} {
			p := full
			change(&p)
			if _, err := svc.Schedule(ctx, p); err == nil {
				t.Fatal("missing object accepted")
			}
		}
	})
	t.Run("status corrections notes and rescheduling", func(t *testing.T) {
		for _, status := range []string{"registered", "attended", "cancelled", "no_show", "attended", "registered"} {
			got, err := q.UpdateTrialStatus(ctx, dbsqlc.UpdateTrialStatusParams{ID: historical, Status: status})
			must(err)
			if got.Status != status {
				t.Fatal(got)
			}
		}
		_, err := q.UpdateTrialStatus(ctx, dbsqlc.UpdateTrialStatusParams{ID: historical, Status: "invalid"})
		requirePostgresCode(t, err, "23514")
		changed := pgtype.Text{String: "Corrigée", Valid: true}
		got, err := q.UpdateTrialNotes(ctx, dbsqlc.UpdateTrialNotesParams{ID: historical, Notes: changed})
		must(err)
		if got.Notes != changed {
			t.Fatal(got)
		}
		var durable string
		must(db.QueryRow(ctx, "SELECT notes FROM persons WHERE id=$1", person).Scan(&durable))
		if durable != "Durable" {
			t.Fatal(durable)
		}
		got, err = svc.Reschedule(ctx, dbsqlc.RescheduleTrialParams{ID: historical, ActivityID: a2, TrialDate: date("2026-09-22")})
		must(err)
		if got.ActivityID != a2 || got.GroupID.Valid || got.GroupSlotID.Valid || got.Notes != changed || got.Status != "registered" || got.PersonID != person {
			t.Fatal(got)
		}
		_, err = svc.Reschedule(ctx, dbsqlc.RescheduleTrialParams{ID: historical, ActivityID: a, GroupID: full.GroupID, GroupSlotID: full.GroupSlotID, TrialDate: full.TrialDate})
		must(err)
		got, err = q.UpdateTrialNotes(ctx, dbsqlc.UpdateTrialNotesParams{ID: historical})
		must(err)
		if got.Notes.Valid {
			t.Fatal(got)
		}
	})
	t.Run("inactive targets reject scheduling but preserve history", func(t *testing.T) {
		for _, tc := range []struct {
			table string
			id    int32
		}{{"activities", a}, {"groups", g}, {"group_slots", slot}} {
			exec("UPDATE "+tc.table+" SET is_active=false WHERE id=$1", tc.id)
			if _, err := svc.Schedule(ctx, full); err == nil {
				t.Fatal("inactive accepted", tc.table)
			}
			if _, err := svc.Reschedule(ctx, dbsqlc.RescheduleTrialParams{ID: historical, ActivityID: a, GroupID: full.GroupID, GroupSlotID: full.GroupSlotID, TrialDate: full.TrialDate}); err == nil {
				t.Fatal("inactive reschedule accepted")
			}
			got, err := q.GetTrial(ctx, historical)
			must(err)
			if got.Location.String != "Dojo municipal" || got.GroupName.String != "Enfants" || got.ActivityName != "JJB" {
				t.Fatal(got)
			}
			exec("UPDATE "+tc.table+" SET is_active=true WHERE id=$1", tc.id)
		}
		exec("UPDATE activities SET is_active=false WHERE id=$1", a)
		exec("UPDATE groups SET is_active=false WHERE id=$1", g)
		exec("UPDATE group_slots SET is_active=false,valid_until='2026-09-08' WHERE id=$1", slot)
		got, err := q.GetTrial(ctx, historical)
		must(err)
		if got.TrialDate != full.TrialDate || got.GroupSlotID != full.GroupSlotID {
			t.Fatal(got)
		}
		all, err := q.ListPersonTrials(ctx, person)
		must(err)
		if len(all) != 8 {
			t.Fatal(all)
		}
		daily, err := q.ListTrialsByDate(ctx, full.TrialDate)
		must(err)
		if len(daily) != 5 {
			t.Fatal(daily)
		}
		upcoming, err := q.ListUpcomingTrials(ctx, full.TrialDate)
		must(err)
		if len(upcoming) != 6 {
			t.Fatal(upcoming)
		}
		_, err = q.UpdateTrialStatus(ctx, dbsqlc.UpdateTrialStatusParams{ID: historical, Status: "attended"})
		must(err)
		_, err = q.UpdateTrialNotes(ctx, dbsqlc.UpdateTrialNotesParams{ID: historical, Notes: note})
		must(err)
	})
}
