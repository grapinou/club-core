package application

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
	"github.com/grapinou/club-core/internal/memberships"
)

func emergencyForm() url.Values {
	return url.Values{"first_name": {"Marie"}, "last_name": {"Contact"}, "phone_number": {"0611223344"}, "relationship": {"Amie"}, "email": {""}, "action": {"add"}}
}
func p442Post(t *testing.T, b *browser, path string, v url.Values, want int) string {
	t.Helper()
	v.Set("csrf_token", b.csrf(t, path))
	r := b.call("POST", path, v)
	if r.Code != want {
		t.Fatalf("%s: %d want %d: %s", path, r.Code, want, r.Body.String())
	}
	return r.Body.String()
}
func visibleText(body string) string {
	return html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(body, " "))
}
func TestP442DashboardAndConfirmation(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	body := officeOK(t, b, "/admin", "Adhésions en attente")
	if strings.Contains(body, "Ouvrir l’Annuaire") || strings.Contains(body, "Essais passés sans résultat") {
		t.Fatal("dashboard duplicates menu")
	}
	if strings.Count(body, `href="/dashboard"`) != 1 {
		t.Fatal("secretary must have a single personal destination")
	}
	for _, path := range []string{"/join/submitted", "/join/child/submitted"} {
		body := newBrowser(f.app.Handler).call("GET", path, nil).Body.String()
		for _, label := range []string{"Saisir un code de vérification", "Contacter le club", "Selon votre situation", "/registration/verify"} {
			if strings.Contains(body, label) {
				t.Fatal("confirmation exposes internal step", label)
			}
		}
		if !strings.Contains(body, `class="btn btn-primary" href="/">Retour à l’accueil`) {
			t.Fatal("missing primary home link")
		}
	}
}
func TestP442AdultEmergencyRequiredAndPropagation(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	form := f.joinForm(b)
	for _, key := range []string{"first_name", "last_name", "phone_number", "relationship"} {
		b.ip = "192.0.2." + fmt.Sprint(len(key)) + ":1234"
		v := cloneForm(form)
		v.Del("emergency_" + key)
		if r := b.call("POST", "/join", v); r.Code != 422 || !strings.Contains(r.Body.String(), "emergency_"+key+"-error") {
			t.Fatal("required emergency", key, r.Code)
		}
	}
	invalid := cloneForm(form)
	invalid.Set("emergency_email", "invalid")
	if b.call("POST", "/join", invalid).Code != 422 {
		t.Fatal("invalid optional email")
	}
	b.ip = "198.51.100.45:1234"
	sub := f.joinSubmit(b, form)
	a := f.publicApplication(sub)
	if !a.MembershipID.Valid {
		t.Fatal("not finalized")
	}
	owner := f.id("SELECT person_id FROM memberships WHERE id=?1", a.MembershipID.Int32)
	contact := f.id("SELECT contact_person_id FROM person_emergency_contacts WHERE person_id=?1", owner)
	if f.count(`SELECT count(*) FROM persons WHERE id=?1 AND first_name='Marie' AND last_name='Urgence' AND phone_number='33601020304' AND email IS NULL`, contact) != 1 {
		t.Fatal("contact persistence")
	}
	if f.count(`SELECT count(*) FROM person_emergency_contacts WHERE person_id=?1 AND relationship_label='Amie' AND priority=1`, owner) != 1 {
		t.Fatal("contact relation")
	}
	if f.count(`SELECT count(*) FROM users WHERE person_id=?1`, contact) != 0 || f.count(`SELECT count(*) FROM person_guardians`) > 0 || f.count(`SELECT count(*) FROM guardian_access_grants`) > 0 {
		t.Fatal("emergency created rights")
	}
	f.joinSubmit(b, form)
	if f.count(`SELECT count(*) FROM person_emergency_contacts WHERE person_id=?1`, owner) != 1 {
		t.Fatal("duplicate finalization")
	}
	// A matching applicant stages the contact until identity resolution succeeds.
	known := newBrowser(f.app.Handler)
	known.ip = "198.51.100.46:1234"
	v := f.joinForm(known)
	knownJoin(v)
	v.Set("emergency_email", "urgence@example.test")
	pending := f.joinSubmit(known, v)
	if f.publicApplication(pending).MembershipID.Valid || f.count(`SELECT count(*) FROM registration_application_emergency_contacts WHERE application_id=(SELECT id FROM registration_applications WHERE submission_id=?1) AND contact_person_id IS NULL`, pending) != 1 {
		t.Fatal("missing staging")
	}
	f.must(f.app.Reviews.LinkPerson(t.Context(), f.approver, pending, f.person))
	if f.count(`SELECT count(*) FROM person_emergency_contacts e JOIN persons p ON p.id=e.contact_person_id WHERE e.person_id=?1 AND p.email='urgence@example.test'`, f.person) != 1 {
		t.Fatal("resolved contact missing")
	}
}
func TestP442EmergencyOfficeAndSelfService(t *testing.T) {
	f := newFixture(t)
	admin := f.membershipAdminBrowser()
	_, adult := f.personalBrowser(f.person, "adult")
	child, parent := f.guardianPair()
	_, family := f.personalBrowser(parent, "parent")
	ctx := f.authenticatedContext(f.approver)
	_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	cases := []struct {
		b     *browser
		path  string
		owner int32
	}{{admin, officePerson(f.person) + "/emergency", f.person}, {adult, "/me/emergency", f.person}, {family, personalChild(child) + "/emergency", child}}
	for _, tc := range cases {
		f.personalOK(tc.b, tc.path, "Ajouter un contact")
		v := emergencyForm()
		p442Post(t, tc.b, tc.path, v, 303)
		second := emergencyForm()
		second.Set("first_name", "Second")
		second.Set("relationship", "mother")
		p442Post(t, tc.b, tc.path, second, 303)
		id := f.id(`SELECT id FROM person_emergency_contacts WHERE person_id=?1 ORDER BY priority DESC LIMIT 1`, tc.owner)
		contact := f.id(`SELECT contact_person_id FROM person_emergency_contacts WHERE id=?1`, id)
		updated := emergencyForm()
		updated.Set("action", "update")
		updated.Set("contact_id", fmt.Sprint(id))
		updated.Set("priority", "1")
		updated.Set("first_name", "Modifiée")
		updated.Set("relationship", "Père")
		updated.Set("email", "contact@example.test")
		p442Post(t, tc.b, tc.path, updated, 303)
		if f.count(`SELECT count(*) FROM person_emergency_contacts WHERE id=?1 AND priority=1 AND relationship_label='Père'`, id) != 1 || f.count(`SELECT count(*) FROM persons WHERE id=?1 AND first_name='Modifiée' AND email='contact@example.test'`, contact) != 1 {
			t.Fatal("update/priority")
		}
		body := visibleText(f.personalOK(tc.b, tc.path))
		for _, word := range []string{"mother", "father", "guardian", "other"} {
			if strings.Contains(body, word) {
				t.Fatal("untranslated relation", word)
			}
		}
		remove := url.Values{"action": {"remove"}, "contact_id": {fmt.Sprint(id)}}
		p442Post(t, tc.b, tc.path, remove, 303)
		if f.count(`SELECT count(*) FROM person_emergency_contacts WHERE id=?1`, id) != 0 || f.count(`SELECT count(*) FROM persons WHERE id=?1`, contact) != 1 {
			t.Fatal("relation removal destroyed Person")
		}
		if f.count(`SELECT count(*) FROM person_emergency_contacts WHERE person_id=?1 AND priority>(SELECT count(*) FROM person_emergency_contacts WHERE person_id=?1)`, tc.owner) != 0 {
			t.Fatal("priority not compacted")
		}
	}
	f.personalOK(admin, officePerson(f.person), "Contacts d’urgence", "Gérer les contacts d’urgence")
	f.personalOK(admin, officePerson(child), "Contacts d’urgence")
	// Browser IDs never broaden own or child authority; foreign relation ownership is checked.
	foreign := f.id(`SELECT id FROM person_emergency_contacts WHERE person_id=?1 LIMIT 1`, child)
	p442Post(t, adult, "/me/emergency", url.Values{"action": {"remove"}, "contact_id": {fmt.Sprint(foreign)}, "person_id": {fmt.Sprint(child)}}, 404)
	f.personalDenied(adult, personalChild(child)+"/emergency")
	f.must(f.app.GuardianAccess.Revoke(ctx, child, parent))
	f.personalDenied(family, personalChild(child)+"/emergency")
	if adult.call("POST", "/me/emergency", emergencyForm()).Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	f.exec("DELETE FROM user_roles WHERE user_id=?1", f.approver)
	if admin.call("GET", officePerson(f.person)+"/emergency", nil).Code != 403 {
		t.Fatal("office permission bypass")
	}
}
func TestP442SharedEmergencyCoordinatesProtected(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	_, b := f.personalBrowser(parent, "parent")
	ctx := f.authenticatedContext(f.approver)
	_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority) VALUES(?1,?2,'mother',1)`, child, parent)
	id := f.id(`SELECT id FROM person_emergency_contacts WHERE person_id=?1`, child)
	path := personalChild(child) + "/emergency"
	f.personalOK(b, path, "Mère", "readonly")
	v := url.Values{"action": {"update"}, "contact_id": {fmt.Sprint(id)}, "first_name": {"Claire"}, "last_name": {"Famille"}, "phone_number": {""}, "email": {"claire@example.test"}, "relationship": {"Mère"}, "priority": {"1"}}
	p442Post(t, b, path, v, 303) // Incomplete shared coordinates do not block editing the label.
	v.Set("email", "forged@example.test")
	p442Post(t, b, path, v, 404)
	if f.personEmail(parent) != "claire@example.test" {
		t.Fatal("email workflow bypass")
	}
}
func TestP442FamilyRelationsAndIndependentAccess(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	child, parent := f.guardianPair()
	path := officePerson(child) + "/family"
	other := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Paul','Responsable','1980-01-01') RETURNING id`)
	add := url.Values{"action": {"add"}, "guardian_id": {fmt.Sprint(other)}, "relationship": {"father"}, "primary": {"yes"}}
	p442Post(t, b, path, add, 303)
	if f.count(`SELECT count(*) FROM guardian_access_grants`) != 0 {
		t.Fatal("relationship granted digital access")
	}
	update := cloneForm(add)
	update.Set("action", "update")
	update.Set("relationship", "other")
	p442Post(t, b, path, update, 303)
	if f.count(`SELECT count(*) FROM person_guardians WHERE child_person_id=?1 AND guardian_person_id=?2 AND relationship_type='other' AND is_primary_contact`, child, other) != 1 {
		t.Fatal("family update")
	}
	if f.count(`SELECT count(*) FROM person_guardians WHERE child_person_id=?1 AND is_primary_contact`, child) != 1 {
		t.Fatal("multiple primary contacts")
	}
	ctx := f.authenticatedContext(f.approver)
	_, err := f.app.GuardianAccess.Grant(ctx, child, other)
	f.must(err)
	update.Set("primary", "no")
	update.Set("relationship", "guardian")
	p442Post(t, b, path, update, 303)
	if f.count(`SELECT count(*) FROM guardian_access_grants WHERE child_person_id=?1 AND guardian_person_id=?2 AND revoked_at IS NULL`, child, other) != 1 {
		t.Fatal("relationship update changed access")
	}
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority) VALUES(?1,?2,'mother',1)`, child, parent)
	body := officeOK(t, b, officePerson(child), "Accès familial : Autorisé", "btn btn-sm btn-outline", "Mère")
	if !strings.Contains(body, fmt.Sprintf(`href="/persons/%d"`, parent)) || strings.Contains(body, fmt.Sprintf(`href="/persons/%d/edit">Claire`, parent)) {
		t.Fatal("Person name links to edit")
	}
	for _, word := range []string{"mother", "father", "guardian", "other"} {
		if strings.Contains(visibleText(body), word) {
			t.Fatal("untranslated family", word)
		}
	}
	p442Post(t, b, path, url.Values{"action": {"remove"}, "guardian_id": {fmt.Sprint(other)}}, 303)
	if f.count(`SELECT count(*) FROM person_guardians WHERE child_person_id=?1 AND guardian_person_id=?2`, child, other) != 0 || f.count(`SELECT count(*) FROM guardian_access_grants WHERE child_person_id=?1 AND guardian_person_id=?2 AND revoked_at IS NOT NULL`, child, other) != 1 {
		t.Fatal("removal did not preserve revoked history")
	}
	if f.count(`SELECT count(*) FROM persons WHERE id=?1`, other) != 1 {
		t.Fatal("guardian destroyed")
	}
	p442Post(t, b, path, add, 303)
	if f.count(`SELECT count(*) FROM guardian_access_grants WHERE child_person_id=?1 AND guardian_person_id=?2 AND revoked_at IS NULL`, child, other) != 0 {
		t.Fatal("relationship recreation revived grant")
	}
	_, outsider := f.personalBrowser(f.person, "outsider")
	if outsider.call("GET", path, nil).Code != 403 {
		t.Fatal("family permission")
	}
}
func TestP442ProfileAndFamilyDashboard(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	user, b := f.personalBrowser(parent, "parent")
	form := url.Values{"first_name": {"Marie"}, "last_name": {"Dupont"}, "phone_number": {"0612345678"}, "address": {"12 rue de Paris"}, "email": {"forged@example.test"}, "birth_date": {"2015-01-01"}, "person_id": {fmt.Sprint(f.person)}}
	f.accountPost(b, "/me/account/profile", form, 303)
	if f.count(`SELECT count(*) FROM persons WHERE id=?1 AND first_name='Claire' AND last_name='Famille' AND phone_number='33612345678' AND address='12 rue de Paris' AND email='claire@example.test' AND birth_date='1980-01-01'`, parent) != 1 {
		t.Fatal("profile scope/secure fields")
	}
	if f.count(`SELECT count(*) FROM persons WHERE id=?1 AND first_name='Rémi'`, f.person) != 1 {
		t.Fatal("forged Person update")
	}
	if f.count(`SELECT count(*) FROM account_security_events WHERE user_id=?1 AND event='profile_contact_updated'`, user) != 1 {
		t.Fatal("profile audit")
	}
	body := f.personalOK(b, "/me/account/profile", "Modifier mes coordonnées", "Un nouvel email ne sera utilisé")
	if strings.Contains(body, `name="first_name"`) || strings.Contains(body, `name="last_name"`) || strings.Contains(body, `name="birth_date"`) || strings.Contains(body, `name="email"`) {
		t.Fatal("unsafe profile fields")
	}
	ctx := f.authenticatedContext(f.approver)
	_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	f.exec(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES(?1,?2,?3,'pending')`, child, f.season, f.kind)
	body = f.personalOK(b, "/dashboard", "Demandes en cours", "<h2>Mes enfants</h2>", "Gérer les contacts d’urgence")
	if strings.Contains(body, "<h2>Mon adhésion</h2>") || strings.Contains(body, "Mes adhésions") {
		t.Fatal("parent without membership empty block")
	}
	if strings.Index(body, "<h2>Demandes en cours") > strings.Index(body, "<h2>Mes enfants") {
		t.Fatal("pending order")
	}
	f.exec(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES(?1,?2,?3,'active')`, parent, f.season, f.kind)
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name='secretary'`, user)
	body = f.personalOK(b, "/dashboard", "<h2>Mon adhésion</h2>", "<h2>Mes enfants</h2>")
	if strings.Index(body, "<h2>Mon adhésion") > strings.Index(body, "<h2>Mes enfants") {
		t.Fatal("personal/family order")
	}
}
func TestP442MembershipGroupCompatibility(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	m := f.request()
	same := f.id(`INSERT INTO groups(activity_id,name) VALUES(?1,'Compatible neutre') RETURNING id`, f.activity)
	incompatible := f.id(`INSERT INTO groups(activity_id,name) VALUES(?1,'Incompatible neutre') RETURNING id`, f.activity)
	otherActivity := f.id(`INSERT INTO activities(name) VALUES('Autre activité') RETURNING id`)
	foreign := f.id(`INSERT INTO groups(activity_id,name) VALUES(?1,'Autre groupe') RETURNING id`, otherActivity)
	path := dossierPath(m.ID) + "/groups"
	body := officeOK(t, b, path, "Aucun groupe compatible")
	if strings.Contains(body, "Compatible neutre") {
		t.Fatal("empty config bypass")
	}
	f.exec(`INSERT INTO membership_type_groups(membership_type_id,group_id) VALUES(?1,?2),(?1,?3)`, f.kind, same, foreign)
	body = officeOK(t, b, path, "Seul groupe compatible, prérempli")
	form := recipeForm(t, body, path)
	if form.Get("group_id") != fmt.Sprint(same) || strings.Contains(body, "Autre groupe") || strings.Contains(body, "Incompatible neutre") {
		t.Fatal("prefill/filter")
	}
	tx, err := f.db.BeginTx(t.Context(), nil)
	f.must(err)
	for _, group := range []int32{incompatible, foreign} {
		if err = f.memberships.AssignGroupTx(t.Context(), tx, dbsqlc.AssignMembershipGroupParams{MembershipID: m.ID, GroupID: group, JoinedAt: dbtypes.Date{Time: f.app.Administration.Today().Time, Valid: true}}); err == nil {
			t.Fatal("incompatible/activity group accepted")
		}
	}
	f.must(tx.Rollback())
	f.exec(`INSERT INTO membership_type_groups(membership_type_id,group_id) VALUES(?1,?2)`, f.kind, incompatible)
	body = officeOK(t, b, path, "Choisissez explicitement")
	if recipeForm(t, body, path).Get("group_id") != "" {
		t.Fatal("multiple default")
	}
	officePost(t, b, path, url.Values{"group_id": {""}, "joined_at": {f.app.Administration.Today().Time.Format("2006-01-02")}}, 303)
	if f.count(`SELECT count(*) FROM membership_groups WHERE membership_id=?1`, m.ID) != 0 {
		t.Fatal("sans groupe ignored")
	}
}

func TestP442SourceTrialGroupRespectsCompatibility(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		t.Run(fmt.Sprint(compatible), func(t *testing.T) {
			f := newFixture(t)
			b := f.membershipAdminBrowser()
			group, _ := f.officeGroup()
			if !compatible {
				f.exec(`DELETE FROM membership_type_groups WHERE membership_type_id=?1 AND group_id=?2`, f.kind, group)
			}
			trial := f.id(`INSERT INTO trial_registrations(person_id,activity_id,group_id,trial_date,status) VALUES(?1,?2,?3,'2026-09-16','attended') RETURNING id`, f.person, f.activity, group)
			officePost(t, b, officePerson(f.person)+"/memberships/new", url.Values{"source_trial": {fmt.Sprint(trial)}, "season_id": {fmt.Sprint(f.season)}, "type_id": {fmt.Sprint(f.kind)}, "activities": {fmt.Sprint(f.activity)}}, 303)
			membership := f.id(`SELECT id FROM memberships WHERE source_trial_id=?1`, trial)
			want := 0
			event := "membership_trial_group_skipped"
			if compatible {
				want = 1
				event = "membership_trial_group_assigned"
			}
			if f.count(`SELECT count(*) FROM membership_groups WHERE membership_id=?1 AND group_id=?2`, membership, group) != want || f.count(`SELECT count(*) FROM administrative_events WHERE resource_id=?1 AND action=?2`, membership, event) != 1 {
				t.Fatal("trial compatibility adoption")
			}
		})
	}
}

func TestP442GroupSuggestionsIndependentPerActivity(t *testing.T) {
	f := newFixture(t)
	b := f.membershipAdminBrowser()
	group, _ := f.officeGroup()
	other := f.id(`INSERT INTO activities(name) VALUES('Seconde pratique') RETURNING id`)
	otherGroup := f.id(`INSERT INTO groups(activity_id,name) VALUES(?1,'Groupe seconde pratique') RETURNING id`, other)
	f.exec(`INSERT INTO membership_type_groups(membership_type_id,group_id) VALUES(?1,?2)`, f.kind, otherGroup)
	m, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: f.person, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity, other}})
	f.must(err)
	body := officeOK(t, b, dossierPath(m.ID)+"/groups", "Groupe seconde pratique", "Groupe adultes")
	for _, id := range []int32{group, otherGroup} {
		if !strings.Contains(body, fmt.Sprintf(`<option value="%d" selected>`, id)) {
			t.Fatal("unique group missing per activity", id)
		}
	}
	if strings.Count(body, "Seul groupe compatible, prérempli") != 2 {
		t.Fatal("cross-activity arbitrary selection")
	}
}

func TestP442RelationsTranslatedAcrossDossiersAndPersonalViews(t *testing.T) {
	f := newFixture(t)
	public := newBrowser(f.app.Handler)
	form := f.childForm(public)
	form.Set("action", "review")
	recap := public.call("POST", "/join/child", form)
	if recap.Code != 200 || !strings.Contains(visibleText(recap.Body.String()), "Mère") {
		t.Fatal("public relationship label", recap.Code)
	}
	form.Set("action", "submit")
	submission := f.childSubmit(public, form)
	admin := f.membershipAdminBrowser()
	details, err := f.app.Reviews.GetDetails(t.Context(), f.approver, submission)
	f.must(err)
	child := details.Submission.ResolvedPersonID.Int32
	parent := details.Child.Guardian.ResolvedPersonID.Int32
	assertFrench := func(path, body string) {
		t.Helper()
		for _, code := range []string{"mother", "father", "guardian", "other"} {
			if strings.Contains(visibleText(body), code) {
				t.Fatal("untranslated relationship", path, code)
			}
		}
	}
	assertFrench("public recap", recap.Body.String())
	review := officeOK(t, admin, reviewPath(submission), "Mère")
	if !strings.Contains(review, fmt.Sprintf(`href="/persons/%d">Claire Famille`, parent)) || strings.Contains(review, fmt.Sprintf(`href="/persons/%d/edit">Claire Famille`, parent)) {
		t.Fatal("review guardian name must open Person")
	}
	assertFrench("review", review)
	f.must(f.app.RegistrationApplications.ConfirmGuardian(f.authenticatedContext(f.approver), submission))
	m := f.publicApplication(submission).MembershipID.Int32
	u, err := dbsqlc.New(f.db).GetUserByPerson(t.Context(), parent)
	f.must(err)
	activation := newBrowser(f.app.Handler)
	v := activationForm(activation.csrf(t, "/activate"), codeFrom(t, f.mail.messages[len(f.mail.messages)-1]), "a secure password")
	v.Set("username", u.Username)
	if response := activation.call("POST", "/activate", v); response.Code != 303 {
		t.Fatal("parent activation", response.Code)
	}
	family := f.loginBrowser(u.Username)
	for _, code := range []string{"mother", "father", "guardian", "other"} {
		f.exec(`UPDATE person_guardians SET relationship_type=?3 WHERE child_person_id=?1 AND guardian_person_id=?2`, child, parent, code)
		f.exec(`UPDATE person_emergency_contacts SET relationship_label=?2 WHERE person_id=?1`, child, code)
		for _, path := range []string{officePerson(child), officePerson(parent), officePerson(child) + "/family", officePerson(child) + "/emergency", dossierPath(m), reviewPath(submission)} {
			body := officeOK(t, admin, path)
			assertFrench(path, body)
			if path == dossierPath(m) && !strings.Contains(body, fmt.Sprintf(`href="/persons/%d">Claire Famille`, parent)) {
				t.Fatal("membership guardian name must open Person")
			}
		}
		for _, path := range []string{"/dashboard", personalChild(child), personalChild(child) + "/emergency", familyMembership(child, m)} {
			assertFrench(path, f.personalOK(family, path))
		}
	}
}
