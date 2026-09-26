package database

import (
	"database/sql"
	"errors"
	"github.com/pressly/goose/v3"
	"os"
	"sync"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestP3SourceTrialValidationAndConcurrency(t *testing.T) {
	f := newMembershipFixture(t)
	ctx := t.Context()
	person := f.person("Origin", "1990-01-01")
	trial := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,'2026-09-16','attended') RETURNING id", person, f.activity)
	other := f.person("Other", "1990-01-01")
	otherActivity := f.id("INSERT INTO activities(name) VALUES('Other activity') RETURNING id")
	for _, tc := range []struct {
		name   string
		change func(*memberships.Request)
	}{
		{"missing", func(r *memberships.Request) { r.SourceTrialID = 999999 }},
		{"foreign person", func(r *memberships.Request) { r.PersonID = other; r.Consents[0].GivenByPersonID = other }},
		{"wrong activity", func(r *memberships.Request) { r.ActivityIDs = []int32{otherActivity} }},
		{"not attended", func(r *memberships.Request) {
			f.exec("UPDATE trial_registrations SET status='registered' WHERE id=$1", trial)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := f.request(person)
			r.SourceTrialID = trial
			tc.change(&r)
			if _, err := f.svc.CreateRequest(ctx, r); !errors.Is(err, memberships.ErrInvalidRequest) {
				t.Fatalf("invalid source accepted: %v", err)
			}
			f.exec("UPDATE trial_registrations SET status='attended' WHERE id=$1", trial)
		})
	}
	// A source is unique across seasons, including two concurrent conversions.
	r1, r2 := f.request(person), f.request(person)
	r1.SourceTrialID, r2.SourceTrialID = trial, trial
	r2.SeasonID = f.nextSeason()
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, r := range []memberships.Request{r1, r2} {
		wg.Add(1)
		go func(r memberships.Request) { defer wg.Done(); _, err := f.svc.CreateRequest(ctx, r); outcomes <- err }(r)
	}
	wg.Wait()
	close(outcomes)
	success := 0
	for err := range outcomes {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("source not unique", success)
	}
	id := f.id("SELECT id FROM memberships WHERE source_trial_id=$1", trial)
	if _, err := f.svc.CreateRequest(ctx, r1); err == nil {
		t.Fatal("duplicate season/source")
	}
	a := f.approve(id)
	if !a.Membership.SourceTrialID.Valid || a.Membership.SourceTrialID.Int32 != trial {
		t.Fatal("origin lost on approval")
	}
	if _, err := f.db.Exec(ctx, "DELETE FROM trial_registrations WHERE id=$1", trial); err == nil {
		t.Fatal("origin deletion allowed")
	}
	d, err := f.svc.GetDetails(ctx, id)
	f.must(err)
	if d.SourceTrial == nil || d.SourceTrial.ID != trial {
		t.Fatal("origin projection")
	}
	_, err = trials.New(f.db).Reschedule(ctx, dbsqlc.RescheduleTrialParams{ID: trial, ActivityID: otherActivity, TrialDate: pgtype.Date{Time: d.SourceTrial.TrialDate.Time, Valid: true}})
	if !errors.Is(err, trials.ErrConverted) {
		t.Fatal("converted trial rescheduled", err)
	}
	// A subsequent direct request is independent of the source and remains possible.
	r := f.request(person)
	r.SeasonID = f.nextSeason()
	direct := f.create(r)
	if direct.SourceTrialID.Valid || f.approve(direct.ID).Membership.SourceTrialID.Valid {
		t.Fatal("direct source non-null")
	}
}

func TestP3MinorExistingAccountPreserved(t *testing.T) {
	f := newMembershipFixture(t)
	child := f.person("Child", "2018-01-01")
	parent := f.guardian(child, "parent@example.test", true)
	f.emergency(child, parent)
	user := f.id("INSERT INTO users(person_id,username,is_active) VALUES($1,'historical.child',false) RETURNING id", child)
	r := f.request(child)
	r.Consents[0].GivenByPersonID = parent
	a := f.approve(f.create(r).ID)
	if a.User.ID != user || a.User.IsActive || a.ActivationDelivery != nil {
		t.Fatal("historical child account changed")
	}
}

func TestP3SourceMigrationPreservesHistory(t *testing.T) {
	f := newMembershipFixture(t)
	historical := f.create(f.request(f.person("Historical", "1990-01-01")))
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.DownTo(t.Context(), 31)
	f.must(err)
	_, err = provider.Up(t.Context())
	f.must(err)
	saved, err := dbsqlc.New(f.db).GetMembership(t.Context(), historical.ID)
	f.must(err)
	if saved.Status != "pending" || saved.PersonID != historical.PersonID || saved.SourceTrialID.Valid {
		t.Fatal("historical membership changed")
	}
	trial := f.id("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,'2026-09-16','attended') RETURNING id", historical.PersonID, f.activity)
	f.exec("UPDATE memberships SET source_trial_id=$2 WHERE id=$1", historical.ID, trial)
	if _, err = provider.DownTo(t.Context(), 31); err == nil {
		t.Fatal("lossy origin rollback allowed")
	}
	saved, err = dbsqlc.New(f.db).GetMembership(t.Context(), historical.ID)
	f.must(err)
	if saved.SourceTrialID.Int32 != trial {
		t.Fatal("failed rollback lost origin")
	}
}
