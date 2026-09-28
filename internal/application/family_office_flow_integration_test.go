package application

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/websecurity"
)

func (f *fixture) familyForm(b *browser, path string) url.Values {
	f.t.Helper()
	v := f.childForm(b)
	page := b.call("GET", path, nil)
	if page.Code != 200 {
		f.t.Fatal(page.Code, page.Body.String())
	}
	v.Set("csrf_token", hiddenValue(f.t, page.Body.String(), "csrf_token"))
	v.Set("presentation", hiddenValue(f.t, page.Body.String(), "presentation"))
	for key := range v {
		if strings.HasPrefix(key, "guardian_") {
			delete(v, key)
		}
	}
	return v
}
func TestP42FamilyEntryAndSessionIdentity(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	user, b := f.personalBrowser(parent, "parent.p42")
	_, adult := f.personalBrowser(f.person, "adult.p42")
	for _, browser := range []*browser{b, adult} {
		body := f.personalOK(browser, "/dashboard")
		if strings.Contains(body, "Ajouter un enfant") {
			t.Fatal("family action without effective grant")
		}
		if r := browser.call("GET", "/me/children/new", nil); r.Code != 403 {
			t.Fatal("family entry must be denied", r.Code)
		}
	}
	_, err := f.app.GuardianAccess.Grant(f.authenticatedContext(f.approver), child, parent)
	f.must(err)
	f.personalOK(b, "/dashboard", "Ajouter un enfant")
	page := f.personalOK(b, "/me/children/new", "Claire", "claire@example.test")
	for _, key := range []string{"guardian_first_name", "guardian_last_name", "guardian_email", "guardian_phone_number"} {
		if strings.Contains(page, `name="`+key+`"`) {
			t.Fatal("guardian re-entry", key)
		}
	}
	f.personalOK(b, "/join/child", `name="guardian_first_name"`)
	v := f.familyForm(b, "/me/children/new")
	v.Set("first_name", "Nouvel enfant")
	v.Set("last_name", "Autre famille")
	v.Set("guardian_person_id", fmt.Sprint(f.person))
	v.Set("person_id", fmt.Sprint(f.person))
	v.Set("guardian_first_name", "FORGED")
	v.Set("guardian_email", "forged@example.test")
	v.Set("action", "review")
	r := b.call("POST", "/me/children/new", v)
	if r.Code != 200 || strings.Contains(r.Body.String(), "FORGED") || strings.Contains(r.Body.String(), `name="guardian_person_id"`) {
		t.Fatal("forged review", r.Code, r.Body.String())
	}
	if f.count(`SELECT count(*) FROM registration_applications`) != 0 {
		t.Fatal("review persisted")
	}
	v.Set("action", "submit")
	noCSRF := cloneForm(v)
	noCSRF.Del("csrf_token")
	if r = b.call("POST", "/me/children/new", noCSRF); r.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	for i := 0; i < 2; i++ {
		if r = b.call("POST", "/me/children/new", v); r.Code != 303 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	if f.count(`SELECT count(*) FROM registration_applications`) != 1 {
		t.Fatal("retry duplicated")
	}
	id := f.id(`SELECT max(submission_id) FROM registration_applications`)
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Child.Guardian.ResolvedPersonID.Int32 != parent || d.Child.Guardian.ResolvedByUserID.Int32 != user || d.Child.Guardian.Email != "claire@example.test" {
		t.Fatal("session was not authoritative")
	}
	if d.Application.LastErrorCode.String != "guardian_confirmation_required" || d.Application.MembershipID.Valid {
		t.Fatal("new relation skipped review")
	}
	if f.count(`SELECT count(*) FROM guardian_access_grants`) != 1 || f.count(`SELECT count(*) FROM person_guardians`) != 1 {
		t.Fatal("premature family grant")
	}
	// An exact existing child match remains an administrative decision, even when
	// the requesting guardian has a different, valid family space.
	other := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Arthur','Famille',$1) RETURNING id`, v.Get("birth_date"))
	v = f.familyForm(b, "/me/children/new")
	if r = b.call("POST", "/me/children/new", v); r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	id = f.id(`SELECT max(submission_id) FROM registration_applications`)
	d, err = f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Submission.Status != "awaiting_identity_review" || d.Submission.ResolvedPersonID.Valid || d.Application.MembershipID.Valid {
		t.Fatal("existing child automatically resolved")
	}
	if f.count(`SELECT count(*) FROM person_guardians WHERE child_person_id=$1`, other) != 0 {
		t.Fatal("arbitrary relationship")
	}
	f.exec(`UPDATE persons SET email=NULL WHERE id=$1`, parent)
	missingEmail := f.familyForm(b, "/me/children/new")
	if r = b.call("POST", "/me/children/new", missingEmail); r.Code != 422 || !strings.Contains(r.Body.String(), "Complétez l’adresse email de votre compte") {
		t.Fatal("missing account contact not explained", r.Code)
	}
	f.exec(`UPDATE persons SET email='claire@example.test' WHERE id=$1`, parent)
	f.must(f.app.GuardianAccess.Revoke(f.authenticatedContext(f.approver), child, parent))
	if r = b.call("POST", "/me/children/new", v); r.Code != 403 {
		t.Fatal("revoked family entry")
	}
}

func TestP42ManagedChildActionsAndUniqueSeason(t *testing.T) {
	f := newFixture(t)
	consent := f.consentDefinition()
	child, parent := f.guardianPair()
	_, b := f.personalBrowser(parent, "known.p42")
	_, err := f.app.GuardianAccess.Grant(f.authenticatedContext(f.approver), child, parent)
	f.must(err)
	path := personalChild(child) + "/join"
	for _, denied := range []string{personalChild(f.person) + "/join", "/me/children/0/join"} {
		if r := b.call("GET", denied, nil); r.Code != 403 {
			t.Fatal("unmanaged child request", r.Code)
		}
	}

	f.personalOK(b, "/dashboard", "Demander une adhésion", path)
	f.personalOK(b, personalChild(child), "Demander une adhésion")
	v := f.familyForm(b, path)
	for _, key := range []string{"first_name", "last_name", "birth_date"} {
		v.Set(key, "FORGED")
	}
	v.Set("guardian_person_id", fmt.Sprint(f.person))
	v.Set("child_person_id", fmt.Sprint(f.person))
	r := b.call("POST", path, v)
	if r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	id := f.id(`SELECT id FROM memberships WHERE person_id=$1 AND season_id=$2`, child, f.season)
	if r.Header().Get("Location") != familyMembership(child, id) {
		t.Fatal("wrong destination")
	}
	if f.count(`SELECT count(*) FROM membership_consents WHERE membership_id=$1 AND given_by_person_id=$2 AND consent_definition_id=$3 AND decision='refused'`, id, parent, consent) != 1 {
		t.Fatal("consent attribution")
	}
	for _, route := range []string{"/dashboard", personalChild(child)} {
		body := f.personalOK(b, route, "Voir le dossier", familyMembership(child, id))
		if strings.Contains(body, "Demander une adhésion") {
			t.Fatal("pending duplicate action")
		}
	}
	if r = b.call("POST", path, v); r.Code != 422 || !strings.Contains(r.Body.String(), "existe déjà") {
		t.Fatal("duplicate not explained", r.Code)
	}
	if f.count(`SELECT count(*) FROM memberships WHERE person_id=$1`, child) != 1 {
		t.Fatal("duplicate membership")
	}
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES($1,$2,1)`, child, parent)
	_, err = f.app.Accounts.ApproveMembership(t.Context(), id, f.approver, nil)
	f.must(err)
	f.personalOK(b, "/dashboard", "Voir l’adhésion", familyMembership(child, id))
	// A second open season permits a new request independently of the first.
	f.id(`INSERT INTO seasons(name,starts_at,ends_at) VALUES('Suivante','2027-09-01','2028-08-31') RETURNING id`)
	f.personalOK(b, personalChild(child), "Voir l’adhésion", "Demander une adhésion", "Suivante")
	f.must(f.app.GuardianAccess.Revoke(f.authenticatedContext(f.approver), child, parent))
	if r = b.call("POST", path, v); r.Code != 403 {
		t.Fatal("revoked child mutation")
	}
}

func TestP42RolesFiltersArchiveAndHistory(t *testing.T) {
	f := newFixture(t)
	membership := f.request()
	child := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Petit','Filtre','2016-01-01') RETURNING id`)
	f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES($1,$2,'guardian')`, child, f.person)
	prospect := f.id(`INSERT INTO persons(first_name,last_name,birth_date,notes) VALUES('Prospect','Filtre','1990-01-01','Historique conservé') RETURNING id`)
	trial := f.id(`INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,CURRENT_DATE-1,'attended') RETURNING id`, prospect, f.activity)
	_, parent := f.guardianPair()
	office := f.membershipAdminBrowser()
	f.exec(`DELETE FROM user_roles WHERE user_id=$1`, f.approver)
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'`, f.approver)
	officeOK(t, office, "/admin", "Administration", "Secrétaire")
	if r := office.call("GET", "/admin/users", nil); r.Code != 403 {
		t.Fatal("secretary gained president permission")
	}
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='president'`, f.approver)
	officeOK(t, office, "/admin", "Président", "Secrétaire")
	if r := office.call("GET", "/admin/users", nil); r.Code != 200 {
		t.Fatal("president permission changed")
	}
	f.exec(`DELETE FROM user_roles WHERE user_id=$1 AND role_id=(SELECT id FROM roles WHERE name='secretary')`, f.approver)
	body := officeOK(t, office, "/admin", "Président")
	if strings.Contains(body, ">Secrétaire</span>") {
		t.Fatal("stale role")
	}
	_, ordinary := f.personalBrowser(parent, "ordinary.p42")
	if strings.Contains(f.personalOK(ordinary, "/dashboard"), "administrative-roles") {
		t.Fatal("false administrative badge")
	}
	if r := ordinary.call("GET", "/admin", nil); r.Code != 403 {
		t.Fatal("ordinary administrative access")
	}
	for _, tc := range []struct {
		category        string
		present, absent []int32
	}{
		{"prospects", []int32{prospect}, []int32{f.person, parent}},
		{"memberships", []int32{f.person}, []int32{prospect, parent}},
		{"guardians", []int32{f.person, parent}, []int32{prospect}},
	} {
		body := officeOK(t, office, "/persons?category="+tc.category)
		for _, id := range tc.present {
			if !strings.Contains(body, `href="`+officePerson(id)+`"`) {
				t.Fatal("missing category person", tc.category, id)
			}
		}
		for _, id := range tc.absent {
			if strings.Contains(body, `href="`+officePerson(id)+`"`) {
				t.Fatal("wrong category person", tc.category, id)
			}
		}
	}
	page := officeOK(t, office, officePerson(prospect), "Archiver", "historique sera conservé")
	token := hiddenValue(t, page, "csrf_token")
	if r := ordinary.call("POST", officePerson(prospect)+"/archive", url.Values{"csrf_token": {token}}); r.Code != 403 {
		t.Fatal("unauthorized archive")
	}
	if r := office.call("POST", officePerson(prospect)+"/archive", url.Values{}); r.Code != 403 {
		t.Fatal("archive CSRF")
	}
	for _, id := range []int32{prospect, f.person} {
		r := office.call("POST", officePerson(id)+"/archive", url.Values{"csrf_token": {token}})
		if r.Code != 303 {
			t.Fatal("archive", r.Code)
		}
		if strings.Contains(officeOK(t, office, "/persons"), `href="`+officePerson(id)+`"`) {
			t.Fatal("archived in current list")
		}
		officeOK(t, office, "/persons/archived", officePerson(id)+"/restore")
	}
	if f.count(`SELECT count(*) FROM trial_registrations WHERE id=$1`, trial) != 1 || f.count(`SELECT count(*) FROM memberships WHERE id=$1`, membership.ID) != 1 || f.count(`SELECT count(*) FROM person_guardians WHERE guardian_person_id=$1`, f.person) != 1 || f.count(`SELECT count(*) FROM persons WHERE id=$1 AND notes='Historique conservé'`, prospect) != 1 {
		t.Fatal("archive erased history")
	}
	for _, id := range []int32{prospect, f.person} {
		if r := office.call("POST", officePerson(id)+"/restore", url.Values{}); r.Code != 403 {
			t.Fatal("restore CSRF")
		}
		if r := office.call("POST", officePerson(id)+"/restore", url.Values{"csrf_token": {token}}); r.Code != 303 {
			t.Fatal("restore")
		}
		officeOK(t, office, "/persons", `href="`+officePerson(id)+`"`)
	}
	// Filter remains in a search POST.
	r := office.call("POST", "/persons/search", url.Values{"csrf_token": {token}, "category": {"guardians"}, "search": {"Dupont"}})
	if r.Code != 200 || !strings.Contains(r.Body.String(), officePerson(f.person)) {
		t.Fatal("filtered search")
	}
}

type p42ReadOnlyReview struct{}

func (p42ReadOnlyReview) HasPermission(_ context.Context, _ int32, p authorization.Permission) (bool, error) {
	return p == authorization.RegistrationsReview || p == authorization.MembershipsRead, nil
}

func TestP42ReviewOffersExistingApproval(t *testing.T) {
	f := newFixture(t)
	f.exec(`DELETE FROM user_roles WHERE user_id=$1`, f.approver)
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'`, f.approver)
	public := newBrowser(f.app.Handler)
	v := f.childForm(public)
	v.Set("emergency_contact", "no")
	sub := f.childSubmit(public, v)
	ctx := f.authenticatedContext(f.approver)
	f.must(f.app.RegistrationApplications.ConfirmGuardian(ctx, sub))
	m := f.publicApplication(sub).MembershipID.Int32
	office := f.membershipAdminBrowser()
	body := officeOK(t, office, reviewPath(sub), "Adhésion à compléter", "Ouvrir le dossier d’adhésion", "Contact d&#39;urgence manquant")
	if strings.Contains(body, "Valider l’adhésion") {
		t.Fatal("incomplete approval offered")
	}
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, sub)
	f.must(err)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES($1,$2,1)`, d.Submission.ResolvedPersonID, d.Child.Guardian.ResolvedPersonID)
	body = officeOK(t, office, reviewPath(sub), "Adhésion prête à valider", "Valider l’adhésion", approvePath(m), "La relation avec le responsable est résolue")
	// Exercise the handler's independent approval-permission branch. Current
	// built-in reviewer roles all approve; no production policy is changed here.
	mux := http.NewServeMux()
	checker := p42ReadOnlyReview{}
	access := handlers.NewAccess("Club Core", checker)
	handlers.NewRegistrationHandler("Club Core", time.UTC, f.app.Reviews, f.app.RegistrationApplications, f.memberships, checker).Register(mux, access, websecurity.NewCSRF(false))
	readOnly := newBrowser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mux.ServeHTTP(w, r.WithContext(ctx)) }))
	limited := readOnly.call("GET", reviewPath(sub), nil)
	if limited.Code != 200 || strings.Contains(limited.Body.String(), "Valider l’adhésion") {
		t.Fatal("read-only reviewer approval")
	}
	if r := office.call("POST", approvePath(m), url.Values{}); r.Code != 403 {
		t.Fatal("CSRF")
	}
	token := hiddenValue(t, body, "csrf_token")
	r := office.call("POST", approvePath(m), url.Values{"csrf_token": {token}})
	if r.Code != 303 || !strings.HasPrefix(r.Header().Get("Location"), dossierPath(m)+"?notice=approved") {
		t.Fatal("approval destination", r.Code, r.Header())
	}
	officeOK(t, office, r.Header().Get("Location"), "Adhésion validée")
	r = office.call("POST", approvePath(m), url.Values{"csrf_token": {token}})
	if !strings.Contains(r.Header().Get("Location"), "already_processed") {
		t.Fatal("double validation")
	}
	if strings.Contains(officeOK(t, office, reviewPath(sub)), "Valider l’adhésion") {
		t.Fatal("active offered again")
	}
}
