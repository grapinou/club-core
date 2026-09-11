package database

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/pressly/goose/v3"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/consents"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type membershipFixture struct {
	t                                            *testing.T
	db                                           *pgxpool.Pool
	svc                                          *memberships.Service
	activation                                   *activation.Service
	season, kind, activity, definition, approver int32
	serial                                       int
}

func newMembershipFixture(t *testing.T) *membershipFixture {
	f := &membershipFixture{t: t, db: newTestDatabase(t)}
	loc, err := time.LoadLocation("Europe/Paris")
	f.must(err)
	f.svc, err = memberships.New(f.db, time.Hour, loc)
	f.must(err)
	f.activation, err = activation.New(f.db, time.Hour)
	f.must(err)
	f.season = f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2026','2026-09-01','2027-08-31') RETURNING id")
	f.kind = f.id("INSERT INTO membership_types(name) VALUES ('Standard') RETURNING id")
	f.activity = f.id("INSERT INTO activities(name) VALUES ('Judo') RETURNING id")
	f.definition = f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES ('photo',1,'Photo','Version 1') RETURNING id")
	admin := f.person("Admin", "1980-01-01")
	f.approver = f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'admin','preserved',now()) RETURNING id", admin)
	return f
}
func (f *membershipFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}
func (f *membershipFixture) id(query string, args ...any) int32 {
	f.t.Helper()
	var id int32
	f.must(f.db.QueryRow(f.t.Context(), query, args...).Scan(&id))
	return id
}
func (f *membershipFixture) exec(query string, args ...any) {
	f.t.Helper()
	_, err := f.db.Exec(f.t.Context(), query, args...)
	f.must(err)
}
func (f *membershipFixture) person(first string, birth any) int32 {
	return f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES ($1,'Dupont',$2::date) RETURNING id", first, birth)
}
func (f *membershipFixture) request(person int32) memberships.Request {
	return memberships.Request{PersonID: person, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}, Consents: []memberships.Decision{{ConsentDefinitionID: f.definition, Decision: "refused", GivenByPersonID: person}}}
}
func (f *membershipFixture) create(r memberships.Request) dbsqlc.Membership {
	f.t.Helper()
	m, err := f.svc.CreateRequest(f.t.Context(), r)
	f.must(err)
	return m
}
func (f *membershipFixture) approve(id int32) memberships.Approval {
	f.t.Helper()
	a, err := f.svc.ApproveMembership(f.t.Context(), id, f.approver, nil)
	f.must(err)
	return a
}
func (f *membershipFixture) nextSeason() int32 {
	f.serial++
	return f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ($1,'2028-09-01','2029-08-31') RETURNING id", fmt.Sprint("renewal", f.serial))
}
func (f *membershipFixture) guardian(child int32, email any, primary bool) int32 {
	p := f.person("Guardian", "1980-01-01")
	f.exec("UPDATE persons SET email=$2 WHERE id=$1", p, email)
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES ($1,$2,'guardian',$3)", child, p, primary)
	return p
}
func (f *membershipFixture) emergency(child, contact int32) {
	f.exec("INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES ($1,$2,1)", child, contact)
}
func TestCreateMembershipRequest(t *testing.T) {
	f := newMembershipFixture(t)
	for _, tc := range []struct {
		name   string
		mutate func(*memberships.Request)
	}{
		{"missing person", func(r *memberships.Request) { r.PersonID = -1 }},
		{"missing birth", func(r *memberships.Request) { f.exec("UPDATE persons SET birth_date=NULL WHERE id=$1", r.PersonID) }},
		{"missing season", func(r *memberships.Request) { r.SeasonID = -1 }},
		{"inactive season", func(r *memberships.Request) {
			r.SeasonID = f.id("INSERT INTO seasons(name,starts_at,ends_at,is_active) VALUES ('inactive','2026-01-01','2026-12-31',false) RETURNING id")
		}},
		{"missing type", func(r *memberships.Request) { r.MembershipTypeID = -1 }},
		{"inactive type", func(r *memberships.Request) {
			r.MembershipTypeID = f.id("INSERT INTO membership_types(name,is_active) VALUES ('inactive',false) RETURNING id")
		}},
		{"no activity", func(r *memberships.Request) { r.ActivityIDs = nil }},
		{"missing activity", func(r *memberships.Request) { r.ActivityIDs = []int32{-1} }},
		{"inactive activity", func(r *memberships.Request) {
			r.ActivityIDs = []int32{f.id("INSERT INTO activities(name,is_active) VALUES ('inactive',false) RETURNING id")}
		}},
		{"duplicate activity", func(r *memberships.Request) { r.ActivityIDs = append(r.ActivityIDs, f.activity) }},
		{"missing answer", func(r *memberships.Request) { r.Consents = nil }},
		{"withdrawn first", func(r *memberships.Request) { r.Consents[0].Decision = "withdrawn" }},
		{"duplicate answer", func(r *memberships.Request) { r.Consents = append(r.Consents, r.Consents[0]) }},
		{"unexpected definition", func(r *memberships.Request) { r.Consents[0].ConsentDefinitionID = -1 }},
		{"unauthorized giver", func(r *memberships.Request) { r.Consents[0].GivenByPersonID = f.person("Stranger", "1980-01-01") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := f.request(f.person("Applicant", "1990-01-01"))
			tc.mutate(&r)
			if _, err := f.svc.CreateRequest(t.Context(), r); err == nil {
				t.Fatal("accepted invalid request")
			}
			var count int
			f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM memberships WHERE person_id=$1", r.PersonID).Scan(&count))
			if count != 0 {
				t.Fatal("partial request committed")
			}
		})
	}
	p := f.person("Rémi", "1990-01-01")
	grant := f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES ('rules',1,'Rules','Rules v1') RETURNING id")
	f.id("INSERT INTO consent_definitions(code,version,title,description,is_active) VALUES ('old',1,'Old','Old',false) RETURNING id")
	r := f.request(p)
	r.Consents = append(r.Consents, memberships.Decision{ConsentDefinitionID: grant, Decision: "granted", GivenByPersonID: p})
	m := f.create(r)
	if m.Status != "pending" || !m.RequestedAt.Valid || m.ApprovedAt.Valid {
		t.Fatal(m)
	}
	if _, err := f.svc.CreateRequest(t.Context(), r); err == nil {
		t.Fatal("duplicate accepted")
	}
	var count int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM users WHERE person_id=$1", p).Scan(&count))
	if count != 0 {
		t.Fatal("premature user")
	}
	d, err := f.svc.GetDetails(t.Context(), m.ID)
	f.must(err)
	if len(d.ConsentRequirements) != 2 || len(d.Activities) != 1 || len(d.Completeness.BlockingIssues) != 0 {
		t.Fatal(d)
	}
	got := []string{}
	for _, c := range d.ConsentRequirements {
		got = append(got, c.Decision.String)
		if c.PresentedAt != m.RequestedAt {
			t.Fatal("snapshot timestamp")
		}
	}
	if !slices.Contains(got, "granted") || !slices.Contains(got, "refused") {
		t.Fatal(got)
	}
	f.exec("UPDATE consent_definitions SET is_active=false WHERE id=$1", grant)
	f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES ('rules',2,'Rules','New wording') RETURNING id")
	_, err = consents.New(f.db).WithdrawConsent(t.Context(), m.ID, grant, p)
	f.must(err)
	a := f.approve(m.ID)
	if a.Membership.Status != "active" {
		t.Fatal(a.Membership)
	}
	d, err = f.svc.GetDetails(t.Context(), m.ID)
	f.must(err)
	if len(d.ConsentRequirements) != 2 || len(d.Completeness.BlockingIssues) != 0 {
		t.Fatal(d)
	}
	if d.Membership.ApproverUsername.String != "admin" || !d.Membership.UserID.Valid {
		t.Fatal(d.Membership)
	}
}

func TestApproveMembershipCompleteness(t *testing.T) {
	f := newMembershipFixture(t)
	for _, tc := range []struct {
		name, birth         string
		guardian, emergency bool
		issue               string
	}{
		{"adult", "1990-01-01", false, true, ""},
		{"adult warning", "1990-01-01", false, false, ""},
		{"minor no guardian", "2020-01-01", false, true, "minor_missing_guardian"},
		{"minor no emergency", "2020-01-01", true, false, "minor_missing_emergency"},
		{"minor complete", "2020-01-01", true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := f.person(tc.name, tc.birth)
			contact := f.person("Contact", "1980-01-01")
			if tc.guardian {
				contact = f.guardian(p, nil, true)
			}
			if tc.emergency {
				f.emergency(p, contact)
			}
			r := f.request(p)
			if tc.guardian {
				r.Consents[0].GivenByPersonID = contact
			}
			m := f.create(r)
			a, err := f.svc.ApproveMembership(t.Context(), m.ID, f.approver, nil)
			if tc.issue != "" {
				var incomplete *memberships.IncompleteError
				if !errors.As(err, &incomplete) || !slices.Contains(incomplete.Completeness.BlockingIssues, tc.issue) {
					t.Fatalf("%v", err)
				}
				var n int
				f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM users WHERE person_id=$1", p).Scan(&n))
				if n != 0 {
					t.Fatal("user on refusal")
				}
			} else {
				f.must(err)
				if a.Membership.Status != "active" || !a.Membership.ApprovedAt.Valid || a.Membership.ApprovedByUserID.Int32 != f.approver || !a.Membership.JoinedAt.Valid {
					t.Fatal(a.Membership)
				}
				if !tc.emergency && !slices.Contains(a.Completeness.Warnings, "adult_missing_emergency") {
					t.Fatal(a.Completeness)
				}
				if _, err = f.svc.ApproveMembership(t.Context(), m.ID, f.approver, nil); !errors.Is(err, memberships.ErrNotPending) {
					t.Fatal(err)
				}
			}
		})
	}
	for _, issue := range []string{"missing_birth_date", "missing_activity", "unanswered_consent", "missing approver"} {
		t.Run(issue, func(t *testing.T) {
			p := f.person("Incomplete", "1990-01-01")
			m := f.create(f.request(p))
			approver := f.approver
			switch issue {
			case "missing_birth_date":
				f.exec("UPDATE persons SET birth_date=NULL WHERE id=$1", p)
			case "missing_activity":
				f.exec("DELETE FROM membership_activities WHERE membership_id=$1", m.ID)
			case "unanswered_consent":
				extra := f.id("INSERT INTO consent_definitions(code,version,title,description,is_active) VALUES ('unanswered',1,'Missing','Missing',false) RETURNING id")
				f.exec("INSERT INTO membership_consent_requirements VALUES ($1,$2,now())", m.ID, extra)
				// A withdrawal without initial response is insufficient, even for direct SQL.
				f.exec("INSERT INTO membership_consents(membership_id,consent_definition_id,decision,given_by_person_id) VALUES ($1,$2,'withdrawn',$3)", m.ID, extra, p)
			case "missing approver":
				approver = -1
			}
			_, err := f.svc.ApproveMembership(t.Context(), m.ID, approver, nil)
			if err == nil {
				t.Fatal("approved incomplete")
			}
		})
	}
	// Exact birthday and unrelated practice-group assignment.
	loc, _ := time.LoadLocation("Europe/Paris")
	now := time.Now().In(loc)
	p := f.person("Birthday", now.AddDate(-18, 0, 0).Format("2006-01-02"))
	m := f.create(f.request(p))
	group := f.id("INSERT INTO groups(activity_id,name) VALUES ($1,'Children') RETURNING id", f.activity)
	f.exec("INSERT INTO membership_groups(membership_id,group_id,joined_at) VALUES ($1,$2,current_date)", m.ID, group)
	f.exec("UPDATE memberships SET joined_at='2020-01-01' WHERE id=$1", m.ID)
	note := "Validated manually"
	a, err := f.svc.ApproveMembership(t.Context(), m.ID, f.approver, &note)
	f.must(err)
	if *a.Completeness.IsMinor || a.Membership.JoinedAt.Time.Format("2006-01-02") != "2020-01-01" || a.Membership.AdminNote.String != note {
		t.Fatal(a)
	}
}

func TestMembershipUserRenewalAndEmail(t *testing.T) {
	f := newMembershipFixture(t)
	for _, tc := range []struct {
		name                string
		own, primary, other any
		want                string
	}{
		{"own", " teen@example.test ", "primary@example.test", "other@example.test", "teen@example.test"},
		{"primary", " ", "primary@example.test", "other@example.test", "primary@example.test"},
		{"other", nil, nil, "other@example.test", "other@example.test"},
		{"none", nil, " ", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := f.person("Rémi", "1990-01-01")
			f.exec("UPDATE persons SET email=$2 WHERE id=$1", p, tc.own)
			other := f.guardian(p, tc.other, false)
			primary := f.guardian(p, tc.primary, true)
			f.guardian(p, "", false)
			a := f.approve(f.create(f.request(p)).ID)
			d := a.ActivationDelivery
			if d == nil || a.User.PasswordHash.Valid || a.User.ActivatedAt.Valid || !a.User.IsActive {
				t.Fatal("new user state")
			}
			if tc.want == "" {
				if d.RecipientEmail != nil || d.RecipientPersonID != nil {
					t.Fatal("unexpected recipient")
				}
			} else {
				wantID := other
				if tc.name == "own" {
					wantID = p
				} else if tc.name == "primary" {
					wantID = primary
				}
				if d.RecipientEmail == nil || *d.RecipientEmail != tc.want || *d.RecipientPersonID != wantID {
					t.Fatal("wrong recipient")
				}
			}
			var email *string
			f.must(f.db.QueryRow(t.Context(), "SELECT email FROM persons WHERE id=$1", p).Scan(&email))
			if tc.own == nil && email != nil {
				t.Fatal("copied guardian email")
			}
			r := f.request(p)
			r.SeasonID = f.nextSeason()
			f.exec("UPDATE users SET is_active=false WHERE id=$1", a.User.ID)
			renewed := f.approve(f.create(r).ID)
			if renewed.User.ID != a.User.ID || renewed.User.Username != a.User.Username || !renewed.User.IsActive || renewed.ActivationDelivery == nil {
				t.Fatal("renewal user mismatch")
			}
			_, err := f.activation.Activate(t.Context(), d.Username, d.PlaintextCode, "a secure password")
			if !errors.Is(err, activation.ErrInvalidCode) {
				t.Fatal("old code usable", err)
			}
			activated, err := f.activation.Activate(t.Context(), d.Username, renewed.ActivationDelivery.PlaintextCode, "a secure password")
			f.must(err)
			for _, disabled := range []bool{false, true} {
				f.exec("UPDATE users SET is_active=$2 WHERE id=$1", a.User.ID, !disabled)
				r.SeasonID = f.nextSeason()
				next := f.approve(f.create(r).ID)
				if next.User.ID != a.User.ID || next.User.Username != a.User.Username || next.User.PasswordHash != activated.PasswordHash || next.User.ActivatedAt != activated.ActivatedAt || !next.User.IsActive || next.ActivationDelivery != nil {
					t.Fatal("activated user changed")
				}
			}
		})
	}
	var names []string
	rows, err := f.db.Query(t.Context(), "SELECT username FROM users WHERE username LIKE 'remi.dupont%' ORDER BY username")
	f.must(err)
	for rows.Next() {
		var name string
		f.must(rows.Scan(&name))
		names = append(names, name)
	}
	f.must(rows.Err())
	rows.Close()
	if !slices.Equal(names, []string{"remi.dupont", "remi.dupont2", "remi.dupont3", "remi.dupont4"}) {
		t.Fatal(names)
	}
}
func TestActivationCodes(t *testing.T) {
	f := newMembershipFixture(t)
	for _, state := range []string{"correct", "wrong", "expired", "invalidated", "used"} {
		t.Run(state, func(t *testing.T) {
			p := f.person("Code", "1990-01-01")
			a := f.approve(f.create(f.request(p)).ID)
			d := a.ActivationDelivery
			var stored []byte
			f.must(f.db.QueryRow(t.Context(), "SELECT code_hash FROM user_activation_codes WHERE user_id=$1", a.User.ID).Scan(&stored))
			digest := sha256.Sum256([]byte(d.PlaintextCode))
			if string(stored) == d.PlaintextCode || string(stored) != string(digest[:]) || len(d.PlaintextCode) != 20 {
				t.Fatal("unsafe code storage")
			}
			code := d.PlaintextCode
			switch state {
			case "wrong":
				code = "wrong"
			case "expired":
				f.exec("UPDATE user_activation_codes SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE user_id=$1", a.User.ID)
			case "invalidated":
				f.exec("UPDATE user_activation_codes SET invalidated_at=now() WHERE user_id=$1", a.User.ID)
			case "used":
				f.exec("UPDATE user_activation_codes SET used_at=now() WHERE user_id=$1", a.User.ID)
			}
			user, err := f.activation.Activate(t.Context(), d.Username, code, "a secure password")
			if state == "correct" {
				f.must(err)
				if !user.ActivatedAt.Valid || !user.PasswordHash.Valid {
					t.Fatal("not activated")
				}
				f.must(bcrypt.CompareHashAndPassword([]byte(user.PasswordHash.String), []byte("a secure password")))
				var used bool
				f.must(f.db.QueryRow(t.Context(), "SELECT used_at IS NOT NULL FROM user_activation_codes WHERE user_id=$1", user.ID).Scan(&used))
				if !used {
					t.Fatal("not consumed")
				}
				_, err = f.activation.Activate(t.Context(), d.Username, code, "another password")
				if !errors.Is(err, activation.ErrInvalidCode) {
					t.Fatal("replayed", err)
				}
			} else {
				if !errors.Is(err, activation.ErrInvalidCode) {
					t.Fatal(err)
				}
				storedUser, e := dbsqlc.New(f.db).GetUserByPerson(t.Context(), p)
				f.must(e)
				if storedUser.PasswordHash.Valid || storedUser.ActivatedAt.Valid {
					t.Fatal("invalid code mutated user")
				}
			}
		})
	}
	if _, err := f.activation.Activate(t.Context(), "absent", "anything", "a secure password"); !errors.Is(err, activation.ErrInvalidCode) {
		t.Fatal(err)
	}
	if _, err := activation.New(f.db, 0); err == nil {
		t.Fatal("zero validity")
	}
	if _, err := f.activation.Activate(t.Context(), "admin", "anything", "short"); !errors.Is(err, activation.ErrInvalidPassword) {
		t.Fatal(err)
	}
}

func TestConcurrentApprovalsAndActivation(t *testing.T) {
	f := newMembershipFixture(t)
	ids := []int32{}
	for range 5 {
		p := f.person("Concurrent", "1990-01-01")
		ids = append(ids, f.create(f.request(p)).ID)
	}
	run := func(ids []int32) []memberships.Approval {
		t.Helper()
		results := make([]memberships.Approval, len(ids))
		errs := make([]error, len(ids))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, id := range ids {
			wg.Go(func() { <-start; results[i], errs[i] = f.svc.ApproveMembership(t.Context(), id, f.approver, nil) })
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			f.must(err)
		}
		return results
	}
	results := run(ids)
	names := map[string]bool{}
	for _, a := range results {
		if names[a.User.Username] {
			t.Fatal("duplicate username")
		}
		names[a.User.Username] = true
	}
	for _, name := range []string{"concurrent.dupont", "concurrent.dupont2", "concurrent.dupont3", "concurrent.dupont4", "concurrent.dupont5"} {
		if !names[name] {
			t.Fatal(names)
		}
	}
	p := f.person("Renew Concurrent", "1990-01-01")
	r := f.request(p)
	first := f.create(r)
	r.SeasonID = f.nextSeason()
	second := f.create(r)
	renewals := run([]int32{first.ID, second.ID})
	if renewals[0].User.ID != renewals[1].User.ID {
		t.Fatal("duplicate user")
	}
	var current int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM user_activation_codes WHERE user_id=$1 AND invalidated_at IS NULL AND used_at IS NULL", renewals[0].User.ID).Scan(&current))
	if current != 1 {
		t.Fatal(current)
	}
	// Two simultaneous attempts to consume the same valid code: exactly one wins.
	d := results[0].ActivationDelivery
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = f.activation.Activate(t.Context(), d.Username, d.PlaintextCode, "a secure password")
		})
	}
	wg.Wait()
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, activation.ErrInvalidCode) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("activation race", errs)
	}
}

func TestMembershipActivationMigration(t *testing.T) {
	pool := newTestDatabase(t)
	ctx := t.Context()
	db, err := sql.Open("pgx", pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.DownTo(ctx, 15); err != nil {
		t.Fatal(err)
	}
	exec := func(query string) {
		t.Helper()
		if _, e := db.ExecContext(ctx, query); e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO persons(first_name,last_name) VALUES ('Legacy','One'),('Legacy','Two')")
	exec("INSERT INTO users(person_id,login_email,password_hash,is_active,created_at) SELECT id,'legacy'||id||'@example.test','preserved',id=1,'2020-01-01' FROM persons")
	exec("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2020','2020-01-01','2020-12-31')")
	exec("INSERT INTO membership_types(name) VALUES ('Old')")
	exec("INSERT INTO memberships(person_id,season_id,membership_type_id,status,created_at) VALUES (1,1,1,'pending','2020-02-01')")
	exec("INSERT INTO consent_definitions(code,version,title,description,is_active) VALUES ('old',1,'Old','Evidence',false),('new',1,'New','Not presented',true)")
	exec("INSERT INTO membership_consents(membership_id,consent_definition_id,decision,given_by_person_id) VALUES (1,1,'refused',1)")
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var ok bool
	err = db.QueryRowContext(ctx, `SELECT bool_and(username='legacy.'||id AND password_hash='preserved'
 AND activated_at=created_at AND is_active=(id=1) AND login_email='legacy'||id||'@example.test') FROM users`).Scan(&ok)
	if err != nil || !ok {
		t.Fatal("backfill", ok, err)
	}
	err = db.QueryRowContext(ctx, "SELECT requested_at=created_at AND approved_at IS NULL FROM memberships WHERE id=1").Scan(&ok)
	if err != nil || !ok {
		t.Fatal("membership backfill", ok, err)
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM membership_consent_requirements WHERE consent_definition_id=1").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM membership_consent_requirements WHERE consent_definition_id=2").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err = provider.DownTo(ctx, 15); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO persons(first_name,last_name) VALUES ('New','User')")
	exec("INSERT INTO users(person_id,username) VALUES (3,'new.user')")
	if _, err = provider.DownTo(ctx, 15); err == nil {
		t.Fatal("lossy rollback accepted")
	}
	if err = db.QueryRowContext(ctx, "SELECT to_regclass('user_activation_codes') IS NOT NULL").Scan(&ok); err != nil || !ok {
		t.Fatal("rollback not atomic", err)
	}
}

func TestUnactivatedRenewalAndSharedFamilyEmail(t *testing.T) {
	f := newMembershipFixture(t)
	parent := f.person("Parent", "1980-01-01")
	f.exec("UPDATE persons SET email='family@example.test' WHERE id=$1", parent)
	for range 2 {
		p := f.person("Child", "2020-01-01")
		f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES ($1,$2,'guardian')", p, parent)
		// A later guardian with a valid email must not replace the first fallback.
		f.guardian(p, "later@example.test", false)
		f.emergency(p, parent)
		r := f.request(p)
		r.Consents[0].GivenByPersonID = parent
		first := f.approve(f.create(r).ID)
		r.SeasonID = f.nextSeason()
		second := f.approve(f.create(r).ID)
		if first.User.ID != second.User.ID || second.ActivationDelivery == nil || *second.ActivationDelivery.RecipientEmail != "family@example.test" || *second.ActivationDelivery.RecipientPersonID != parent {
			t.Fatal("family channel or account reuse")
		}
		var invalidated, current int
		f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FILTER (WHERE invalidated_at IS NOT NULL),count(*) FILTER (WHERE invalidated_at IS NULL) FROM user_activation_codes WHERE user_id=$1", first.User.ID).Scan(&invalidated, &current))
		if invalidated != 1 || current != 1 {
			t.Fatal("code history", invalidated, current)
		}
	}
}

func TestApprovalRollbackAndDuplicateRace(t *testing.T) {
	f := newMembershipFixture(t)
	p := f.person("Atomic", "1990-01-01")
	m := f.create(f.request(p))
	// Force failure after account/code preparation, at the final membership update.
	f.exec(`CREATE FUNCTION reject_approval_test() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'forced approval failure'; END; $$`)
	f.exec("CREATE TRIGGER reject_approval_test BEFORE UPDATE ON memberships FOR EACH ROW EXECUTE FUNCTION reject_approval_test()")
	result, err := f.svc.ApproveMembership(t.Context(), m.ID, f.approver, nil)
	if err == nil || result.ActivationDelivery != nil {
		t.Fatal("failed approval returned delivery")
	}
	var count int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM users WHERE person_id=$1", p).Scan(&count))
	if count != 0 {
		t.Fatal("partial account")
	}
	var status string
	f.must(f.db.QueryRow(t.Context(), "SELECT status FROM memberships WHERE id=$1", m.ID).Scan(&status))
	if status != "pending" {
		t.Fatal(status)
	}
	f.exec("DROP TRIGGER reject_approval_test ON memberships")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = f.svc.ApproveMembership(t.Context(), m.ID, f.approver, nil) })
	}
	wg.Wait()
	successes := 0
	for _, e := range errs {
		if e == nil {
			successes++
		} else if !errors.Is(e, memberships.ErrNotPending) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatal("duplicate approval", errs)
	}
	d, err := f.svc.GetDetails(t.Context(), m.ID)
	f.must(err)
	if !d.Account.Exists || !d.Account.IsActive || d.Account.IsActivated || !d.Account.NeedsActivation {
		t.Fatal(d.Account)
	}
	for _, query := range []string{
		"DELETE FROM membership_consent_requirements WHERE membership_id=$1",
		"UPDATE membership_consent_requirements SET presented_at=now() WHERE membership_id=$1",
	} {
		_, err = f.db.Exec(t.Context(), query, m.ID)
		requirePostgresCode(t, err, "23514")
	}
}
