package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/outbox"
	"github.com/pressly/goose/v3"
)

func emailInput() identityresolution.SubmissionInput {
	in := registrationInput()
	in.Email = registrationText(" REMI@EXAMPLE.TEST ")
	return in
}
func (f *fixture) stage(in identityresolution.SubmissionInput) int32 {
	f.t.Helper()
	_, err := identityresolution.NewSubmitter(f.db).CreateSubmission(f.t.Context(), in)
	f.must(err)
	return f.id("SELECT max(id) FROM registration_submissions")
}
func verificationFrom(t *testing.T, m mailer.Message) (string, string) {
	t.Helper()
	ref := regexp.MustCompile(`Référence : ([a-f0-9]{64})`).FindStringSubmatch(m.Text)
	code := regexp.MustCompile(`Code : ([0-9]{20})`).FindStringSubmatch(m.Text)
	if len(ref) != 2 || len(code) != 2 {
		t.Fatal("missing verification delivery")
	}
	return ref[1], code[1]
}
func (f *fixture) emailState(id int32) string {
	f.t.Helper()
	var state string
	f.must(f.db.QueryRow(f.t.Context(), "SELECT status FROM registration_submissions WHERE id=$1", id).Scan(&state))
	return state
}
func TestEmailEligibility(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name     string
		mutate   func(*identityresolution.SubmissionInput)
		setup    string
		reset    string
		eligible bool
	}{
		{name: "unique strong", eligible: true},
		{name: "strong phone different email", mutate: func(in *identityresolution.SubmissionInput) { in.Email = registrationText("new@example.test") }, setup: "UPDATE persons SET phone_number='0612345678' WHERE id=$1", reset: "UPDATE persons SET phone_number=NULL WHERE id=$1"},
		{name: "possible", mutate: func(in *identityresolution.SubmissionInput) { in.Email = registrationText("different@example.test") }},
		{name: "weak", mutate: func(in *identityresolution.SubmissionInput) {
			in.Email.Valid = false
			in.BirthDate.Valid = false
			in.PhoneNumber.Valid = false
		}},
		{name: "missing submitted email", mutate: func(in *identityresolution.SubmissionInput) { in.Email.Valid = false }},
		{name: "missing known email", setup: "UPDATE persons SET email=NULL WHERE id=$1", reset: "UPDATE persons SET email='remi@example.test' WHERE id=$1"},
		{name: "archived", setup: "UPDATE persons SET archived_at=now() WHERE id=$1", reset: "UPDATE persons SET archived_at=NULL WHERE id=$1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != "" {
				f.exec(tc.setup, f.person)
			}
			in := emailInput()
			if tc.mutate != nil {
				tc.mutate(&in)
			}
			id := f.stage(in)
			d, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
			if tc.eligible {
				f.must(err)
				if d == nil {
					t.Fatal("no proof")
				}
			} else if !errors.Is(err, identityresolution.ErrNotEligible) {
				t.Fatal("unexpected eligibility", err)
			}
			if tc.reset != "" {
				f.exec(tc.reset, f.person)
			}
		})
	}
	// Both multiple strong and a weaker additional candidate are ambiguous.
	f.id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES ('Rémi','Dupont','1990-01-01','remi@example.test') RETURNING id")
	id := f.stage(emailInput())
	if _, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id); !errors.Is(err, identityresolution.ErrNotEligible) {
		t.Fatal("two strong accepted")
	}
	f.exec("UPDATE persons SET birth_date=NULL,email=NULL WHERE first_name='Rémi' AND id<>$1", f.person)
	id = f.stage(emailInput())
	if _, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id); !errors.Is(err, identityresolution.ErrNotEligible) {
		t.Fatal("additional weak accepted")
	}
}
func TestEmailPreparationAndCommitOrdering(t *testing.T) {
	f := newFixture(t)
	id := f.stage(emailInput())
	d, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
	f.must(err)
	if len(d.PublicReference) != 64 || len(d.PlaintextCode) != 20 || f.emailState(id) != "awaiting_email_verification" {
		t.Fatal("invalid proof")
	}
	var stored, recipient []byte
	var serialized string
	f.must(f.db.QueryRow(t.Context(), "SELECT code_hash,recipient_hash,to_jsonb(v)::text FROM registration_email_verifications v WHERE public_reference=$1", d.PublicReference).Scan(&stored, &recipient, &serialized))
	expected := sha256.Sum256([]byte(d.PlaintextCode))
	emailHash := sha256.Sum256([]byte("remi@example.test"))
	if string(stored) != string(expected[:]) || string(recipient) != string(emailHash[:]) || strings.Contains(serialized, d.PlaintextCode) || strings.Contains(serialized, "remi@example.test") {
		t.Fatal("unsafe persistence")
	}
	second, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
	f.must(err)
	if second.PublicReference == d.PublicReference {
		t.Fatal("reused reference")
	}
	if err = f.app.Verifications.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("old code still valid")
	}
	var invalid bool
	f.must(f.db.QueryRow(t.Context(), "SELECT invalidated_at IS NOT NULL FROM registration_email_verifications WHERE public_reference=$1", d.PublicReference).Scan(&invalid))
	if !invalid {
		t.Fatal("history invalidation")
	}
	// A delayed failure of the old delivery cannot invalidate the replacement.
	f.must(f.invalidateDelivery(t.Context(), d))
	if f.emailState(id) != "awaiting_email_verification" {
		t.Fatal("stale compensation")
	}
	f.mail.check = func(m mailer.Message) {
		reference, _ := verificationFrom(t, m)
		var status string
		f.must(f.db.QueryRow(t.Context(), "SELECT s.status FROM registration_submissions s JOIN registration_email_verifications v ON v.submission_id=s.id WHERE v.public_reference=$1", reference).Scan(&status))
		if status != "awaiting_email_verification" {
			t.Fatal("SMTP before commit")
		}
	}
	f.submitAndDispatch(emailInput())
	if len(f.mail.messages) != 1 {
		t.Fatal("email missing")
	}
}
func TestEmailOrchestrationAndSMTPCompensation(t *testing.T) {
	f := newFixture(t)
	f.submitAndDispatch(emailInput())
	f.mail.err = errors.New("private SMTP failure")
	id := f.submitAndDispatch(emailInput())
	if f.emailState(id) != "awaiting_email_verification" {
		t.Fatal("retry must keep dossier queued")
	}
	var invalid bool
	f.must(f.db.QueryRow(t.Context(), "SELECT invalidated_at IS NOT NULL FROM registration_email_verifications WHERE submission_id=$1", id).Scan(&invalid))
	if !invalid {
		t.Fatal("attempt proof not invalidated")
	}
	in := emailInput()
	in.FirstName = "No match"
	a, err := f.app.Submissions.CreateSubmission(t.Context(), in)
	f.must(err)
	b, _ := json.Marshal(a)
	if string(b) != `{"status":"submission accepted"}` {
		t.Fatal("enumeration")
	}
}
func TestEmailVerificationAndNoIdentityMutations(t *testing.T) {
	f := newFixture(t)
	f.id("INSERT INTO users(person_id,username,is_active) VALUES ($1,'unchanged-account',false) RETURNING id", f.person)
	before := f.personSnapshot()
	id := f.submitAndDispatch(emailInput())
	reference, code := verificationFrom(t, f.mail.messages[0])
	if err := f.app.Verifications.VerifyEmail(t.Context(), reference, strings.Repeat("x", 20)); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("bad code")
	}
	if err := f.app.Verifications.VerifyEmail(t.Context(), strings.Repeat("f", 64), code); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("unknown reference")
	}
	f.must(f.app.Verifications.VerifyEmail(t.Context(), reference, code))
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Submission.Status != "resolved" || d.Submission.ResolvedByUserID.Valid || d.Submission.ResolvedPersonID.Int32 != f.person || d.Submission.ResolutionType.String != "existing_person" || !d.Submission.EmailVerified {
		t.Fatal("missing proof audit")
	}
	if before != f.personSnapshot() {
		t.Fatal("Person overwritten")
	}
	var active bool
	var accounts, memberships int
	f.must(f.db.QueryRow(t.Context(), "SELECT is_active,(SELECT count(*) FROM users),(SELECT count(*) FROM memberships) FROM users WHERE person_id=$1", f.person).Scan(&active, &accounts, &memberships))
	if active || accounts != 2 || memberships != 0 {
		t.Fatal("unexpected account/membership side effect")
	}
	if err = f.app.Verifications.VerifyEmail(t.Context(), reference, code); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("replay")
	}
	if err = f.app.Reviews.CreatePerson(t.Context(), f.approver, id); !errors.Is(err, identityresolution.ErrClosed) {
		t.Fatal("admin second decision")
	}
}
func TestEmailChangedCandidateAndExpiry(t *testing.T) {
	f := newFixture(t)
	for _, change := range []string{"UPDATE persons SET email='different@example.test' WHERE id=$1", "UPDATE persons SET archived_at=now() WHERE id=$1"} {
		id := f.stage(emailInput())
		d, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
		f.must(err)
		f.exec(change, f.person)
		if err = f.app.Verifications.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode); !errors.Is(err, identityresolution.ErrVerification) {
			t.Fatal("stale identity accepted")
		}
		if f.emailState(id) != "awaiting_identity_review" {
			t.Fatal("not returned for review")
		}
		f.exec("UPDATE persons SET email='remi@example.test',archived_at=NULL WHERE id=$1", f.person)
	}
	svc, err := identityresolution.NewEmailService(f.db, 20*time.Millisecond)
	f.must(err)
	for _, read := range []string{"count", "list", "detail", "verify"} {
		before, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
		f.must(err)
		id := f.stage(emailInput())
		d, err := svc.PrepareEmailVerification(t.Context(), id)
		f.must(err)
		// Wait on the database's clock, which is also the expiry authority.
		f.exec("SELECT pg_sleep(GREATEST(0,extract(epoch FROM (expires_at-clock_timestamp())))+0.01) FROM registration_email_verifications WHERE public_reference=$1", d.PublicReference)
		switch read {
		case "count":
			_, err = f.app.Reviews.CountOpen(t.Context(), f.approver)
		case "list":
			_, err = f.app.Reviews.List(t.Context(), f.approver)
		case "detail":
			_, err = f.app.Reviews.GetDetails(t.Context(), f.approver, id)
		case "verify":
			err = svc.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode)
			if !errors.Is(err, identityresolution.ErrVerification) {
				t.Fatal("expired accepted")
			}
			err = nil
		}
		f.must(err)
		if f.emailState(id) != "awaiting_identity_review" {
			t.Fatal("expired submission stranded")
		}
		after, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
		f.must(err)
		if after != before+1 {
			t.Fatal("expired proof missing from counter")
		}
		var invalid bool
		f.must(f.db.QueryRow(t.Context(), "SELECT invalidated_at IS NOT NULL FROM registration_email_verifications WHERE public_reference=$1", d.PublicReference).Scan(&invalid))
		if !invalid {
			t.Fatal("expired history not invalidated")
		}
	}
}
func TestEmailConcurrentOperations(t *testing.T) {
	f := newFixture(t)
	// Two preparations serialize and retain exactly one live proof.
	id := f.stage(emailInput())
	deliveries := make(chan *identityresolution.EmailDelivery, 2)
	errs := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			d, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
			deliveries <- d
			errs <- err
		}()
	}
	close(start)
	for range 2 {
		f.must(<-errs)
	}
	a, b := <-deliveries, <-deliveries
	if a.PublicReference == b.PublicReference {
		t.Fatal("reference collision")
	}
	var live int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM registration_email_verifications WHERE submission_id=$1 AND used_at IS NULL AND invalidated_at IS NULL", id).Scan(&live))
	if live != 1 {
		t.Fatal("multiple live proofs")
	}
	for _, adminRace := range []bool{false, true} {
		id = f.stage(emailInput())
		d, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
		f.must(err)
		start = make(chan struct{})
		out := make(chan error, 2)
		go func() {
			<-start
			out <- f.app.Verifications.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode)
		}()
		go func() {
			<-start
			if adminRace {
				out <- f.app.Reviews.LinkPerson(t.Context(), f.approver, id, f.person)
			} else {
				out <- f.app.Verifications.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode)
			}
		}()
		close(start)
		success := 0
		for range 2 {
			e := <-out
			if e == nil {
				success++
			} else if !errors.Is(e, identityresolution.ErrClosed) && !errors.Is(e, identityresolution.ErrVerification) {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("multiple resolutions")
		}
		details, e := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
		f.must(e)
		if details.Submission.Status != "resolved" {
			t.Fatal("no final resolution")
		}
	}
	// Explicitly exercise administration winning, regardless of scheduler above.
	id = f.stage(emailInput())
	d, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
	f.must(err)
	f.must(f.app.Reviews.CreatePerson(t.Context(), f.approver, id))
	if err = f.app.Verifications.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("admin resolution replaced")
	}
}
func TestEmailVerificationHTTP(t *testing.T) {
	f := newFixture(t)
	id := f.submitAndDispatch(emailInput())
	reference, code := verificationFrom(t, f.mail.messages[0])
	b := newBrowser(f.app.Handler)
	token := b.csrf(t, "/registration/verify")
	get := b.call("GET", "/registration/verify?reference="+reference, nil)
	if get.Code != 200 || get.Header().Get("Cache-Control") != "no-store" || strings.Contains(get.Body.String(), reference) || strings.Contains(get.Body.String(), "remi@example.test") {
		t.Fatal("public GET leaked data")
	}
	bad := b.call("POST", "/registration/verify", url.Values{"reference": {reference}, "code": {code}})
	if bad.Code != 403 || f.emailState(id) != "awaiting_email_verification" {
		t.Fatal("CSRF mutation")
	}
	form := url.Values{"csrf_token": {token}, "reference": {reference}, "code": {strings.Repeat("0", 20)}}
	wrong := b.call("POST", "/registration/verify", form)
	if wrong.Code != 303 || wrong.Header().Get("Location") != "/registration/verify?result=failed" {
		t.Fatal("error PRG")
	}
	errorPage := b.call("GET", wrong.Header().Get("Location"), nil)
	if !strings.Contains(html.UnescapeString(errorPage.Body.String()), "Impossible de vérifier cette demande avec ces informations.") || strings.Contains(errorPage.Body.String(), reference) || strings.Contains(errorPage.Body.String(), code) {
		t.Fatal("unsafe error page")
	}
	form.Set("code", code)
	success := b.call("POST", "/registration/verify", form)
	if success.Code != 303 || success.Header().Get("Location") != "/registration/verify?result=verified" {
		t.Fatal("success PRG")
	}
	page := b.call("GET", success.Header().Get("Location"), nil)
	if !strings.Contains(html.UnescapeString(page.Body.String()), "Votre identité a été vérifiée.") {
		t.Fatal("success message")
	}
	replay := b.call("POST", "/registration/verify", form)
	if replay.Header().Get("Location") != wrong.Header().Get("Location") {
		t.Fatal("public replay distinction")
	}
	// The shared middleware rejects cross-origin and oversized forms before verification.
	for _, origin := range []bool{true, false} {
		request := httptest.NewRequest("POST", "https://club.example.test/registration/verify", strings.NewReader(form.Encode()+strings.Repeat("x", 9000)))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin {
			request.Header.Set("Origin", "https://evil.example")
		}
		for _, c := range b.cookies {
			request.AddCookie(c)
		}
		response := httptest.NewRecorder()
		f.app.Handler.ServeHTTP(response, request)
		want := 400
		if origin {
			want = 403
		}
		if response.Code != want {
			t.Fatal("sensitive request accepted", response.Code)
		}
	}
}
func TestEmailVerificationRateLimits(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	token := b.csrf(t, "/registration/verify")
	for i := 0; i < 11; i++ {
		r := b.call("POST", "/registration/verify", url.Values{"csrf_token": {token}})
		if i < 10 && r.Code != 303 {
			t.Fatal("early limit")
		}
		if i == 10 && (r.Code != 429 || r.Header().Get("Retry-After") == "") {
			t.Fatal("missing IP limit")
		}
	}
	// Fresh instance: global budget shared across many addresses.
	f2 := newFixture(t)
	for i := 0; i < 121; i++ {
		b := newBrowser(f2.app.Handler)
		b.ip = fmt.Sprintf("192.0.2.%d:1234", i+1)
		token := b.csrf(t, "/registration/verify")
		r := b.call("POST", "/registration/verify", url.Values{"csrf_token": {token}})
		if i < 120 && r.Code != 303 {
			t.Fatal("global early limit")
		}
		if i == 120 && (r.Code != 429 || r.Header().Get("Retry-After") == "") {
			t.Fatal("missing global limit")
		}
	}
}
func TestEmailAdministrativeAuditAndMigrationDown(t *testing.T) {
	f := newFixture(t)
	id := f.submitAndDispatch(emailInput())
	reference, code := verificationFrom(t, f.mail.messages[0])
	b := f.membershipAdminBrowser()
	count, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
	f.must(err)
	if count != 0 {
		t.Fatal("email counted as review")
	}
	r := b.call("GET", "/registration-reviews", nil)
	if !strings.Contains(r.Body.String(), "Vérification email en cours") || !strings.Contains(r.Body.String(), "Vérifications (0)") {
		t.Fatal("pending email presentation")
	}
	f.must(f.app.Verifications.VerifyEmail(t.Context(), reference, code))
	r = b.call("GET", reviewPath(id), nil)
	body := html.UnescapeString(r.Body.String())
	for _, v := range []string{"Vérification d’un email connu", "Intervention administrative : Aucune", "Résolue le"} {
		if !strings.Contains(body, v) {
			t.Fatal("missing automatic audit", v)
		}
	}
	for _, secret := range []string{reference, code, "code_hash", "recipient_hash"} {
		if strings.Contains(body, secret) {
			t.Fatal("proof leaked to administrator")
		}
	}
	for _, role := range []string{"secretary", "treasurer", "coach", "none"} {
		f.exec("DELETE FROM user_roles WHERE user_id=$1", f.approver)
		if role != "none" {
			f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name=$2", f.approver, role)
		}
		r = b.call("GET", reviewPath(id), nil)
		want := 403
		if role == "secretary" {
			want = 200
		}
		if r.Code != want {
			t.Fatal("proof RBAC")
		}
	}
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	if _, err = provider.DownTo(t.Context(), 18); err == nil {
		t.Fatal("audit erased by Down")
	}
	var used bool
	f.must(f.db.QueryRow(t.Context(), "SELECT used_at IS NOT NULL FROM registration_email_verifications WHERE public_reference=$1", reference).Scan(&used))
	if !used {
		t.Fatal("proof lost")
	}
	if _, err = f.db.Exec(t.Context(), "DELETE FROM registration_email_verifications WHERE public_reference=$1", reference); err == nil {
		t.Fatal("proof deleted")
	}
}

func TestEmailResolutionRollbackAndPreparationFailure(t *testing.T) {
	f := newFixture(t)
	id := f.stage(emailInput())
	d, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
	f.must(err)
	f.exec(`CREATE FUNCTION fail_email_resolution_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='resolved' THEN RAISE EXCEPTION 'private resolution error'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER fail_email_resolution_test BEFORE UPDATE ON registration_submissions FOR EACH ROW EXECUTE FUNCTION fail_email_resolution_test()`)
	if err = f.app.Verifications.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("unsafe resolution error", err)
	}
	var used bool
	f.must(f.db.QueryRow(t.Context(), "SELECT used_at IS NOT NULL FROM registration_email_verifications WHERE public_reference=$1", d.PublicReference).Scan(&used))
	if used || f.emailState(id) != "awaiting_email_verification" {
		t.Fatal("proof consumed without resolution")
	}
	f.exec("DROP TRIGGER fail_email_resolution_test ON registration_submissions")
	f.must(f.app.Verifications.VerifyEmail(t.Context(), d.PublicReference, d.PlaintextCode))
	// Failure while choosing the queued status rolls back the entire submission.
	f.exec(`CREATE FUNCTION fail_email_preparation_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='awaiting_email_verification' THEN RAISE EXCEPTION 'private preparation error'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER fail_email_preparation_test BEFORE UPDATE ON registration_submissions FOR EACH ROW EXECUTE FUNCTION fail_email_preparation_test()`)
	before := f.registrationSnapshot()
	if _, err = f.app.Submissions.CreateSubmission(t.Context(), emailInput()); !errors.Is(err, identityresolution.ErrUnavailable) {
		t.Fatal("enqueue failure accepted")
	}
	if before != f.registrationSnapshot() {
		t.Fatal("partial submission")
	}
	var count int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM registration_email_verifications WHERE submission_id=$1", id).Scan(&count))
	if count != 1 || f.emailState(id) != "resolved" || len(f.mail.messages) != 0 {
		t.Fatal("partially prepared or sent proof")
	}
	f.exec("DROP TRIGGER fail_email_preparation_test ON registration_submissions")
	// A changed candidate makes re-preparation definitively ineligible and returns
	// the old pending proof to review immediately rather than waiting for expiry.
	id = f.stage(emailInput())
	d, err = f.app.Verifications.PrepareEmailVerification(t.Context(), id)
	f.must(err)
	f.exec("UPDATE persons SET email='changed@example.test' WHERE id=$1", f.person)
	if _, err = f.app.Verifications.PrepareEmailVerification(t.Context(), id); !errors.Is(err, identityresolution.ErrNotEligible) {
		t.Fatal("changed email eligible")
	}
	if f.emailState(id) != "awaiting_identity_review" {
		t.Fatal("ineligible pending proof stranded")
	}
}

func TestEmailCompensationAssociationAndConstraints(t *testing.T) {
	f := newFixture(t)
	first := f.stage(emailInput())
	a, err := f.app.Verifications.PrepareEmailVerification(t.Context(), first)
	f.must(err)
	second := f.stage(emailInput())
	b, err := f.app.Verifications.PrepareEmailVerification(t.Context(), second)
	f.must(err)
	mismatched := *a
	mismatched.PublicReference = b.PublicReference
	if err = f.invalidateDelivery(t.Context(), &mismatched); err == nil {
		t.Fatal("unrelated reference accepted for compensation")
	}
	if f.emailState(first) != "awaiting_email_verification" || f.emailState(second) != "awaiting_email_verification" {
		t.Fatal("cross-submission compensation")
	}
	// The database enforces one live challenge, not only the service lock.
	_, err = f.db.Exec(t.Context(), `INSERT INTO registration_email_verifications(submission_id,person_id,public_reference,code_hash,recipient_hash,expires_at) SELECT submission_id,person_id,$2,code_hash,recipient_hash,expires_at FROM registration_email_verifications WHERE public_reference=$1`, a.PublicReference, strings.Repeat("e", 64))
	if err == nil {
		t.Fatal("multiple live challenges allowed by database")
	}
	// A new homonym appearing after preparation also prevents automatic resolution.
	f.id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES ('Rémi','Dupont','1990-01-01','remi@example.test') RETURNING id")
	if err = f.app.Verifications.VerifyEmail(t.Context(), a.PublicReference, a.PlaintextCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("late identity conflict accepted")
	}
	if f.emailState(first) != "awaiting_identity_review" {
		t.Fatal("late conflict not returned to review")
	}
}

func TestEmailHTTPRejectionsAreIndistinguishable(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	token := b.csrf(t, "/registration/verify")
	var publicBody string
	for _, reason := range []string{"unknown", "wrong", "invalidated", "used", "admin-resolved", "unavailable", "expired"} {
		id := f.stage(emailInput())
		svc := f.app.Verifications
		if reason == "expired" {
			var err error
			svc, err = identityresolution.NewEmailService(f.db, 20*time.Millisecond)
			f.must(err)
		}
		d, err := svc.PrepareEmailVerification(t.Context(), id)
		f.must(err)
		ref, code := d.PublicReference, d.PlaintextCode
		switch reason {
		case "unknown":
			ref = strings.Repeat("f", 64)
		case "wrong":
			code = strings.Repeat("x", 20)
		case "invalidated":
			f.must(f.invalidateDelivery(t.Context(), d))
		case "used":
			f.must(svc.VerifyEmail(t.Context(), ref, code))
		case "admin-resolved":
			f.must(f.app.Reviews.LinkPerson(t.Context(), f.approver, id, f.person))
		case "unavailable":
			f.exec("UPDATE persons SET archived_at=now() WHERE id=$1", f.person)
		case "expired":
			f.exec("SELECT pg_sleep(GREATEST(0,extract(epoch FROM (expires_at-clock_timestamp())))+0.01) FROM registration_email_verifications WHERE public_reference=$1", ref)
		}
		response := b.call("POST", "/registration/verify", url.Values{"csrf_token": {token}, "reference": {ref}, "code": {code}})
		if response.Code != 303 || response.Header().Get("Location") != "/registration/verify?result=failed" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("publicly distinguishable rejection", reason)
		}
		page := b.call("GET", response.Header().Get("Location"), nil)
		if publicBody == "" {
			publicBody = page.Body.String()
		} else if publicBody != page.Body.String() {
			t.Fatal("different failure page", reason)
		}
		for _, secret := range []string{d.PublicReference, d.PlaintextCode, "remi@example.test", "RÉMI", "Dupont"} {
			if strings.Contains(page.Body.String(), secret) || strings.Contains(response.Body.String(), secret) {
				t.Fatal("public disclosure", reason)
			}
		}
		if reason == "unavailable" {
			f.exec("UPDATE persons SET archived_at=NULL WHERE id=$1", f.person)
		}
	}
}
func TestEmailCompensationFailureLogsNoSecretsAndEventuallyRecovers(t *testing.T) {
	f := newFixture(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	svc, err := identityresolution.NewEmailService(f.db, time.Hour)
	f.must(err)
	submitter := identityresolution.NewEmailSubmitter(f.db, svc)
	f.exec(`CREATE FUNCTION fail_email_compensation_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'sensitive database detail'; END $$`)
	f.exec(`CREATE TRIGGER fail_email_compensation_test BEFORE UPDATE ON registration_email_verifications FOR EACH ROW EXECUTE FUNCTION fail_email_compensation_test()`)
	f.mail.err = errors.New("sensitive SMTP detail")
	result, err := submitter.CreateSubmission(t.Context(), emailInput())
	f.must(err)
	if result.Status != "submission accepted" {
		t.Fatal("compensation failure disclosed")
	}
	worker := outbox.New(f.db, svc, f.mail, "club@example.test", "https://club.example.test")
	if _, err = worker.ProcessOne(t.Context()); err == nil {
		t.Fatal("compensation error missing")
	}
	// Run logs DB categories; ProcessOne exposes errors to its internal caller.
	if logs.Len() != 0 && strings.Contains(logs.String(), "sensitive") {
		t.Fatal("sensitive operational error logged")
	}
	ref, code := verificationFrom(t, f.mail.messages[0])
	for _, secret := range []string{ref, code, "remi@example.test", "sensitive SMTP detail", "sensitive database detail"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("sensitive log disclosure")
		}
	}
	f.exec("DROP TRIGGER fail_email_compensation_test ON registration_email_verifications")
	f.exec("UPDATE registration_verification_outbox SET lease_until=clock_timestamp()-interval '1 second',attempt_count=3 WHERE status='processing'")
	_, err = worker.ProcessOne(t.Context())
	f.must(err)
	count, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
	f.must(err)
	if count != 1 {
		t.Fatal("compensation outage stranded dossier")
	}
}

// Verification UI tests explicitly drive asynchronous delivery before using a code.
func (f *fixture) submitAndDispatch(in identityresolution.SubmissionInput) int32 {
	f.t.Helper()
	id := f.submit(in)
	_, err := f.app.VerificationOutbox.ProcessOne(f.t.Context())
	f.must(err)
	return id
}

func (f *fixture) invalidateDelivery(ctx context.Context, d *identityresolution.EmailDelivery) error {
	tx, err := f.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = identityresolution.LockDeliveryDecision(ctx, tx, d.SubmissionID); err != nil {
		return err
	}
	if _, err = dbsqlc.New(tx).LockRegistrationSubmission(ctx, d.SubmissionID); err != nil {
		return err
	}
	if err = identityresolution.InvalidateDeliveryAttempt(ctx, tx, d); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
