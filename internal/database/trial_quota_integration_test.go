package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pressly/goose/v3"
)

type quotaFixture struct {
	*membershipFixture
	trials *trials.Service
	person int32
}

func newQuotaFixture(t *testing.T) *quotaFixture {
	f := newMembershipFixture(t)
	f.id("INSERT INTO organizations(name,max_trials_per_person_per_season) VALUES('Association culturelle',3) RETURNING id")
	return &quotaFixture{f, trials.New(f.db), f.person("Alice", "1990-01-01")}
}
func quotaDate(raw string) pgtype.Date {
	d, _ := time.Parse("2006-01-02", raw)
	return pgtype.Date{Time: d, Valid: true}
}
func (f *quotaFixture) schedule(person int32, date string) (dbsqlc.TrialRegistration, error) {
	return f.trials.Schedule(f.t.Context(), dbsqlc.CreateTrialParams{PersonID: person, ActivityID: f.activity, TrialDate: quotaDate(date)})
}
func (f *quotaFixture) add(date string) dbsqlc.TrialRegistration {
	f.t.Helper()
	tr, err := f.schedule(f.person, date)
	f.must(err)
	return tr
}
func (f *quotaFixture) status(id int32, status string) error {
	tx, err := f.db.Begin(f.t.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(f.t.Context())
	_, err = f.trials.UpdateStatusTx(f.t.Context(), tx, dbsqlc.UpdateTrialStatusParams{ID: id, Status: status})
	if err != nil {
		return err
	}
	return tx.Commit(f.t.Context())
}
func (f *quotaFixture) move(id int32, date string) error {
	_, err := f.trials.Reschedule(f.t.Context(), dbsqlc.RescheduleTrialParams{ID: id, ActivityID: f.activity, TrialDate: quotaDate(date)})
	return err
}
func (f *quotaFixture) usage(season int32) trials.QuotaUsage {
	f.t.Helper()
	rows, err := f.trials.Quotas(f.t.Context(), f.person)
	f.must(err)
	for _, r := range rows {
		if r.SeasonID == season {
			return r
		}
	}
	f.t.Fatal("missing season usage")
	return trials.QuotaUsage{}
}
func expectQuota(t *testing.T, err error) {
	t.Helper()
	var q *trials.QuotaExceededError
	if !errors.As(err, &q) {
		t.Fatalf("expected quota error, got %v", err)
	}
}

func TestP31TrialQuotaLifecycle(t *testing.T) {
	f := newQuotaFixture(t)
	cancelled := f.add("2026-09-10")
	f.must(f.status(cancelled.ID, "cancelled"))
	noShow := f.add("2026-09-11")
	f.must(f.status(noShow.ID, "no_show"))
	a := f.add("2026-09-12")
	f.must(f.status(a.ID, "attended"))
	b := f.add("2026-09-13")
	u := f.usage(f.season)
	if u.AttendedCount != 1 || u.RegisteredCount != 1 || u.UsedOrReserved != 2 || u.Remaining != 1 || u.Limit.Int32 != 3 {
		t.Fatal("wrong counts", u)
	}
	f.must(f.status(b.ID, "attended"))
	c := f.add("2026-09-14")
	_, err := f.schedule(f.person, "2026-09-15")
	expectQuota(t, err)
	expectQuota(t, f.status(noShow.ID, "attended"))
	expectQuota(t, f.status(cancelled.ID, "registered"))
	f.must(f.move(c.ID, "2026-09-16")) // same season, even at capacity
	if u = f.usage(f.season); u.UsedOrReserved != 3 || u.Remaining != 0 {
		t.Fatal(u)
	}
	f.must(f.status(c.ID, "cancelled"))
	f.add("2026-09-17")
	f.must(f.status(a.ID, "no_show"))
	f.add("2026-09-18")
	if u = f.usage(f.season); u.UsedOrReserved != 3 {
		t.Fatal(u)
	}
	other := f.membershipFixture.person("David", "1990-01-01")
	_, err = f.schedule(other, "2026-09-19")
	f.must(err)
	next := f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES('2027/2028','2027-09-01','2028-08-31') RETURNING id")
	d := f.add("2027-09-12")
	expectQuota(t, f.move(d.ID, "2026-09-20"))
	f.must(f.move(b.ID, "2027-09-13"))
	if f.usage(f.season).UsedOrReserved != 2 || f.usage(next).UsedOrReserved != 2 {
		t.Fatal("season transfer counts")
	}
	// No child/account/membership mutation is involved in quota consumption.
	f.exec("UPDATE organizations SET max_trials_per_person_per_season=1")
	f.must(f.status(d.ID, "attended")) // no extra reservation after lowering limit
	f.must(f.move(d.ID, "2027-09-14"))
	_, err = f.schedule(f.person, "2027-09-15")
	expectQuota(t, err)
	if f.usage(next).Remaining != 0 {
		t.Fatal("negative remaining")
	}
	_, err = f.schedule(other, "2027-09-16")
	f.must(err)
	_, err = f.schedule(other, "2027-09-17")
	expectQuota(t, err) // limit 1 permits exactly one reservation in a fresh season
	f.exec("UPDATE organizations SET max_trials_per_person_per_season=NULL")
	for range 5 {
		f.add("2027-09-16")
	}
	if f.usage(next).Limit.Valid {
		t.Fatal("unlimited became limited")
	}
}

func TestP31TrialQuotaSeasonResolutionAndP3(t *testing.T) {
	f := newQuotaFixture(t)
	_, err := f.schedule(f.person, "2030-09-16")
	if !errors.Is(err, trials.ErrQuotaSeason) {
		t.Fatal("missing season", err)
	}
	overlapping := f.id("INSERT INTO seasons(name,starts_at,ends_at,is_active) VALUES('Historique superposé','2026-01-01','2026-12-31',false) RETURNING id")
	_, err = f.schedule(f.person, "2026-09-16")
	if !errors.Is(err, trials.ErrQuotaSeason) {
		t.Fatal("ambiguous season", err)
	}
	group := f.id("INSERT INTO groups(activity_id,name) VALUES($1,'Découverte') RETURNING id", f.activity)
	slot := f.id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,valid_from) VALUES($1,$2,3,'14:00','16:00','2026-09-01') RETURNING id", group, f.season)
	p := dbsqlc.CreateTrialParams{PersonID: f.person, ActivityID: f.activity, GroupID: pgtype.Int4{Int32: group, Valid: true}, GroupSlotID: pgtype.Int4{Int32: slot, Valid: true}, TrialDate: quotaDate("2026-09-16")}
	tr, err := f.trials.Schedule(t.Context(), p)
	f.must(err) // slot determines season despite overlap
	f.must(f.status(tr.ID, "attended"))
	req := f.request(f.person)
	req.SourceTrialID = tr.ID
	member := f.create(req)
	f.approve(member.ID)
	if f.usage(f.season).AttendedCount != 1 {
		t.Fatal("membership erased trial usage")
	}
	if err = f.move(tr.ID, "2026-09-23"); !errors.Is(err, trials.ErrConverted) {
		t.Fatal("P3 protection", err)
	}
	f.exec("UPDATE organizations SET max_trials_per_person_per_season=NULL")
	unknown := f.add("2026-09-17")
	f.add("2030-09-16") // unlimited preserves undetermined historical dates
	f.exec("UPDATE organizations SET max_trials_per_person_per_season=3")
	_, err = f.trials.Schedule(t.Context(), p)
	if !errors.Is(err, trials.ErrQuotaSeason) {
		t.Fatal("ambiguous historical reservation ignored", err)
	}
	if !f.usage(f.season).Ambiguous {
		t.Fatal("ambiguity not exposed")
	}
	f.must(f.status(unknown.ID, "cancelled")) // always possible to release ambiguous reservations
	_, err = f.trials.Schedule(t.Context(), p)
	f.must(err)
	f.exec("DELETE FROM seasons WHERE id=$1", overlapping)
	f.exec("UPDATE seasons SET is_active=false WHERE id=$1", f.season)
	if f.usage(f.season).UsedOrReserved != 2 {
		t.Fatal("inactive history vanished")
	}
}

func TestP31TrialQuotaConcurrency(t *testing.T) {
	for _, mode := range []string{"two_creations", "creation_and_status", "creation_and_transfer"} {
		t.Run(mode, func(t *testing.T) {
			f := newQuotaFixture(t)
			f.add("2026-09-12")
			f.add("2026-09-13")
			var special dbsqlc.TrialRegistration
			if mode == "creation_and_status" {
				special = f.add("2026-09-14")
				f.must(f.status(special.ID, "no_show"))
			}
			if mode == "creation_and_transfer" {
				f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES('Next','2027-09-01','2028-08-31') RETURNING id")
				special = f.add("2027-09-14")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			start := make(chan struct{})
			done := make(chan error, 2)
			var ready sync.WaitGroup
			ready.Add(2)
			for i := range 2 {
				go func(i int) {
					tx, err := f.db.Begin(ctx)
					if err != nil {
						ready.Done()
						done <- err
						return
					}
					defer tx.Rollback(ctx)
					ready.Done()
					<-start
					if i == 1 && mode == "creation_and_status" {
						_, err = f.trials.UpdateStatusTx(ctx, tx, dbsqlc.UpdateTrialStatusParams{ID: special.ID, Status: "attended"})
					} else if i == 1 && mode == "creation_and_transfer" {
						_, err = f.trials.RescheduleTx(ctx, tx, dbsqlc.RescheduleTrialParams{ID: special.ID, ActivityID: f.activity, TrialDate: quotaDate("2026-09-15")})
					} else {
						_, err = f.trials.ScheduleTx(ctx, tx, dbsqlc.CreateTrialParams{PersonID: f.person, ActivityID: f.activity, TrialDate: quotaDate("2026-09-15")})
					}
					if err == nil {
						err = tx.Commit(ctx)
					}
					done <- err
				}(i)
			}
			ready.Wait()
			close(start)
			successes, denials := 0, 0
			for range 2 {
				err := <-done
				var quota *trials.QuotaExceededError
				if err == nil {
					successes++
				} else if errors.As(err, &quota) {
					denials++
				} else {
					t.Fatal("unexpected concurrent failure", err)
				}
			}
			if successes != 1 || denials != 1 || f.usage(f.season).UsedOrReserved != 3 {
				t.Fatal("overbooked", successes, denials)
			}
		})
	}
}

func TestP31TrialQuotaMigration(t *testing.T) {
	f := newMembershipFixture(t)
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.DownTo(t.Context(), 32)
	f.must(err)
	id := f.id("INSERT INTO organizations(name) VALUES('Association historique') RETURNING id")
	_, err = provider.Up(t.Context())
	f.must(err)
	org, err := dbsqlc.New(f.db).GetActiveOrganization(t.Context())
	f.must(err)
	if org.ID != id || org.MaxTrialsPerPersonPerSeason.Valid {
		t.Fatal("existing association must remain unlimited")
	}
	f.exec("UPDATE organizations SET max_trials_per_person_per_season=3")
	if _, err = provider.DownTo(t.Context(), 32); err == nil {
		t.Fatal("lossy quota rollback allowed")
	}
	org, err = dbsqlc.New(f.db).GetActiveOrganization(t.Context())
	f.must(err)
	if !org.MaxTrialsPerPersonPerSeason.Valid || org.MaxTrialsPerPersonPerSeason.Int32 != 3 {
		t.Fatal("failed rollback lost quota")
	}
}
