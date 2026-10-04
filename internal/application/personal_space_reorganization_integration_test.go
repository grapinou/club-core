package application

import (
	"fmt"
	"strings"
	"testing"
)

func personalContent(t *testing.T, body string) string {
	t.Helper()
	return pagePart(t, body, `<main id="main-content"`, `</main>`)
}

func TestP4412DashboardHierarchyAndEmergencyCounts(t *testing.T) {
	f := newFixture(t)
	f.exec(`UPDATE persons SET first_name='Rémi Alexandre' WHERE id=?`, f.person)
	_, b := f.personalBrowser(f.person, "member")
	empty := personalContent(t, f.personalOK(b, "/dashboard", "Bonjour Rémi Alexandre.", "Retrouvez ici votre dossier et ceux de votre famille."))
	if strings.Contains(empty, "À suivre") || strings.Contains(empty, "Ma famille") || strings.Contains(empty, "Documents") {
		t.Fatal("empty optional sections")
	}
	if !strings.Contains(empty, "Mon dossier") || !strings.Contains(empty, "Aucun contact d’urgence renseigné") {
		t.Fatal("empty dossier")
	}
	current := f.request()
	f.exec(`UPDATE memberships SET requested_at='2026-10-04 10:00:00' WHERE id=?`, current.ID)
	oldSeason := f.id(`INSERT INTO seasons(name,starts_at,ends_at) VALUES('2025/2026','2025-09-01','2026-08-31') RETURNING id`)
	history := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES(?,?,?,'ended') RETURNING id`, f.person, oldSeason, f.kind)
	for i := 1; i <= 2; i++ {
		contact := f.id(`INSERT INTO persons(first_name,last_name,phone_number) VALUES(?,'Confidentiel',?) RETURNING id`, fmt.Sprintf("CONTACT_%d", i), fmt.Sprintf("060000000%d", i))
		f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority) VALUES(?,?,'PRIVATE_RELATION',?)`, f.person, contact, i)
	}
	body := personalContent(t, f.personalOK(b, "/dashboard", "À suivre", "Mon dossier", "Contacts d’urgence : 2 renseignés", "Demande reçue le 04/10/2026 · En cours"))
	if strings.Index(body, "À suivre") > strings.Index(body, "Mon dossier") || strings.Index(body, "Mon dossier") > strings.Index(body, "Mon compte") {
		t.Fatal("dashboard priorities")
	}
	dossier := pagePart(t, body, `id="mon-dossier"`, `</section>`)
	historyBlock := pagePart(t, dossier, "<details>", "</details>")
	if !strings.Contains(historyBlock, personalMembership(history)) || strings.Contains(historyBlock, personalMembership(current.ID)) {
		t.Fatal("history priority")
	}
	if !strings.Contains(dossier, personalMembership(current.ID)) || !strings.Contains(dossier, `href="/me/emergency"`) {
		t.Fatal("current dossier links")
	}
	for _, unwanted := range []string{"CONTACT_1", "CONTACT_2", "0600000001", "PRIVATE_RELATION", "Demandes en cours", "Mon adhésion", "/contact", "/logout", "Documents"} {
		if strings.Contains(body, unwanted) {
			t.Fatal("dashboard detail or duplicate", unwanted)
		}
	}
	f.personalOK(b, "/me/emergency", "CONTACT_1", "CONTACT_2", "0600000001", "PRIVATE_RELATION")
	f.exec(`UPDATE memberships SET status='active' WHERE id=?`, current.ID)
	if strings.Contains(personalContent(t, f.personalOK(b, "/dashboard")), "À suivre") {
		t.Fatal("empty follow-up block")
	}
}

func TestP4412FamilyRowsAndChildInformation(t *testing.T) {
	f := newFixture(t)
	arthur, parent := f.guardianPair()
	user, b := f.personalBrowser(parent, "parent")
	louise := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Louise','Famille','2015-01-01') RETURNING id`)
	hidden := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('PRIVATE_CHILD','Famille','2015-01-01') RETURNING id`)
	f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES(?,?,'mother'),(?,?,'mother')`, louise, parent, hidden, parent)
	ctx := f.authenticatedContext(f.approver)
	for _, child := range []int32{arthur, louise} {
		_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
		f.must(err)
	}
	active := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES(?,?,?,'active') RETURNING id`, arthur, f.season, f.kind)
	f.exec(`INSERT INTO membership_activities(membership_id,activity_id) VALUES(?,?)`, active, f.activity)
	group := f.id(`INSERT INTO groups(activity_id,name) VALUES(?,'Groupe enfants') RETURNING id`, f.activity)
	f.exec(`INSERT INTO membership_groups(membership_id,group_id,joined_at) VALUES(?,?,CURRENT_DATE)`, active, group)
	contact := f.id(`INSERT INTO persons(first_name,last_name,phone_number) VALUES('CONTACT_ENFANT','Autorisé','0612345678') RETURNING id`)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority) VALUES(?,?,'mother',1)`, arthur, contact)
	body := personalContent(t, f.personalOK(b, "/dashboard", "Ma famille", "Ajouter un enfant", "Arthur Famille", "Louise Famille", "Groupe enfants"))
	family := pagePart(t, body, `id="ma-famille"`, `</section>`)
	if strings.Contains(family, `class="section-panel"`) || strings.Count(family, `class="summary-row"`) != 2 {
		t.Fatal("nested family panels")
	}
	for _, name := range []string{"Arthur Famille", "Louise Famille"} {
		row := pagePart(t, family, "<h3>"+name, "</article>")
		if !strings.Contains(row, "Voir son dossier") {
			t.Fatal("missing child dossier action")
		}
	}
	if !strings.Contains(family, "Voir l’adhésion") || !strings.Contains(family, "Demander une adhésion") || !strings.Contains(family, "Aucune adhésion enregistrée.") || !strings.Contains(family, "Aucun contact d’urgence renseigné") || !strings.Contains(family, "Contact d’urgence : renseigné") {
		t.Fatal("family actions and states")
	}
	for _, secret := range []string{"CONTACT_ENFANT", "0612345678", "PRIVATE_CHILD"} {
		if strings.Contains(body, secret) {
			t.Fatal("family summary privacy", secret)
		}
	}
	childBody := personalContent(t, f.personalOK(b, personalChild(arthur), "CONTACT_ENFANT", "0612345678", "12/09/2011"))
	information := pagePart(t, childBody, "<h2>Informations</h2>", "</section>")
	if strings.Contains(information, "urgence") || strings.Count(childBody, "Gérer les contacts d’urgence") != 1 || strings.Count(childBody, "<h2>Contacts d’urgence</h2>") != 1 {
		t.Fatal("duplicate child contacts")
	}
	if strings.Contains(childBody, `class="card`) || !strings.Contains(childBody, `class="summary-row"`) {
		t.Fatal("large child membership cards")
	}
	if strings.Index(childBody, "Informations</h2>") > strings.Index(childBody, "Contacts d’urgence</h2>") || strings.Index(childBody, "Contacts d’urgence</h2>") > strings.Index(childBody, "Adhésions</h2>") {
		t.Fatal("child section order")
	}
	// Administrative functions do not expand family access.
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?,id FROM roles WHERE name='secretary'`, user)
	body = f.personalOK(b, "/dashboard", "Secrétaire", "Mon tableau de bord")
	if strings.Contains(body, "PRIVATE_CHILD") {
		t.Fatal("office role broadened dashboard")
	}
	f.personalDenied(b, personalChild(hidden))
	f.personalDenied(b, familyMembership(hidden, active))
}

func TestP4412AccountAndMembershipContext(t *testing.T) {
	f := newFixture(t)
	_, b := f.personalBrowser(f.person, "member")
	f.exec(`UPDATE persons SET phone_number='0601020304',address='12 rue du Club' WHERE id=?`, f.person)
	account := personalContent(t, f.personalOK(b, "/me/account", "Identifiant", "member", "remi@example.test", "12 rue du Club"))
	identity := pagePart(t, account, "<h2>Identité", "</section>")
	security := pagePart(t, account, "<h2>Connexion et sécurité", "</section>")
	if strings.Contains(identity, "member") || strings.Contains(identity, "Nom d’utilisateur") || !strings.Contains(identity, "vérifiées par le club") || !strings.Contains(security, "member") || !strings.Contains(security, `href="/me/account/password"`) {
		t.Fatal("account hierarchy")
	}
	for _, unwanted := range []string{"Contacts d’urgence", "/me/emergency", "État du compte", "contactez le club", "name=\"username\""} {
		if strings.Contains(account, unwanted) {
			t.Fatal("account redundant/editable content", unwanted)
		}
	}
	own := f.request()
	arthur, parent := f.guardianPair()
	_, guardian := f.personalBrowser(parent, "guardian")
	_, err := f.app.GuardianAccess.Grant(f.authenticatedContext(f.approver), arthur, parent)
	f.must(err)
	childMembership := f.id(`INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES(?,?,?,'pending') RETURNING id`, arthur, f.season, f.kind)
	for _, tc := range []struct {
		b                      *browser
		path, returnURL, label string
	}{
		{b, "/me/account", "/dashboard", "Retour à mon espace"},
		{b, "/me/emergency", "/dashboard", "Retour à mon espace"},
		{b, personalMembership(own.ID), "/dashboard", "Retour à mon espace"},
		{guardian, personalChild(arthur), "/dashboard", "Retour à mon espace"},
		{guardian, familyMembership(arthur, childMembership), personalChild(arthur), "Retour au dossier d’Arthur"},
	} {
		body := personalContent(t, f.personalOK(tc.b, tc.path))
		if strings.Count(body, ">Retour ") != 1 || !strings.Contains(body, `href="`+tc.returnURL+`">`+tc.label) || strings.Contains(body, "contactez le club") {
			t.Fatal("contextual return", tc.path)
		}
		if strings.Contains(tc.path, "memberships") {
			if !strings.Contains(body, "<h2>Pratique</h2>") || !strings.Contains(body, "<h2>Consentements</h2>") || strings.Contains(body, "<h2>Activités</h2>") || strings.Contains(body, "<h2>Groupes actuels</h2>") {
				t.Fatal("practice grouping")
			}
		}
	}
	// New read-only context does not weaken the resource ownership check.
	f.personalDenied(guardian, familyMembership(arthur, own.ID))
}
