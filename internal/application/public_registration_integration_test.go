package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/registrationapplications"
	"github.com/pressly/goose/v3"
)

func hiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	v := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]*)"`).FindStringSubmatch(body)
	if len(v) != 2 {
		t.Fatalf("missing field %s", name)
	}
	return html.UnescapeString(v[1])
}
func (f *fixture) joinForm(b *browser) url.Values {
	f.t.Helper()
	response := b.call("GET", "/join", nil)
	if response.Code != 200 {
		f.t.Fatalf("join GET %d", response.Code)
	}
	form := url.Values{"csrf_token": {hiddenValue(f.t, response.Body.String(), "csrf_token")}, "presentation": {hiddenValue(f.t, response.Body.String(), "presentation")}, "first_name": {"Alice"}, "last_name": {"Nouveau"}, "birth_date": {"1990-01-01"}, "email": {"alice@example.test"}, "phone_number": {"0612345678"}, "address": {"1 rue du Club"}, "season_id": {fmt.Sprint(f.season)}, "membership_type_id": {fmt.Sprint(f.kind)}, "activity_id": {fmt.Sprint(f.activity)}, "action": {"submit"}}
	defs, err := dbsqlc.New(f.db).ListActiveConsentDefinitions(f.t.Context())
	f.must(err)
	for _, d := range defs {
		form.Set(fmt.Sprintf("consent_%d", d.ID), "refused")
	}
	return form
}
func knownJoin(form url.Values) {
	form.Set("first_name", "Rémi")
	form.Set("last_name", "Dupont")
	form.Set("email", "remi@example.test")
}
func cloneForm(in url.Values) url.Values {
	out := url.Values{}
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}
func (f *fixture) joinSubmit(b *browser, form url.Values) int32 {
	f.t.Helper()
	response := b.call("POST", "/join", form)
	if response.Code != 303 || response.Header().Get("Location") != "/join/submitted" {
		f.t.Fatalf("join POST %d: %s", response.Code, response.Body.String())
	}
	return f.id(`SELECT max(submission_id) FROM registration_applications`)
}
func (f *fixture) publicApplication(sub int32) dbsqlc.GetRegistrationApplicationDetailsRow {
	f.t.Helper()
	a, err := dbsqlc.New(f.db).GetRegistrationApplicationDetails(f.t.Context(), sub)
	f.must(err)
	return a
}
func (f *fixture) consentDefinition() int32 {
	return f.id(`INSERT INTO consent_definitions(code,version,title,description) VALUES ('image_web',2,'Droit à l’image — site internet','Texte complet <script>alert(1)</script> du consentement.') RETURNING id`)
}

func TestPublicJoinFormAndNewMembership(t *testing.T) {
	f := newFixture(t)
	consent := f.consentDefinition()
	f.id(`INSERT INTO seasons(name,starts_at,ends_at,is_active) VALUES ('HiddenSeason','2000-01-01','2001-01-01',false) RETURNING id`)
	f.id(`INSERT INTO membership_types(name,is_active) VALUES ('HiddenType',false) RETURNING id`)
	f.id(`INSERT INTO activities(name,is_active) VALUES ('HiddenActivity',false) RETURNING id`)
	f.id(`INSERT INTO consent_definitions(code,version,title,description,is_active) VALUES ('hidden',1,'HiddenConsent','HiddenDescription',false) RETURNING id`)
	b := newBrowser(f.app.Handler)
	page := b.call("GET", "/join", nil)
	body := html.UnescapeString(page.Body.String())
	for _, want := range []string{"Adhérer", "Je souhaite m'inscrire moi-même", "Saison :", "Droit à l’image", "version 2", "Texte complet", "J'accepte", "Je refuse", "<fieldset", "<legend", "for=\"birth_date\"", "aria-describedby", "name=\"csrf_token\""} {
		if !strings.Contains(body, want) {
			t.Fatal("missing form content", want)
		}
	}
	for _, hidden := range []string{"HiddenSeason", "HiddenType", "HiddenActivity", "HiddenConsent", "<select id=\"season_id\"", "name=\"password\"", "name=\"username\"", "name=\"group_id\""} {
		if strings.Contains(body, hidden) {
			t.Fatal("unexpected form field", hidden)
		}
	}
	if strings.Contains(page.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("consent XSS")
	}
	for _, header := range []string{"Cache-Control", "Content-Security-Policy", "Referrer-Policy", "X-Content-Type-Options"} {
		if page.Header().Get(header) == "" {
			t.Fatal("missing security header")
		}
	}
	form := f.joinForm(b)
	form.Set("action", "review")
	review := b.call("POST", "/join", form)
	if review.Code != 200 || !strings.Contains(review.Body.String(), "Récapitulatif") || !strings.Contains(review.Body.String(), "alice@example.test") {
		t.Fatal("missing summary")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_submissions`); n != 0 {
		t.Fatal("summary persisted identity")
	}
	form.Set("action", "submit")
	sub := f.joinSubmit(b, form)
	a := f.publicApplication(sub)
	if a.Status != "membership_created" || !a.MembershipID.Valid || !a.FinalizedAt.Valid {
		t.Fatal("application not finalized")
	}
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, sub)
	f.must(err)
	if d.Submission.Status != "resolved" || d.Submission.ResolutionType.String != "new_person" || d.Submission.ResolvedByUserID.Valid || d.Submission.EmailVerified {
		t.Fatal("automatic identity audit")
	}
	m, err := f.memberships.GetDetails(t.Context(), a.MembershipID.Int32)
	f.must(err)
	if m.Membership.Membership.Status != "pending" || len(m.Activities) != 1 || len(m.ConsentRequirements) != 1 {
		t.Fatal("membership intent missing")
	}
	if m.ConsentRequirements[0].ID != consent || m.ConsentRequirements[0].Decision.String != "refused" || m.ConsentRequirements[0].GivenByPersonID.Int32 != d.Submission.ResolvedPersonID.Int32 {
		t.Fatal("wrong consent giver or snapshot")
	}
	if len(m.Completeness.BlockingIssues) != 0 || len(m.Completeness.Warnings) != 1 || m.Completeness.Warnings[0] != "adult_missing_emergency" {
		t.Fatal("adult emergency policy")
	}
	if n := f.id(`SELECT count(*)::integer FROM users`); n != 1 || len(f.mail.messages) != 0 {
		t.Fatal("premature account or activation")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_verification_outbox`); n != 0 {
		t.Fatal("new person sent verification")
	}
	// PRG refresh and retried POST from the same presentation are harmless.
	f.joinSubmit(b, form)
	b.call("GET", "/join/submitted", nil)
	if n := f.id(`SELECT count(*)::integer FROM memberships`); n != 1 {
		t.Fatal("duplicate POST membership")
	}
	admin := f.membershipAdminBrowser()
	detail := admin.call("GET", reviewPath(sub), nil)
	if !strings.Contains(detail.Body.String(), fmt.Sprintf("/memberships/%d", a.MembershipID.Int32)) || !strings.Contains(html.UnescapeString(detail.Body.String()), "Création automatique") {
		t.Fatal("missing application admin audit")
	}
}

func TestPublicJoinValidation(t *testing.T) {
	f := newFixture(t)
	consent := f.consentDefinition()
	inactiveType := f.id(`INSERT INTO membership_types(name,is_active) VALUES ('Inactive',false) RETURNING id`)
	inactiveActivity := f.id(`INSERT INTO activities(name,is_active) VALUES ('Inactive',false) RETURNING id`)
	inactiveConsent := f.id(`INSERT INTO consent_definitions(code,version,title,description,is_active) VALUES ('inactive',1,'Inactive','Old',false) RETURNING id`)
	cases := []struct {
		name   string
		change func(url.Values)
	}{
		{"first name", func(v url.Values) { v.Set("first_name", " ") }},
		{"last name", func(v url.Values) { v.Set("last_name", "") }},
		{"long name", func(v url.Values) { v.Set("first_name", strings.Repeat("a", 201)) }},
		{"missing birth", func(v url.Values) { v.Del("birth_date") }},
		{"invalid birth", func(v url.Values) { v.Set("birth_date", "2020-02-30") }},
		{"minor", func(v url.Values) { v.Set("birth_date", time.Now().AddDate(-10, 0, 0).Format("2006-01-02")) }},
		{"future birth", func(v url.Values) { v.Set("birth_date", "2099-01-01") }},
		{"missing email", func(v url.Values) { v.Del("email") }},
		{"invalid email", func(v url.Values) { v.Set("email", "Alice <alice@example.test>") }},
		{"unknown season", func(v url.Values) { v.Set("season_id", "2147483647") }},
		{"unknown type", func(v url.Values) { v.Set("membership_type_id", "2147483647") }},
		{"inactive type", func(v url.Values) { v.Set("membership_type_id", fmt.Sprint(inactiveType)) }},
		{"unknown activity", func(v url.Values) { v.Set("activity_id", "2147483647") }},
		{"inactive activity", func(v url.Values) { v.Set("activity_id", fmt.Sprint(inactiveActivity)) }},
		{"no activity", func(v url.Values) { v.Del("activity_id") }},
		{"duplicate activity", func(v url.Values) { v.Add("activity_id", fmt.Sprint(f.activity)) }},
		{"missing consent", func(v url.Values) { v.Del(fmt.Sprintf("consent_%d", consent)) }},
		{"withdrawn", func(v url.Values) { v.Set(fmt.Sprintf("consent_%d", consent), "withdrawn") }},
		{"injected consent", func(v url.Values) { v.Set("consent_2147483647", "granted") }},
		{"inactive injected", func(v url.Values) { v.Set(fmt.Sprintf("consent_%d", inactiveConsent), "granted") }},
		{"duplicate answer", func(v url.Values) { v.Add(fmt.Sprintf("consent_%d", consent), "granted") }},
		{"duplicate identity field", func(v url.Values) { v.Add("email", "other@example.test") }},
		{"tampered presentation", func(v url.Values) { v.Set("presentation", v.Get("presentation")+"x") }},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBrowser(f.app.Handler)
			b.ip = fmt.Sprintf("192.0.2.%d:1234", i+1)
			form := f.joinForm(b)
			tc.change(form)
			response := b.call("POST", "/join", form)
			if response.Code != 422 {
				t.Fatalf("expected validation: %d", response.Code)
			}
			if !strings.Contains(response.Body.String(), "1 rue du Club") {
				t.Fatal("lost entered values")
			}
			if tc.name == "minor" && !strings.Contains(html.UnescapeString(response.Body.String()), "Utilisez le parcours") {
				t.Fatal("minor UX")
			}
		})
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_submissions`); n != 0 {
		t.Fatal("invalid form persisted")
	}
	if n := f.id(`SELECT count(*)::integer FROM persons`); n != 2 {
		t.Fatal("invalid/minor created Person")
	}
}

func TestPublicJoinEmailAndConsentSnapshot(t *testing.T) {
	f := newFixture(t)
	old := f.consentDefinition()
	b := newBrowser(f.app.Handler)
	form := f.joinForm(b)
	knownJoin(form)
	sub := f.joinSubmit(b, form)
	a := f.publicApplication(sub)
	if a.Status != "awaiting_identity" || f.emailState(sub) != "awaiting_email_verification" {
		t.Fatal("existing identity not queued")
	}
	if n := f.id(`SELECT count(*)::integer FROM memberships`); n != 0 || len(f.mail.messages) != 0 {
		t.Fatal("synchronous finalization or SMTP")
	}
	if state, _ := f.jobState(sub); state != "pending" {
		t.Fatal("outbox not pending")
	}
	f.exec(`UPDATE consent_definitions SET is_active=false WHERE id=$1`, old)
	f.id(`INSERT INTO consent_definitions(code,version,title,description) VALUES ('image_web',3,'New text','New version, never presented') RETURNING id`)
	f.drive()
	ref, code := verificationFrom(t, f.mail.messages[0])
	token := b.csrf(t, "/registration/verify")
	response := b.call("POST", "/registration/verify", url.Values{"csrf_token": {token}, "reference": {ref}, "code": {code}})
	if response.Code != 303 || response.Header().Get("Location") != "/registration/verify?result=membership" {
		t.Fatal("verification finalizer", response.Header())
	}
	a = f.publicApplication(sub)
	if a.Status != "membership_created" {
		t.Fatal("email not finalized")
	}
	m, err := f.memberships.GetDetails(t.Context(), a.MembershipID.Int32)
	f.must(err)
	if len(m.ConsentRequirements) != 1 || m.ConsentRequirements[0].ID != old || m.ConsentRequirements[0].Version != 2 || m.ConsentRequirements[0].GivenByPersonID.Int32 != f.person || m.ConsentRequirements[0].Decision.String != "refused" {
		t.Fatal("consents recalculated")
	}
	var sameTime bool
	f.must(f.db.QueryRow(t.Context(), `SELECT r.presented_at=c.presented_at FROM membership_consent_requirements r JOIN registration_application_consents c ON c.consent_definition_id=r.consent_definition_id WHERE r.membership_id=$1 AND c.application_id=$2`, a.MembershipID.Int32, a.ID).Scan(&sameTime))
	if !sameTime {
		t.Fatal("presentation date lost")
	}
	if err = f.app.Verifications.VerifyEmail(t.Context(), ref, code); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("verification replay")
	}
	again, err := f.app.RegistrationApplications.Finalize(t.Context(), sub)
	f.must(err)
	if again.MembershipID != a.MembershipID {
		t.Fatal("non-idempotent finalizer")
	}
	if n := f.id(`SELECT count(*)::integer FROM users`); n != 1 {
		t.Fatal("created User before approval")
	}
}

func TestPublicJoinPresentedVersionsSurviveBeforePOST(t *testing.T) {
	f := newFixture(t)
	old := f.consentDefinition()
	b := newBrowser(f.app.Handler)
	form := f.joinForm(b)
	f.exec(`UPDATE consent_definitions SET is_active=false WHERE id=$1`, old)
	next := f.id(`INSERT INTO consent_definitions(code,version,title,description) VALUES ('image_web',3,'New version','Not yet presented') RETURNING id`)
	sub := f.joinSubmit(b, form)
	a := f.publicApplication(sub)
	if n := f.id(`SELECT count(*)::integer FROM membership_consent_requirements WHERE membership_id=$1 AND consent_definition_id=$2`, a.MembershipID.Int32, old); n != 1 {
		t.Fatal("original presentation not honored")
	}
	if n := f.id(`SELECT count(*)::integer FROM membership_consent_requirements WHERE membership_id=$1 AND consent_definition_id=$2`, a.MembershipID.Int32, next); n != 0 {
		t.Fatal("unseen version injected")
	}
	// A different browser cannot reuse the signed presentation with its own CSRF.
	other := newBrowser(f.app.Handler)
	other.ip = "198.51.100.1:1"
	fresh := f.joinForm(other)
	fresh.Set("presentation", form.Get("presentation"))
	if response := other.call("POST", "/join", fresh); response.Code != 422 {
		t.Fatal("presentation not browser-bound")
	}
}

func TestPublicJoinAmbiguityAndAdminFinalization(t *testing.T) {
	for _, create := range []bool{false, true} {
		t.Run(fmt.Sprint(create), func(t *testing.T) {
			f := newFixture(t)
			f.consentDefinition()
			b := newBrowser(f.app.Handler)
			form := f.joinForm(b)
			knownJoin(form)
			form.Set("email", "different@example.test")
			sub := f.joinSubmit(b, form)
			if f.emailState(sub) != "awaiting_identity_review" || f.publicApplication(sub).Status != "awaiting_identity" {
				t.Fatal("ambiguous not reviewed")
			}
			if n := f.id(`SELECT count(*)::integer FROM persons`); n != 2 {
				t.Fatal("premature person")
			}
			var err error
			if create {
				err = f.app.Reviews.CreatePerson(t.Context(), f.approver, sub)
			} else {
				err = f.app.Reviews.LinkPerson(t.Context(), f.approver, sub, f.person)
			}
			f.must(err)
			a := f.publicApplication(sub)
			if a.Status != "membership_created" {
				t.Fatal("admin resolution not finalized")
			}
			if err = f.app.Reviews.CreatePerson(t.Context(), f.approver, sub); !errors.Is(err, identityresolution.ErrClosed) {
				t.Fatal("second admin resolution")
			}
			if n := f.id(`SELECT count(*)::integer FROM memberships`); n != 1 {
				t.Fatal("duplicate admin membership")
			}
		})
	}
}

func TestPublicJoinBusinessChangesPreserveIdentity(t *testing.T) {
	for _, kind := range []string{"season", "type", "activity", "duplicate", "sql failure"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			b := newBrowser(f.app.Handler)
			form := f.joinForm(b)
			knownJoin(form)
			sub := f.joinSubmit(b, form)
			f.drive()
			switch kind {
			case "season":
				f.exec(`UPDATE seasons SET is_active=false WHERE id=$1`, f.season)
			case "type":
				f.exec(`UPDATE membership_types SET is_active=false WHERE id=$1`, f.kind)
			case "activity":
				f.exec(`UPDATE activities SET is_active=false WHERE id=$1`, f.activity)
			case "duplicate":
				_, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: f.person, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}})
				f.must(err)
			case "sql failure":
				f.exec(`CREATE FUNCTION fail_public_finalize_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='membership_created' THEN RAISE EXCEPTION 'private failure'; END IF; RETURN NEW; END $$`)
				f.exec(`CREATE TRIGGER fail_public_finalize_test BEFORE UPDATE ON registration_applications FOR EACH ROW EXECUTE FUNCTION fail_public_finalize_test()`)
			}
			ref, code := verificationFrom(t, f.mail.messages[0])
			outcome, err := f.app.Verifications.VerifyEmailOutcome(t.Context(), ref, code)
			f.must(err)
			if outcome != "review" || f.emailState(sub) != "resolved" {
				t.Fatal("identity proof undone")
			}
			a := f.publicApplication(sub)
			if a.Status != "needs_review" || a.MembershipID.Valid || !a.LastErrorCode.Valid {
				t.Fatal("missing application review")
			}
			expected := int32(0)
			if kind == "duplicate" {
				expected = 1
				if a.LastErrorCode.String != "membership_already_exists" {
					t.Fatal("duplicate category")
				}
			}
			if n := f.id(`SELECT count(*)::integer FROM memberships`); n != expected {
				t.Fatal("invalid/partial membership")
			}
			count, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
			f.must(err)
			if count != 1 {
				t.Fatal("needs_review missing from badge")
			}
			admin := f.membershipAdminBrowser()
			page := admin.call("GET", reviewPath(sub), nil)
			if page.Code != 200 || !strings.Contains(html.UnescapeString(page.Body.String()), "Vérification complémentaire nécessaire") {
				t.Fatal("admin cannot find application")
			}
			if kind != "duplicate" {
				f.exec(`UPDATE seasons SET is_active=true WHERE id=$1`, f.season)
				f.exec(`UPDATE membership_types SET is_active=true WHERE id=$1`, f.kind)
				f.exec(`UPDATE activities SET is_active=true WHERE id=$1`, f.activity)
				if kind == "sql failure" {
					f.exec(`DROP TRIGGER fail_public_finalize_test ON registration_applications`)
				}
				token := admin.csrf(t, reviewPath(sub))
				response := admin.call("POST", reviewPath(sub)+"/finalize-application", url.Values{"csrf_token": {token}})
				if response.Code != 303 || f.publicApplication(sub).Status != "membership_created" {
					t.Fatal("review retry not usable")
				}
			}
		})
	}
}

func TestPublicJoinAtomicNewPerson(t *testing.T) {
	f := newFixture(t)
	f.consentDefinition()
	f.exec(`CREATE FUNCTION fail_public_create_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='membership_created' THEN RAISE EXCEPTION 'private failure'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER fail_public_create_test BEFORE UPDATE ON registration_applications FOR EACH ROW EXECUTE FUNCTION fail_public_create_test()`)
	b := newBrowser(f.app.Handler)
	form := f.joinForm(b)
	response := b.call("POST", "/join", form)
	if response.Code != 503 || strings.Contains(response.Body.String(), "private failure") {
		t.Fatal("unsafe creation error")
	}
	for _, table := range []string{"registration_submissions", "registration_applications", "registration_application_activities", "registration_application_consents", "memberships", "membership_activities", "membership_consent_requirements", "membership_consents"} {
		if n := f.id(`SELECT count(*)::integer FROM ` + table); n != 0 {
			t.Fatal("partial creation", table)
		}
	}
	if n := f.id(`SELECT count(*)::integer FROM persons`); n != 2 {
		t.Fatal("orphan new Person")
	}
}

func TestPublicJoinPrivacyAndExistingUsers(t *testing.T) {
	var generic string
	for i, kind := range []string{"new", "known", "ambiguous", "quota", "active user", "disabled user", "unactivated user"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			if strings.Contains(kind, "user") {
				uid := f.id(`INSERT INTO users(person_id,username,is_active,activated_at) VALUES ($1,'member',true,clock_timestamp()) RETURNING id`, f.person)
				if kind == "disabled user" {
					f.exec(`UPDATE users SET is_active=false WHERE id=$1`, uid)
				}
				if kind == "unactivated user" {
					f.exec(`UPDATE users SET activated_at=NULL WHERE id=$1`, uid)
				}
			}
			var before string
			f.must(f.db.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u`).Scan(&before))
			if kind == "quota" {
				for n := 0; n < 3; n++ {
					f.submit(emailInput())
				}
			}
			b := newBrowser(f.app.Handler)
			b.ip = fmt.Sprintf("198.51.100.%d:1", i+1)
			form := f.joinForm(b)
			if kind != "new" {
				knownJoin(form)
			}
			if kind == "ambiguous" {
				form.Set("email", "new@example.test")
			}
			sub := f.joinSubmit(b, form)
			page := b.call("GET", "/join/submitted", nil)
			if generic == "" {
				generic = page.Body.String()
			} else if page.Body.String() != generic {
				t.Fatal("public identity distinction")
			}
			for _, secret := range []string{"remi@example.test", "alice@example.test", "Person", "User", "candidat", "trouvé votre compte"} {
				if strings.Contains(page.Body.String(), secret) {
					t.Fatal("public disclosure", secret)
				}
			}
			if kind == "known" || strings.Contains(kind, "user") {
				f.drive()
				ref, code := verificationFrom(t, f.mail.messages[0])
				f.must(f.app.Verifications.VerifyEmail(t.Context(), ref, code))
				if f.publicApplication(sub).Status != "membership_created" {
					t.Fatal("User state blocked request")
				}
			}
			var after string
			f.must(f.db.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u`).Scan(&after))
			if before != after {
				t.Fatal("User changed before approval")
			}
		})
	}
}

func TestPublicJoinHTTPSecurityAndLimits(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	form := f.joinForm(b)
	for i, kind := range []string{"csrf", "origin", "body"} {
		values := cloneForm(form)
		if kind == "csrf" {
			values.Set("csrf_token", strings.Repeat("a", 64))
		}
		if kind == "body" {
			values.Set("address", strings.Repeat("x", 33*1024))
		}
		request := httptest.NewRequest("POST", "https://club.example.test/join", strings.NewReader(values.Encode()))
		request.RemoteAddr = fmt.Sprintf("198.51.100.%d:1", i+1)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range b.cookies {
			request.AddCookie(c)
		}
		if kind == "origin" {
			request.Header.Set("Origin", "https://evil.test")
		}
		response := httptest.NewRecorder()
		f.app.Handler.ServeHTTP(response, request)
		want := 403
		if kind == "body" {
			want = 400
		}
		if response.Code != want {
			t.Fatal("unsafe POST", kind, response.Code)
		}
	}
	// The sixth POST from one peer is limited, even with changing spoofed XFF.
	for i := 0; i < 6; i++ {
		request := httptest.NewRequest("POST", "https://club.example.test/join", strings.NewReader(form.Encode()))
		request.RemoteAddr = "203.0.113.1:1"
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		for _, c := range b.cookies {
			request.AddCookie(c)
		}
		response := httptest.NewRecorder()
		f.app.Handler.ServeHTTP(response, request)
		if i < 5 && response.Code != 303 {
			t.Fatal("premature IP limit")
		}
		if i == 5 && (response.Code != 429 || response.Header().Get("Retry-After") == "") {
			t.Fatal("IP limiter bypass")
		}
	}
	// Budget is the Application.SubmissionLimiter instance, also used before parsing.
	if f.app.SubmissionLimiter.Allow("203.0.113.1:123") {
		t.Fatal("different limiter instance")
	}
	for i := 0; i < 52; i++ {
		request := httptest.NewRequest("POST", "https://club.example.test/join", nil)
		request.RemoteAddr = fmt.Sprintf("10.1.0.%d:1", i)
		response := httptest.NewRecorder()
		f.app.Handler.ServeHTTP(response, request)
		if i == 51 && response.Code != 429 {
			t.Fatal("global budget not enforced before CSRF")
		}
	}
}

func TestPublicJoinConcurrency(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	form := f.joinForm(b)
	// Read cookies and forms only in concurrent calls; avoid mutating test browser.
	post := func(values url.Values) int {
		request := httptest.NewRequest("POST", "https://club.example.test/join", strings.NewReader(values.Encode()))
		request.RemoteAddr = "203.0.113.99:1"
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range b.cookies {
			request.AddCookie(c)
		}
		response := httptest.NewRecorder()
		f.app.Handler.ServeHTTP(response, request)
		return response.Code
	}
	codes := make(chan int, 2)
	for n := 0; n < 2; n++ {
		go func() { codes <- post(form) }()
	}
	for n := 0; n < 2; n++ {
		if code := <-codes; code != 303 {
			t.Fatal("concurrent POST", code)
		}
	}
	if n := f.id(`SELECT count(*)::integer FROM memberships`); n != 1 {
		t.Fatal("same form duplicated membership")
	}
	sub := f.id(`SELECT max(submission_id) FROM registration_applications`)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.app.RegistrationApplications.Finalize(t.Context(), sub)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		f.must(err)
	}
	if n := f.id(`SELECT count(*)::integer FROM memberships`); n != 1 {
		t.Fatal("concurrent finalizer duplicate")
	}
}

func TestPublicJoinVerifyVersusAdmin(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	form := f.joinForm(b)
	knownJoin(form)
	sub := f.joinSubmit(b, form)
	f.drive()
	ref, code := verificationFrom(t, f.mail.messages[0])
	results := make(chan error, 2)
	go func() { results <- f.app.Verifications.VerifyEmail(t.Context(), ref, code) }()
	go func() { results <- f.app.Reviews.LinkPerson(t.Context(), f.approver, sub, f.person) }()
	wins := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else if !errors.Is(err, identityresolution.ErrClosed) && !errors.Is(err, identityresolution.ErrVerification) {
			t.Fatal(err)
		}
	}
	if wins != 1 || f.publicApplication(sub).Status != "membership_created" {
		t.Fatal("resolution race")
	}
	if n := f.id(`SELECT count(*)::integer FROM memberships`); n != 1 {
		t.Fatal("double resolution membership")
	}
}

func TestPublicJoinMigrationAudit(t *testing.T) {
	f := newFixture(t)
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.DownTo(t.Context(), 20)
	f.must(err)
	_, err = provider.Up(t.Context())
	f.must(err)
	b := newBrowser(f.app.Handler)
	sub := f.joinSubmit(b, f.joinForm(b))
	a := f.publicApplication(sub)
	if _, err = provider.DownTo(t.Context(), 20); err == nil {
		t.Fatal("application audit discarded")
	}
	if _, err = f.db.Exec(t.Context(), `DELETE FROM registration_applications WHERE id=$1`, a.ID); err == nil {
		t.Fatal("application erased")
	}
	if _, err = f.db.Exec(t.Context(), `UPDATE registration_application_activities SET activity_id=activity_id WHERE application_id=$1`, a.ID); err == nil {
		t.Fatal("snapshot changed")
	}
}

// Keep domain entrypoint coverage separate from HTTP parsing and browser CSRF.
func TestPublicJoinDomainValidation(t *testing.T) {
	f := newFixture(t)
	c, err := f.app.RegistrationApplications.Catalog(t.Context())
	f.must(err)
	token, err := f.app.RegistrationApplications.Present(c, "browser")
	f.must(err)
	in := registrationapplications.Input{Identity: emailInput(), SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}, Presentation: token}
	in.Identity.BirthDate.Valid = false
	if _, err = f.app.RegistrationApplications.Submit(context.Background(), in, "browser"); err == nil {
		t.Fatal("domain accepted missing birth")
	}
}

func TestPublicJoinConcurrentResolvedPersonAndFinalizers(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	ids := make([]int32, 2)
	for i := range ids {
		form := f.joinForm(b)
		knownJoin(form)
		ids[i] = f.joinSubmit(b, form)
		f.drive()
	}
	results := make(chan error, 2)
	for _, message := range f.mail.messages {
		ref, code := verificationFrom(t, message)
		go func() { results <- f.app.Verifications.VerifyEmail(t.Context(), ref, code) }()
	}
	for range ids {
		f.must(<-results)
	}
	var retryID int32
	for _, id := range ids {
		if f.emailState(id) != "resolved" {
			t.Fatal("concurrent duplicate lost proof")
		}
		if a := f.publicApplication(id); a.Status == "needs_review" {
			retryID = id
		}
	}
	if retryID == 0 || f.id(`SELECT count(*)::integer FROM memberships`) != 1 {
		t.Fatal("person/season uniqueness")
	}
	// Exercise two finalizers doing real creation after an availability correction.
	freshSeason := f.id(`INSERT INTO seasons(name,starts_at,ends_at) VALUES ('Next','2027-09-01','2028-08-31') RETURNING id`)
	form := f.joinForm(b)
	knownJoin(form)
	form.Set("season_id", fmt.Sprint(freshSeason))
	sub := f.joinSubmit(b, form)
	f.drive()
	f.exec(`UPDATE activities SET is_active=false WHERE id=$1`, f.activity)
	ref, code := verificationFrom(t, f.mail.messages[len(f.mail.messages)-1])
	f.must(f.app.Verifications.VerifyEmail(t.Context(), ref, code))
	f.exec(`UPDATE activities SET is_active=true WHERE id=$1`, f.activity)
	for i := 0; i < 2; i++ {
		go func() { _, err := f.app.RegistrationApplications.Finalize(t.Context(), sub); results <- err }()
	}
	for i := 0; i < 2; i++ {
		f.must(<-results)
	}
	if f.publicApplication(sub).Status != "membership_created" || f.id(`SELECT count(*)::integer FROM memberships WHERE person_id=$1 AND season_id=$2`, f.person, freshSeason) != 1 {
		t.Fatal("concurrent finalizers created duplicates")
	}
}
