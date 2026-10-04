package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/passwordreset"
	"golang.org/x/crypto/bcrypt"
)

func resetToken(t *testing.T, m mailer.Message) string {
	t.Helper()
	matches := regexp.MustCompile(`https://club.example.test/password/reset\?token=([0-9a-f]{64})`).FindStringSubmatch(m.Text)
	if len(matches) != 2 {
		t.Fatal("missing password reset link")
	}
	return matches[1]
}
func (f *fixture) resetCount(where string, args ...any) int {
	f.t.Helper()
	var n int
	f.must(f.db.QueryRowContext(f.t.Context(), `SELECT count(*) FROM user_password_reset_requests WHERE `+where, args...).Scan(&n))
	return n
}
func (f *fixture) prepareReset(username string) string {
	f.t.Helper()
	f.must(f.app.PasswordReset.RequestUsername(f.t.Context(), username))
	return resetToken(f.t, f.mail.messages[len(f.mail.messages)-1])
}
func resetForm(csrf, token, password, confirmation string) url.Values {
	return url.Values{"csrf_token": {csrf}, "token": {token}, "password": {password}, "confirmation": {confirmation}}
}

func TestPasswordResetEligibilityRecipientAndRotation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter string
		want  error
	}{
		{"eligible", "", nil}, {"unknown", `DELETE FROM users WHERE username='member'`, passwordreset.ErrIneligible},
		{"disabled", `UPDATE users SET is_active=FALSE WHERE username='member'`, passwordreset.ErrIneligible},
		{"unactivated", `UPDATE users SET activated_at=NULL WHERE username='member'`, passwordreset.ErrIneligible},
		{"no password", `UPDATE users SET password_hash=NULL WHERE username='member'`, passwordreset.ErrIneligible},
		{"no channel", `UPDATE persons SET email=NULL`, passwordreset.ErrNoChannel},
		{"invalid email", `UPDATE persons SET email='not an email'`, passwordreset.ErrNoChannel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			user, _ := f.personalBrowser(f.person, "member")
			f.exec(`UPDATE users SET login_email='stale@example.test' WHERE id=?`, user)
			if tc.alter != "" {
				f.exec(tc.alter)
			}
			err := f.app.PasswordReset.RequestUsername(t.Context(), "member")
			if !errors.Is(err, tc.want) {
				t.Fatal("eligibility", err)
			}
			if tc.want != nil {
				if len(f.mail.messages) != 0 || f.resetCount("1=1") != 0 {
					t.Fatal("ineligible account received a reset")
				}
				return
			}
			first := resetToken(t, f.mail.messages[0])
			m := f.mail.messages[0]
			if m.To != "remi@example.test" || m.Subject != "Réinitialisation de votre mot de passe Club Core" || !strings.Contains(m.Text, "30 minutes") || !strings.Contains(m.Text, "ignorer ce message") || strings.Contains(m.Text, "a secure password") {
				t.Fatal("recovery email")
			}
			var hash []byte
			f.must(f.db.QueryRow(`SELECT token_hash FROM user_password_reset_requests`).Scan(&hash))
			want := sha256.Sum256([]byte(first))
			if string(hash) != string(want[:]) || strings.Contains(string(hash), first) {
				t.Fatal("plaintext token persisted")
			}
			second := f.prepareReset("member")
			if first == second || !errors.Is(f.app.PasswordReset.Validate(t.Context(), first), passwordreset.ErrInvalidToken) || f.app.PasswordReset.Validate(t.Context(), second) != nil || f.resetCount("invalidated_at IS NOT NULL") != 1 {
				t.Fatal("rotation")
			}
			f.prepareReset("member")
			if err = f.app.PasswordReset.RequestUsername(t.Context(), "member"); !errors.Is(err, passwordreset.ErrLimited) || len(f.mail.messages) != 3 {
				t.Fatal("account budget", err)
			}
		})
	}
	t.Run("family channel", func(t *testing.T) {
		f := newFixture(t)
		f.personalBrowser(f.person, "member")
		f.exec(`UPDATE persons SET email=NULL WHERE id=?`, f.person)
		secondary := f.id(`INSERT INTO persons(first_name,last_name,email) VALUES('Autre','Responsable','secondary@example.test') RETURNING id`)
		primary := f.id(`INSERT INTO persons(first_name,last_name,email) VALUES('Premier','Responsable','primary@example.test') RETURNING id`)
		f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES(?,?,'other',FALSE),(?,?,'other',TRUE)`, f.person, secondary, f.person, primary)
		f.prepareReset("member")
		if f.mail.messages[0].To != "primary@example.test" {
			t.Fatal("family recipient")
		}
	})
	t.Run("SMTP failure invalidates link", func(t *testing.T) {
		f := newFixture(t)
		f.personalBrowser(f.person, "member")
		f.mail.err = errors.New("private SMTP failure")
		if err := f.app.PasswordReset.RequestUsername(t.Context(), "member"); !errors.Is(err, passwordreset.ErrDelivery) {
			t.Fatal(err)
		}
		if f.resetCount("invalidated_at IS NOT NULL") != 1 {
			t.Fatal("failed delivery link remains active")
		}
	})
}

func TestPasswordResetAtomicLifecycleAndSessions(t *testing.T) {
	var logs bytes.Buffer
	previousLogWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogWriter)
	f := newFixture(t)
	user, first := f.personalBrowser(f.person, "member")
	second := f.loginBrowser("member")
	m := f.request()
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?,id FROM roles WHERE name='coach'`, user)
	personBefore := f.personSnapshot()
	q := dbsqlc.New(f.db)
	membershipBefore, err := q.GetMembership(t.Context(), m.ID)
	f.must(err)
	rolesBefore, err := q.ListUserRoles(t.Context(), user)
	f.must(err)
	emailHash := sha256.Sum256([]byte("new@example.test"))
	codeHash := sha256.Sum256([]byte("code"))
	f.exec(`INSERT INTO user_email_change_requests(user_id,person_id,new_email,new_email_normalized,new_email_hash,code_hash,expires_at) VALUES(?,?,'new@example.test','new@example.test',?,?,strftime('%Y-%m-%d %H:%M:%f','now','+1 hour'))`, user, f.person, emailHash[:], codeHash[:])
	token := f.prepareReset("member")
	anon := newBrowser(f.app.Handler)
	csrf := anon.csrf(t, "/password/reset?token="+token)
	newPassword := "a different secure password"
	r := anon.call("POST", "/password/reset", resetForm(csrf, token, newPassword, newPassword))
	if r.Code != 303 || r.Header().Get("Location") != "/login?password_reset=1" {
		t.Fatal("reset failed", r.Code)
	}
	if r.Header().Get("Cache-Control") != "no-store" || r.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("reset page headers")
	}
	for _, c := range r.Result().Cookies() {
		if strings.Contains(c.Name, "session") {
			t.Fatal("automatic login")
		}
	}
	u, err := q.GetUserByID(t.Context(), user)
	f.must(err)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash.String), []byte(newPassword)) != nil || u.PasswordHash.String == newPassword || !u.IsActive {
		t.Fatal("stored password")
	}
	login, err := auth.New(q)
	f.must(err)
	if _, err = login.Authenticate(t.Context(), "member", "a secure password"); err == nil {
		t.Fatal("old password accepted")
	}
	if _, err = login.Authenticate(t.Context(), "member", newPassword); err != nil {
		t.Fatal("new password rejected")
	}
	for _, b := range []*browser{first, second} {
		if r = b.call("GET", "/me/account/password", nil); r.Code != 303 || r.Header().Get("Location") != "/login" {
			t.Fatal("old session survived")
		}
	}
	if err = f.app.PasswordReset.Reset(t.Context(), token, newPassword, newPassword); !errors.Is(err, passwordreset.ErrInvalidToken) {
		t.Fatal("reused token")
	}
	var n int
	for _, query := range []string{
		`SELECT count(*) FROM account_security_events WHERE user_id=? AND event='password_changed'`,
		`SELECT count(*) FROM user_email_change_requests WHERE user_id=? AND invalidated_at IS NOT NULL`,
		`SELECT count(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=? AND r.name='coach'`,
	} {
		f.must(f.db.QueryRow(query, user).Scan(&n))
		if n != 1 {
			t.Fatal("reset side effect", n)
		}
	}
	var personID, status string
	f.must(f.db.QueryRow(`SELECT person_id,status FROM memberships WHERE id=?`, m.ID).Scan(&personID, &status))
	if personID != fmt.Sprint(f.person) || status != "pending" {
		t.Fatal("membership changed")
	}
	if f.personEmail(f.person) != "remi@example.test" {
		t.Fatal("email changed")
	}
	membershipAfter, err := q.GetMembership(t.Context(), m.ID)
	f.must(err)
	rolesAfter, err := q.ListUserRoles(t.Context(), user)
	f.must(err)
	if f.personSnapshot() != personBefore || !reflect.DeepEqual(membershipBefore, membershipAfter) || !reflect.DeepEqual(rolesBefore, rolesAfter) {
		t.Fatal("reset changed person, membership or roles")
	}
	if f.resetCount("used_at IS NOT NULL") != 1 || len(f.mail.messages) != 2 || !strings.Contains(f.mail.messages[1].Text, "contactez le club") || strings.Contains(f.mail.messages[1].Text, token) || strings.Contains(f.mail.messages[1].Text, newPassword) {
		t.Fatal("post-reset notification")
	}
	page := anon.call("GET", "/login?password_reset=1", nil).Body.String()
	if !strings.Contains(page, "Votre mot de passe a été modifié") || !strings.Contains(page, "Mot de passe oublié ?") {
		t.Fatal("login notice")
	}
	f.assertNoDeliverySecrets(page)
	var securityAudit string
	f.must(f.db.QueryRow(`SELECT coalesce(json_group_array(json_object('user',user_id,'person',person_id,'event',event,'created_at',created_at)),'[]') FROM account_security_events`).Scan(&securityAudit))
	for _, output := range []string{logs.String(), securityAudit, page} {
		if strings.Contains(output, token) || strings.Contains(output, newPassword) {
			t.Fatal("reset secret in logs, audit or confirmation")
		}
	}
}

func TestPasswordResetTokenAndPasswordErrors(t *testing.T) {
	for _, state := range []string{"incorrect", "expired", "used", "invalidated", "disabled", "unactivated", "no password"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			user, _ := f.personalBrowser(f.person, "member")
			token := f.prepareReset("member")
			switch state {
			case "incorrect":
				token = strings.Repeat("0", 64)
			case "expired":
				f.exec(`UPDATE user_password_reset_requests SET created_at=strftime('%Y-%m-%d %H:%M:%f','now','-2 hours'),expires_at=strftime('%Y-%m-%d %H:%M:%f','now','-1 hour')`)
			case "used":
				f.exec(`UPDATE user_password_reset_requests SET used_at=strftime('%Y-%m-%d %H:%M:%f','now')`)
			case "invalidated":
				f.exec(`UPDATE user_password_reset_requests SET invalidated_at=strftime('%Y-%m-%d %H:%M:%f','now')`)
			case "disabled":
				f.exec(`UPDATE users SET is_active=FALSE WHERE id=?`, user)
			case "unactivated":
				f.exec(`UPDATE users SET activated_at=NULL WHERE id=?`, user)
			case "no password":
				f.exec(`UPDATE users SET password_hash=NULL WHERE id=?`, user)
			}
			if !errors.Is(f.app.PasswordReset.Validate(t.Context(), token), passwordreset.ErrInvalidToken) || !errors.Is(f.app.PasswordReset.Reset(t.Context(), token, "new valid password", "new valid password"), passwordreset.ErrInvalidToken) {
				t.Fatal("invalid token accepted")
			}
			b := newBrowser(f.app.Handler)
			r := b.call("GET", "/password/reset?token="+token, nil)
			if r.Code != 422 || strings.Contains(r.Body.String(), token) || !strings.Contains(r.Body.String(), "Ce lien est invalide") {
				t.Fatal("invalid token UI")
			}
		})
	}
	f := newFixture(t)
	f.personalBrowser(f.person, "member")
	token := f.prepareReset("member")
	b := newBrowser(f.app.Handler)
	csrf := b.csrf(t, "/password/reset?token="+token)
	for _, tc := range []struct{ pw, confirm string }{{"short", "short"}, {strings.Repeat("a", 73), strings.Repeat("a", 73)}, {strings.Repeat("é", 37), strings.Repeat("é", 37)}, {"a valid password", "different password"}} {
		r := b.call("POST", "/password/reset", resetForm(csrf, token, tc.pw, tc.confirm))
		if r.Code != 422 || !strings.Contains(r.Body.String(), "de 12 à 72 octets") {
			t.Fatal("password validation", r.Code)
		}
		if f.app.PasswordReset.Validate(t.Context(), token) != nil {
			t.Fatal("validation consumed token")
		}
	}
	f.mail.err = errors.New("notification unavailable")
	pw := strings.Repeat("é", 6) // exactly 12 bytes; no second character-count policy
	if err := f.app.PasswordReset.Reset(t.Context(), token, pw, pw); err != nil {
		t.Fatal("notification rolled back password", err)
	}
}

func TestPasswordResetConcurrentConsumptionAndRollback(t *testing.T) {
	f := newFixture(t)
	f.personalBrowser(f.person, "member")
	token := f.prepareReset("member")
	var successes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := f.app.PasswordReset.Reset(t.Context(), token, "new valid password", "new valid password")
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, passwordreset.ErrInvalidToken) {
				t.Error("concurrent reset", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 || f.resetCount("used_at IS NOT NULL") != 1 {
		t.Fatal("double consumption")
	}
	token = f.prepareReset("member")
	f.exec(`CREATE TRIGGER reject_reset_audit BEFORE INSERT ON account_security_events BEGIN SELECT RAISE(ABORT,'test audit failure'); END;`)
	if err := f.app.PasswordReset.Reset(t.Context(), token, "another secure password", "another secure password"); err == nil {
		t.Fatal("partial reset committed")
	}
	if f.app.PasswordReset.Validate(t.Context(), token) != nil {
		t.Fatal("rollback consumed token")
	}
	login, err := auth.New(dbsqlc.New(f.db))
	f.must(err)
	if _, err = login.Authenticate(t.Context(), "member", "new valid password"); err != nil {
		t.Fatal("rollback changed password")
	}
}

func TestAdministrativePasswordResetPermissionAndFeedback(t *testing.T) {
	for _, role := range []string{"president", "secretary", "treasurer", "coach", "member"} {
		t.Run(role, func(t *testing.T) {
			f := newFixture(t)
			target, _ := f.personalBrowser(f.person, "target")
			b := f.membershipAdminBrowser()
			f.exec(`DELETE FROM user_roles WHERE user_id=?`, f.approver)
			if role != "member" {
				f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?,id FROM roles WHERE name=?`, f.approver, role)
			}
			path := fmt.Sprintf("/persons/%d", f.person)
			csrf := b.csrf(t, "/login")
			r := b.call("POST", path+"/password-reset", url.Values{"csrf_token": {csrf}, "password": {"ATTACKER CHOSEN PASSWORD"}, "user_id": {fmt.Sprint(f.approver)}})
			allowed := role == "president" || role == "secretary"
			if !allowed {
				if r.Code != 403 || len(f.mail.messages) != 0 {
					t.Fatal("unauthorized reset", r.Code)
				}
				return
			}
			if r.Code != 303 || !strings.Contains(r.Header().Get("Location"), "password_reset=sent") {
				t.Fatal("authorized reset", r.Code)
			}
			page := b.call("GET", path+"?password_reset=sent", nil)
			if !strings.Contains(page.Body.String(), "Envoyer un lien de réinitialisation") || !strings.Contains(page.Body.String(), "Le lien de réinitialisation a été envoyé") || strings.Contains(page.Body.String(), "name=\"password\"") || strings.Contains(page.Body.String(), resetToken(t, f.mail.messages[0])) {
				t.Fatal("account reset UI")
			}
			var actor int32
			f.must(f.db.QueryRow(`SELECT requested_by_user_id FROM user_password_reset_requests WHERE user_id=?`, target).Scan(&actor))
			if actor != f.approver {
				t.Fatal("request audit")
			}
			u, err := dbsqlc.New(f.db).GetUserByID(t.Context(), target)
			f.must(err)
			if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash.String), []byte("a secure password")) != nil {
				t.Fatal("administrator chose password")
			}
			for _, change := range []string{`UPDATE users SET activated_at=NULL WHERE id=?`, `UPDATE users SET is_active=FALSE WHERE id=?`} {
				f.exec(change, target)
				page = b.call("GET", path, nil)
				if strings.Contains(page.Body.String(), "Envoyer un lien de réinitialisation") {
					t.Fatal("ineligible reset action")
				}
				r = b.call("POST", path+"/password-reset", url.Values{"csrf_token": {csrf}})
				if !strings.Contains(r.Header().Get("Location"), "ineligible") {
					t.Fatal("activation mixed with recovery")
				}
			}
		})
	}
	for _, state := range []string{"no_channel", "send_failed", "limited"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			f.personalBrowser(f.person, "target")
			b := p43Secretary(f)
			switch state {
			case "no_channel":
				f.exec(`UPDATE persons SET email=NULL WHERE id=?`, f.person)
			case "send_failed":
				f.mail.err = errors.New("SMTP failed")
			case "limited":
				for i := 0; i < 3; i++ {
					f.prepareReset("target")
				}
			}
			path := fmt.Sprintf("/persons/%d", f.person)
			r := b.call("POST", path+"/password-reset", url.Values{"csrf_token": {b.csrf(t, path)}})
			if r.Code != 303 || !strings.Contains(r.Header().Get("Location"), "password_reset="+state) {
				t.Fatal("operational feedback", r.Code, r.Header().Get("Location"))
			}
			page := b.call("GET", path+"?password_reset="+state, nil)
			if !strings.Contains(page.Body.String(), "alert-danger") {
				t.Fatal("missing operational notice")
			}
		})
	}
}

// This mailbox supports asynchronous public delivery without sharing the older,
// deliberately synchronous integration-test fake.
type resetMailbox struct{ messages chan mailer.Message }

func (m *resetMailbox) Send(_ context.Context, message mailer.Message) error {
	m.messages <- message
	return nil
}
func TestPublicRecoveryHTTPAndDelivery(t *testing.T) {
	f := newFixture(t)
	f.personalBrowser(f.person, "member")
	box := &resetMailbox{messages: make(chan mailer.Message, 8)}
	app, err := NewWithMailer(config.Config{SiteName: "Club Core"}, config.Runtime{RegistrationVerificationTTL: time.Hour, ActivationValidity: time.Hour, BaseURL: "https://club.example.test", SecureCookies: true, Location: time.UTC, SMTP: mailer.SMTPConfig{From: "club@example.test"}}, f.db, box)
	f.must(err)
	b := newBrowser(app.Handler)
	csrf := b.csrf(t, "/password/forgot")
	r := b.call("POST", "/password/forgot", url.Values{"username": {"member"}, "csrf_token": {csrf}})
	if r.Code != 303 {
		t.Fatal("public request")
	}
	page := b.call("GET", r.Header().Get("Location"), nil).Body.String()
	if !strings.Contains(page, "Si ce compte existe") || strings.Contains(page, "remi@example.test") || strings.Contains(page, "name=\"token\"") {
		t.Fatal("public leak")
	}
	var message mailer.Message
	select {
	case message = <-box.messages:
	case <-time.After(2 * time.Second):
		t.Fatal("public email not delivered")
	}
	token := resetToken(t, message)
	if strings.Contains(page, token) {
		t.Fatal("public token leak")
	}
	csrf = b.csrf(t, "/password/reset?token="+token)
	// CSRF covers reset POST, including cross-origin requests with otherwise valid cookies.
	req := httptest.NewRequest("POST", "https://club.example.test/password/reset", strings.NewReader(resetForm(csrf, token, "a newer password", "a newer password").Encode()))
	req.Header.Set("Origin", "https://evil.example.test")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	denied := httptest.NewRecorder()
	app.Handler.ServeHTTP(denied, req)
	if denied.Code != http.StatusForbidden {
		t.Fatal("cross-origin reset")
	}
	r = b.call("POST", "/password/reset", resetForm(csrf, token, "a newer password", "a newer password"))
	if r.Code != 303 {
		t.Fatal("public reset", r.Code)
	}
	<-box.messages // notification completed after commit
}

func TestResetServiceRevokesUnboundSessionsAndAuthenticatedChangeInvalidatesLink(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	token := f.prepareReset("member")
	sessions := auth.NewSessions(false)
	var requests []*http.Request
	for _, bound := range []bool{false, true} {
		request := httptest.NewRequest("GET", "http://localhost/", nil)
		response := httptest.NewRecorder()
		var err error
		if bound {
			err = sessions.CreateAuthenticated(response, request, user, [32]byte{1})
		} else {
			err = sessions.Create(response, request, user)
		}
		f.must(err)
		request.AddCookie(response.Result().Cookies()[0])
		requests = append(requests, request)
	}
	service := passwordreset.New(f.db, f.mail, sessions, "club@example.test", "https://club.example.test", 30*time.Minute)
	f.must(service.Reset(t.Context(), token, "a different password", "a different password"))
	for _, r := range requests {
		if _, ok := sessions.UserID(r); ok {
			t.Fatal("service retained a local session")
		}
	}
	// The original application's credential-bound browser is invalid after reset.
	if r := b.call("GET", "/me/account/password", nil); r.Code != 303 {
		t.Fatal("password-bound session survived")
	}
	token = f.prepareReset("member")
	b = newBrowser(f.app.Handler)
	csrf := b.csrf(t, "/login")
	r := b.call("POST", "/login", url.Values{"csrf_token": {csrf}, "username": {"member"}, "password": {"a different password"}})
	if r.Code != 303 {
		t.Fatal("login after reset")
	}
	f.accountPost(b, "/me/account/password", url.Values{"current_password": {"a different password"}, "new_password": {"another valid password"}, "confirmation": {"another valid password"}}, 303)
	if !errors.Is(f.app.PasswordReset.Validate(t.Context(), token), passwordreset.ErrInvalidToken) {
		t.Fatal("authenticated change retained recovery link")
	}
}
