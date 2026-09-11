package application

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"golang.org/x/crypto/bcrypt"
)

type fakeMailer struct {
	messages []mailer.Message
	err      error
	check    func(mailer.Message)
}

func (f *fakeMailer) Send(_ context.Context, m mailer.Message) error {
	if f.check != nil {
		f.check(m)
	}
	f.messages = append(f.messages, m)
	return f.err
}

type fixture struct {
	t                                        *testing.T
	db                                       *pgxpool.Pool
	app                                      *Application
	mail                                     *fakeMailer
	memberships                              *memberships.Service
	person, approver, season, kind, activity int32
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("club_test"), postgres.WithUsername("club"), postgres.WithPassword("test"), postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	migrationDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer migrationDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, migrationDB, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &fixture{t: t, db: pool, mail: &fakeMailer{}}
	runtime := config.Runtime{BaseURL: "https://club.example.test", SecureCookies: true, ActivationValidity: time.Hour, Location: time.UTC, SMTP: mailer.SMTPConfig{From: "club@example.test"}}
	f.app, err = NewWithMailer(config.Config{SiteName: "Club Core"}, runtime, pool, f.mail)
	f.must(err)
	f.memberships, err = memberships.New(pool, time.Hour, time.UTC)
	f.must(err)
	f.person = f.id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES ('Rémi','Dupont','1990-01-01','remi@example.test') RETURNING id")
	admin := f.id("INSERT INTO persons(first_name,last_name) VALUES ('Admin','Club') RETURNING id")
	f.approver = f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'admin','preserved',now()) RETURNING id", admin)
	f.season = f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2026','2026-09-01','2027-08-31') RETURNING id")
	f.kind = f.id("INSERT INTO membership_types(name) VALUES ('Standard') RETURNING id")
	f.activity = f.id("INSERT INTO activities(name) VALUES ('Practice') RETURNING id")
	return f
}
func (f *fixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) id(query string, args ...any) int32 {
	f.t.Helper()
	var id int32
	f.must(f.db.QueryRow(f.t.Context(), query, args...).Scan(&id))
	return id
}
func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	_, err := f.db.Exec(f.t.Context(), query, args...)
	f.must(err)
}
func (f *fixture) request() dbsqlc.Membership {
	f.t.Helper()
	m, err := f.memberships.CreateRequest(f.t.Context(), memberships.Request{PersonID: f.person, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}})
	f.must(err)
	return m
}
func (f *fixture) approved() accounts.ApprovalResult {
	f.t.Helper()
	a, err := f.app.Accounts.ApproveMembership(f.t.Context(), f.request().ID, f.approver, nil)
	f.must(err)
	return a
}
func codeFrom(t *testing.T, m mailer.Message) string {
	t.Helper()
	found := regexp.MustCompile("Code d'activation : ([0-9]{20})").FindStringSubmatch(m.Text)
	if len(found) != 2 {
		t.Fatal("missing activation code")
	}
	return found[1]
}

type browser struct {
	handler http.Handler
	cookies map[string]*http.Cookie
	ip      string
}

func newBrowser(handler http.Handler) *browser {
	return &browser{handler: handler, cookies: map[string]*http.Cookie{}, ip: "192.0.2.1:12345"}
}
func (b *browser) call(method, path string, form url.Values) *httptest.ResponseRecorder {
	body := ""
	if form != nil {
		body = form.Encode()
	}
	request := httptest.NewRequest(method, "https://club.example.test"+path, strings.NewReader(body))
	request.RemoteAddr = b.ip
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "https://club.example.test")
	}
	for _, c := range b.cookies {
		request.AddCookie(c)
	}
	response := httptest.NewRecorder()
	b.handler.ServeHTTP(response, request)
	for _, c := range response.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return response
}
func (b *browser) csrf(t *testing.T, path string) string {
	t.Helper()
	response := b.call("GET", path, nil)
	if response.Code != 200 {
		t.Fatal("GET failed", response.Code)
	}
	found := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(found) != 2 {
		t.Fatal("missing CSRF token")
	}
	return found[1]
}
func activationForm(token, code, password string) url.Values {
	return url.Values{"csrf_token": {token}, "username": {"remi.dupont"}, "code": {code}, "password": {password}, "confirmation": {password}}
}
func TestFullActivationAndUsernameLogin(t *testing.T) {
	f := newFixture(t)
	m := f.request()
	// A separate pool connection must already see both approval and code at Send.
	f.mail.check = func(message mailer.Message) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		var status string
		f.must(f.db.QueryRow(ctx, "SELECT status FROM memberships WHERE id=$1", m.ID).Scan(&status))
		if status != "active" {
			t.Fatal("mail before committed approval")
		}
		digest := sha256.Sum256([]byte(codeFrom(t, message)))
		var exists bool
		f.must(f.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_activation_codes WHERE code_hash=$1)", digest[:]).Scan(&exists))
		if !exists {
			t.Fatal("mail before committed code")
		}
	}
	a, err := f.app.Accounts.ApproveMembership(t.Context(), m.ID, f.approver, nil)
	f.must(err)
	if a.DeliveryStatus != accounts.Sent || len(f.mail.messages) != 1 {
		t.Fatal("missing mail")
	}
	code := codeFrom(t, f.mail.messages[0])
	b := newBrowser(f.app.Handler)
	token := b.csrf(t, "/activate")
	response := b.call("POST", "/activate", activationForm(token, code, "a secure password"))
	if response.Code != 303 || response.Header().Get("Location") != "/login?activated=1" {
		t.Fatal("activation redirect", response.Code)
	}
	if strings.Contains(response.Body.String(), code) || strings.Contains(response.Body.String(), "a secure password") {
		t.Fatal("secret leak")
	}
	page := b.call("GET", "/login?activated=1", nil)
	if !strings.Contains(page.Body.String(), "Votre compte est activé.") {
		t.Fatal("missing success message")
	}
	user, err := dbsqlc.New(f.db).GetUserByUsername(t.Context(), "remi.dupont")
	f.must(err)
	if !user.ActivatedAt.Valid {
		t.Fatal("not activated")
	}
	f.must(bcrypt.CompareHashAndPassword([]byte(user.PasswordHash.String), []byte("a secure password")))
	// No current membership is required to log in.
	f.exec("UPDATE memberships SET status='ended',ended_at=current_date WHERE id=$1", m.ID)
	response = b.call("POST", "/login", url.Values{"csrf_token": {token}, "username": {"remi.dupont"}, "password": {"a secure password"}})
	if response.Code != 303 || response.Header().Get("Location") != "/" {
		t.Fatal("login")
	}
	cookie := b.cookies["__Host-club_session"]
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatal("unsafe session cookie")
	}
	page = b.call("GET", "/login", nil)
	if !strings.Contains(page.Body.String(), "Vous êtes connecté.") {
		t.Fatal("session not read by middleware")
	}
	old := cookie.Value
	response = b.call("POST", "/login", url.Values{"csrf_token": {token}, "username": {"remi.dupont"}, "password": {"a secure password"}})
	if response.Code != 303 || b.cookies["__Host-club_session"].Value == old {
		t.Fatal("session not rotated")
	}
	response = b.call("POST", "/logout", url.Values{"csrf_token": {token}})
	if response.Code != 303 || b.cookies["__Host-club_session"] != nil {
		t.Fatal("logout")
	}
	// Already activated account on renewal: no additional delivery.
	f.mail.check = nil
	f.season = f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2028','2028-09-01','2029-08-31') RETURNING id")
	renewal := f.approved()
	if renewal.UserID != a.UserID || renewal.DeliveryStatus != accounts.NotRequired || len(f.mail.messages) != 1 {
		t.Fatal("renewal delivery")
	}
}
func TestApprovalFailureAndResend(t *testing.T) {
	f := newFixture(t)
	smtpFailure := errors.New("SMTP failure with untrusted diagnostic")
	f.mail.err = smtpFailure
	m := f.request()
	a, err := f.app.Accounts.ApproveMembership(t.Context(), m.ID, f.approver, nil)
	var deliveryErr *accounts.DeliveryError
	if !errors.Is(err, smtpFailure) || !errors.As(err, &deliveryErr) || !deliveryErr.ApprovalCommitted || err.Error() != "Adhésion validée, mais email d'activation non envoyé." || a.DeliveryStatus != accounts.SendFailed {
		t.Fatal("wrong operational result")
	}
	var status string
	f.must(f.db.QueryRow(t.Context(), "SELECT status FROM memberships WHERE id=$1", m.ID).Scan(&status))
	if status != "active" {
		t.Fatal("SMTP rolled back membership")
	}
	oldCode := codeFrom(t, f.mail.messages[0])
	oldHash := sha256.Sum256([]byte(oldCode))
	f.exec("UPDATE persons SET email='changed@example.test' WHERE id=$1", f.person)
	f.mail.err = nil
	f.mail.check = func(message mailer.Message) {
		var invalidated bool
		f.must(f.db.QueryRow(t.Context(), "SELECT invalidated_at IS NOT NULL FROM user_activation_codes WHERE code_hash=$1", oldHash[:]).Scan(&invalidated))
		if !invalidated {
			t.Fatal("resend before commit")
		}
		if message.To != "changed@example.test" {
			t.Fatal("recipient not resolved again")
		}
	}
	resend, err := f.app.Accounts.ResendActivation(t.Context(), a.UserID)
	f.must(err)
	if resend.DeliveryStatus != accounts.Sent || len(f.mail.messages) != 2 {
		t.Fatal("resend")
	}
	f.mail.check = nil
	f.exec("UPDATE users SET is_active=false WHERE id=$1", a.UserID)
	if _, err = f.app.Accounts.ResendActivation(t.Context(), a.UserID); !errors.Is(err, accounts.ErrDisabled) {
		t.Fatal("disabled accepted")
	}
	f.exec("UPDATE users SET is_active=true,activated_at=now(),password_hash='preserved' WHERE id=$1", a.UserID)
	if _, err = f.app.Accounts.ResendActivation(t.Context(), a.UserID); !errors.Is(err, accounts.ErrAlreadyActivated) {
		t.Fatal("activated accepted")
	}
	if len(f.mail.messages) != 2 {
		t.Fatal("unexpected resend")
	}
}
func TestNoEmailAndCommitFailure(t *testing.T) {
	f := newFixture(t)
	f.exec("UPDATE persons SET email=NULL WHERE id=$1", f.person)
	a := f.approved()
	if a.DeliveryStatus != accounts.NoChannel || a.Membership.Status != "active" || len(f.mail.messages) != 0 {
		t.Fatal("no channel approval")
	}
	result, err := f.app.Accounts.ResendActivation(t.Context(), a.UserID)
	f.must(err)
	if result.DeliveryStatus != accounts.NoChannel || len(f.mail.messages) != 0 {
		t.Fatal("no channel resend")
	}
	guardian := f.id("INSERT INTO persons(first_name,last_name,email) VALUES ('Parent','Dupont','parent@example.test') RETURNING id")
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES ($1,$2,'guardian',true)", f.person, guardian)
	result, err = f.app.Accounts.ResendActivation(t.Context(), a.UserID)
	f.must(err)
	if result.DeliveryStatus != accounts.Sent || f.mail.messages[0].To != "parent@example.test" {
		t.Fatal("guardian fallback")
	}
	f.exec("UPDATE persons SET email=NULL WHERE id=$1", guardian)
	other := f.id("INSERT INTO persons(first_name,last_name,email) VALUES ('Other','Guardian','other@example.test') RETURNING id")
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES ($1,$2,'guardian')", f.person, other)
	result, err = f.app.Accounts.ResendActivation(t.Context(), a.UserID)
	f.must(err)
	if result.DeliveryStatus != accounts.Sent || f.mail.messages[1].To != "other@example.test" {
		t.Fatal("other guardian fallback")
	}
	f.season = f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2028','2028-09-01','2029-08-31') RETURNING id")
	m := f.request()
	f.exec(`CREATE FUNCTION fail_commit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced commit failure'; END; $$`)
	f.exec("CREATE CONSTRAINT TRIGGER fail_commit_test AFTER UPDATE ON memberships DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_commit_test()")
	_, err = f.app.Accounts.ApproveMembership(t.Context(), m.ID, f.approver, nil)
	if err == nil || len(f.mail.messages) != 2 {
		t.Fatal("delivery despite failed commit")
	}
	var status string
	f.must(f.db.QueryRow(t.Context(), "SELECT status FROM memberships WHERE id=$1", m.ID).Scan(&status))
	if status != "pending" {
		t.Fatal("commit failure not rolled back")
	}
}
func TestActivationHTTPGenericFailures(t *testing.T) {
	f := newFixture(t)
	a := f.approved()
	cases := []string{"confirmation", "short password", "wrong", "expired", "invalidated", "used", "unknown username"}
	for i, state := range cases {
		t.Run(state, func(t *testing.T) {
			_, err := f.app.Accounts.ResendActivation(t.Context(), a.UserID)
			f.must(err)
			code := codeFrom(t, f.mail.messages[len(f.mail.messages)-1])
			b := newBrowser(f.app.Handler)
			b.ip = fmt.Sprintf("192.0.2.%d:1234", i+1)
			token := b.csrf(t, "/activate")
			form := activationForm(token, code, "a secure password")
			switch state {
			case "confirmation":
				form.Set("confirmation", "different password")
			case "short password":
				form.Set("password", "short")
				form.Set("confirmation", "short")
			case "wrong":
				form.Set("code", "wrong-private-code")
			case "unknown username":
				form.Set("username", "unknown.private")
			case "expired":
				f.exec("UPDATE user_activation_codes SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE user_id=$1", a.UserID)
			case "invalidated":
				f.exec("UPDATE user_activation_codes SET invalidated_at=now() WHERE user_id=$1", a.UserID)
			case "used":
				f.exec("UPDATE user_activation_codes SET used_at=now() WHERE user_id=$1", a.UserID)
			}
			response := b.call("POST", "/activate", form)
			if response.Code != 303 || response.Header().Get("Location") != "/activate?error=1" {
				t.Fatal("inconsistent public failure")
			}
			page := b.call("GET", "/activate?error=1", nil)
			if !strings.Contains(page.Body.String(), "Impossible d&#39;activer le compte avec ces informations.") {
				t.Fatal("missing generic message")
			}
			for _, secret := range []string{code, "a secure password", "wrong-private-code", "unknown.private"} {
				if strings.Contains(response.Body.String()+page.Body.String()+response.Header().Get("Location"), secret) {
					t.Fatal("response leaks input")
				}
			}
			user, e := dbsqlc.New(f.db).GetUserByID(t.Context(), a.UserID)
			f.must(e)
			if user.ActivatedAt.Valid || user.PasswordHash.Valid {
				t.Fatal("failed activation changed account")
			}
		})
	}
}
func TestUsernameLoginFailuresAndIndependentUser(t *testing.T) {
	f := newFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	uid := f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'independent',$2,now()) RETURNING id", f.person, string(hash))
	login, err := auth.New(dbsqlc.New(f.db))
	f.must(err)
	id, err := login.Authenticate(t.Context(), "independent", "a secure password")
	f.must(err)
	if id != uid {
		t.Fatal("login without any membership")
	}
	for i, state := range []string{"unknown", "wrong password", "disabled", "unactivated", "null hash", "email"} {
		t.Run(state, func(t *testing.T) {
			f.exec("UPDATE users SET is_active=true,activated_at=now(),password_hash=$2 WHERE id=$1", uid, string(hash))
			username, password := "independent", "a secure password"
			switch state {
			case "unknown":
				username = "missing"
			case "wrong password":
				password = "wrong password"
			case "disabled":
				f.exec("UPDATE users SET is_active=false WHERE id=$1", uid)
			case "unactivated":
				f.exec("UPDATE users SET activated_at=NULL WHERE id=$1", uid)
			case "null hash":
				f.exec("UPDATE users SET password_hash=NULL WHERE id=$1", uid)
			case "email":
				username = "remi@example.test"
			}
			b := newBrowser(f.app.Handler)
			b.ip = fmt.Sprintf("192.0.2.%d:1234", i+10)
			token := b.csrf(t, "/login")
			response := b.call("POST", "/login", url.Values{"csrf_token": {token}, "username": {username}, "password": {password}})
			if response.Code != 303 || response.Header().Get("Location") != "/login?error=1" || b.cookies["__Host-club_session"] != nil {
				t.Fatal("invalid login accepted")
			}
			page := b.call("GET", "/login?error=1", nil)
			if !strings.Contains(page.Body.String(), "Identifiant ou mot de passe incorrect.") {
				t.Fatal("missing generic login message")
			}
		})
	}
}
