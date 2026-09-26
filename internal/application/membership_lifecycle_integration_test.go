package application

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

func TestP3MembershipLifecycle(t *testing.T) {
	f := newFixture(t)
	admin := f.membershipAdminBrowser()
	group, _ := f.officeGroup()
	def := f.id("INSERT INTO consent_definitions(code,version,title,description) VALUES('image',1,'Images','Décision libre') RETURNING id")
	for _, scenario := range []string{"adult_trial", "direct", "child_trial"} {
		t.Run(scenario, func(t *testing.T) {
			birth := "1990-01-01"
			if scenario == "child_trial" {
				birth = "2018-01-01"
			}
			person := f.id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES($1,'Cycle',$2,'cycle@example.test') RETURNING id", scenario, birth)
			giver := person
			var parentBrowser *browser
			var parent int32
			if scenario == "child_trial" {
				parent = f.id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES('Parent','Cycle','1980-01-01','parent@example.test') RETURNING id")
				f.exec("INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES($1,$2,'guardian')", person, parent)
				giver = parent

			}
			var trial int32
			if scenario != "direct" {
				// Schedule and mark attendance through the authorized HTTP services.
				r := officePost(t, admin, officePerson(person)+"/trials/new", url.Values{"activity_id": {fmt.Sprint(f.activity)}, "group_id": {fmt.Sprint(group)}, "trial_date": {"2026-09-16"}}, 303)
				if !strings.HasPrefix(r.Header().Get("Location"), "/trials/") {
					t.Fatal("trial PRG")
				}
				trial = f.id("SELECT id FROM trial_registrations WHERE person_id=$1", person)
				officePost(t, admin, officeTrial(trial)+"/status", url.Values{"revision": {"0"}, "status": {"attended"}}, 303)
				officeOK(t, admin, officeTrial(trial), "Préparer une demande")
			}
			form := url.Values{"season_id": {fmt.Sprint(f.season)}, "type_id": {fmt.Sprint(f.kind)}, "activities": {fmt.Sprint(f.activity)}, "definitions": {fmt.Sprint(def)}, "giver_id": {fmt.Sprint(giver)}, "consent-" + fmt.Sprint(def): {"refused"}}
			path := officePerson(person) + "/memberships/new"
			if trial != 0 {
				form.Set("source_trial", fmt.Sprint(trial))
				officeOK(t, admin, path+"?trial="+fmt.Sprint(trial), "Depuis l’essai", "Groupe adultes")
			}
			officePost(t, admin, path, form, 303)
			id := f.id("SELECT id FROM memberships WHERE person_id=$1", person)
			m, err := dbsqlc.New(f.db).GetMembership(t.Context(), id)
			f.must(err)
			if m.Status != "pending" || m.SourceTrialID.Valid != (trial != 0) || m.SourceTrialID.Int32 != trial {
				t.Fatal("pending origin", m)
			}
			if f.count("SELECT count(*) FROM membership_groups WHERE membership_id=$1", id) != 0 {
				t.Fatal("automatic group")
			}
			if trial != 0 {
				officeOK(t, admin, dossierPath(id), "Essai du 16/09/2026", "Groupe adultes", "Voir l’essai")
				officeOK(t, admin, officeTrial(trial), "Voir le dossier d’adhésion")
				if r := admin.call("GET", path+"?trial="+fmt.Sprint(trial), nil); r.Code != 303 || r.Header().Get("Location") != dossierPath(id) {
					t.Fatal("existing membership navigation")
				}
			} else {
				officeOK(t, admin, dossierPath(id), "Demande d’adhésion directe")
			}
			if parent != 0 {
				// Guardian relationship alone grants neither family access nor completeness.
				f.personalDenied(admin, familyMembership(person, id))
				denied := p3Approve(t, admin, id)
				if !strings.Contains(denied.Header().Get("Location"), "incomplete") {
					t.Fatal("incomplete minor approved")
				}
				if f.count("SELECT count(*) FROM users WHERE person_id=$1", person) != 0 {
					t.Fatal("child account on failure")
				}
				familyPath := fmt.Sprintf("/persons/%d/guardians/%d", person, parent)
				officePost(t, admin, familyPath+"/emergency", url.Values{}, 303)
				officePost(t, admin, familyPath+"/grant", url.Values{}, 303)
				officePost(t, admin, familyPath+"/activation", url.Values{}, 303)
				parentUser, err := dbsqlc.New(f.db).GetUserByPerson(t.Context(), parent)
				f.must(err)
				activationBrowser := newBrowser(f.app.Handler)
				activation := activationForm(activationBrowser.csrf(t, "/activate"), codeFrom(t, f.mail.messages[len(f.mail.messages)-1]), "a secure password")
				activation.Set("username", parentUser.Username)
				if response := activationBrowser.call("POST", "/activate", activation); response.Code != 303 {
					t.Fatal("guardian activation")
				}
				parentBrowser = f.loginBrowser(parentUser.Username)
				officeOK(t, admin, officePerson(person), "Accès familial autorisé", "Contact d’urgence enregistré")
			}
			p3Approve(t, admin, id)
			m, err = dbsqlc.New(f.db).GetMembership(t.Context(), id)
			f.must(err)
			if m.Status != "active" || m.SourceTrialID.Int32 != trial || m.SourceTrialID.Valid != (trial != 0) {
				t.Fatal("active origin", m)
			}
			officeOK(t, admin, dossierPath(id), "Active", "Refusé")
			officeOK(t, admin, officePerson(person), "Demande reçue", "Validée")
			twice := p3Approve(t, admin, id)
			if !strings.Contains(twice.Header().Get("Location"), "already_processed") {
				t.Fatal("double approval")
			}
			if parent != 0 {
				if f.count("SELECT count(*) FROM users WHERE person_id=$1", person) != 0 {
					t.Fatal("artificial child account")
				}
				f.personalOK(parentBrowser, familyMembership(person, id), "Active", "Refusé")
				f.personalDenied(admin, familyMembership(person, id))
				other := f.id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Other','Family','1980-01-01') RETURNING id")
				_, outsider := f.personalBrowser(other, "other.p3")
				f.personalDenied(outsider, familyMembership(person, id))
				officePost(t, admin, fmt.Sprintf("/persons/%d/guardians/%d/revoke", person, parent), url.Values{}, 303)
				f.personalDenied(parentBrowser, familyMembership(person, id))
			} else {
				u, err := dbsqlc.New(f.db).GetUserByPerson(t.Context(), person)
				f.must(err)
				if !u.IsActive || u.ActivatedAt.Valid {
					t.Fatal("new account state")
				}
				activationBrowser := newBrowser(f.app.Handler)
				activation := activationForm(activationBrowser.csrf(t, "/activate"), codeFrom(t, f.mail.messages[len(f.mail.messages)-1]), "a secure password")
				activation.Set("username", u.Username)
				if r := activationBrowser.call("POST", "/activate", activation); r.Code != 303 {
					t.Fatal("activation", r.Code)
				}
				member := f.loginBrowser(u.Username)
				f.personalOK(member, personalMembership(id), "Active")
			}
		})
	}
}

func TestP3FamilyDossierSecurity(t *testing.T) {
	f := newFixture(t)
	admin := f.membershipAdminBrowser()
	child, parent := f.guardianPair()
	stranger := f.id("INSERT INTO persons(first_name,last_name) VALUES('Stranger','Family') RETURNING id")
	_, member := f.personalBrowser(f.person, "ordinary.p3")
	for _, action := range []string{"emergency", "grant", "revoke", "activation"} {
		path := fmt.Sprintf("/persons/%d/guardians/%d/%s", child, parent, action)
		if r := newBrowser(f.app.Handler).call("POST", path, nil); r.Code != 303 {
			t.Fatal("anonymous", action, r.Code)
		}
		if r := member.call("POST", path, nil); r.Code != 403 {
			t.Fatal("permission", action, r.Code)
		}
		if r := admin.call("POST", path, url.Values{"csrf_token": {"invalid"}}); r.Code != 403 {
			t.Fatal("csrf", action, r.Code)
		}
	}
	officePost(t, admin, fmt.Sprintf("/persons/%d/guardians/%d/emergency", child, stranger), url.Values{}, 404)
	officePost(t, admin, fmt.Sprintf("/persons/%d/guardians/%d/grant", child, stranger), url.Values{}, 422)
	officePost(t, admin, fmt.Sprintf("/persons/%d/guardians/%d/activation", child, parent), url.Values{}, 422)
	if f.count("SELECT count(*) FROM person_emergency_contacts") != 0 || f.count("SELECT count(*) FROM guardian_access_grants") != 0 {
		t.Fatal("unauthorized family mutation")
	}
}

func p3Approve(t *testing.T, b *browser, id int32) *httptest.ResponseRecorder {
	t.Helper()
	r := b.call("POST", approvePath(id), url.Values{"csrf_token": {b.csrf(t, "/persons")}})
	if r.Code != 303 || !strings.HasPrefix(r.Header().Get("Location"), dossierPath(id)+"?notice=") {
		t.Fatalf("approval PRG: %d %s", r.Code, r.Header().Get("Location"))
	}
	return r
}
