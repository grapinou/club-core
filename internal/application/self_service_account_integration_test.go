package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/mailer"
)

func (f *fixture) accountPost(b *browser, path string, form url.Values, want int) string {
	f.t.Helper()
	form.Set("csrf_token", b.csrf(f.t, path))
	r := b.call("POST", path, form)
	if r.Code != want {
		f.t.Fatalf("%s status %d want %d: %s", path, r.Code, want, r.Body.String())
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		f.t.Fatal("account mutation cache")
	}
	if strings.Contains(r.Header().Get("Location"), "@") {
		f.t.Fatal("PII redirect")
	}
	for _, cookie := range b.cookies {
		if strings.Contains(cookie.Name, "session") && strings.Contains(r.Body.String(), cookie.Value) {
			f.t.Fatal("session token in HTML")
		}
	}
	f.assertNoDeliverySecrets(r.Body.String())
	return r.Body.String()
}
func (f *fixture) personEmail(id int32) string {
	f.t.Helper()
	var v string
	f.must(f.db.QueryRow(f.t.Context(), `SELECT coalesce(email,'') FROM persons WHERE id=$1`, id).Scan(&v))
	return v
}
func emailChangeCode(t *testing.T, m mailer.Message) string {
	t.Helper()
	match := regexp.MustCompile(`Code de vérification : ([0-9]{20})`).FindStringSubmatch(m.Text)
	if len(match) != 2 {
		t.Fatal("missing email change code")
	}
	return match[1]
}
func TestSelfServiceProfileAndHTTPBoundary(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	paths := []string{"/me/account/profile", "/me/account/email", "/me/account/email/verify", "/me/account/password"}
	for _, path := range paths {
		anon := newBrowser(f.app.Handler)
		for _, method := range []string{"GET", "POST"} {
			r := anon.call(method, path, nil)
			if r.Code != 303 || r.Header().Get("Location") != "/login" {
				t.Fatal("anonymous", path)
			}
		}
		f.personalOK(b, path, "csrf_token")
		token := b.csrf(t, path)
		for _, origin := range []string{"https://evil.example.test", "https://club.example.test"} {
			data := "csrf_token=" + url.QueryEscape(token)
			if origin == "https://club.example.test" {
				data += "&address=" + strings.Repeat("x", 40000)
			}
			req := httptest.NewRequest("POST", "https://club.example.test"+path, strings.NewReader(data))
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for _, c := range b.cookies {
				req.AddCookie(c)
			}
			res := httptest.NewRecorder()
			f.app.Handler.ServeHTTP(res, req)
			want := 403
			if origin == "https://club.example.test" {
				want = 400
			}
			if res.Code != want {
				t.Fatal("origin/body protection", path, res.Code)
			}
		}

		if r := b.call("POST", path, url.Values{}); r.Code != 403 {
			t.Fatal("CSRF", path, r.Code)
		}
	}
	f.exec(`UPDATE persons SET address='Ancienne adresse' WHERE id=$1`, f.person)
	f.personalOK(b, "/me/account/profile", "Ancienne adresse")
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'`, user)
	foreign := f.id(`INSERT INTO persons(first_name,last_name,phone_number,address) VALUES('Autre','Personne','0123456789','Intacte') RETURNING id`)
	form := url.Values{"phone_number": {" +33 (0)6 12 34 56 78 "}, "address": {"  12 rue du Club\nParis  "}, "person_id": {fmt.Sprint(foreign)}, "user_id": {fmt.Sprint(f.approver)}, "first_name": {"INJECTED"}, "username": {"INJECTED"}}
	// Use the exact existing normalization convention (French 06... -> 336...).
	form.Set("phone_number", " 06 12 34 56 78 ")
	f.accountPost(b, paths[0], form, 303)
	var phone, address, name string
	f.must(f.db.QueryRow(t.Context(), `SELECT phone_number,address,first_name FROM persons WHERE id=$1`, f.person).Scan(&phone, &address, &name))
	if phone != "33612345678" || address != "12 rue du Club\nParis" || name != "Rémi" {
		t.Fatal("contact update", phone, address, name)
	}
	var untouched string
	f.must(f.db.QueryRow(t.Context(), `SELECT address FROM persons WHERE id=$1`, foreign).Scan(&untouched))
	if untouched != "Intacte" {
		t.Fatal("admin IDOR")
	}
	f.personalOK(b, paths[0]+"?saved=1", "Vos coordonnées ont été mises à jour")
	body := f.accountPost(b, paths[0], url.Values{"phone_number": {"invalid-phone"}, "address": {"adresse conservée"}}, 422)
	if !strings.Contains(body, "invalid-phone") || !strings.Contains(body, "adresse conservée") || !strings.Contains(body, `aria-describedby="phone_number-error"`) {
		t.Fatal("invalid values lost")
	}
	f.accountPost(b, paths[0], url.Values{"phone_number": {""}, "address": {strings.Repeat("x", 2001)}}, 422)
	f.accountPost(b, paths[0], url.Values{"phone_number": {" "}, "address": {" "}}, 303)
	if f.count(`SELECT count(*) FROM persons WHERE id=$1 AND phone_number IS NULL AND address IS NULL`, f.person) != 1 {
		t.Fatal("optional fields")
	}
	if f.count(`SELECT count(*) FROM account_security_events WHERE user_id=$1 AND event='profile_contact_updated'`, user) != 2 {
		t.Fatal("audit")
	}
	for _, value := range []string{"first_name", "last_name", "birth_date", "username", "person_id", "user_id"} {
		if strings.Contains(f.personalOK(b, paths[0]), `name="`+value+`"`) {
			t.Fatal("editable identity", value)
		}
	}
}
func TestSelfServiceEmailLifecycle(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	request := "/me/account/email"
	verify := request + "/verify"
	input := func(email, password string) url.Values {
		return url.Values{"new_email": {email}, "current_password": {password}}
	}
	f.accountPost(b, request, input("next@example.test", "wrong"), 422)
	f.accountPost(b, request, input("not-an-email", "a secure password"), 422)
	f.accountPost(b, request, input("next@example.test", "a secure password"), 303)
	if f.personEmail(f.person) != "remi@example.test" {
		t.Fatal("email changed before proof")
	}
	code := emailChangeCode(t, f.mail.messages[0])
	digest := sha256.Sum256([]byte(code))
	if f.count(`SELECT count(*) FROM user_email_change_requests WHERE user_id=$1 AND code_hash=$2 AND new_email_normalized='next@example.test'`, user, digest[:]) != 1 {
		t.Fatal("code storage")
	}
	if strings.Contains(f.personalOK(b, verify), code) {
		t.Fatal("code leaked")
	}
	f.accountPost(b, verify, url.Values{"code": {"000"}}, 422)
	f.exec(`UPDATE user_email_change_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, user)
	f.accountPost(b, verify, url.Values{"code": {code}}, 422)
	// A replacement invalidates even expired active rows.
	f.accountPost(b, request, input("shared@example.test", "a secure password"), 303)
	second := emailChangeCode(t, f.mail.messages[1])
	f.accountPost(b, verify, url.Values{"code": {code}}, 422)
	f.id(`INSERT INTO persons(first_name,last_name,email) VALUES('Shared','Family','shared@example.test') RETURNING id`)
	f.accountPost(b, verify, url.Values{"code": {second}}, 303)
	if f.personEmail(f.person) != "shared@example.test" {
		t.Fatal("email not applied")
	}
	if len(f.mail.messages) != 3 || f.mail.messages[2].To != "remi@example.test" {
		t.Fatal("old address notification")
	}
	f.accountPost(b, verify, url.Values{"code": {second}}, 422)
	f.personalOK(b, verify+"?saved=1", "Votre nouvelle adresse email a été vérifiée")
	f.accountPost(b, request, input("third@example.test", "a secure password"), 303)
	f.accountPost(b, request, input("fourth@example.test", "a secure password"), 429)
	if f.count(`SELECT count(*) FROM account_security_events WHERE user_id=$1 AND event='email_changed'`, user) != 1 {
		t.Fatal("email audit")
	}
	u, err := dbsqlc.New(f.db).GetUserByID(t.Context(), user)
	f.must(err)
	if u.Username != "member" || u.PersonID != f.person {
		t.Fatal("identity changed")
	}
}
func TestSelfServicePasswordAndSessions(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	other := f.loginBrowser("member")
	path := "/me/account/password"
	input := func(current, password, confirmation string) url.Values {
		return url.Values{"current_password": {current}, "new_password": {password}, "confirmation": {confirmation}}
	}
	for _, v := range []url.Values{
		input("wrong", "new secure password", "new secure password"), input("a secure password", "short", "short"),
		input("a secure password", strings.Repeat("x", 73), strings.Repeat("x", 73)), input("a secure password", "new secure password", "mismatch"),
		input("a secure password", "a secure password", "a secure password"),
	} {
		body := f.accountPost(b, path, v, 422)
		if strings.Contains(body, `value="a secure password"`) || strings.Contains(body, `value="new secure password"`) {
			t.Fatal("password echoed")
		}
	}
	oldCookie := ""
	for name, c := range b.cookies {
		if strings.Contains(name, "session") {
			oldCookie = c.Value
		}
	}
	f.accountPost(b, path, input("a secure password", "new secure password", "new secure password"), 303)
	f.personalOK(b, path+"?saved=1", "Votre mot de passe a été modifié")
	for name, c := range b.cookies {
		if strings.Contains(name, "session") && c.Value == oldCookie {
			t.Fatal("session fixation")
		}
	}
	if r := other.call("GET", "/me/account", nil); r.Code != 303 {
		t.Fatal("other session survived")
	}
	login, err := auth.New(dbsqlc.New(f.db))
	f.must(err)
	if _, err = login.Authenticate(t.Context(), "member", "a secure password"); err == nil {
		t.Fatal("old password")
	}
	if id, err := login.Authenticate(t.Context(), "member", "new secure password"); err != nil || id != user {
		t.Fatal("new password")
	}
	if f.count(`SELECT count(*) FROM account_security_events WHERE event='password_changed' AND user_id=$1`, user) != 1 {
		t.Fatal("password audit")
	}
	fresh := newBrowser(f.app.Handler)
	r := fresh.call("POST", "/login", url.Values{"csrf_token": {fresh.csrf(t, "/login")}, "username": {"member"}, "password": {"new secure password"}})
	if r.Code != 303 || r.Header().Get("Location") != "/dashboard" {
		t.Fatal("new HTTP login")
	}
	f.personalOK(fresh, "/me/account", "member")
}

type lockedAccountMailer struct {
	mu       sync.Mutex
	messages []mailer.Message
}

func (m *lockedAccountMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}
func TestSelfServiceConcurrency(t *testing.T) {
	f := newFixture(t)
	user, _ := f.personalBrowser(f.person, "member")
	ctx := f.authenticatedContext(user)
	sender := &lockedAccountMailer{}
	s := accounts.NewSelfService(f.db, sender, "club@example.test", time.Hour)
	results := make(chan error, 2)
	run := func(fn func(int) error) []error {
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			go func(i int) { <-start; results <- fn(i) }(i)
		}
		close(start)
		return []error{<-results, <-results}
	}
	for _, err := range run(func(i int) error {
		return s.RequestEmail(ctx, fmt.Sprintf("next%d@example.test", i), "a secure password")
	}) {
		f.must(err)
	}
	if f.count(`SELECT count(*) FROM user_email_change_requests WHERE user_id=$1 AND invalidated_at IS NULL AND used_at IS NULL`, user) != 1 {
		t.Fatal("multiple active")
	}
	if f.count(`SELECT count(*) FROM user_email_change_requests WHERE user_id=$1 AND invalidated_at IS NOT NULL`, user) != 1 {
		t.Fatal("old not invalidated")
	}
	var activeHash []byte
	f.must(f.db.QueryRow(t.Context(), `SELECT code_hash FROM user_email_change_requests WHERE user_id=$1 AND invalidated_at IS NULL`, user).Scan(&activeHash))
	activeCode := ""
	for _, m := range sender.messages {
		code := emailChangeCode(t, m)
		d := sha256.Sum256([]byte(code))
		if string(d[:]) == string(activeHash) {
			activeCode = code
		}
	}
	wins := 0
	for _, err := range run(func(_ int) error { return s.VerifyEmail(ctx, activeCode) }) {
		if err == nil {
			wins++
		}
	}
	if wins != 1 || f.count(`SELECT count(*) FROM account_security_events WHERE event='email_changed'`) != 1 {
		t.Fatal("double verification", wins)
	}
	wins = 0
	for _, err := range run(func(i int) error {
		_, err := s.ChangePassword(ctx, "a secure password", fmt.Sprintf("new password number %d", i), fmt.Sprintf("new password number %d", i))
		return err
	}) {
		if err == nil {
			wins++
		}
	}
	if wins != 1 || f.count(`SELECT count(*) FROM account_security_events WHERE event='password_changed'`) != 1 {
		t.Fatal("password lost update", wins)
	}
}
func TestSelfServiceDisabledDeliveryAndRollback(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	ctx := f.authenticatedContext(user)
	s := accounts.NewSelfService(f.db, mailer.Disabled{}, "", time.Hour)
	if err := s.RequestEmail(ctx, "next@example.test", "a secure password"); err != accounts.ErrEmailChangeDelivery {
		t.Fatal("disabled delivery", err)
	}
	if f.personEmail(f.person) != "remi@example.test" {
		t.Fatal("disabled transport changed email")
	}
	f.personalOK(b, "/me/account/email/verify")
	// A failing audit insert must roll back both the proof consumption and email.
	f.exec(`DELETE FROM user_email_change_requests`)
	f.must(f.app.SelfService.RequestEmail(ctx, "next@example.test", "a secure password"))
	code := emailChangeCode(t, f.mail.messages[0])
	f.exec(`ALTER TABLE account_security_events ADD CONSTRAINT test_reject_event CHECK (event <> 'email_changed')`)
	if err := f.app.SelfService.VerifyEmail(ctx, code); err == nil {
		t.Fatal("audit failure expected")
	}
	if f.personEmail(f.person) != "remi@example.test" || f.count(`SELECT count(*) FROM user_email_change_requests WHERE used_at IS NULL`) != 1 {
		t.Fatal("non-atomic email")
	}
}

func TestSelfServiceEmailUserIsolationAndNotificationFailure(t *testing.T) {
	f := newFixture(t)
	_, a := f.personalBrowser(f.person, "member")
	foreign := f.id(`INSERT INTO persons(first_name,last_name,email) VALUES('Autre','Membre','foreign@example.test') RETURNING id`)
	user, b := f.personalBrowser(foreign, "other")
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'`, user)
	request := "/me/account/email"
	verify := request + "/verify"
	f.accountPost(a, request, url.Values{"new_email": {"first@example.test"}, "current_password": {"a secure password"}}, 303)
	firstCode := emailChangeCode(t, f.mail.messages[0])
	f.accountPost(b, verify, url.Values{"code": {firstCode}, "person_id": {fmt.Sprint(f.person)}}, 422)
	f.accountPost(b, request, url.Values{"new_email": {"other-new@example.test"}, "current_password": {"a secure password"}, "person_id": {fmt.Sprint(f.person)}}, 303)
	otherCode := emailChangeCode(t, f.mail.messages[1])
	f.accountPost(b, verify, url.Values{"code": {firstCode}}, 422)
	f.mail.err = errors.New("private SMTP failure detail")
	f.accountPost(b, verify, url.Values{"code": {otherCode}, "person_id": {fmt.Sprint(f.person)}}, 303)
	if f.personEmail(foreign) != "other-new@example.test" || f.personEmail(f.person) != "remi@example.test" {
		t.Fatal("user isolation / notification rollback")
	}
	f.mail.err = nil
	f.accountPost(a, "/me/account/password", url.Values{"current_password": {"a secure password"}, "new_password": {"new secure password"}, "confirmation": {"new secure password"}}, 303)
	f.accountPost(a, verify, url.Values{"code": {firstCode}}, 422)
}
