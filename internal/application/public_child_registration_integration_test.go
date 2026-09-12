package application

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/pressly/goose/v3"
	"html"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/minorsafety"
)

func (f *fixture) childForm(b *browser) url.Values {
	f.t.Helper()
	page := b.call("GET", "/join/child", nil)
	if page.Code != 200 {
		f.t.Fatal(page.Code, page.Body.String())
	}
	v := url.Values{"csrf_token": {hiddenValue(f.t, page.Body.String(), "csrf_token")}, "presentation": {hiddenValue(f.t, page.Body.String(), "presentation")}, "guardian_first_name": {"Claire"}, "guardian_last_name": {"Famille"}, "guardian_email": {"claire@example.test"}, "guardian_birth_date": {"1980-01-01"}, "first_name": {"Arthur"}, "last_name": {"Famille"}, "birth_date": {time.Now().AddDate(-12, 0, 0).Format("2006-01-02")}, "relationship_type": {"mother"}, "emergency_contact": {"yes"}, "season_id": {fmt.Sprint(f.season)}, "membership_type_id": {fmt.Sprint(f.kind)}, "activity_id": {fmt.Sprint(f.activity)}, "action": {"submit"}}
	defs, err := dbsqlc.New(f.db).ListActiveConsentDefinitions(f.t.Context())
	f.must(err)
	for _, d := range defs {
		v.Set(fmt.Sprintf("consent_%d", d.ID), "refused")
	}
	return v
}
func (f *fixture) childSubmit(b *browser, v url.Values) int32 {
	f.t.Helper()
	r := b.call("POST", "/join/child", v)
	if r.Code != 303 || r.Header().Get("Location") != "/join/child/submitted" {
		f.t.Fatalf("child submit %d: %s", r.Code, r.Body.String())
	}
	return f.id(`SELECT max(submission_id) FROM registration_applications`)
}
func TestPublicChildNewFamilyConfirmation(t *testing.T) {
	f := newFixture(t)
	consent := f.consentDefinition()
	b := newBrowser(f.app.Handler)
	v := f.childForm(b)
	page := b.call("GET", "/join/child", nil)
	for _, want := range []string{"Vos informations", "Informations de l’enfant", "guardian_email", "relationship_type", "emergency_contact", "Texte complet"} {
		if !strings.Contains(html.UnescapeString(page.Body.String()), want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(page.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("XSS")
	}
	v.Set("action", "review")
	r := b.call("POST", "/join/child", v)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Récapitulatif") {
		t.Fatal(r.Code, r.Body.String())
	}
	if f.count(`SELECT count(*) FROM guardian_identity_claims`) != 0 {
		t.Fatal("review persisted")
	}
	v.Set("action", "edit")
	if r = b.call("POST", "/join/child", v); r.Code != 200 {
		t.Fatal("edit", r.Code)
	}
	v.Set("action", "submit")
	id := f.childSubmit(b, v)
	f.childSubmit(b, v)
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Child == nil || d.Child.Guardian.Status != "resolved" || d.Submission.Status != "resolved" || d.Application.LastErrorCode.String != "guardian_confirmation_required" {
		t.Fatalf("%+v", d)
	}
	child, guardian := d.Submission.ResolvedPersonID.Int32, d.Child.Guardian.ResolvedPersonID.Int32
	if child == guardian || d.Submission.Email.Valid || d.Submission.PhoneNumber.Valid || d.Submission.Address.Valid {
		t.Fatal("identity contamination")
	}
	if f.count(`SELECT count(*) FROM guardian_identity_claims`) != 1 || f.count(`SELECT count(*) FROM child_registration_applications`) != 1 || f.count(`SELECT count(*) FROM persons WHERE id=ANY($1::integer[])`, []int32{child, guardian}) != 2 {
		t.Fatal("duplicate staging")
	}
	for _, table := range []string{"person_guardians", "guardian_access_grants", "person_emergency_contacts", "memberships", "registration_verification_outbox"} {
		if f.count("SELECT count(*) FROM "+table) != 0 {
			t.Fatal("premature", table)
		}
	}
	if f.count(`SELECT count(*) FROM users`) != 1 {
		t.Fatal("premature user")
	}
	ctx := f.authenticatedContext(f.approver)
	f.mail.err = errors.New("SMTP unavailable")
	f.must(f.app.RegistrationApplications.ConfirmGuardian(ctx, id))
	f.must(f.app.RegistrationApplications.ConfirmGuardian(ctx, id))
	a := f.publicApplication(id)
	if a.Status != "membership_created" {
		t.Fatal(a)
	}
	md, err := f.memberships.GetDetails(t.Context(), a.MembershipID.Int32)
	f.must(err)
	if md.Membership.Membership.PersonID != child || md.Membership.Membership.Status != "pending" || len(md.Activities) != 1 {
		t.Fatal("membership")
	}
	if f.count(`SELECT count(*) FROM membership_consents WHERE membership_id=$1 AND given_by_person_id=$2 AND consent_definition_id=$3`, a.MembershipID, guardian, consent) != 1 {
		t.Fatal("consent giver")
	}
	if f.count(`SELECT count(*) FROM person_guardians WHERE child_person_id=$1 AND guardian_person_id=$2 AND is_primary_contact`, child, guardian) != 1 || f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 || f.count(`SELECT count(*) FROM person_emergency_contacts WHERE priority=1`) != 1 {
		t.Fatal("relations")
	}
	if f.count(`SELECT count(*) FROM users WHERE person_id=$1`, child) != 0 || f.count(`SELECT count(*) FROM memberships WHERE person_id=$1`, guardian) != 0 {
		t.Fatal("child User or guardian membership")
	}
	if len(f.mail.messages) != 1 || f.count(`SELECT count(*) FROM user_activation_codes`) != 1 {
		t.Fatal("activation not reused")
	}
	user := f.id(`SELECT id FROM users WHERE person_id=$1`, guardian)
	if f.count(`SELECT count(*) FROM users WHERE id=$1 AND activated_at IS NOT NULL`, user) != 0 {
		t.Fatal("SMTP activated")
	}
	f.exec(`UPDATE users SET password_hash='hash',activated_at=now() WHERE id=$1`, user)
	guardianCtx := f.authenticatedContext(user)
	ok, err := f.app.GuardianAccess.CanManageChild(guardianCtx, child)
	f.must(err)
	if !ok {
		t.Fatal("guardian access")
	}
	// A child User is a test fixture only, created after proving the workflow creates none.
	childUser := f.activeUser(child, "child-fixture")
	result, err := f.app.MinorSafety.EvaluatePrivateConversation(guardianCtx, []int32{user, childUser})
	f.must(err)
	if result.Decision != minorsafety.Allowed {
		t.Fatal(result)
	}
}

func TestPublicChildValidation(t *testing.T) {
	f := newFixture(t)
	consent := f.consentDefinition()
	b := newBrowser(f.app.Handler)
	base := f.childForm(b)
	tests := []struct {
		name   string
		change func(url.Values)
		code   int
	}{
		{"missing guardian email", func(v url.Values) { v.Del("guardian_email") }, 422},
		{"bad guardian email", func(v url.Values) { v.Set("guardian_email", "bad") }, 422},
		{"bad child email", func(v url.Values) { v.Set("email", "bad") }, 422},
		{"missing birth", func(v url.Values) { v.Del("birth_date") }, 422},
		{"future", func(v url.Values) { v.Set("birth_date", time.Now().AddDate(1, 0, 0).Format("2006-01-02")) }, 422},
		{"adult", func(v url.Values) { v.Set("birth_date", time.Now().AddDate(-18, 0, 0).Format("2006-01-02")) }, 422},
		{"relationship", func(v url.Values) { v.Set("relationship_type", "invented") }, 422},
		{"emergency", func(v url.Values) { v.Del("emergency_contact") }, 422},
		{"activity", func(v url.Values) { v.Del("activity_id") }, 422},
		{"missing consent", func(v url.Values) { v.Del(fmt.Sprintf("consent_%d", consent)) }, 422},
		{"bad consent", func(v url.Values) { v.Set(fmt.Sprintf("consent_%d", consent), "bad") }, 422},
		{"csrf", func(v url.Values) { v.Del("csrf_token") }, 403},
		{"body", func(v url.Values) { v.Set("address", strings.Repeat("x", 33000)) }, 400},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := cloneForm(base)
			tt.change(v)
			b.ip = fmt.Sprintf("192.0.2.%d:1", i+2)
			r := b.call("POST", "/join/child", v)
			if r.Code != tt.code {
				t.Fatalf("%d %s", r.Code, r.Body.String())
			}
		})
	}
	if f.count(`SELECT count(*) FROM guardian_identity_claims`) != 0 {
		t.Fatal("invalid persisted")
	}
	for _, age := range []int{14, 15, 17} {
		v := cloneForm(base)
		v.Set("birth_date", time.Now().AddDate(-age, 0, 0).Format("2006-01-02"))
		v.Set("action", "review")
		b.ip = fmt.Sprintf("198.51.100.%d:1", age)
		r := b.call("POST", "/join/child", v)
		if r.Code != 200 {
			t.Fatal(age, r.Code)
		}
	}
}

func TestPublicChildKnownFamilyAndDuplicate(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	f.activeUser(parent, "known-parent")
	b := newBrowser(f.app.Handler)
	v := f.childForm(b)
	v.Set("birth_date", "2011-09-12")
	v.Set("relationship_type", "father")
	id := f.childSubmit(b, v)
	ctx := f.authenticatedContext(f.approver)
	d, err := f.app.Reviews.GetDetails(ctx, f.approver, id)
	f.must(err)
	if d.Child.Guardian.Status != "awaiting_review" || d.Submission.Status != "awaiting_identity_review" || len(d.Child.Candidates) != 1 || len(d.Candidates) != 1 {
		t.Fatal("not conservative")
	}
	if !d.Child.Candidates[0].MatchedEmail || !d.Child.Candidates[0].MatchedBirthDate || d.Child.Candidates[0].Confidence != "strong" {
		t.Fatal("matching reasons")
	}
	f.must(f.app.Reviews.ResolveGuardian(ctx, f.approver, id, &parent))
	d, err = f.app.Reviews.GetDetails(ctx, f.approver, id)
	f.must(err)
	if d.Submission.Status == "resolved" || d.Application.MembershipID.Valid {
		t.Fatal("guardian resolved child")
	}
	f.must(f.app.Reviews.LinkPerson(ctx, f.approver, id, child))
	a := f.publicApplication(id)
	if a.Status != "membership_created" {
		t.Fatal(a)
	}
	if f.count(`SELECT count(*) FROM person_guardians WHERE relationship_type='mother'`) != 1 || f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 {
		t.Fatal("known relation replaced")
	}
	// Second application has independent staging, preserves identity/relation and refuses duplicate membership.
	id2 := f.childSubmit(b, f.childForm(b))
	f.must(f.app.Reviews.LinkPerson(ctx, f.approver, id2, child))
	f.must(f.app.Reviews.ResolveGuardian(ctx, f.approver, id2, &parent))
	a = f.publicApplication(id2)
	if a.Status != "needs_review" || a.LastErrorCode.String != "membership_already_exists" || a.MembershipID.Valid {
		t.Fatal(a)
	}
	if f.count(`SELECT count(*) FROM memberships`) != 1 || f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 || f.count(`SELECT count(*) FROM person_emergency_contacts`) != 1 {
		t.Fatal("duplicates")
	}
	if err = f.app.Reviews.ResolveGuardian(ctx, f.approver, id2, &parent); !errors.Is(err, identityresolution.ErrClosed) {
		t.Fatal(err)
	}
}

func TestPublicChildConcurrentConfirmationAndEmergencyNo(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	v := f.childForm(b)
	v.Set("emergency_contact", "no")
	id := f.childSubmit(b, v)
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	child, guardian := d.Submission.ResolvedPersonID.Int32, d.Child.Guardian.ResolvedPersonID.Int32
	f.activeUser(guardian, "already-active")
	other := f.id(`INSERT INTO persons(first_name,last_name) VALUES('Other','Guardian') RETURNING id`)
	f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES($1,$2,'other',true)`, child, other)
	ctx := f.authenticatedContext(f.approver)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.app.RegistrationApplications.ConfirmGuardian(ctx, id) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		f.must(err)
	}
	a := f.publicApplication(id)
	md, err := f.memberships.GetDetails(t.Context(), a.MembershipID.Int32)
	f.must(err)
	if !strings.Contains(fmt.Sprint(md.Completeness.BlockingIssues), "minor_missing_emergency") {
		t.Fatal(md.Completeness)
	}
	if f.count(`SELECT count(*) FROM person_guardians WHERE child_person_id=$1`, child) != 2 || f.count(`SELECT count(*) FROM person_guardians WHERE guardian_person_id=$1 AND is_primary_contact`, other) != 1 {
		t.Fatal("primary replaced")
	}
	if f.count(`SELECT count(*) FROM person_emergency_contacts`) != 0 || f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 || f.count(`SELECT count(*) FROM memberships`) != 1 {
		t.Fatal("duplicates or fake emergency")
	}
}

func TestPublicChildMixedIdentitiesAndAdminSecurity(t *testing.T) {
	for _, existingGuardian := range []bool{false, true} {
		t.Run(fmt.Sprint(existingGuardian), func(t *testing.T) {
			f := newFixture(t)
			b := newBrowser(f.app.Handler)
			v := f.childForm(b)
			if existingGuardian {
				f.id(`INSERT INTO persons(first_name,last_name,email) VALUES('Claire','Famille','claire@example.test') RETURNING id`)
				f.id(`INSERT INTO persons(first_name,last_name,email) VALUES('Claire','Famille','other@example.test') RETURNING id`)
			} else {
				f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Arthur','Famille','2011-09-12') RETURNING id`)
			}
			id := f.childSubmit(b, v)
			d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
			f.must(err)
			if existingGuardian && (d.Child.Guardian.Status != "awaiting_review" || len(d.Child.Candidates) != 2 || d.Submission.Status != "resolved") {
				t.Fatal("guardian ambiguity")
			}
			if !existingGuardian && (d.Child.Guardian.Status != "resolved" || d.Submission.Status != "awaiting_identity_review") {
				t.Fatal("child ambiguity")
			}
			if r := b.call("POST", reviewPath(id)+"/confirm-guardian", url.Values{}); r.Code == 303 && r.Header().Get("Location") == reviewPath(id) {
				t.Fatal("anonymous confirmation")
			}
			admin := f.membershipAdminBrowser()
			path := reviewPath(id)
			csrf := admin.csrf(t, path)
			if r := admin.call("POST", path+"/confirm-guardian", url.Values{}); r.Code != 403 {
				t.Fatal("CSRF", r.Code)
			}
			if r := admin.call("POST", path+"/confirm-guardian", url.Values{"csrf_token": {csrf}}); r.Code == 303 {
				t.Fatal("unresolved confirmed")
			}
			action := "create-person"
			if existingGuardian {
				action = "create-guardian"
			}
			if r := admin.call("POST", path+"/"+action, url.Values{"csrf_token": {csrf}, "actor_id": {fmt.Sprint(f.person)}}); r.Code != 303 {
				t.Fatal("resolve", r.Code, r.Body.String())
			}
			d, err = f.app.Reviews.GetDetails(t.Context(), f.approver, id)
			f.must(err)
			actor := d.Submission.ResolvedByUserID
			if existingGuardian {
				actor = d.Child.Guardian.ResolvedByUserID
			}
			if !actor.Valid {
				t.Fatal("missing audit")
			}
			if f.count(`SELECT count(*) FROM person_guardians`) != 0 {
				t.Fatal("resolution confirmed relation")
			}
			if r := admin.call("POST", path+"/confirm-guardian", url.Values{"csrf_token": {csrf}}); r.Code != 303 {
				t.Fatal("confirm", r.Code, r.Body.String())
			}
			if r := admin.call("GET", path, nil); r.Code != 200 || !strings.Contains(r.Body.String(), "non activé") {
				t.Fatal("account state missing")
			}
			for _, query := range []string{`DELETE FROM guardian_identity_claims`, `UPDATE guardian_identity_claims SET first_name='changed'`, `DELETE FROM child_registration_applications`, `UPDATE child_registration_applications SET emergency_contact_requested=false`} {
				if _, err = f.db.Exec(t.Context(), query); err == nil {
					t.Fatal("audit mutable", query)
				}
			}
		})
	}
}

func TestPublicChildConcurrentSubmissionAndFinalizers(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	v := f.childForm(b)
	// Separate browsers share only immutable cookies and the same signed presentation.
	var wg sync.WaitGroup
	codes := make(chan int, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copyBrowser := newBrowser(f.app.Handler)
			for k, c := range b.cookies {
				copyBrowser.cookies[k] = c
			}
			codes <- copyBrowser.call("POST", "/join/child", cloneForm(v)).Code
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 303 {
			t.Fatal(code)
		}
	}
	if f.count(`SELECT count(*) FROM guardian_identity_claims`) != 1 || f.count(`SELECT count(*) FROM registration_submissions`) != 1 {
		t.Fatal("duplicate identity")
	}
	id := f.id(`SELECT submission_id FROM registration_applications`)
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	f.activeUser(d.Child.Guardian.ResolvedPersonID.Int32, "concurrent-guardian")
	ctx := f.authenticatedContext(f.approver)
	errs := make(chan error, 4)
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errs <- f.app.RegistrationApplications.ConfirmGuardian(ctx, id)
			} else {
				_, err := f.app.RegistrationApplications.Finalize(ctx, id)
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		f.must(err)
	}
	if f.count(`SELECT count(*) FROM memberships`) != 1 || f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 {
		t.Fatal("finalizer duplicates")
	}
}

func TestPublicChildEmergencyPriorityAndPrivacy(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	ctx := f.authenticatedContext(f.approver)
	f.activeUser(parent, "parent-priority")
	_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES($1,$2,4)`, child, f.person)
	b := newBrowser(f.app.Handler)
	before := b.call("GET", "/join/child/submitted", nil).Body.String()
	id := f.childSubmit(b, f.childForm(b))
	f.must(f.app.Reviews.ResolveGuardian(ctx, f.approver, id, &parent))
	f.must(f.app.Reviews.LinkPerson(ctx, f.approver, id, child))
	if f.count(`SELECT count(*) FROM person_emergency_contacts WHERE contact_person_id=$1 AND priority=5`, parent) != 1 {
		t.Fatal("priority")
	}
	if f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 {
		t.Fatal("grant duplicate")
	}
	after := b.call("GET", "/join/child/submitted", nil).Body.String()
	if before != after {
		t.Fatal("public match disclosure")
	}
	for _, pii := range []string{"Claire", "Arthur", "claire@example.test", "candidate", "Person #"} {
		if strings.Contains(after, pii) {
			t.Fatal("public PII", pii)
		}
	}
}

func TestPublicChildMigrationDownAndAudit(t *testing.T) {
	f := newFixture(t)
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.DownTo(t.Context(), 22)
	f.must(err)
	_, err = provider.Up(t.Context())
	f.must(err)
	b := newBrowser(f.app.Handler)
	f.childSubmit(b, f.childForm(b))
	if _, err = provider.DownTo(t.Context(), 22); err == nil {
		t.Fatal("discarded child audit")
	}
	for _, query := range []string{`TRUNCATE guardian_identity_claims CASCADE`, `TRUNCATE child_registration_applications`, `TRUNCATE guardian_identity_claim_candidates`} {
		if _, err = f.db.Exec(t.Context(), query); err == nil {
			t.Fatal("truncated audit")
		}
	}
}

func TestPublicChildConfirmationRejectsChangedAdult(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	id := f.childSubmit(b, f.childForm(b))
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	f.exec(`UPDATE persons SET birth_date='1990-01-01' WHERE id=$1`, d.Submission.ResolvedPersonID)
	ctx := f.authenticatedContext(f.approver)
	if err = f.app.RegistrationApplications.ConfirmGuardian(ctx, id); err == nil {
		t.Fatal("adult confirmed")
	}
	a, err := f.app.RegistrationApplications.Finalize(ctx, id)
	f.must(err)
	if a.LastErrorCode.String != "member_not_minor" {
		t.Fatal(a)
	}
	if f.count(`SELECT count(*) FROM person_guardians`) != 0 || f.count(`SELECT count(*) FROM memberships`) != 0 {
		t.Fatal("adult side effects")
	}
}

func TestPublicChildConcurrentIdentityResolution(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	b := newBrowser(f.app.Handler)
	id := f.childSubmit(b, f.childForm(b))
	ctx := f.authenticatedContext(f.approver)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.app.Reviews.ResolveGuardian(ctx, f.approver, id, nil) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, identityresolution.ErrClosed) {
			t.Fatal(err)
		}
	}
	if f.count(`SELECT count(*) FROM persons WHERE first_name='Claire'`) != 2 {
		t.Fatal("concurrent new guardian duplicated")
	}
	d, err := f.app.Reviews.GetDetails(ctx, f.approver, id)
	f.must(err)
	newGuardian := d.Child.Guardian.ResolvedPersonID.Int32
	if newGuardian == parent {
		t.Fatal("create merged candidate")
	}
	f.activeUser(newGuardian, "resolved-guardian")
	errs = make(chan error, 6)
	for i := range 6 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errs <- f.app.Reviews.LinkPerson(ctx, f.approver, id, child)
			} else {
				errs <- f.app.RegistrationApplications.ConfirmGuardian(ctx, id)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, identityresolution.ErrClosed) && !errors.Is(err, identityresolution.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	f.must(f.app.RegistrationApplications.ConfirmGuardian(ctx, id))
	if f.count(`SELECT count(*) FROM memberships`) != 1 || f.count(`SELECT count(*) FROM person_guardians`) != 2 || f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 {
		t.Fatal("concurrent identity/confirmation duplicates")
	}
}

func TestPublicChildRBACOriginAndSnapshot(t *testing.T) {
	f := newFixture(t)
	definition := f.consentDefinition()
	b := newBrowser(f.app.Handler)
	v := f.childForm(b)
	req := httptest.NewRequest("POST", "https://club.example.test/join/child", strings.NewReader(v.Encode()))
	req.Header.Set("Origin", "https://evil.test")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	response := httptest.NewRecorder()
	f.app.Handler.ServeHTTP(response, req)
	if response.Code != 403 {
		t.Fatal("cross origin", response.Code)
	}
	id := f.childSubmit(b, v)
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	outsider := f.activeUser(f.person, "outsider")
	outsiderCtx := f.authenticatedContext(outsider)
	if err = f.app.RegistrationApplications.ConfirmGuardian(outsiderCtx, id); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("RBAC", err)
	}
	if err = f.app.Reviews.ResolveGuardian(outsiderCtx, f.approver, id, nil); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("actor substitution", err)
	}
	if err = f.app.RegistrationApplications.ConfirmGuardian(t.Context(), id); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("anonymous", err)
	}
	f.exec(`UPDATE consent_definitions SET is_active=false WHERE id=$1`, definition)
	newer := f.id(`INSERT INTO consent_definitions(code,version,title,description) VALUES('image_web',3,'New','New wording') RETURNING id`)
	ctx := f.authenticatedContext(f.approver)
	f.must(f.app.RegistrationApplications.ConfirmGuardian(ctx, id))
	a := f.publicApplication(id)
	if f.count(`SELECT count(*) FROM membership_consent_requirements WHERE membership_id=$1 AND consent_definition_id=$2`, a.MembershipID, definition) != 1 || f.count(`SELECT count(*) FROM membership_consent_requirements WHERE membership_id=$1 AND consent_definition_id=$2`, a.MembershipID, newer) != 0 {
		t.Fatal("snapshot changed")
	}
	if f.count(`SELECT count(*) FROM membership_consents WHERE given_by_person_id=$1`, d.Submission.ResolvedPersonID) != 0 {
		t.Fatal("child gave consent")
	}
}
