package application

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestUsersAndRolesWebAdministration(t *testing.T) {
	f := newFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	f.exec("UPDATE users SET password_hash=$2,login_email='admin@example.test' WHERE id=$1", f.approver, string(hash))
	account := f.approved()
	if len(f.mail.messages) != 1 {
		t.Fatal("activation email missing")
	}
	activation := newBrowser(f.app.Handler)
	activationToken := activation.csrf(t, "/activate")
	if got := activation.call("POST", "/activate", activationForm(activationToken, codeFrom(t, f.mail.messages[0]), "a secure password")); got.Code != 303 {
		t.Fatalf("account activation: %d", got.Code)
	}
	member := account.UserID
	path := fmt.Sprintf("/admin/users/%d", member)
	mutation := path + "/roles"
	anonymous := newBrowser(f.app.Handler)
	if got := anonymous.call("GET", "/admin/users", nil); got.Code != 303 {
		t.Fatalf("anonymous access: %d", got.Code)
	}
	if got := anonymous.call("POST", mutation, url.Values{"role": {"secretary"}, "action": {"add"}}); got.Code != 303 {
		t.Fatalf("anonymous mutation: %d", got.Code)
	}
	ordinary := f.loginBrowser("remi.dupont")
	if got := ordinary.call("GET", "/admin/users", nil); got.Code != 403 {
		t.Fatalf("ordinary access: %d", got.Code)
	}
	if got := ordinary.call("POST", mutation, url.Values{"role": {"secretary"}, "action": {"add"}}); got.Code != 403 {
		t.Fatalf("ordinary mutation: %d", got.Code)
	}
	admin := f.loginBrowser("admin")
	list := admin.call("GET", "/admin/users", nil)
	if list.Code != 200 || !strings.Contains(list.Body.String(), "Rémi Dupont") || !strings.Contains(list.Body.String(), "admin@example.test") || !strings.Contains(list.Body.String(), "Président") {
		t.Fatal("user list missing identities or roles")
	}
	if strings.Contains(list.Body.String(), string(hash)) || strings.Contains(list.Body.String(), "a secure password") || strings.Contains(list.Body.String(), "preserved") {
		t.Fatal("credential exposed")
	}
	filtered := admin.call("GET", "/admin/users?q=Rémi", nil)
	if filtered.Code != 200 || !strings.Contains(filtered.Body.String(), "Rémi Dupont") || strings.Contains(filtered.Body.String(), "Admin Club") {
		t.Fatal("user search")
	}
	token := admin.csrf(t, path)
	if got := admin.call("POST", mutation, url.Values{"role": {"secretary"}, "action": {"add"}, "csrf_token": {"invalid"}}); got.Code != 403 {
		t.Fatalf("csrf: %d", got.Code)
	}
	var count int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND r.name='secretary'", member).Scan(&count))
	if count != 0 {
		t.Fatal("CSRF mutation persisted")
	}
	change := func(id int32, role, action string) int {
		t.Helper()
		response := admin.call("POST", fmt.Sprintf("/admin/users/%d/roles", id), url.Values{"role": {role}, "action": {action}, "csrf_token": {token}})
		return response.Code
	}
	if got := change(member, "secretary", "add"); got != 303 {
		t.Fatalf("grant: %d", got)
	}
	if got := ordinary.call("GET", "/persons", nil); got.Code != 200 {
		t.Fatal("granted role not effective")
	}
	if got := change(member, "secretary", "remove"); got != 303 {
		t.Fatalf("revoke: %d", got)
	}
	if got := ordinary.call("GET", "/persons", nil); got.Code != 403 {
		t.Fatal("revoked role still effective")
	}
	if got := change(f.approver, "president", "remove"); got != 303 {
		t.Fatalf("last manager response: %d", got)
	}
	self := admin.call("GET", fmt.Sprintf("/admin/users/%d?error=last", f.approver), nil)
	if !strings.Contains(self.Body.String(), "aucun autre compte") {
		t.Fatal("last manager message missing")
	}
	if got := admin.call("GET", "/admin/users", nil); got.Code != 200 {
		t.Fatal("last manager lost access")
	}
	if got := change(member, "president", "add"); got != 303 {
		t.Fatalf("second manager grant: %d", got)
	}
	if got := change(f.approver, "president", "remove"); got != 303 {
		t.Fatalf("first manager revoke: %d", got)
	}
	if got := admin.call("GET", "/admin/users", nil); got.Code != 403 {
		t.Fatal("first manager retained access")
	}
	if got := ordinary.call("GET", "/admin/users", nil); got.Code != 200 {
		t.Fatal("second manager lacks access")
	}
	if got := ordinary.call("GET", path, nil); got.Code != 200 || !strings.Contains(got.Body.String(), "Président") {
		t.Fatal("new manager detail")
	}
	var auditCount int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM administrative_events WHERE actor_user_id=$1 AND resource_type='user' AND role_name IN ('secretary','president') AND action IN ('role_granted','role_revoked')", f.approver).Scan(&auditCount))
	if auditCount != 4 {
		t.Fatalf("role audit count: %d", auditCount)
	}
}

func TestConcurrentRoleRemovalKeepsOneManager(t *testing.T) {
	f := newFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	f.must(err)
	f.exec("UPDATE users SET password_hash=$2 WHERE id=$1", f.approver, string(hash))
	person := f.id("INSERT INTO persons(first_name,last_name) VALUES ('Other','Manager') RETURNING id")
	other := f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'othermanager',$2,now()) RETURNING id", person, string(hash))
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='president'", other)
	browsers := []*browser{f.loginBrowser("admin"), f.loginBrowser("othermanager")}
	ids := []int32{f.approver, other}
	results := make(chan int, 2)
	for i, b := range browsers {
		id := ids[i]
		token := b.csrf(t, fmt.Sprintf("/admin/users/%d", id))
		go func(b *browser) {
			results <- b.call("POST", fmt.Sprintf("/admin/users/%d/roles", id), url.Values{"csrf_token": {token}, "role": {"president"}, "action": {"remove"}}).Code
		}(b)
	}
	for range 2 {
		if code := <-results; code != 303 {
			t.Fatalf("concurrent response: %d", code)
		}
	}
	var managers int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE r.name='president'").Scan(&managers))
	if managers != 1 {
		t.Fatalf("managers remaining: %d", managers)
	}
	var audits int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM administrative_events WHERE action='role_revoked'").Scan(&audits))
	if audits != 1 {
		t.Fatalf("revocation audit count: %d", audits)
	}
}
