package application

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/initialsetup"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func newSetupApplication(t *testing.T) (*pgxpool.Pool, *Application, *initialsetup.Service) {
	t.Helper()
	db := newApplicationDatabase(t, "club_setup_test")
	runtime := config.Runtime{RegistrationVerificationTTL: config.DefaultRegistrationVerificationTTL, BaseURL: "https://club.example.test", SecureCookies: true, ActivationValidity: time.Hour, Location: time.UTC, SMTP: mailer.SMTPConfig{From: "club@example.test"}}
	app, err := NewWithMailer(config.Config{SiteName: "Club Core"}, runtime, db, &fakeMailer{})
	if err != nil {
		t.Fatal(err)
	}
	return db, app, initialsetup.New(db)
}

func setupForm(token, secret, username string) url.Values {
	return url.Values{"csrf_token": {token}, "setup_secret": {secret}, "first_name": {"Camille"}, "last_name": {"Martin"}, "username": {username}, "email": {username + "@example.test"}, "password": {"a secure password"}, "confirmation": {"a secure password"}}
}

func setupCounts(t *testing.T, db *pgxpool.Pool) (int, int, int) {
	t.Helper()
	var persons, users, assignments int
	err := db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM persons),(SELECT count(*) FROM users),(SELECT count(*) FROM user_roles)`).Scan(&persons, &users, &assignments)
	if err != nil {
		t.Fatal(err)
	}
	return persons, users, assignments
}

func TestInitialSetupLifecycle(t *testing.T) {
	db, app, setup := newSetupApplication(t)
	initial, err := setup.Status(t.Context())
	if err != nil || initial.Initialized || initial.Ready {
		t.Fatalf("initial state: %+v %v", initial, err)
	}
	b := newBrowser(app.Handler)
	if page := b.call("GET", "/setup", nil); page.Code != 200 || !strings.Contains(page.Body.String(), "n’a pas encore été préparé") {
		t.Fatal("unissued setup page")
	}
	secret, err := setup.IssueSecret(t.Context(), false)
	if err != nil || len(secret) != 64 {
		t.Fatalf("secret generation: length=%d err=%v", len(secret), err)
	}
	var stored []byte
	var generation int
	if err := db.QueryRow(t.Context(), `SELECT secret_hash,secret_generation FROM installation_setup`).Scan(&stored, &generation); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(secret))
	if len(stored) != 32 || string(stored) != string(digest[:]) || strings.Contains(string(stored), secret) || generation != 1 {
		t.Fatal("secret stored incorrectly")
	}
	if _, err = setup.IssueSecret(t.Context(), false); !errors.Is(err, initialsetup.ErrSecretExists) {
		t.Fatal("ordinary issue replaced secret", err)
	}
	newSecret, err := setup.IssueSecret(t.Context(), true)
	if err != nil || newSecret == secret {
		t.Fatal("rotation failed", err)
	}
	page := b.call("GET", "/setup", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Créer mon compte administrateur") || strings.Contains(page.Body.String(), newSecret) {
		t.Fatal("setup form or secret exposure")
	}
	token := b.csrf(t, "/setup")
	wrong := b.call("POST", "/setup", setupForm(token, secret, "camille"))
	if wrong.Code != 422 || !strings.Contains(wrong.Body.String(), "incorrect ou n’est plus valide") {
		t.Fatalf("rotated secret: %d", wrong.Code)
	}
	if strings.Contains(wrong.Body.String(), secret) || strings.Contains(wrong.Body.String(), newSecret) {
		t.Fatal("setup code echoed in response")
	}
	if p, u, r := setupCounts(t, db); p != 0 || u != 0 || r != 0 {
		t.Fatal("partial wrong-secret creation")
	}
	bad := setupForm(token, newSecret, "camille")
	bad.Set("password", "short")
	bad.Set("confirmation", "short")
	if response := b.call("POST", "/setup", bad); response.Code != 422 {
		t.Fatalf("invalid password: %d", response.Code)
	}
	bad = setupForm(token, newSecret, "camille")
	bad.Set("email", "bad-address")
	if response := b.call("POST", "/setup", bad); response.Code != 422 {
		t.Fatalf("invalid email: %d", response.Code)
	}
	bad = setupForm(token, newSecret, "Bad User")
	if response := b.call("POST", "/setup", bad); response.Code != 422 {
		t.Fatalf("invalid username: %d", response.Code)
	}
	if p, u, r := setupCounts(t, db); p != 0 || u != 0 || r != 0 {
		t.Fatal("partial invalid-form creation")
	}
	noCSRF := setupForm("invalid", newSecret, "camille")
	if response := b.call("POST", "/setup", noCSRF); response.Code != 403 {
		t.Fatalf("setup CSRF: %d", response.Code)
	}
	response := b.call("POST", "/setup", setupForm(token, newSecret, "camille"))
	if response.Code != 303 || response.Header().Get("Location") != "/login?setup=1" {
		t.Fatalf("setup success: %d", response.Code)
	}
	if p, u, r := setupCounts(t, db); p != 1 || u != 1 || r != 1 {
		t.Fatalf("created counts %d %d %d", p, u, r)
	}
	state, err := setup.Status(t.Context())
	if err != nil || !state.Initialized || state.Ready {
		t.Fatalf("completed state: %+v %v", state, err)
	}
	var active, activated bool
	var username, passwordHash string
	var firstUser, person int32
	var consumed []byte
	err = db.QueryRow(t.Context(), `SELECT u.id,u.person_id,u.username,u.password_hash,u.is_active,u.activated_at IS NOT NULL,s.secret_hash FROM users u CROSS JOIN installation_setup s`).Scan(&firstUser, &person, &username, &passwordHash, &active, &activated, &consumed)
	if err != nil || username != "camille" || !active || !activated || consumed != nil || passwordHash == "a secure password" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("a secure password")) != nil {
		t.Fatal("first account invalid", err)
	}
	var role string
	if err := db.QueryRow(t.Context(), `SELECT r.name FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1`, firstUser).Scan(&role); err != nil || role != "president" {
		t.Fatal("missing management role", err)
	}
	if page := b.call("GET", "/setup", nil); page.Code != 303 || page.Header().Get("Location") != "/login" {
		t.Fatal("setup remained open")
	}
	if second := b.call("POST", "/setup", setupForm(token, newSecret, "second")); second.Code != 409 || !strings.Contains(second.Body.String(), "déjà été configurée") {
		t.Fatalf("secret reused: %d", second.Code)
	}
	if _, err := setup.IssueSecret(t.Context(), true); !errors.Is(err, initialsetup.ErrInitialized) {
		t.Fatal("rotation after setup", err)
	}
	loginToken := b.csrf(t, "/login")
	logged := b.call("POST", "/login", url.Values{"csrf_token": {loginToken}, "username": {"camille"}, "password": {"a secure password"}})
	if logged.Code != 303 {
		t.Fatal("first admin login")
	}
	if users := b.call("GET", "/admin/users", nil); users.Code != 200 {
		t.Fatal("first admin cannot manage roles")
	}
	roleToken := b.csrf(t, fmt.Sprintf("/admin/users/%d", firstUser))
	last := b.call("POST", fmt.Sprintf("/admin/users/%d/roles", firstUser), url.Values{"csrf_token": {roleToken}, "role": {"president"}, "action": {"remove"}})
	if last.Code != 303 || !strings.Contains(b.call("GET", last.Header().Get("Location"), nil).Body.String(), "aucun autre compte") {
		t.Fatal("last manager guard after setup")
	}
	otherPerson := int32(0)
	if err := db.QueryRow(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES ('Second','Member') RETURNING id`).Scan(&otherPerson); err != nil {
		t.Fatal(err)
	}
	otherHash, err := bcrypt.GenerateFromPassword([]byte("a secure password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	var otherUser int32
	if err := db.QueryRow(t.Context(), `INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'second',$2,clock_timestamp()) RETURNING id`, otherPerson, string(otherHash)).Scan(&otherUser); err != nil {
		t.Fatal(err)
	}
	grant := b.call("POST", fmt.Sprintf("/admin/users/%d/roles", otherUser), url.Values{"csrf_token": {roleToken}, "role": {"secretary"}, "action": {"add"}})
	if grant.Code != 303 {
		t.Fatal("first admin cannot grant role")
	}
	member := newBrowser(app.Handler)
	loginToken = member.csrf(t, "/login")
	if got := member.call("POST", "/login", url.Values{"csrf_token": {loginToken}, "username": {"second"}, "password": {"a secure password"}}); got.Code != 303 {
		t.Fatal("second login")
	}
	if got := member.call("GET", "/persons", nil); got.Code != 200 {
		t.Fatal("second role ineffective")
	}
	_ = person
}

func TestInitialSetupConcurrentAndDuplicate(t *testing.T) {
	db, app, setup := newSetupApplication(t)
	secret, err := setup.IssueSecret(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	browsers := []*browser{newBrowser(app.Handler), newBrowser(app.Handler)}
	tokens := []string{browsers[0].csrf(t, "/setup"), browsers[1].csrf(t, "/setup")}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i, b := range browsers {
		wg.Add(1)
		go func(i int, b *browser) {
			defer wg.Done()
			results <- b.call("POST", "/setup", setupForm(tokens[i], secret, fmt.Sprintf("admin%d", i))).Code
		}(i, b)
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[303] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent setup: %+v", counts)
	}
	if p, u, r := setupCounts(t, db); p != 1 || u != 1 || r != 1 {
		t.Fatalf("concurrent rows: %d %d %d", p, u, r)
	}
}

func TestInitialSetupDuplicateAndRateLimit(t *testing.T) {
	db, app, setup := newSetupApplication(t)
	secret, err := setup.IssueSecret(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	var person int32
	if err := db.QueryRow(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES ('Existing','Person') RETURNING id`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), `INSERT INTO users(person_id,username) VALUES ($1,'camille')`, person); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(app.Handler)
	token := b.csrf(t, "/setup")
	if result := b.call("POST", "/setup", setupForm(token, secret, "camille")); result.Code != 422 {
		t.Fatalf("duplicate username: %d", result.Code)
	}
	if p, u, r := setupCounts(t, db); p != 1 || u != 1 || r != 0 {
		t.Fatal("partial duplicate creation")
	}
	state, err := setup.Status(t.Context())
	if err != nil || state.Initialized != false || !state.Ready {
		t.Fatal("duplicate changed installation state")
	}
	for i := 0; i < 9; i++ {
		if result := b.call("POST", "/setup", setupForm(token, "wrong", fmt.Sprintf("try%d", i))); result.Code != 422 {
			t.Fatalf("attempt %d: %d", i, result.Code)
		}
	}
	if result := b.call("POST", "/setup", setupForm(token, "wrong", "lasttry")); result.Code != 429 {
		t.Fatalf("setup limiter: %d", result.Code)
	}
}

func TestLocalRoleBootstrapClosesSetup(t *testing.T) {
	db, app, setup := newSetupApplication(t)
	secret, err := setup.IssueSecret(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	var person, user int32
	if err := db.QueryRow(t.Context(), `INSERT INTO persons(first_name,last_name) VALUES ('Local','Admin') RETURNING id`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(t.Context(), `INSERT INTO users(person_id,username) VALUES ($1,'localadmin') RETURNING id`, person).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := setup.WithLocalRoleGrant(t.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='president'`, user)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), `DELETE FROM user_roles WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	state, err := setup.Status(t.Context())
	if err != nil || !state.Initialized || state.Ready {
		t.Fatal("local bootstrap did not permanently close setup")
	}
	if _, err := setup.IssueSecret(t.Context(), true); !errors.Is(err, initialsetup.ErrInitialized) {
		t.Fatal("closed setup regenerated secret", err)
	}
	b := newBrowser(app.Handler)
	if result := b.call("GET", "/setup", nil); result.Code != 303 {
		t.Fatal("setup reopened after local bootstrap")
	}
	if strings.Contains(b.call("GET", "/login", nil).Body.String(), secret) {
		t.Fatal("secret leaked")
	}
}
