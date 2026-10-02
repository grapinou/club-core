package application

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func pagePart(t *testing.T, body, start, end string) string {
	t.Helper()
	a := strings.Index(body, start)
	if a < 0 {
		t.Fatalf("missing %s", start)
	}
	b := strings.Index(body[a:], end)
	if b < 0 {
		t.Fatalf("missing %s", end)
	}
	return body[a : a+b]
}

func TestPersonalAccountUXNavigationAndLogout(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	for _, path := range []string{"/dashboard", "/me/account", "/me/account/profile"} {
		body := f.personalOK(b, path)
		nav := pagePart(t, body, `<header class="site-header">`, `</header>`)
		if strings.Count(nav, `href="/dashboard"`) != 1 || strings.Contains(nav, `href="/me/account"`) {
			t.Fatal("ambiguous personal navigation", path)
		}
		for _, want := range []string{`href="/">Site du club`, `method="post" action="/logout"`, `name="csrf_token"`, "Se déconnecter"} {
			if !strings.Contains(nav, want) {
				t.Fatal("connected navigation", path, want)
			}
		}
		if strings.Contains(nav, `value=""`) {
			t.Fatal("empty logout CSRF")
		}
	}
	body := f.personalOK(b, "/dashboard", `href="/me/account">Mon compte`, "Contacts d’urgence")
	content := pagePart(t, body, `<main id="main-content"`, `</main>`)
	for _, unwanted := range []string{"Contacter le club", "/logout", "/me/account/profile", "<form", "Ouvrir l’administration"} {
		if strings.Contains(content, unwanted) {
			t.Fatal("dashboard content", unwanted)
		}
	}
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name='secretary'`, user)
	body = f.personalOK(b, "/dashboard")
	nav := pagePart(t, body, `<nav class="admin-nav"`, `</nav>`)
	role, personal, management := strings.Index(nav, "Secrétaire"), strings.Index(nav, `href="/dashboard"`), strings.Index(nav, `href="/admin"`)
	if role < 0 || personal < role || management < personal {
		t.Fatal("role/personal/management order")
	}
	f.exec(`DELETE FROM user_roles WHERE user_id=?1`, user)
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name='treasurer'`, user)
	body = f.personalOK(b, "/dashboard", "Trésorier", "Mon espace")
	nav = pagePart(t, body, `<header class="site-header">`, `</header>`)
	if strings.Contains(nav, `href="/admin"`) || strings.Index(nav, "Trésorier") > strings.Index(nav, `href="/dashboard"`) {
		t.Fatal("role badge granted management")
	}
	if b.call("GET", "/admin", nil).Code != 403 {
		t.Fatal("role badge bypassed RBAC")
	}
	if b.call("POST", "/logout", url.Values{}).Code != 403 {
		t.Fatal("logout without CSRF")
	}
	if b.call("POST", "/logout", url.Values{"csrf_token": {strings.Repeat("0", 64)}}).Code != 403 {
		t.Fatal("logout with invalid CSRF")
	}
	f.personalOK(b, "/me/account")
	r := b.call("POST", "/logout", url.Values{"csrf_token": {b.csrf(t, "/dashboard")}})
	if r.Code != 303 || r.Header().Get("Location") != "/login" {
		t.Fatal("navigation logout")
	}
	if b.call("GET", "/dashboard", nil).Code != 303 {
		t.Fatal("session survived logout")
	}
}

func TestPersonalAccountUXHeaderAndEditing(t *testing.T) {
	f := newFixture(t)
	user, b := f.personalBrowser(f.person, "member")
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name IN ('secretary','coach')`, user)
	body := f.personalOK(b, "/me/account", "<title>Mon compte", "Éditer")
	content := pagePart(t, body, `<main id="main-content"`, `</main>`)
	header := pagePart(t, content, `<header class="page-header`, `</header>`)
	for _, want := range []string{"Rémi Dupont", "Fonctions au club", "Secrétariat", "Encadrement sportif", " · "} {
		if !strings.Contains(header, want) {
			t.Fatal("account header", want)
		}
	}
	if strings.Contains(content, "<h2>Fonctions au club") {
		t.Fatal("duplicate functions")
	}
	identity := pagePart(t, content, "<h2>Identité", "</section>")
	if !strings.Contains(identity, `href="/me/account/identity"`) || !strings.Contains(identity, "vérifiées par le club") || !strings.Contains(identity, "nom d’utilisateur reste fixe") {
		t.Fatal("identity correction explanation")
	}
	coordinates := pagePart(t, content, "<h2>Coordonnées", "</section>")
	if !strings.Contains(coordinates, `href="/me/account/profile"`) || !strings.Contains(coordinates, ">Éditer</a>") || strings.Contains(coordinates, `href="/me/account/email"`) || strings.Contains(coordinates, "Modifier mes coordonnées") {
		t.Fatal("coordinate actions")
	}
}

func TestPersonalDashboardEmergencyPrivacy(t *testing.T) {
	f := newFixture(t)
	child, parent := f.guardianPair()
	user, b := f.personalBrowser(parent, "parent")
	ownContact := f.id(`INSERT INTO persons(first_name,last_name,phone_number) VALUES('Contact personnel','Autorisé','0611223344') RETURNING id`)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES(?1,?2,1)`, parent, ownContact)
	outsider := f.id(`INSERT INTO persons(first_name,last_name) VALUES('PRIVATE_OWNER','Secret') RETURNING id`)
	privateContact := f.id(`INSERT INTO persons(first_name,last_name,phone_number,email) VALUES('PRIVATE_CONTACT','Secret','PRIVATE_PHONE','private@example.test') RETURNING id`)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES(?1,?2,1)`, outsider, privateContact)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES(?1,?2,1)`, child, parent)
	check := func(wantChild bool) string {
		t.Helper()
		body := f.personalOK(b, "/dashboard?person_id=999", "Contact personnel Autorisé", "0611223344", `href="/me/emergency"`)
		for _, secret := range []string{"PRIVATE_OWNER", "PRIVATE_CONTACT", "PRIVATE_PHONE", "private@example.test"} {
			if strings.Contains(body, secret) {
				t.Fatal("unrelated emergency leak", secret)
			}
		}
		if strings.Contains(body, personalChild(child)) != wantChild {
			t.Fatal("emergency child authorization")
		}
		return body
	}
	check(false)
	ctx := f.authenticatedContext(f.approver)
	_, err := f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	body := check(true)
	if !strings.Contains(body, "Claire Famille") || !strings.Contains(body, personalChild(child)+"/emergency") {
		t.Fatal("authorized child emergency consultation/management lost")
	}
	f.personalOK(b, personalChild(child)+"/emergency", "Claire Famille")
	f.exec(`DELETE FROM person_emergency_contacts WHERE person_id=?1`, child)
	if !strings.Contains(check(true), "Aucun contact d’urgence enregistré") {
		t.Fatal("missing emergency status")
	}
	childContact := f.id(`INSERT INTO persons(first_name,last_name,phone_number) VALUES('Contact enfant','Autorisé','0622334455') RETURNING id`)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES(?1,?2,1)`, child, childContact)
	if !strings.Contains(check(true), "Contact enfant Autorisé") {
		t.Fatal("authorized child contact hidden")
	}
	_, minor := f.personalBrowser(child, "minor")
	minorBody := f.personalOK(minor, "/dashboard")
	if strings.Contains(minorBody, "Contact enfant Autorisé") || strings.Contains(minorBody, `href="/me/emergency"`) {
		t.Fatal("minor emergency restriction bypassed")
	}
	f.personalDenied(minor, "/me/emergency")
	f.must(f.app.GuardianAccess.Revoke(ctx, child, parent))
	if strings.Contains(check(false), "Contact enfant Autorisé") {
		t.Fatal("revoked contact leak")
	}
	f.personalDenied(b, personalChild(child)+"/emergency")
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name='secretary'`, user)
	if strings.Contains(check(false), "Contact enfant Autorisé") {
		t.Fatal("administrative role broadened personal access")
	}
}

func TestEmergencyRequiredLabelsPreserveOptionalAndSharedFields(t *testing.T) {
	f := newFixture(t)
	_, b := f.personalBrowser(f.person, "member")
	path := "/me/emergency"
	body := f.personalOK(b, path, "Les champs marqués * sont obligatoires.")
	for _, want := range []string{`for="first-new">Prénom *</label>`, `for="last-new">Nom *</label>`, `for="phone-new">Téléphone *</label>`, `for="relation-new">Lien avec l’adhérent *</label>`, `for="email-new">Email (facultatif)</label>`} {
		if !strings.Contains(body, want) {
			t.Fatal("emergency labels", want)
		}
	}
	for _, id := range []string{"first-new", "last-new", "phone-new", "relation-new"} {
		if !regexp.MustCompile(`<input[^>]+id="` + id + `"[^>]+required`).MatchString(body) {
			t.Fatal("missing required attribute", id)
		}
	}
	p442Post(t, b, path, emergencyForm(), 303)
	contact := f.id(`SELECT contact_person_id FROM person_emergency_contacts WHERE person_id=?1`, f.person)
	relation := f.id(`SELECT id FROM person_emergency_contacts WHERE person_id=?1`, f.person)
	sharedOwner := f.id(`INSERT INTO persons(first_name,last_name) VALUES('Autre','Dossier') RETURNING id`)
	f.exec(`INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES(?1,?2,1)`, sharedOwner, contact)
	body = f.personalOK(b, path)
	label := regexp.MustCompile(`<label[^>]+for="phone-` + fmt.Sprint(relation) + `"[^>]*>[^<]*</label>`).FindString(body)
	input := regexp.MustCompile(`<input[^>]+id="phone-` + fmt.Sprint(relation) + `"[^>]*>`).FindString(body)
	if label == "" || strings.Contains(label, "*") || input == "" || strings.Contains(input, "required") || !strings.Contains(input, "readonly") {
		t.Fatal("shared phone obligation changed")
	}
}

func TestChildEmergencyRequiredLabel(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	body := b.call("GET", "/join/child", nil).Body.String()
	for _, want := range []string{"Contact d’urgence *</legend>", "Les champs marqués * sont obligatoires."} {
		if !strings.Contains(body, want) {
			t.Fatal("required emergency label", want)
		}
	}
	for _, value := range []string{"yes", "no"} {
		if !regexp.MustCompile(`name="emergency_contact" value="` + value + `" required`).MatchString(body) {
			t.Fatal("required emergency choice", value)
		}
	}
	form := f.childForm(b)
	form.Del("emergency_contact")
	if b.call("POST", "/join/child", form).Code != 422 {
		t.Fatal("missing choice accepted")
	}
	form.Set("emergency_contact", "no")
	r := b.call("POST", "/join/child", form)
	if r.Code != 200 && r.Code != 303 {
		t.Fatal("negative choice rejected", r.Code)
	}
}
