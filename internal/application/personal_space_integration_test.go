package application

import (
	"context"
	"fmt"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/personalspace"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func (f *fixture) personalBrowser(person int32, name string) (int32, *browser) {
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	user := f.id(`INSERT INTO users(person_id,username,password_hash,activated_at) VALUES($1,$2,$3,now()) RETURNING id`, person, name, string(hash))
	return user, f.loginBrowser(name)
}
func personalMembership(id int32) string { return fmt.Sprintf("/me/memberships/%d", id) }
func personalChild(id int32) string      { return fmt.Sprintf("/me/children/%d", id) }
func familyMembership(child, id int32) string {
	return fmt.Sprintf("/me/children/%d/memberships/%d", child, id)
}
func (f *fixture) personalOK(b *browser, path string, required ...string) string {
	f.t.Helper()
	r := b.call("GET", path, nil)
	if r.Code != 200 {
		f.t.Fatalf("%s status %d: %s", path, r.Code, r.Body.String())
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		f.t.Fatal("personal cache")
	}
	if r.Header().Get("X-Content-Type-Options") != "nosniff" || r.Header().Get("Content-Security-Policy") == "" {
		f.t.Fatal("personal security headers")
	}
	body := html.UnescapeString(r.Body.String())
	for _, v := range required {
		if !strings.Contains(body, v) {
			f.t.Fatalf("%s missing %q", path, v)
		}
	}
	for _, v := range []string{"SECRET_ADMIN_NOTE", "SECRET_PERSON_NOTE", "password_hash", "$2a$", "$2b$", "SECRET_CANDIDATE", "guardian_identity_review", "safe_error_code", "SECRET_OTHER_CONTACT", "SECRET_CHILD_CONTACT"} {
		if strings.Contains(body, v) {
			f.t.Fatalf("%s leaked %q", path, v)
		}
	}
	f.assertNoDeliverySecrets(body)
	return body
}
func (f *fixture) personalDenied(b *browser, path string) {
	f.t.Helper()
	r := b.call("GET", path, nil)
	if r.Code != 404 {
		f.t.Fatalf("%s status %d want 404", path, r.Code)
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		f.t.Fatal("denial cache")
	}
}
func TestPersonalAccountOwnershipAndHistory(t *testing.T) {
	f := newFixture(t)
	f.exec(`UPDATE persons SET notes='SECRET_PERSON_NOTE',phone_number='0601020304' WHERE id=$1`, f.person)
	user, b := f.personalBrowser(f.person, "member")
	f.personalOK(b, "/dashboard", "Vous n’avez actuellement aucune adhésion", "Vous ne gérez actuellement aucun enfant")
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'`, user)
	f.personalOK(b, "/me/account", "Fonctions au club", "Secrétariat")
	f.exec(`DELETE FROM user_roles WHERE user_id=$1`, user)
	if strings.Contains(f.personalOK(b, "/me/account"), "Secrétariat") {
		t.Fatal("stale function")
	}
	m := f.request()
	f.exec(`UPDATE memberships SET admin_note='SECRET_ADMIN_NOTE' WHERE id=$1`, m.ID)
	old := f.id(`INSERT INTO seasons(name,starts_at,ends_at) VALUES('Saison passée','2025-09-01','2026-08-31') RETURNING id`)
	history := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'ended') RETURNING id`, f.person, old, f.kind)
	foreign := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Autre','Personne','1990-01-01') RETURNING id`)
	foreignMembership := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'pending') RETURNING id`, foreign, f.season, f.kind)
	paths := []string{"/me", "/me/account", personalMembership(m.ID), personalChild(foreign), familyMembership(foreign, m.ID)}
	anon := newBrowser(f.app.Handler)
	for _, p := range paths {
		r := anon.call("GET", p, nil)
		if r.Code != 303 || r.Header().Get("Location") != "/login" || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("anonymous", p, r.Code)
		}
	}
	f.personalOK(b, "/me/account?person_id=999", "Rémi", "Dupont", "member", "remi@example.test", "0601020304", "Actif")
	f.personalOK(b, "/dashboard", "Saison passée", personalMembership(history), personalMembership(m.ID), "Mon compte")
	f.personalOK(b, personalMembership(m.ID), "En attente", "Practice", "Dossier complet", "Aucun consentement n’est enregistré", "Aucun groupe n’est actuellement associé")
	f.personalOK(b, personalMembership(history), "Saison passée", "Terminée")
	for _, p := range []string{personalMembership(foreignMembership), personalMembership(999999), "/me/memberships/nope", "/me/memberships/-1", personalChild(foreign)} {
		f.personalDenied(b, p)
	}
	admin := f.membershipAdminBrowser()
	f.personalDenied(admin, personalMembership(m.ID))
	if r := admin.call("GET", dossierPath(m.ID), nil); r.Code != 200 {
		t.Fatal("admin route regression")
	}
	if r := b.call("GET", "/me", nil); r.Code != 303 || r.Header().Get("Location") != "/dashboard" {
		t.Fatal("me alias")
	}
	for _, p := range paths {
		r := b.call("POST", p, nil)
		if r.Code != 405 && r.Code != 404 {
			t.Fatal("write route", p, r.Code)
		}
	}
}
func TestPersonalFamilyAuthorizationPrivacyAndSnapshots(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	user, b := f.personalBrowser(parent, "claire")
	admin := f.membershipAdminBrowser()
	ctx := f.authenticatedContext(f.approver)
	f.exec(`UPDATE persons SET email='SECRET_CHILD_CONTACT@example.test',phone_number='SECRET_CHILD_CONTACT',address='SECRET_CHILD_CONTACT',notes='SECRET_PERSON_NOTE' WHERE id=$1`, child)
	other := f.id(`INSERT INTO persons(first_name,last_name,birth_date,email,phone_number,address) VALUES('SECRET_OTHER_CONTACT','Responsable','1980-01-01','SECRET_OTHER_CONTACT@example.test','SECRET_OTHER_CONTACT','SECRET_OTHER_CONTACT') RETURNING id`)
	f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES($1,$2,'other')`, child, other)
	defs := []int32{}
	for _, code := range []string{"own", "other", "withdraw"} {
		defs = append(defs, f.id(`INSERT INTO consent_definitions(code,version,title,description) VALUES($1,1,$1,'Texte initial <script>wording</script>') RETURNING id`, code))
	}
	req := memberships.Request{PersonID: child, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}, Consents: []memberships.Decision{
		{ConsentDefinitionID: defs[0], GivenByPersonID: parent, Decision: "refused"},
		{ConsentDefinitionID: defs[1], GivenByPersonID: other, Decision: "granted"},
		{ConsentDefinitionID: defs[2], GivenByPersonID: other, Decision: "granted"},
	}}
	m, err := f.memberships.CreateRequest(t.Context(), req)
	f.must(err)
	f.exec(`UPDATE memberships SET admin_note='SECRET_ADMIN_NOTE' WHERE id=$1`, m.ID)
	f.exec(`INSERT INTO membership_consents(membership_id,consent_definition_id,decision,given_by_person_id) VALUES($1,$2,'withdrawn',$3)`, m.ID, defs[2], other)
	f.exec(`UPDATE consent_definitions SET is_active=false`)
	f.id(`INSERT INTO consent_definitions(code,version,title,description) VALUES('own',2,'NEW_VERSION_MUST_NOT_APPEAR','New text') RETURNING id`)
	// No User is needed or created for the child.
	if n := f.count(`SELECT count(*) FROM users WHERE person_id=$1`, child); n != 0 {
		t.Fatal("child user")
	}
	for _, p := range []string{personalChild(child), familyMembership(child, m.ID)} {
		f.personalDenied(b, p)
		f.personalDenied(admin, p)
	}
	_, err = f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	f.personalOK(b, personalChild(child), "Arthur Famille", "12/09/2011", "contact d’urgence", familyMembership(child, m.ID))
	body := f.personalOK(b, familyMembership(child, m.ID), "version 1", "Refusé", "Retiré", "Décision enregistrée par vous", "Décision enregistrée par un responsable", "contact d’urgence")
	if strings.Contains(body, "NEW_VERSION_MUST_NOT_APPEAR") {
		t.Fatal("wrong snapshot")
	}
	raw := b.call("GET", familyMembership(child, m.ID), nil).Body.String()
	if strings.Contains(raw, "<script>wording</script>") {
		t.Fatal("XSS")
	}
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES($1,$2,1)`, child, other)
	f.personalOK(b, personalChild(child), "Contact d’urgence enregistré")
	f.personalOK(b, familyMembership(child, m.ID), "Dossier complet", "Refusé")
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES($1,$2,2)`, child, parent)
	f.personalOK(b, personalChild(child), "Vous êtes enregistré comme contact d’urgence")
	f.exec(`UPDATE consent_definitions SET is_active=false`)
	adult := f.request()
	child2 := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Second','Enfant','2015-01-01') RETURNING id`)
	otherM := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'pending') RETURNING id`, child2, f.season, f.kind)
	for _, p := range []string{familyMembership(child, adult.ID), familyMembership(child, otherM), familyMembership(child, 999999), familyMembership(child2, m.ID), personalMembership(m.ID)} {
		f.personalDenied(b, p)
	}
	old := f.id(`INSERT INTO seasons(name,starts_at,ends_at) VALUES('Historique enfant','2024-09-01','2025-08-31') RETURNING id`)
	history := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'ended') RETURNING id`, child, old, f.kind)
	f.personalOK(b, personalChild(child), "Historique enfant")
	f.personalOK(b, familyMembership(child, history), "Historique enfant")
	// Every subsequent request must recheck eligibility, including historical seasons.
	denied := func() {
		for _, p := range []string{personalChild(child), familyMembership(child, m.ID), familyMembership(child, history)} {
			f.personalDenied(b, p)
		}
	}
	f.exec(`UPDATE persons SET birth_date=(CURRENT_DATE-interval '18 years')::date WHERE id=$1`, child)
	denied()
	f.exec(`UPDATE persons SET birth_date='2011-09-12',archived_at=now() WHERE id=$1`, child)
	denied()
	f.exec(`UPDATE persons SET archived_at=NULL WHERE id=$1`, child)
	f.exec(`UPDATE persons SET archived_at=now() WHERE id=$1`, parent)
	// The session may be rejected or the resource hidden when the guardian is archived.
	r := b.call("GET", personalChild(child), nil)
	if r.Code != 404 && r.Code != 303 {
		t.Fatal("archived guardian", r.Code)
	}
	f.exec(`UPDATE persons SET archived_at=NULL WHERE id=$1`, parent)
	b = f.loginBrowser("claire")
	f.must(f.app.GuardianAccess.Revoke(ctx, child, parent))
	denied()
	_, err = f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	f.must(f.app.GuardianAccess.RemoveRelationship(ctx, child, parent))
	denied()
	f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES($1,$2,'mother')`, child, parent)
	_, err = f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	f.exec(`UPDATE users SET is_active=false WHERE id=$1`, user)
	for _, p := range []string{personalChild(child), familyMembership(child, m.ID), "/me/account"} {
		r := b.call("GET", p, nil)
		if r.Code != 303 || r.Header().Get("Location") != "/login" {
			t.Fatal("disabled guardian", r.Code)
		}
	}
}

// Count database round trips, not CSS or implementation-shaped HTML.
type personalCountingDB struct {
	dbsqlc.DBTX
	queries int
}

func (c *personalCountingDB) Query(ctx context.Context, q string, args ...interface{}) (pgx.Rows, error) {
	c.queries++
	return c.DBTX.Query(ctx, q, args...)
}
func TestPersonalGroupsAndDashboardBatching(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	m := f.request()
	group := f.id(`INSERT INTO groups(activity_id,name) VALUES($1,'Groupe actuel') RETURNING id`, f.activity)
	f.exec(`INSERT INTO membership_groups(membership_id,group_id,joined_at) VALUES($1,$2,CURRENT_DATE)`, m.ID, group)
	f.exec(`INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from) VALUES($1,$2,3,'18:30','20:00','Dojo municipal',CURRENT_DATE)`, group, f.season)
	f.exec(`INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from,valid_until) VALUES($1,$2,1,'10:00','11:00','OLD_SLOT',CURRENT_DATE-10,CURRENT_DATE-1)`, group, f.season)
	f.exec(`INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from,is_active) VALUES($1,$2,1,'10:00','11:00','INACTIVE_SLOT',CURRENT_DATE,false)`, group, f.season)
	for _, name := range []string{"LEFT_GROUP", "FUTURE_GROUP", "INACTIVE_GROUP", "Sans créneau"} {
		g := f.id(`INSERT INTO groups(activity_id,name,is_active) VALUES($1,$2,$3) RETURNING id`, f.activity, name, name != "INACTIVE_GROUP")
		if name == "LEFT_GROUP" {
			f.exec(`INSERT INTO membership_groups(membership_id,group_id,joined_at,left_at) VALUES($1,$2,CURRENT_DATE-3,CURRENT_DATE)`, m.ID, g)
		} else if name == "FUTURE_GROUP" {
			f.exec(`INSERT INTO membership_groups(membership_id,group_id,joined_at) VALUES($1,$2,CURRENT_DATE+1)`, m.ID, g)
		} else {
			f.exec(`INSERT INTO membership_groups(membership_id,group_id,joined_at) VALUES($1,$2,CURRENT_DATE)`, m.ID, g)
		}
	}
	body := f.personalOK(b, personalMembership(m.ID), "Groupe actuel", "Mercredi", "18:30", "20:00", "Dojo municipal", "Sans créneau", "Aucun créneau actuel renseigné")
	for _, v := range []string{"LEFT_GROUP", "FUTURE_GROUP", "INACTIVE_GROUP", "OLD_SLOT", "INACTIVE_SLOT"} {
		if strings.Contains(body, v) {
			t.Fatal("noncurrent group/slot", v)
		}
	}
	counter := &personalCountingDB{DBTX: f.db}
	s := personalspace.New(dbsqlc.New(counter), f.app.GuardianAccess, time.UTC)
	ctx := f.authenticatedContext(user)
	if _, err := s.GetDashboard(ctx); err != nil {
		t.Fatal(err)
	}
	baseline := counter.queries
	adminCtx := f.authenticatedContext(f.approver)
	for i := 0; i < 4; i++ {
		child := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES($1,'Famille','2015-01-01') RETURNING id`, fmt.Sprintf("Enfant %d", i))
		f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES($1,$2,'guardian')`, child, f.person)
		_, err := f.app.GuardianAccess.Grant(adminCtx, child, f.person)
		f.must(err)
		if i != 3 {
			f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES($1,$2,$3,'pending') RETURNING id`, child, f.season, f.kind)
		}
	}
	counter.queries = 0
	d, err := s.GetDashboard(ctx)
	f.must(err)
	if baseline != 1 || counter.queries != baseline || len(d.Children) != 4 || len(d.Memberships) != 1 {
		t.Fatal("batching", baseline, counter.queries, d)
	}
	for i, c := range d.Children {
		want := 1
		if i == 3 {
			want = 0
		}
		if len(c.Memberships) != want {
			t.Fatal("batch association")
		}
	}
	// Services also reject anonymous contexts, independently of the handlers.
	if _, err = s.GetMyMembership(t.Context(), m.ID); err != personalspace.ErrNotFound {
		t.Fatal("service anonymous")
	}
}
