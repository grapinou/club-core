package application

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/memberships"
	"golang.org/x/crypto/bcrypt"
)

func TestUXDashboardScopeAndComposition(t *testing.T) {
	f := newFixture(t)
	anon := newBrowser(f.app.Handler)
	page := anon.call("GET", "/", nil)
	for _, label := range []string{"Accueil", "Le club", "Où / Quand", "Adhérer", "Contact", "Connexion"} {
		if !strings.Contains(page.Body.String(), label) {
			t.Fatal("public navigation", label)
		}
	}
	for _, path := range []string{"/memberships", "/registration-reviews", "/persons"} {
		if strings.Contains(page.Body.String(), `href="`+path+`"`) {
			t.Fatal("public admin link", path)
		}
	}
	if r := anon.call("GET", "/dashboard", nil); r.Code != 303 || r.Header().Get("Location") != "/login" {
		t.Fatal("anonymous dashboard")
	}
	f.request()
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	user := f.id(`INSERT INTO users(person_id,username,password_hash,activated_at) VALUES($1,'member',$2,now()) RETURNING id`, f.person, string(hash))
	member := f.loginBrowser("member")
	page = member.call("GET", "/dashboard?person_id=999&user_id=999", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Practice") || !strings.Contains(page.Body.String(), "En attente") || !strings.Contains(page.Body.String(), "Vous ne gérez actuellement aucun enfant.") {
		t.Fatal("own membership", page.Code)
	}
	if page.Header().Get("Cache-Control") != "no-store" || !strings.Contains(page.Body.String(), `name="csrf_token"`) {
		t.Fatal("dashboard protections")
	}
	f.assertNoDeliverySecrets(page.Body.String())
	for _, path := range []string{"/memberships", "/registration-reviews", "/persons"} {
		if strings.Contains(page.Body.String(), `href="`+path) {
			t.Fatal("member admin link")
		}
	}
	child := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Enfant autorisé','Famille','2012-01-01') RETURNING id`)
	historical := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Enfant historique','Famille','2012-01-01') RETURNING id`)
	stranger := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Enfant sans lien','Famille','2012-01-01') RETURNING id`)
	for _, id := range []int32{child, historical} {
		f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES($1,$2,'guardian')`, id, f.person)
	}
	ctx := f.authenticatedContext(f.approver)
	_, err = f.app.GuardianAccess.Grant(ctx, child, f.person)
	f.must(err)
	_, err = f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: child, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}})
	f.must(err)
	page = member.call("GET", fmt.Sprintf("/dashboard?person_id=%d", stranger), nil)
	body := html.UnescapeString(page.Body.String())
	if !strings.Contains(body, "Enfant autorisé") || strings.Contains(body, "Enfant historique") || strings.Contains(body, "Enfant sans lien") {
		t.Fatal("guardian scope")
	}
	// A member can also be guardian and administrator: all sections remain present.
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'`, user)
	page = member.call("GET", "/dashboard", nil)
	body = html.UnescapeString(page.Body.String())
	for _, value := range []string{"Practice", "Enfant autorisé", "Administration", `href="/memberships"`, `href="/registration-reviews"`, `href="/persons"`} {
		if !strings.Contains(body, value) {
			t.Fatal("composed sections", value)
		}
	}
	f.exec(`DELETE FROM user_roles WHERE user_id=$1`, user)
	page = member.call("GET", "/dashboard", nil)
	if strings.Contains(page.Body.String(), `href="/registration-reviews"`) {
		t.Fatal("revoked admin nav")
	}
	f.exec(`UPDATE persons SET birth_date='1990-01-01' WHERE id=$1`, child)
	page = member.call("GET", "/dashboard", nil)
	if strings.Contains(html.UnescapeString(page.Body.String()), "Enfant autorisé") {
		t.Fatal("adult disclosed")
	}
	f.exec(`UPDATE persons SET birth_date='2012-01-01' WHERE id=$1`, child)
	f.must(f.app.GuardianAccess.Revoke(ctx, child, f.person))
	page = member.call("GET", "/dashboard", nil)
	if strings.Contains(html.UnescapeString(page.Body.String()), "Enfant autorisé") {
		t.Fatal("revoked grant disclosed")
	}
	if r := member.call("POST", "/logout", url.Values{}); r.Code != 403 {
		t.Fatal("logout CSRF")
	}
}

func TestUXFormsEmptyStatesAndReviewPriority(t *testing.T) {
	f := newFixture(t)
	b := newBrowser(f.app.Handler)
	for _, path := range []string{"/join", "/join/child"} {
		page := b.call("GET", path, nil)
		body := html.UnescapeString(page.Body.String())
		for _, label := range []string{"Je m’inscris", "J’inscris mon enfant", "Adhésion d’un adulte", "Inscription d’un mineur"} {
			if !strings.Contains(body, label) {
				t.Fatal(path, label)
			}
		}
	}
	childPage := b.call("GET", "/join/child", nil)
	for _, label := range []string{"Votre email de responsable", "Email de l’enfant", "Votre lien avec l’enfant", "Contact d’urgence"} {
		if !strings.Contains(html.UnescapeString(childPage.Body.String()), label) {
			t.Fatal("child labels", label)
		}
	}
	if !strings.Contains(childPage.Body.String(), `aria-describedby="guardian_email-error"`) {
		t.Fatal("guardian error association")
	}
	form := f.childForm(b)
	form.Set("guardian_email", "incorrect")
	r := b.call("POST", "/join/child", form)
	if r.Code != 422 || !strings.Contains(r.Body.String(), `value="incorrect"`) {
		t.Fatal("validation values lost")
	}
	admin := f.membershipAdminBrowser()
	for path, want := range map[string]string{"/memberships": "Aucune adhésion en attente.", "/registration-reviews": "Aucune vérification ne nécessite votre intervention.", "/persons/archived": "Aucune personne archivée."} {
		r := admin.call("GET", path, nil)
		if r.Code != 200 || !strings.Contains(html.UnescapeString(r.Body.String()), want) {
			t.Fatal("empty state", path)
		}
	}
	id := f.childSubmit(b, f.childForm(b))
	r = admin.call("GET", reviewPath(id), nil)
	body := html.UnescapeString(r.Body.String())
	reason := strings.Index(body, "Lien avec le responsable à confirmer")
	history := strings.Index(body, "<h2>Historique et résolution")
	if reason < 0 || history < reason || !strings.Contains(body, "Confirmer ce responsable pour cet enfant") {
		t.Fatal("review priority")
	}
	if strings.Contains(body, ">awaiting_review<") || strings.Contains(body, ">mother<") {
		t.Fatal("raw statuses")
	}
	if r = admin.call("POST", reviewPath(id)+"/confirm-guardian", url.Values{}); r.Code != 403 {
		t.Fatal("confirmation lost CSRF")
	}
}
