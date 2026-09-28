package application

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/memberships"
)

func TestP41FamilyVisibilityFollowsEffectiveAccess(t *testing.T) {
	f := newFixture(t)
	_, b := f.personalBrowser(f.person, "adult")
	visible := func(want bool, children ...int32) {
		t.Helper()
		body := f.personalOK(b, "/dashboard")
		if strings.Contains(body, "Mon espace familial") != want {
			t.Fatalf("family section visible, want %v", want)
		}
		for _, child := range children {
			if !strings.Contains(body, `href="`+personalChild(child)+`"`) {
				t.Fatal("missing authorized child link")
			}
		}
		if !want && strings.Contains(body, "/me/children/") {
			t.Fatal("family navigation without effective access")
		}
	}
	visible(false) // Adult with no family relationship.
	children := []int32{}
	for i := 0; i < 2; i++ {
		child := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES($1,'Famille',CURRENT_DATE-interval '10 years') RETURNING id`, fmt.Sprintf("Enfant %d", i))
		f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES($1,$2,'guardian')`, child, f.person)
		children = append(children, child)
	}
	m := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'pending') RETURNING id`, children[0], f.season, f.kind)
	visible(false) // Relationship alone is never enough.
	f.personalDenied(b, personalChild(children[0]))
	f.personalDenied(b, familyMembership(children[0], m))
	ctx := f.authenticatedContext(f.approver)
	for _, child := range children {
		_, err := f.app.GuardianAccess.Grant(ctx, child, f.person)
		f.must(err)
	}
	visible(true, children...)
	f.personalOK(b, personalChild(children[0]))
	f.personalOK(b, familyMembership(children[0], m))
	f.must(f.app.GuardianAccess.Revoke(ctx, children[0], f.person))
	visible(true, children[1]) // One remaining access keeps the section visible.
	if strings.Contains(f.personalOK(b, "/dashboard"), personalChild(children[0])+`"`) {
		t.Fatal("revoked child link remains")
	}
	f.personalDenied(b, personalChild(children[0]))
	f.personalDenied(b, familyMembership(children[0], m))
	f.must(f.app.GuardianAccess.Revoke(ctx, children[1], f.person))
	visible(false) // Same session, next request, no re-login.
	f.personalDenied(b, personalChild(children[1]))
	admin := f.membershipAdminBrowser()
	f.personalDenied(admin, personalChild(children[0]))
}

func TestP41OfficeAttentionAndNextActions(t *testing.T) {
	f := newFixture(t)
	ready := f.request()
	child, parent := f.guardianPair()
	incomplete, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: child, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}})
	f.must(err)
	activePerson := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Compte','À activer','1990-01-01') RETURNING id`)
	active, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: activePerson, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}})
	f.must(err)
	_, err = f.app.Accounts.ApproveMembership(t.Context(), active.ID, f.approver, nil)
	f.must(err)
	prospect := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Essai','À traiter','1990-01-01') RETURNING id`)
	past := f.id(`INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,CURRENT_DATE-1,'registered') RETURNING id`, prospect, f.activity)
	future := f.id(`INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES($1,$2,CURRENT_DATE+1,'registered') RETURNING id`, prospect, f.activity)
	f.exec(`DELETE FROM user_roles WHERE user_id=$1`, f.approver)
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'`, f.approver)
	b := f.membershipAdminBrowser()
	body := officeOK(t, b, "/admin", "Prête à valider", "Compléter le dossier", dossierPath(ready.ID), dossierPath(incomplete.ID), dossierPath(active.ID)+"#compte", officeTrial(past))
	for _, path := range []string{`href="/admin/users"`, `href="/admin/config"`} {
		if strings.Contains(body, path) {
			t.Fatal("secretary sees unavailable function")
		}
	}
	for _, path := range []string{"/admin/users", "/admin/config"} {
		if r := b.call("GET", path, nil); r.Code != 403 {
			t.Fatal("secretary permission regression", path, r.Code)
		}
	}
	body = officeOK(t, b, dossierPath(ready.ID), "Dossier prêt à être validé", "Valider l’adhésion")
	if strings.Contains(body, `disabled>Valider`) || strings.Index(body, "/approve") > strings.Index(body, `id="groupes"`) {
		t.Fatal("ready approval must be available in the primary dossier before secondary details")
	}
	body = officeOK(t, b, dossierPath(incomplete.ID), "Contact d&#39;urgence manquant", "disabled>Valider", officePerson(child))
	if r := b.call("POST", approvePath(ready.ID), url.Values{}); r.Code != 403 {
		t.Fatal("CSRF protection lost")
	}
	trial := officeOK(t, b, officeTrial(past), "Marquer présent", "Résultat de l’essai")
	if strings.Index(trial, "Marquer présent") > strings.Index(trial, "<h2>Séance") {
		t.Fatal("past trial result action buried")
	}
	officeOK(t, b, officeTrial(future), "Modifier ou reprogrammer", "Renseigner le résultat")
	f.exec(`UPDATE trial_registrations SET status='attended' WHERE id=$1`, past)
	officeOK(t, b, officeTrial(past), "Préparer une demande d’adhésion", "Corriger le résultat")
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES($1,$2,1)`, child, parent)
	officeOK(t, b, dossierPath(incomplete.ID), "Dossier prêt à être validé")
	// Dashboard reevaluates completeness and activation on every request.
	body = officeOK(t, b, "/admin")
	if strings.Contains(body, "Compléter le dossier") {
		t.Fatal("stale completeness")
	}
	f.exec(`UPDATE users SET activated_at=now(),password_hash='test-hash' WHERE person_id=$1`, activePerson)
	body = officeOK(t, b, "/admin")
	if strings.Contains(body, dossierPath(active.ID)+"#compte") {
		t.Fatal("stale activation reminder")
	}
	f.exec(`UPDATE memberships SET status='ended' WHERE id=$1`, active.ID)
	f.exec(`UPDATE users SET activated_at=NULL,password_hash=NULL WHERE person_id=$1`, activePerson)
	if strings.Contains(officeOK(t, b, "/admin"), dossierPath(active.ID)+"#compte") {
		t.Fatal("historical membership shown as immediate activation task")
	}
}
