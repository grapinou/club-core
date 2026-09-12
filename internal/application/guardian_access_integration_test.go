package application

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/minorsafety"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

// Obtain context through real session middleware; no exported impersonation helper.
func (f *fixture) authenticatedContext(user int32) context.Context {
	f.t.Helper()
	sessions := auth.NewSessions(false)
	login, err := auth.New(dbsqlc.New(f.db))
	f.must(err)
	request := httptest.NewRequest("GET", "http://club.test/", nil).WithContext(f.t.Context())
	response := httptest.NewRecorder()
	f.must(sessions.Create(response, request, user))
	for _, cookie := range response.Result().Cookies() {
		request.AddCookie(cookie)
	}
	var ctx context.Context
	sessions.Middleware(login, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), request)
	if _, ok := auth.UserID(ctx); !ok {
		f.t.Fatal("missing authenticated context")
	}
	return ctx
}
func (f *fixture) guardianPair() (int32, int32) {
	child := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES ('Arthur','Famille','2011-09-12') RETURNING id")
	guardian := f.id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES ('Claire','Famille','1980-01-01','claire@example.test') RETURNING id")
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES ($1,$2,'mother')", child, guardian)
	return child, guardian
}
func (f *fixture) activeUser(person int32, username string) int32 {
	return f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,$2,'hash',now()) RETURNING id", person, username)
}
func (f *fixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	f.must(f.db.QueryRow(f.t.Context(), query, args...).Scan(&n))
	return n
}

func TestGuardianGrantLifecycleAndEffectiveAccess(t *testing.T) {
	f := newFixture(t)
	ctx := f.authenticatedContext(f.approver)
	child, parent := f.guardianPair()
	user := f.activeUser(parent, "claire")
	parentCtx := f.authenticatedContext(user)
	childUser := f.activeUser(child, "arthur") // Test fixture only; no workflow creates it.
	childCtx := f.authenticatedContext(childUser)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	g := guardianaccess.New(f.db, authorization.New(dbsqlc.New(f.db)), time.UTC, guardianaccess.WithClock(func() time.Time { return now }))
	check := func(want bool) {
		t.Helper()
		got, err := g.CanManageChild(parentCtx, child)
		f.must(err)
		if got != want {
			t.Fatalf("access=%v want %v", got, want)
		}
	}
	check(false)
	if _, err := g.Grant(parentCtx, child, parent); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("RBAC: %v", err)
	}
	if _, err := g.Grant(ctx, child, f.person); !errors.Is(err, guardianaccess.ErrIneligible) {
		t.Fatalf("missing relation: %v", err)
	}
	if _, err := g.Grant(ctx, child, child); !errors.Is(err, guardianaccess.ErrIneligible) {
		t.Fatalf("self: %v", err)
	}
	grant, err := g.Grant(ctx, child, parent)
	f.must(err)
	if !grant.GrantedByUserID.Valid || grant.GrantedByUserID.Int32 != f.approver || !grant.GrantedAt.Valid {
		t.Fatal("missing audit")
	}
	again, err := g.Grant(ctx, child, parent)
	f.must(err)
	if again.ID != grant.ID {
		t.Fatal("duplicate grant")
	}
	check(true)
	if got, err := g.CanManageChild(parentCtx, f.person); err != nil || got {
		t.Fatalf("resource scope: %v %v", got, err)
	}
	if got, err := g.CanManageChild(t.Context(), child); err != nil || got {
		t.Fatal("anonymous")
	}
	people, err := g.ListManagedChildren(parentCtx)
	f.must(err)
	if len(people) != 1 || people[0].PersonID != child {
		t.Fatalf("%+v", people)
	}
	transparent, err := g.ListActiveGuardiansForChild(childCtx, child)
	f.must(err)
	if len(transparent) != 1 || transparent[0].PersonID != parent || transparent[0].Relationship != "mother" {
		t.Fatalf("%+v", transparent)
	}
	if _, err = g.ListActiveGuardiansForChild(parentCtx, child); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("unauthorized listing")
	}
	for _, query := range []string{
		"DELETE FROM guardian_access_grants",
		"TRUNCATE guardian_access_grants",
		"UPDATE guardian_access_grants SET granted_at=granted_at + interval '1 second'",
		"DELETE FROM person_guardians WHERE child_person_id=$1",
	} {
		var err error
		if query == "DELETE FROM person_guardians WHERE child_person_id=$1" {
			_, err = f.db.Exec(t.Context(), query, child)
		} else {
			_, err = f.db.Exec(t.Context(), query)
		}
		if err == nil {
			t.Fatalf("audit protection: %s", query)
		}
	}
	// Multiple guardians, neither relying on primary_contact.
	second := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES ('Thomas','Famille','1980-01-01') RETURNING id")
	secondUser := f.activeUser(second, "thomas")
	secondCtx := f.authenticatedContext(secondUser)
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES ($1,$2,'father')", child, second)
	_, err = g.Grant(ctx, child, second)
	f.must(err)
	f.must(g.Revoke(ctx, child, parent))
	check(false)
	if got, err := g.CanManageChild(secondCtx, child); err != nil || !got {
		t.Fatal("independent grants")
	}
	transparent, err = g.ListActiveGuardiansForChild(childCtx, child)
	f.must(err)
	if len(transparent) != 1 {
		t.Fatal("revoked listed")
	}
	historical := f.count("SELECT count(*) FROM guardian_access_grants WHERE id=$1 AND revoked_at IS NOT NULL AND revoked_by_user_id=$2", grant.ID, f.approver)
	if historical != 1 {
		t.Fatal("revocation audit")
	}
	regrant, err := g.Grant(ctx, child, parent)
	f.must(err)
	if regrant.ID == grant.ID {
		t.Fatal("history overwritten")
	}
	check(true)
	for _, tc := range []struct {
		off, on string
		id      int32
	}{
		{"UPDATE persons SET archived_at=now() WHERE id=$1", "UPDATE persons SET archived_at=NULL WHERE id=$1", parent},
		{"UPDATE persons SET archived_at=now() WHERE id=$1", "UPDATE persons SET archived_at=NULL WHERE id=$1", child},
		{"UPDATE users SET is_active=false WHERE id=$1", "UPDATE users SET is_active=true WHERE id=$1", user},
	} {
		f.exec(tc.off, tc.id)
		check(false)
		f.exec(tc.on, tc.id)
		check(true)
	}
	f.exec("UPDATE persons SET birth_date=NULL WHERE id=$1", child)
	check(false)
	for _, tc := range []struct {
		birth, date string
		want        bool
	}{
		{"2008-09-13", "2026-09-12", true}, {"2008-09-13", "2026-09-13", false},
		{"2008-02-29", "2026-02-28", true}, {"2008-02-29", "2026-03-01", false},
	} {
		f.exec("UPDATE persons SET birth_date=$2 WHERE id=$1", child, tc.birth)
		now, _ = time.Parse("2006-01-02", tc.date)
		check(tc.want)
		b, _ := time.Parse("2006-01-02", tc.birth)
		if memberships.IsMinor(b, now) != tc.want {
			t.Fatal("majority drift")
		}
	}
	transparent, err = g.ListActiveGuardiansForChild(childCtx, child)
	f.must(err)
	if len(transparent) != 0 {
		t.Fatal("majority rights listed")
	}
	if f.count("SELECT count(*) FROM guardian_access_grants WHERE revoked_at IS NULL") != 2 {
		t.Fatal("majority destroyed history")
	}
	f.must(g.RemoveRelationship(ctx, child, parent))
	// Restore a minor birth date so refusal proves removal, not majority.
	f.exec("UPDATE persons SET birth_date='2011-09-12' WHERE id=$1", child)
	check(false)

	if f.count("SELECT count(*) FROM person_guardians WHERE child_person_id=$1 AND guardian_person_id=$2", child, parent) != 0 {
		t.Fatal("relation not removed")
	}
	if f.count("SELECT count(*) FROM guardian_access_grants WHERE guardian_person_id=$1", parent) != 2 {
		t.Fatal("history lost")
	}
}

func TestIndependentUserAndGuardianActivation(t *testing.T) {
	f := newFixture(t)
	ctx := f.authenticatedContext(f.approver)
	if _, err := f.app.Accounts.EnsureUserForPerson(t.Context(), f.person); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("anonymous provisioning")
	}
	user, err := f.app.Accounts.EnsureUserForPerson(ctx, f.person)
	f.must(err)
	if user.Username != "remi.dupont" {
		t.Fatalf("%+v", user)
	}
	duplicate := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES ('Rémi','Dupont','1990-01-01') RETURNING id")
	collision, err := f.app.Accounts.EnsureUserForPerson(ctx, duplicate)
	f.must(err)
	if collision.Username != "remi.dupont2" {
		t.Fatal(collision.Username)
	}
	reused, err := f.app.Accounts.EnsureUserForPerson(ctx, f.person)
	f.must(err)
	if reused.ID != user.ID {
		t.Fatal("not reused")
	}
	if f.count("SELECT count(*) FROM memberships") != 0 || len(f.mail.messages) != 0 {
		t.Fatal("unexpected membership/email")
	}
	f.exec("UPDATE users SET is_active=false WHERE id=$1", user.ID)
	reused, err = f.app.Accounts.EnsureUserForPerson(ctx, f.person)
	f.must(err)
	if reused.IsActive {
		t.Fatal("generic primitive reactivated")
	}
	child, parent := f.guardianPair()
	if _, err = f.app.Accounts.EnsureGuardianUser(ctx, child, parent); !errors.Is(err, guardianaccess.ErrIneligible) {
		t.Fatal("grant required", err)
	}
	_, err = f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	f.mail.check = func(msg mailer.Message) {
		if f.count("SELECT count(*) FROM users WHERE person_id=$1", parent) != 1 || f.count("SELECT count(*) FROM user_activation_codes c JOIN users u ON u.id=c.user_id WHERE u.person_id=$1", parent) != 1 {
			t.Fatal("SMTP before commit")
		}
	}
	result, err := f.app.Accounts.EnsureGuardianUser(ctx, child, parent)
	f.must(err)
	if result.DeliveryStatus != accounts.Sent || len(f.mail.messages) != 1 {
		t.Fatalf("%+v", result)
	}
	if f.count("SELECT count(*) FROM users WHERE person_id=$1", child) != 0 || f.count("SELECT count(*) FROM memberships") != 0 {
		t.Fatal("child account or membership created")
	}
	f.mail.check = nil
	// Missing own email never falls back to the child's email or another relative.
	f.exec("UPDATE persons SET email=NULL WHERE id=$1", parent)
	f.exec("UPDATE persons SET email='child@example.test' WHERE id=$1", child)
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES ($1,$2,'other')", parent, f.person)
	result, err = f.app.Accounts.EnsureGuardianUser(ctx, child, parent)
	f.must(err)
	if result.DeliveryStatus != accounts.NoChannel || len(f.mail.messages) != 1 {
		t.Fatalf("%+v", result)
	}
	f.exec("UPDATE persons SET email='invalid' WHERE id=$1", parent)
	result, err = f.app.Accounts.EnsureGuardianUser(ctx, child, parent)
	f.must(err)
	if result.DeliveryStatus != accounts.NoChannel {
		t.Fatal("invalid email delivered")
	}
	f.exec("UPDATE persons SET email='claire@example.test' WHERE id=$1", parent)
	result, err = f.app.Accounts.EnsureGuardianUser(ctx, child, parent)
	f.must(err)
	if result.DeliveryStatus != accounts.Sent {
		t.Fatal("activation not sent")
	}
	b := newBrowser(f.app.Handler)
	token := b.csrf(t, "/activate")
	form := activationForm(token, codeFrom(t, f.mail.messages[len(f.mail.messages)-1]), "guardian secure password")
	form.Set("username", "claire.famille")
	response := b.call("POST", "/activate", form)
	if response.Code != 303 {
		t.Fatalf("guardian activation: %d", response.Code)
	}
	response = b.call("POST", "/login", url.Values{"csrf_token": {token}, "username": {"claire.famille"}, "password": {"guardian secure password"}})
	if response.Code != 303 || response.Header().Get("Location") != "/" {
		t.Fatal("guardian login without membership")
	}
	guardianCtx := f.authenticatedContext(result.UserID)
	if allowed, e := f.app.GuardianAccess.CanManageChild(guardianCtx, child); e != nil || !allowed {
		t.Fatal("activated guardian access", e)
	}
	if f.count("SELECT count(*) FROM memberships") != 0 {
		t.Fatal("activation created membership")
	}

	result, err = f.app.Accounts.EnsureGuardianUser(ctx, child, parent)
	f.must(err)
	if result.DeliveryStatus != accounts.NotRequired {
		t.Fatal("activated user changed")
	}
	f.exec("UPDATE users SET is_active=false WHERE id=$1", result.UserID)
	if _, err = f.app.Accounts.EnsureGuardianUser(ctx, child, parent); !errors.Is(err, accounts.ErrDisabled) {
		t.Fatal("disabled guardian reactivated")
	}
	f.exec("UPDATE persons SET birth_date='1990-01-01' WHERE id=$1", child)
	if _, err = f.app.Accounts.EnsureGuardianUser(ctx, child, parent); !errors.Is(err, guardianaccess.ErrIneligible) {
		t.Fatal("adult child accepted")
	}

}

func TestGuardianConcurrency(t *testing.T) {
	f := newFixture(t)
	ctx := f.authenticatedContext(f.approver)
	child, parent := f.guardianPair()
	concurrent := func(a, b func() error) {
		t.Helper()
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, fn := range []func() error{a, b} {
			wg.Add(1)
			go func(fn func() error) { defer wg.Done(); <-start; errs <- fn() }(fn)
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			f.must(err)
		}
	}
	grant := func() error { _, err := f.app.GuardianAccess.Grant(ctx, child, parent); return err }
	concurrent(grant, grant)
	if f.count("SELECT count(*) FROM guardian_access_grants WHERE revoked_at IS NULL") != 1 {
		t.Fatal("duplicate grants")
	}
	concurrent(grant, func() error { return f.app.GuardianAccess.Revoke(ctx, child, parent) })
	if f.count("SELECT count(*) FROM guardian_access_grants WHERE revoked_at IS NULL") > 1 {
		t.Fatal("incoherent grants")
	}
	if f.count("SELECT count(*) FROM guardian_access_grants WHERE revoked_at IS NOT NULL") != 1 {
		t.Fatal("lost revocation")
	}
	ensure := func() error { _, err := f.app.Accounts.EnsureUserForPerson(ctx, parent); return err }
	concurrent(ensure, ensure)
	if f.count("SELECT count(*) FROM users WHERE person_id=$1", parent) != 1 {
		t.Fatal("duplicate users")
	}
	if f.count("SELECT count(*) FROM memberships") != 0 {
		t.Fatal("membership side effect")
	}
}

func TestMinorSafetyUsesAuthenticatedDatabaseFacts(t *testing.T) {
	f := newFixture(t)
	ctx := f.authenticatedContext(f.approver)
	child, parent := f.guardianPair()
	adult := f.activeUser(f.person, "adult")
	parentUser := f.activeUser(parent, "parent")
	childUser := f.activeUser(child, "child")
	adultCtx := f.authenticatedContext(adult)
	childCtx := f.authenticatedContext(childUser)
	check := func(ctx context.Context, ids []int32, want minorsafety.Decision, supervised bool) {
		t.Helper()
		got, err := f.app.MinorSafety.EvaluatePrivateConversation(ctx, ids)
		f.must(err)
		if got.Decision != want || got.Supervised != supervised || got.Basis != minorsafety.RuleClubCoreSafety {
			t.Fatalf("%+v", got)
		}
	}
	check(adultCtx, []int32{adult, childUser}, minorsafety.SupervisionRequired, false)
	check(childCtx, []int32{childUser, adult}, minorsafety.SupervisionRequired, false)
	check(childCtx, []int32{childUser, parentUser}, minorsafety.SupervisionRequired, false)
	_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	check(childCtx, []int32{childUser, parentUser}, minorsafety.Allowed, false)
	check(childCtx, []int32{childUser, parentUser, adult}, minorsafety.Allowed, true)
	check(t.Context(), []int32{childUser, parentUser}, minorsafety.Denied, false)
	check(adultCtx, []int32{childUser, parentUser}, minorsafety.Denied, false)
	check(childCtx, []int32{childUser, parentUser, parentUser}, minorsafety.Denied, false)
	f.exec("UPDATE users SET is_active=false WHERE id=$1", parentUser)
	check(childCtx, []int32{childUser, parentUser, adult}, minorsafety.Denied, false)
	f.exec("UPDATE users SET is_active=true WHERE id=$1", parentUser)
	f.must(f.app.GuardianAccess.Revoke(ctx, child, parent))
	check(childCtx, []int32{childUser, parentUser, adult}, minorsafety.SupervisionRequired, false)
	f.exec("UPDATE persons SET birth_date='2000-01-01' WHERE id=$1", child)
	check(childCtx, []int32{childUser, adult}, minorsafety.NotApplicable, false)
}

func TestGuardianMigrationRollbackProtectsHistory(t *testing.T) {
	f := newFixture(t)
	ctx := f.authenticatedContext(f.approver)
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.DownTo(t.Context(), 21)
	f.must(err)
	_, err = provider.Up(t.Context())
	f.must(err)
	child, parent := f.guardianPair()
	_, err = f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	if _, err = provider.DownTo(t.Context(), 21); err == nil {
		t.Fatal("active audit erased")
	}
	f.must(f.app.GuardianAccess.Revoke(ctx, child, parent))
	if _, err = provider.DownTo(t.Context(), 21); err == nil {
		t.Fatal("historical audit erased")
	}
	if f.count("SELECT count(*) FROM guardian_access_grants WHERE revoked_at IS NOT NULL") != 1 {
		t.Fatal("lost audit")
	}
}

func TestGuardianProvisionSingleConnection(t *testing.T) {
	f := newFixture(t)
	ctx := f.authenticatedContext(f.approver)
	child, parent := f.guardianPair()
	_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	cfg := f.db.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	f.must(err)
	defer pool.Close()
	permissions := authorization.New(dbsqlc.New(pool))
	guardians := guardianaccess.New(pool, permissions, time.UTC)
	activationService, err := activation.New(pool, time.Hour)
	f.must(err)
	service := accounts.New(pool, nil, activationService, f.mail, "club@example.test", "https://club.example.test", permissions)
	service.SetGuardianAccess(guardians)
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := service.EnsureGuardianUser(deadline, child, parent)
	f.must(err)
	if result.DeliveryStatus != accounts.Sent {
		t.Fatalf("%+v", result)
	}
}
