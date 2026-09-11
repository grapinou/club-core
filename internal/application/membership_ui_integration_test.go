package application

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"html"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/memberships"
	"golang.org/x/crypto/bcrypt"
)

func (f *fixture) membershipAdminBrowser() *browser {
	f.t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	f.exec("UPDATE users SET password_hash=$2 WHERE id=$1", f.approver, string(hash))
	return f.loginBrowser("admin")
}
func dossierPath(id int32) string { return fmt.Sprintf("/memberships/%d", id) }
func approvePath(id int32) string { return dossierPath(id) + "/approve" }
func resendPath(id int32) string  { return fmt.Sprintf("/users/%d/resend-activation", id) }
func (f *fixture) assertNoDeliverySecrets(body string) {
	f.t.Helper()
	for _, message := range f.mail.messages {
		if strings.Contains(body, codeFrom(f.t, message)) {
			f.t.Fatal("plaintext activation code leaked")
		}
	}
	for _, secret := range []string{"password_hash", "$2a$", "$2b$", "a secure password"} {
		if strings.Contains(body, secret) {
			f.t.Fatal("account secret leaked")
		}
	}
}
func TestMembershipUIReadsAndPermissions(t *testing.T) {
	f := newFixture(t)
	pending := f.request()
	f.exec("UPDATE memberships SET requested_at='2020-01-01' WHERE id=$1", pending.ID)
	other := f.id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES ('Alice','Separate','1990-01-01','alice@example.test') RETURNING id")
	season := f.id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2030','2030-09-01','2031-08-31') RETURNING id")
	kind := f.id("INSERT INTO membership_types(name) VALUES ('Distinct type') RETURNING id")
	second, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: other, SeasonID: season, MembershipTypeID: kind, ActivityIDs: []int32{f.activity}})
	f.must(err)
	active, err := f.app.Accounts.ApproveMembership(t.Context(), second.ID, f.approver, nil)
	f.must(err)
	f.exec("UPDATE memberships SET requested_at='2030-01-01' WHERE id=$1", second.ID)
	for _, status := range []string{"ended", "cancelled"} {
		p := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES ($1,'History','1990-01-01') RETURNING id", status)
		f.id("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES ($1,$2,$3,$4) RETURNING id", p, f.season, f.kind, status)
	}
	// The batched read and detail use exactly the same evaluator.
	rows, err := f.memberships.List(t.Context())
	f.must(err)
	for _, row := range rows {
		detail, e := f.memberships.GetDetails(t.Context(), row.Membership.Membership.ID)
		f.must(e)
		if !reflect.DeepEqual(row.Completeness, detail.Completeness) {
			t.Fatal("list/detail completeness mismatch")
		}
	}
	for _, role := range []string{"anonymous", "none", "treasurer", "coach", "secretary", "president"} {
		t.Run(role, func(t *testing.T) {
			f.exec("DELETE FROM user_roles WHERE user_id=$1", f.approver)
			if role != "anonymous" && role != "none" {
				f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name=$2", f.approver, role)
			}
			b := newBrowser(f.app.Handler)
			if role != "anonymous" {
				b = f.membershipAdminBrowser()
			}
			allowed := role == "secretary" || role == "president"
			home := b.call("GET", "/", nil)
			if strings.Contains(home.Body.String(), `href="/memberships"`) != allowed {
				t.Fatal("navigation")
			}
			for _, path := range []string{"/memberships", dossierPath(pending.ID), dossierPath(second.ID)} {
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
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing no-store")
				}
				if !allowed {
					if strings.Contains(response.Body.String(), "remi@example.test") {
						t.Fatal("personal data leaked")
					}
					continue
				}
				body := html.UnescapeString(response.Body.String())
				f.assertNoDeliverySecrets(body)
				if path == "/memberships" {
					for _, value := range []string{"Dupont Rémi", "Separate Alice", "Distinct type", "pending", "active", "ended", "cancelled", "Complet"} {
						if !strings.Contains(body, value) {
							t.Fatal("missing list value", value)
						}
					}
					if strings.Index(body, `data-membership-id="`+fmt.Sprint(pending.ID)+`"`) > strings.Index(body, `data-membership-id="`+fmt.Sprint(second.ID)+`"`) {
						t.Fatal("pending ordering")
					}
				} else if path == dossierPath(pending.ID) {
					if strings.Contains(body, "Alice") || !strings.Contains(body, "Contact d'urgence conseillé") {
						t.Fatal("mixed dossier or missing warning")
					}
				} else if !strings.Contains(body, "alice@example.test") || strings.Contains(body, "Valider l'adhésion") {
					t.Fatal("active dossier")
				}
			}
			if !allowed {
				token := b.csrf(t, "/login")
				for _, path := range []string{approvePath(pending.ID), resendPath(active.UserID)} {
					response := b.call("POST", path, url.Values{"csrf_token": {token}, "membership_id": {fmt.Sprint(second.ID)}})
					want := 403
					if role == "anonymous" {
						want = 303
					}
					if response.Code != want {
						t.Fatal("mutation permission", response.Code)
					}
				}
				m, e := dbsqlc.New(f.db).GetMembership(t.Context(), pending.ID)
				f.must(e)
				if m.Status != "pending" || len(f.mail.messages) != 1 {
					t.Fatal("unauthorized side effects")
				}
			}
		})
	}
	b := f.membershipAdminBrowser()
	for _, path := range []string{"/memberships/999999", "/memberships/not-an-id", "/memberships/-1"} {
		if r := b.call("GET", path, nil); r.Code != 404 {
			t.Fatal("missing dossier", r.Code)
		}
	}
}
func TestMembershipUIDetailSnapshotAndMinor(t *testing.T) {
	f := newFixture(t)
	f.exec("UPDATE persons SET birth_date='2020-01-01',phone_number='0600000000',address='Adresse enfant',notes='<script>person-note</script>' WHERE id=$1", f.person)
	guardian := f.id("INSERT INTO persons(first_name,last_name,email,phone_number) VALUES ('Parent','Guardian','parent@example.test','0611111111') RETURNING id")
	f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES ($1,$2,'mother',true)", f.person, guardian)
	emergency := f.id("INSERT INTO persons(first_name,last_name,email) VALUES ('First','Emergency','first@example.test') RETURNING id")
	f.exec("INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority) VALUES ($1,$2,'Voisin',2),($1,$3,'Famille',1)", f.person, guardian, emergency)
	defs := []int32{}
	for _, code := range []string{"grant", "refuse", "withdraw"} {
		defs = append(defs, f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES ($1,1,$1,$2) RETURNING id", code, "Texte présenté "+code))
	}
	req := memberships.Request{PersonID: f.person, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}}
	for i, id := range defs {
		decision := "granted"
		if i == 1 {
			decision = "refused"
		}
		req.Consents = append(req.Consents, memberships.Decision{ConsentDefinitionID: id, GivenByPersonID: guardian, Decision: decision})
	}
	m, err := f.memberships.CreateRequest(t.Context(), req)
	f.must(err)
	f.exec("INSERT INTO membership_consents(membership_id,consent_definition_id,decision,given_by_person_id) VALUES ($1,$2,'withdrawn',$3)", m.ID, defs[2], guardian)
	f.exec("UPDATE consent_definitions SET is_active=false")
	f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES ('grant',2,'Later definition','Must never appear') RETURNING id")
	b := f.membershipAdminBrowser()
	response := b.call("GET", dossierPath(m.ID), nil)
	body := html.UnescapeString(response.Body.String())
	for _, value := range []string{"Mineur", "Practice", "Parent Guardian", "Mère", "0611111111", "First Emergency", "version 1", "Texte présenté", "Accordé (granted)", "Refusé (refused)", "Retiré (withdrawn)", "Dossier prêt à être validé"} {
		if !strings.Contains(body, value) {
			t.Fatal("missing detail", value)
		}
	}
	if strings.Contains(body, "Later definition") || strings.Contains(body, "Must never appear") {
		t.Fatal("current catalog leaked into snapshot")
	}
	if strings.Contains(response.Body.String(), "<script>person-note</script>") {
		t.Fatal("unescaped notes")
	}
	emergencySection := body[strings.Index(body, "<h2 class=\"h4\">Contacts d'urgence"):]
	if strings.Index(emergencySection, "First Emergency") > strings.Index(emergencySection, "Parent Guardian") {
		t.Fatal("priority order")
	}
	// Remove the contacts and add an unanswered requirement to check recalculated blocking presentation.
	f.exec("DELETE FROM person_guardians WHERE child_person_id=$1", f.person)
	f.exec("DELETE FROM person_emergency_contacts WHERE person_id=$1", f.person)
	missing := f.id("INSERT INTO consent_definitions(code,version,title,description,is_active) VALUES ('missing',1,'Missing answer','Snapshot missing answer',false) RETURNING id")
	f.exec("INSERT INTO membership_consent_requirements VALUES ($1,$2,now())", m.ID, missing)
	response = b.call("GET", dossierPath(m.ID), nil)
	body = html.UnescapeString(response.Body.String())
	for _, value := range []string{"Validation impossible", "Responsable légal manquant pour un mineur", "Contact d'urgence manquant pour un mineur", "Une autorisation n'a pas reçu de réponse", "Non renseigné"} {
		if !strings.Contains(body, value) {
			t.Fatal("missing blocker", value)
		}
	}
	token := b.csrf(t, dossierPath(m.ID))
	response = b.call("POST", approvePath(m.ID), url.Values{"csrf_token": {token}})
	if response.Code != 303 || !strings.Contains(response.Header().Get("Location"), "notice=incomplete") {
		t.Fatal("minor approval")
	}
	saved, err := dbsqlc.New(f.db).GetMembership(t.Context(), m.ID)
	f.must(err)
	if saved.Status != "pending" {
		t.Fatal("minor approved")
	}
}
func TestMembershipUIApprovalAndResend(t *testing.T) {
	f := newFixture(t)
	m := f.request()
	b := f.membershipAdminBrowser()
	f.exec("UPDATE persons SET notes='Person note remains' WHERE id=$1", f.person)
	token := b.csrf(t, dossierPath(m.ID))
	response := b.call("POST", approvePath(m.ID), url.Values{"admin_note": {"must not persist"}})
	if response.Code != 403 {
		t.Fatal("missing CSRF")
	}
	saved, err := dbsqlc.New(f.db).GetMembership(t.Context(), m.ID)
	f.must(err)
	if saved.Status != "pending" || len(f.mail.messages) != 0 {
		t.Fatal("CSRF mutation")
	}
	response = b.call("POST", approvePath(m.ID), url.Values{"csrf_token": {token}, "admin_note": {"Validated separately"}, "actorID": {"-999"}, "approver": {"-999"}})
	if response.Code != 303 || response.Header().Get("Location") != dossierPath(m.ID)+"?notice=approved_sent" {
		t.Fatal("approval result", response.Code)
	}
	saved, err = dbsqlc.New(f.db).GetMembership(t.Context(), m.ID)
	f.must(err)
	if saved.Status != "active" || saved.ApprovedByUserID.Int32 != f.approver || saved.AdminNote.String != "Validated separately" {
		t.Fatal("approval metadata")
	}
	page := b.call("GET", response.Header().Get("Location"), nil)
	body := html.UnescapeString(page.Body.String())
	for _, value := range []string{"Adhésion validée et email d'activation envoyé.", "Validée le", "par admin", "Validated separately", "Person note remains", "remi.dupont", "Renvoyer l'activation"} {
		if !strings.Contains(body, value) {
			t.Fatal("approval detail", value)
		}
	}
	if strings.Contains(body, "Valider l'adhésion") {
		t.Fatal("approval button after approval")
	}
	f.assertNoDeliverySecrets(body)
	response = b.call("POST", approvePath(m.ID), url.Values{"csrf_token": {token}})
	if response.Code != 303 || !strings.Contains(response.Header().Get("Location"), "already_processed") || len(f.mail.messages) != 1 {
		t.Fatal("double approval")
	}
	user, err := dbsqlc.New(f.db).GetUserByPerson(t.Context(), f.person)
	f.must(err)
	oldCode := codeFrom(t, f.mail.messages[0])
	digest := sha256.Sum256([]byte(oldCode))
	form := url.Values{"csrf_token": {token}, "membership_id": {fmt.Sprint(m.ID)}, "actorID": {"-999"}}
	response = b.call("POST", resendPath(user.ID), url.Values{"membership_id": {fmt.Sprint(m.ID)}})
	if response.Code != 403 || len(f.mail.messages) != 1 {
		t.Fatal("resend CSRF")
	}
	response = b.call("POST", resendPath(user.ID), form)
	if response.Code != 303 || !strings.Contains(response.Header().Get("Location"), "notice=resent") || len(f.mail.messages) != 2 {
		t.Fatal("resend")
	}
	var invalidated bool
	f.must(f.db.QueryRow(t.Context(), "SELECT invalidated_at IS NOT NULL FROM user_activation_codes WHERE code_hash=$1", digest[:]).Scan(&invalidated))
	if !invalidated {
		t.Fatal("old code still valid")
	}
	page = b.call("GET", response.Header().Get("Location"), nil)
	f.assertNoDeliverySecrets(page.Body.String())
	f.exec("UPDATE persons SET email=NULL WHERE id=$1", f.person)
	response = b.call("POST", resendPath(user.ID), form)
	if !strings.Contains(response.Header().Get("Location"), "resend_no_channel") || len(f.mail.messages) != 2 {
		t.Fatal("no resend channel")
	}
	f.exec("UPDATE persons SET email='remi@example.test' WHERE id=$1", f.person)
	f.mail.err = errors.New("private SMTP diagnostic")
	response = b.call("POST", resendPath(user.ID), form)
	if !strings.Contains(response.Header().Get("Location"), "resend_send_failed") {
		t.Fatal("resend SMTP failure")
	}
	page = b.call("GET", response.Header().Get("Location"), nil)
	body = html.UnescapeString(page.Body.String())
	if !strings.Contains(body, "Nouveau code préparé, mais email non envoyé.") || strings.Contains(body, "private SMTP diagnostic") {
		t.Fatal("unsafe SMTP result")
	}
	f.assertNoDeliverySecrets(body)
	f.exec("UPDATE users SET activated_at=now(),password_hash='private-hash-marker' WHERE id=$1", user.ID)
	page = b.call("GET", dossierPath(m.ID), nil)
	if strings.Contains(page.Body.String(), "Renvoyer l") || strings.Contains(page.Body.String(), "private-hash-marker") || !strings.Contains(page.Body.String(), "Compte activé") {
		t.Fatal("activated account view")
	}
	response = b.call("POST", resendPath(user.ID), form)
	if !strings.Contains(response.Header().Get("Location"), "resend_unavailable") {
		t.Fatal("activated resend accepted")
	}
	// Association is checked before delivery; no caller-controlled redirect URL is accepted.
	form.Set("membership_id", "999999")
	if response = b.call("POST", resendPath(user.ID), form); response.Code != 404 {
		t.Fatal("invalid return association")
	}
}
func TestMembershipUIApprovalDeliveryOutcomes(t *testing.T) {
	for _, outcome := range []string{"no_channel", "send_failed", "not_required"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t)
			m := f.request()
			b := f.membershipAdminBrowser()
			token := b.csrf(t, dossierPath(m.ID))
			expected := "approved"
			var existing int32
			switch outcome {
			case "no_channel":
				f.exec("UPDATE persons SET email=NULL WHERE id=$1", f.person)
				expected = "approved_no_channel"
			case "send_failed":
				f.mail.err = errors.New("private SMTP error")
				expected = "approved_send_failed"
			case "not_required":
				existing = f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'existing','preserved-private-hash',now()) RETURNING id", f.person)
			}
			response := b.call("POST", approvePath(m.ID), url.Values{"csrf_token": {token}})
			if response.Code != 303 || response.Header().Get("Location") != dossierPath(m.ID)+"?notice="+expected {
				t.Fatal("approval delivery", response.Code, response.Header().Get("Location"))
			}
			saved, err := dbsqlc.New(f.db).GetMembership(t.Context(), m.ID)
			f.must(err)
			if saved.Status != "active" {
				t.Fatal("delivery undid membership")
			}
			page := b.call("GET", response.Header().Get("Location"), nil)
			f.assertNoDeliverySecrets(page.Body.String())
			if strings.Contains(page.Body.String(), "preserved-private-hash") || strings.Contains(page.Body.String(), "private SMTP error") {
				t.Fatal("internal value leaked")
			}
			if existing != 0 {
				u, e := dbsqlc.New(f.db).GetUserByPerson(t.Context(), f.person)
				f.must(e)
				if u.ID != existing || len(f.mail.messages) != 0 {
					t.Fatal("existing account not reused")
				}
			}
		})
	}
}

func TestMembershipUIMissingBasicsAndInternalError(t *testing.T) {
	f := newFixture(t)
	m := f.request()
	b := f.membershipAdminBrowser()
	f.exec("UPDATE persons SET birth_date=NULL WHERE id=$1", f.person)
	f.exec("DELETE FROM membership_activities WHERE membership_id=$1", m.ID)
	page := b.call("GET", dossierPath(m.ID), nil)
	body := html.UnescapeString(page.Body.String())
	for _, message := range []string{"Date de naissance manquante", "Aucune activité renseignée", "Validation impossible"} {
		if !strings.Contains(body, message) {
			t.Fatal("missing basic issue", message)
		}
	}
	if strings.Contains(body, b.cookies["__Host-club_session"].Value) {
		t.Fatal("session token leaked")
	}
	token := b.csrf(t, dossierPath(m.ID))
	response := b.call("POST", approvePath(m.ID), url.Values{"csrf_token": {token}})
	if response.Code != 303 || !strings.Contains(response.Header().Get("Location"), "incomplete") {
		t.Fatal("missing basics accepted")
	}
	saved, err := dbsqlc.New(f.db).GetMembership(t.Context(), m.ID)
	f.must(err)
	if saved.Status != "pending" {
		t.Fatal("incomplete membership changed")
	}
	// Isolated PostgreSQL failure: real route must not expose its SQL diagnostic.
	f.exec("ALTER TABLE memberships RENAME TO unavailable_memberships")
	defer f.exec("ALTER TABLE unavailable_memberships RENAME TO memberships")
	for _, path := range []string{"/memberships", dossierPath(m.ID)} {
		r := b.call("GET", path, nil)
		if r.Code != 500 || strings.Contains(r.Body.String(), "unavailable_memberships") || strings.Contains(r.Body.String(), "SQLSTATE") || !strings.Contains(r.Body.String(), "Erreur interne du serveur") {
			t.Fatal("unsafe internal error", r.Code)
		}
	}
}
