package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/grapinou/club-core/internal/identitycorrections"
)

func identityProposal() url.Values {
	return url.Values{"first_name": {"  Camille  "}, "last_name": {"  Dùpont  "}, "birth_date": {"28/02/1991"}}
}
func correctionPath(id int32) string { return fmt.Sprintf("/identity-corrections/%d", id) }
func (f *fixture) correctionID() int32 {
	return f.id("SELECT id FROM identity_correction_requests WHERE person_id=?1 AND status='pending'", f.person)
}

func TestP443NavigationAndImmediateDashboard(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	f.request()
	for i, name := range []string{"PasseSansResultat", "DuJour", "DuLendemain", "ApresDemain"} {
		p := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES(?1,'Essai','1990-01-01') RETURNING id", name)
		f.exec("INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,date(CURRENT_DATE,?3||' days'),'registered')", p, f.activity, i-1)
	}
	body := officeOK(t, b, "/admin", "DuJour", "DuLendemain", "Adhésions en attente")
	main := pagePart(t, body, `<main id="main-content"`, `</main>`)
	if strings.Index(main, "Adhésions en attente") > strings.Index(main, "Essais d’aujourd’hui") || strings.Index(main, "Essais d’aujourd’hui") > strings.Index(main, "Essais de demain") {
		t.Fatal("immediate work order")
	}
	for _, unwanted := range []string{"PasseSansResultat", "ApresDemain", "Résultat à renseigner"} {
		if strings.Contains(main, unwanted) {
			t.Fatal("dashboard trial scope", unwanted)
		}
	}
	officeOK(t, b, "/trials?all=1&search=PasseSansResultat", "PasseSansResultat")
	for _, path := range []string{"/dashboard", "/me/account"} {
		body = officeOK(t, b, path)
		nav := pagePart(t, body, `<nav class="admin-nav"`, `</nav>`)
		if !strings.Contains(nav, `href="/dashboard" aria-current="page"`) || strings.Contains(nav, "personal-space-link") {
			t.Fatal("personal navigation style/current")
		}
	}
	body = officeOK(t, b, "/admin")
	nav := pagePart(t, body, `<nav class="admin-nav"`, `</nav>`)
	if strings.Contains(nav, `href="/dashboard" aria-current="page"`) || !strings.Contains(nav, `href="/admin" aria-current="page"`) {
		t.Fatal("office current link")
	}
	// GET never logs the user out; existing POST/CSRF lifecycle tests remain.
	b.call("GET", "/logout", nil)
	officeOK(t, b, "/admin")
}

func TestP443IdentityRequestReviewAndIsolation(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	office := p43Secretary(f)
	path := "/me/account/identity"
	anon := newBrowser(f.app.Handler)
	for _, method := range []string{"GET", "POST"} {
		if r := anon.call(method, path, nil); r.Code != 303 || r.Header().Get("Location") != "/login" {
			t.Fatal("anonymous identity", method, r.Code)
		}
	}
	body := f.personalOK(b, path, `value="Rémi"`, `value="Dupont"`, `value="01/01/1990"`)
	if strings.Contains(body, `name="username"`) {
		t.Fatal("editable username")
	}
	foreign := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Autre','Intact','1980-01-01') RETURNING id")
	form := identityProposal()
	form.Set("person_id", fmt.Sprint(foreign))
	form.Set("requesting_user_id", fmt.Sprint(f.approver))
	form.Set("username", "INJECTED")
	form.Set("status", "approved")
	form.Set("reviewed_by_user_id", fmt.Sprint(f.approver))
	for _, token := range []string{"", "wrong"} {
		form.Set("csrf_token", token)
		if b.call("POST", path, form).Code != 403 {
			t.Fatal("identity CSRF")
		}
	}
	f.accountPost(b, path, form, 303)
	id := f.correctionID()
	if f.count("SELECT count(*) FROM identity_correction_requests WHERE id=?1 AND person_id=?2 AND requesting_user_id=?3 AND proposed_first_name='Camille' AND proposed_last_name='Dùpont' AND original_first_name='Rémi'", id, f.person, user) != 1 {
		t.Fatal("proposal normalization/ownership")
	}
	if f.count("SELECT count(*) FROM persons WHERE id=?1 AND first_name='Rémi' AND last_name='Dupont' AND birth_date='1990-01-01'", f.person) != 1 {
		t.Fatal("identity changed before approval")
	}
	if f.count("SELECT count(*) FROM persons WHERE id=?1 AND first_name='Autre'", foreign) != 1 {
		t.Fatal("foreign identity changed")
	}
	f.personalOK(b, path+"?sent=1", "Votre identité actuelle reste inchangée", "en cours de vérification")
	f.personalOK(b, "/me/account", "Rémi", "en cours de vérification")
	f.accountPost(b, path, identityProposal(), 409)
	if f.count("SELECT count(*) FROM identity_correction_requests") != 1 {
		t.Fatal("parallel pending requests")
	}
	officeOK(t, office, "/admin", "correction d’identité à vérifier")
	officeOK(t, office, "/registration-reviews", "Corrections d’identité", "Rémi Dupont")
	officeOK(t, office, correctionPath(id), "Rémi", "Camille", "Dùpont", "28/02/1991", "Identité actuelle et proposée", "Valider", "Refuser")
	if b.call("GET", "/identity-corrections", nil).Code != 403 {
		t.Fatal("unauthorized review list")
	}
	for _, method := range []string{"GET", "POST"} {
		if b.call(method, correctionPath(id), url.Values{"csrf_token": {b.csrf(t, path)}, "decision": {"approved"}}).Code != 403 {
			t.Fatal("unauthorized review", method)
		}
	}
	if err := f.app.IdentityCorrections.Review(f.authenticatedContext(user), id, "approved"); err == nil {
		t.Fatal("unauthorized direct service review")
	}
	if office.call("POST", correctionPath(id), url.Values{"csrf_token": {"invalid"}, "decision": {"approved"}}).Code != 403 {
		t.Fatal("invalid review CSRF")
	}
	if office.call("POST", correctionPath(id), url.Values{"decision": {"approved"}}).Code != 403 {
		t.Fatal("review CSRF")
	}
	officePost(t, office, correctionPath(id), url.Values{"decision": {"approved"}}, 303)
	if f.count("SELECT count(*) FROM persons WHERE id=?1 AND first_name='Camille' AND last_name='Dùpont' AND birth_date='1991-02-28'", f.person) != 1 {
		t.Fatal("approved correction missing")
	}
	if f.count("SELECT count(*) FROM users WHERE id=?1 AND username='member' AND person_id=?2", user, f.person) != 1 {
		t.Fatal("account identity binding changed")
	}
	if f.count("SELECT count(*) FROM identity_correction_requests WHERE id=?1 AND status='approved' AND reviewed_by_user_id=?2 AND reviewed_at IS NOT NULL", id, f.approver) != 1 {
		t.Fatal("review evidence")
	}
	if f.count("SELECT count(*) FROM administrative_events WHERE action='person_identity_corrected' AND actor_user_id=?1 AND resource_id=?2", f.approver, f.person) != 1 {
		t.Fatal("identity audit")
	}
	officePost(t, office, correctionPath(id), url.Values{"decision": {"rejected"}}, 409)
	// A new request is possible after a decision; a refusal preserves identity.
	form = identityProposal()
	form.Set("first_name", "Refusée")
	f.accountPost(b, path, form, 303)
	id = f.correctionID()
	officePost(t, office, correctionPath(id), url.Values{"decision": {"rejected"}}, 303)
	if f.count("SELECT count(*) FROM persons WHERE id=?1 AND first_name='Camille'", f.person) != 1 || f.count("SELECT count(*) FROM identity_correction_requests WHERE id=?1 AND status='rejected'", id) != 1 {
		t.Fatal("rejected identity applied")
	}
	if f.count("SELECT count(*) FROM administrative_events WHERE action='person_identity_correction_rejected' AND resource_id=?1", f.person) != 1 {
		t.Fatal("rejection audit")
	}
	// One user cannot see another user's request or internal review details.
	_, other := f.personalBrowser(foreign, "other")
	ownPage := f.personalOK(other, path+"?person_id="+fmt.Sprint(f.person), "Autre")
	if strings.Contains(ownPage, "Camille") || strings.Contains(ownPage, "Refusée") {
		t.Fatal("personal request IDOR")
	}
	if office.call("GET", correctionPath(999999), nil).Code != 404 {
		t.Fatal("unknown correction")
	}
}

func TestP443IdentityValidationConcurrencyAndRollback(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	path := "/me/account/identity"
	for _, change := range []url.Values{
		{"first_name": {""}}, {"last_name": {strings.Repeat("é", 201)}}, {"first_name": {"bad\x00name"}}, {"first_name": {string([]byte{0xff})}},
		{"birth_date": {"30/02/2026"}}, {"birth_date": {"31/12/9999"}}, {"birth_date": {"01/01/0000"}},
	} {
		form := identityProposal()
		for k, v := range change {
			form[k] = v
		}
		f.accountPost(b, path, form, 422)
	}
	if f.count("SELECT count(*) FROM identity_correction_requests") != 0 {
		t.Fatal("invalid proposals persisted")
	}
	ctx := f.authenticatedContext(user)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- f.app.IdentityCorrections.Request(ctx, "Camille", "Dupont", "1991-02-28")
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, pending := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, identitycorrections.ErrPending) {
			pending++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || pending != 1 {
		t.Fatal("concurrent proposal", success, pending)
	}
	id := f.correctionID()
	adminctx := f.authenticatedContext(f.approver)
	f.exec("CREATE TRIGGER reject_identity_audit BEFORE INSERT ON administrative_events WHEN NEW.action='person_identity_corrected' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END;")
	if err := f.app.IdentityCorrections.Review(adminctx, id, "approved"); err == nil {
		t.Fatal("audit failure accepted")
	}
	if f.count("SELECT count(*) FROM persons WHERE id=?1 AND first_name='Rémi'", f.person) != 1 || f.correctionID() != id {
		t.Fatal("non-atomic identity/audit")
	}
	f.exec("DROP TRIGGER reject_identity_audit")
	start = make(chan struct{})
	results = make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- f.app.IdentityCorrections.Review(adminctx, id, "approved")
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, closed := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, identitycorrections.ErrClosed) {
			closed++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || closed != 1 || f.count("SELECT count(*) FROM administrative_events WHERE action='person_identity_corrected'") != 1 {
		t.Fatal("double review")
	}
	if _, err := f.db.ExecContext(t.Context(), "UPDATE identity_correction_requests SET proposed_first_name='changed' WHERE id=?1", id); err == nil {
		t.Fatal("evidence mutable")
	}
	if _, err := f.db.ExecContext(t.Context(), "DELETE FROM identity_correction_requests WHERE id=?1", id); err == nil {
		t.Fatal("evidence removed")
	}
	if err := f.app.IdentityCorrections.Review(context.Background(), id, "approved"); err == nil {
		t.Fatal("anonymous review service")
	}
	// A stale proposal cannot overwrite a legitimate intervening correction.
	f.must(f.app.IdentityCorrections.Request(ctx, "Nouvelle", "Dupont", "1991-02-28"))
	id = f.correctionID()
	f.exec("UPDATE persons SET first_name='Club' WHERE id=?1", f.person)
	if err := f.app.IdentityCorrections.Review(adminctx, id, "approved"); !errors.Is(err, identitycorrections.ErrChanged) {
		t.Fatal("stale reference", err)
	}
	f.must(f.app.IdentityCorrections.Review(adminctx, id, "rejected"))
}

func TestP443UnifiedCoordinatesAndEmail(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	path := "/me/account/profile"
	body := f.personalOK(b, path, `name="new_email"`, `value="remi@example.test"`, `name="phone_number"`, `name="address"`)
	if strings.Contains(body, `autocomplete="current-password" required`) {
		t.Fatal("phone update unnecessarily requires password")
	}
	form := url.Values{"new_email": {"REMI@example.test"}, "phone_number": {"0612345678"}, "address": {"12 rue du Club"}}
	f.accountPost(b, path, form, 303)
	if len(f.mail.messages) != 0 || f.count("SELECT count(*) FROM user_email_change_requests") != 0 {
		t.Fatal("unchanged email requested verification")
	}
	form.Set("new_email", "new@example.test")
	form.Set("current_password", "bad")
	form.Set("address", "new address")
	f.accountPost(b, path, form, 422)
	if f.count("SELECT count(*) FROM persons WHERE id=?1 AND address='12 rue du Club' AND phone_number='33612345678'", f.person) != 1 {
		t.Fatal("bad password partially saved contacts")
	}
	form.Set("current_password", "a secure password")
	f.accountPost(b, path, form, 303)
	if f.personEmail(f.person) != "remi@example.test" || f.count("SELECT count(*) FROM persons WHERE id=?1 AND address='new address'", f.person) != 1 {
		t.Fatal("contact/email pending semantics")
	}
	f.personalOK(b, "/me/account/email/verify?sent=1&contacts_saved=1", "téléphone et adresse ont été enregistrés", "adresse actuelle reste utilisée")
	oldcode := emailChangeCode(t, f.mail.messages[len(f.mail.messages)-1])
	form.Set("new_email", "final@example.test")
	f.accountPost(b, path, form, 303)
	code := emailChangeCode(t, f.mail.messages[len(f.mail.messages)-1])
	f.accountPost(b, "/me/account/email/verify", url.Values{"code": {oldcode}}, 422)
	f.accountPost(b, "/me/account/email/verify", url.Values{"code": {"wrong"}}, 422)
	f.accountPost(b, "/me/account/email/verify", url.Values{"code": {code}}, 303)
	if f.personEmail(f.person) != "final@example.test" {
		t.Fatal("verification did not activate email")
	}
	f.personalOK(b, "/me/account") // Existing email behavior keeps the active session.
	if f.count("SELECT count(*) FROM account_security_events WHERE user_id=?1 AND event='email_change_requested'", user) != 2 || f.count("SELECT count(*) FROM account_security_events WHERE user_id=?1 AND event='email_changed'", user) != 1 {
		t.Fatal("email audit lost")
	}
	f.mail.err = errors.New("delivery unavailable")
	form.Set("new_email", "delivery@example.test")
	form.Set("address", "contacts saved despite delivery")
	body = f.accountPost(b, path, form, 503)
	if !strings.Contains(body, "téléphone et adresse ont été enregistrés") || f.personEmail(f.person) != "final@example.test" || f.count("SELECT count(*) FROM persons WHERE id=?1 AND address='contacts saved despite delivery'", f.person) != 1 {
		t.Fatal("delivery failure lost saved coordinates")
	}
}

func TestP443BirthCorrectionPreservesEffectiveGuardianAccess(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	_, guardian := f.personalBrowser(parent, "guardian")
	_, member := f.personalBrowser(child, "child")
	adminctx := f.authenticatedContext(f.approver)
	_, err := f.app.GuardianAccess.Grant(adminctx, child, parent)
	f.must(err)
	f.personalOK(guardian, personalChild(child))
	f.accountPost(member, "/me/account/identity", url.Values{"first_name": {"Lina"}, "last_name": {"Famille"}, "birth_date": {"01/01/1990"}}, 303)
	id := f.id("SELECT id FROM identity_correction_requests WHERE person_id=?1 AND status='pending'", child)
	f.personalOK(guardian, personalChild(child)) // Pending date cannot change access.
	f.must(f.app.IdentityCorrections.Review(adminctx, id, "approved"))
	f.personalDenied(guardian, personalChild(child)) // Existing civil majority rule.
	if f.count("SELECT count(*) FROM guardian_access_grants WHERE child_person_id=?1 AND guardian_person_id=?2 AND revoked_at IS NULL", child, parent) != 1 {
		t.Fatal("guardian evidence rewritten")
	}
	if f.count("SELECT count(*) FROM person_guardians WHERE child_person_id=?1 AND guardian_person_id=?2", child, parent) != 1 {
		t.Fatal("family relation rewritten")
	}
}
