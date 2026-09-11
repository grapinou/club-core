package application

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/clubctl"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/pressly/goose/v3"
	"golang.org/x/crypto/bcrypt"
)

func (f *fixture) loginBrowser(username string) *browser {
	f.t.Helper()
	b := newBrowser(f.app.Handler)
	token := b.csrf(f.t, "/login")
	response := b.call("POST", "/login", map[string][]string{"csrf_token": {token}, "username": {username}, "password": {"a secure password"}})
	if response.Code != 303 || response.Header().Get("Location") != "/" {
		f.t.Fatal("test login failed")
	}
	return b
}
func (f *fixture) personSnapshot() string {
	f.t.Helper()
	var value string
	f.must(f.db.QueryRow(f.t.Context(), "SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY id)::text,'[]') FROM persons p").Scan(&value))
	return value
}
func TestPersonsRoutePermissionMatrix(t *testing.T) {
	f := newFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	for _, role := range []string{"anonymous", "none", "treasurer", "coach", "secretary", "president"} {
		t.Run(role, func(t *testing.T) {
			b := newBrowser(f.app.Handler)
			allowed := role == "secretary" || role == "president"
			if role != "anonymous" {
				person := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES ($1,'Actor','1990-01-01') RETURNING id", role)
				user := f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,$2,$3,now()) RETURNING id", person, role, string(hash))
				if role != "none" {
					f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name=$2", user, role)
				}
				if role == "none" {
					f.exec("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES ($1,$2,$3,'active')", person, f.season, f.kind)
				}
				b = f.loginBrowser(role)
			}
			home := b.call("GET", "/", nil)
			if home.Code != 200 || strings.Contains(home.Body.String(), `href="/persons"`) != allowed {
				t.Fatal("navigation policy")
			}
			for _, path := range []string{"/persons", "/persons/new", fmt.Sprintf("/persons/%d/edit", f.person), "/persons/archived"} {
				response := b.call("GET", path, nil)
				want := 403
				if role == "anonymous" {
					want = 303
				} else if allowed {
					want = 200
				}
				if response.Code != want {
					t.Fatalf("%s: %d want %d", path, response.Code, want)
				}
				if role == "anonymous" && response.Header().Get("Location") != "/login" {
					t.Fatal("anonymous redirect")
				}
				if !allowed && strings.Contains(response.Body.String(), "remi@example.test") {
					t.Fatal("personal data leaked")
				}
				if allowed && !strings.Contains(response.Body.String(), `name="csrf_token"`) && path != "/persons/archived" {
					t.Fatal("missing form token", path)
				}
			}
			token := b.csrf(t, "/login")
			for _, path := range []string{"/persons", fmt.Sprintf("/persons/%d/edit", f.person), fmt.Sprintf("/persons/%d/archive", f.person), fmt.Sprintf("/persons/%d/restore", f.person)} {
				for _, validCSRF := range []bool{false, true} {
					form := map[string][]string{"FirstName": {role + "Changed"}, "LastName": {"Target"}, "Birthdate": {"1990-01-01"}, "csrf_token": {"invalid"}}
					if validCSRF {
						form["csrf_token"] = []string{token}
					}
					before := f.personSnapshot()
					response := b.call("POST", path, form)
					want := 403
					if role == "anonymous" {
						want = 303
					} else if allowed && validCSRF {
						want = 303
					}
					if response.Code != want {
						t.Fatalf("%s csrf=%v: %d want %d", path, validCSRF, response.Code, want)
					}
					after := f.personSnapshot()
					if (!allowed || !validCSRF) && before != after {
						t.Fatal("unauthorized mutation")
					}
					if allowed && validCSRF && before == after {
						t.Fatal("authorized mutation missing", path)
					}
				}
			}
		})
	}
}
func TestRevocationKeepsSession(t *testing.T) {
	f := newFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	user := f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'secretary',$2,now()) RETURNING id", f.person, string(hash))
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", user)
	b := f.loginBrowser("secretary")
	if response := b.call("GET", "/persons", nil); response.Code != 200 {
		t.Fatal("initial access")
	}
	cookie := b.cookies["__Host-club_session"].Value
	f.exec("DELETE FROM user_roles WHERE user_id=$1", user)
	if response := b.call("GET", "/persons", nil); response.Code != 403 {
		t.Fatal("revocation not immediate")
	}
	page := b.call("GET", "/login", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Vous êtes connecté.") || b.cookies["__Host-club_session"].Value != cookie {
		t.Fatal("role revoked session")
	}
	if strings.Contains(page.Body.String(), `href="/persons"`) {
		t.Fatal("stale navigation")
	}
	f.exec("UPDATE users SET is_active=false WHERE id=$1", user)
	if response := b.call("GET", "/persons", nil); response.Code != 303 {
		t.Fatal("disabled user remains authenticated")
	}
}
func TestPublicRoutesAndLogout(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	for _, path := range []string{"/", "/club", "/contact", "/where", "/when", "/rules", "/login", "/activate"} {
		if r := b.call("GET", path, nil); r.Code != 200 {
			t.Fatal("public route", path, r.Code)
		}
	}
	// FileServer paths are relative to the process directory, as in production.
	static, err := os.ReadFile("../../static/css/style.css")
	f.must(err)
	t.Chdir("../..")
	r := b.call("GET", "/static/css/style.css", nil)
	if r.Code != 200 || r.Body.String() != string(static) {
		t.Fatal("public static")
	}
	if r = b.call("POST", "/logout", nil); r.Code != 303 || r.Header().Get("Location") != "/login" {
		t.Fatal("anonymous logout")
	}
}
func TestAuthorizedAccountUseCases(t *testing.T) {
	f := newFixture(t)
	m := f.request()
	// Existing approver is a president. Removing its role denies before any write.
	f.exec("DELETE FROM user_roles WHERE user_id=$1", f.approver)
	_, err := f.app.Accounts.ApproveMembership(t.Context(), m.ID, f.approver, nil)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("unprivileged approval", err)
	}
	var status string
	f.must(f.db.QueryRow(t.Context(), "SELECT status FROM memberships WHERE id=$1", m.ID).Scan(&status))
	if status != "pending" || len(f.mail.messages) != 0 {
		t.Fatal("unauthorized approval side effects")
	}
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", f.approver)
	a, err := f.app.Accounts.ApproveMembership(t.Context(), m.ID, f.approver, nil)
	f.must(err)
	if a.DeliveryStatus != accounts.Sent {
		t.Fatal("secretary approval")
	}
	var count int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM user_activation_codes WHERE user_id=$1", a.UserID).Scan(&count))
	f.exec("DELETE FROM user_roles WHERE user_id=$1", f.approver)
	if _, err = f.app.Accounts.ResendActivation(t.Context(), f.approver, a.UserID); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("unprivileged resend")
	}
	var after int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM user_activation_codes WHERE user_id=$1", a.UserID).Scan(&after))
	if after != count || len(f.mail.messages) != 1 {
		t.Fatal("unauthorized resend side effects")
	}
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", f.approver)
	resend, err := f.app.Accounts.ResendActivation(t.Context(), f.approver, a.UserID)
	f.must(err)
	if resend.DeliveryStatus != accounts.Sent {
		t.Fatal("secretary resend")
	}
	f.exec("UPDATE users SET is_active=false WHERE id=$1", f.approver)
	if _, err = f.app.Accounts.ResendActivation(t.Context(), f.approver, a.UserID); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("disabled operator")
	}
}
func TestClubctlAndRolesMigration(t *testing.T) {
	f := newFixture(t)
	user := f.id("INSERT INTO users(person_id,username) VALUES ($1,'bootstrap') RETURNING id", f.person)
	q := dbsqlc.New(f.db)
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := clubctl.Run(t.Context(), q, args, &out)
		return out.String(), err
	}
	out, err := run("list-roles", "bootstrap")
	f.must(err)
	if out != "Aucun rôle attribué.\n" {
		t.Fatal(out)
	}
	for range 2 {
		out, err = run("grant-role", "bootstrap", "president")
		f.must(err)
		if !strings.Contains(out, "attribué") {
			t.Fatal(out)
		}
	}
	roles, err := q.ListUserRoles(t.Context(), user)
	f.must(err)
	if len(roles) != 1 || roles[0].Name != "president" {
		t.Fatal(roles)
	}
	_, err = run("grant-role", "bootstrap", "secretary")
	f.must(err)
	out, err = run("list-roles", "bootstrap")
	f.must(err)
	if out != "president\nsecretary\n" {
		t.Fatal(out)
	}
	for range 2 {
		_, err = run("revoke-role", "bootstrap", "president")
		f.must(err)
	}
	out, err = run("list-roles", "bootstrap")
	f.must(err)
	if out != "secretary\n" {
		t.Fatal(out)
	}
	for _, command := range []string{"grant-role", "revoke-role"} {
		if _, err = run(command, "missing", "president"); !errors.Is(err, clubctl.ErrUnknownUser) {
			t.Fatal(err)
		}
		if _, err = run(command, "bootstrap", "member"); !errors.Is(err, clubctl.ErrUnknownRole) {
			t.Fatal(err)
		}
	}
	if _, err = run("list-roles", "missing"); !errors.Is(err, clubctl.ErrUnknownUser) {
		t.Fatal(err)
	}
	if _, err = run("invalid", "bootstrap"); !errors.Is(err, clubctl.ErrUsage) {
		t.Fatal(err)
	}
	// Previously existing reference data and assignments must survive reapplication.
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.DownTo(t.Context(), 16)
	f.must(err)
	f.exec("INSERT INTO roles(name) VALUES ('historical_role')")
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='historical_role'", user)
	_, err = provider.Up(t.Context())
	f.must(err)
	available, err := q.ListAvailableRoles(t.Context())
	f.must(err)
	if len(available) != 5 {
		t.Fatal("duplicate or lost roles", available)
	}
	roles, err = q.ListUserRoles(t.Context(), user)
	f.must(err)
	if len(roles) != 2 {
		t.Fatal("lost assignments", roles)
	}
}
