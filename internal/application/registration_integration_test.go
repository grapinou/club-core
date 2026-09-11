package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func registrationText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
func registrationInput() identityresolution.SubmissionInput {
	return identityresolution.SubmissionInput{FirstName: "  RÉMI ", LastName: " DUPONT ", BirthDate: pgtype.Date{Time: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}, Email: registrationText("New@example.test"), PhoneNumber: registrationText("06 12 34 56 78"), Address: registrationText("Adresse déclarée <script>alert(1)</script>")}
}
func (f *fixture) submit(in identityresolution.SubmissionInput) int32 {
	f.t.Helper()
	accepted, err := f.app.Submissions.CreateSubmission(f.t.Context(), in)
	f.must(err)
	data, err := json.Marshal(accepted)
	f.must(err)
	if string(data) != `{"status":"submission accepted"}` {
		f.t.Fatalf("public disclosure: %s", data)
	}
	return f.id("SELECT max(id) FROM registration_submissions")
}
func reviewPath(id int32) string { return fmt.Sprintf("/registration-reviews/%d", id) }
func (f *fixture) registrationSnapshot() string {
	f.t.Helper()
	var out string
	f.must(f.db.QueryRow(f.t.Context(), "SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id)::text,'[]') FROM registration_submissions s").Scan(&out))
	return out
}
func TestRegistrationSubmissionDetectionAndPrivacy(t *testing.T) {
	f := newFixture(t)
	q := dbsqlc.New(f.db)
	before := f.personSnapshot()
	noMatch := registrationInput()
	noMatch.FirstName = "Different"
	empty := f.submit(noMatch)
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, empty)
	f.must(err)
	if d.Submission.Status != "received" || len(d.Candidates) != 0 {
		t.Fatal("unexpected candidates")
	}
	one := f.submit(registrationInput())
	d, err = f.app.Reviews.GetDetails(t.Context(), f.approver, one)
	f.must(err)
	if d.Submission.Status != "awaiting_identity_review" || len(d.Candidates) != 1 || d.Candidates[0].Confidence != "possible" {
		t.Fatal("expected birth match")
	}
	if d.Submission.FirstName != registrationInput().FirstName || d.Submission.Email != registrationInput().Email || d.Submission.Address != registrationInput().Address {
		t.Fatal("declared data changed")
	}
	if f.personSnapshot() != before {
		t.Fatal("submission created or modified Person")
	}
	strong := f.id("INSERT INTO persons(first_name,last_name,birth_date,email,phone_number) VALUES ('Rémi','Dupont','1990-01-01','new@example.test','+33 6 12 34 56 78') RETURNING id")
	weak := f.id("INSERT INTO persons(first_name,last_name,archived_at) VALUES ('Rémi','Dupont',now()) RETURNING id")
	multiple := f.submit(registrationInput())
	d, err = f.app.Reviews.GetDetails(t.Context(), f.approver, multiple)
	f.must(err)
	if len(d.Candidates) != 3 || d.Candidates[0].PersonID != strong || d.Candidates[0].Confidence != "strong" || d.Candidates[1].Confidence != "possible" || d.Candidates[2].PersonID != weak || d.Candidates[2].Confidence != "weak" {
		t.Fatal("ranking/homonyms/archived")
	}
	f.exec("UPDATE persons SET email='changed@example.test',birth_date=NULL WHERE id=$1", strong)
	historical, err := q.ListRegistrationCandidates(t.Context(), multiple)
	f.must(err)
	if !historical[0].MatchedEmail || !historical[0].MatchedBirthDate || historical[0].Confidence != "strong" || historical[0].Email.String != "changed@example.test" {
		t.Fatal("snapshot changed with Person")
	}
	old, err := q.ListRegistrationCandidates(t.Context(), one)
	f.must(err)
	if len(old) != 1 {
		t.Fatal("late Person altered snapshot")
	}
	// Failure partway through candidate insertion rolls back both staging and evidence.
	f.exec(`CREATE FUNCTION reject_registration_candidate_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.confidence='weak' THEN RAISE EXCEPTION 'private failure'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER reject_registration_candidate_test BEFORE INSERT ON registration_submission_candidates FOR EACH ROW EXECUTE FUNCTION reject_registration_candidate_test()`)
	snapshot := f.registrationSnapshot()
	var count int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM registration_submission_candidates").Scan(&count))
	result, err := f.app.Submissions.CreateSubmission(t.Context(), registrationInput())
	if !errors.Is(err, identityresolution.ErrUnavailable) || result.Status != "" || strings.Contains(err.Error(), "private") {
		t.Fatal("unsafe public error", err)
	}
	var after int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM registration_submission_candidates").Scan(&after))
	if f.registrationSnapshot() != snapshot || count != after {
		t.Fatal("partial submission survived rollback")
	}
}
func TestRegistrationResolutionAndAudit(t *testing.T) {
	f := newFixture(t)
	id := f.submit(registrationInput())
	before := f.personSnapshot()
	if err := f.app.Reviews.LinkPerson(t.Context(), f.approver, id, 999999); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("non candidate accepted", err)
	}
	f.must(f.app.Reviews.LinkPerson(t.Context(), f.approver, id, f.person))
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Submission.Status != "resolved" || d.Submission.ResolutionType.String != "existing_person" || d.Submission.ResolvedPersonID.Int32 != f.person || d.Submission.ResolvedByUserID.Int32 != f.approver || !d.Submission.ResolvedAt.Valid || len(d.Candidates) != 1 {
		t.Fatal("missing audit")
	}
	if f.personSnapshot() != before || d.Submission.Email.String != "New@example.test" {
		t.Fatal("Person overwritten or declared evidence lost")
	}
	for _, action := range []func() error{func() error { return f.app.Reviews.LinkPerson(t.Context(), f.approver, id, f.person) }, func() error { return f.app.Reviews.CreatePerson(t.Context(), f.approver, id) }} {
		if err := action(); !errors.Is(err, identityresolution.ErrClosed) {
			t.Fatal("second resolution", err)
		}
	}
	for _, query := range []string{"UPDATE registration_submissions SET status='received',resolved_person_id=NULL,resolution_type=NULL,resolved_at=NULL,resolved_by_user_id=NULL WHERE id=$1", "DELETE FROM registration_submissions WHERE id=$1", "UPDATE registration_submission_candidates SET confidence='weak' WHERE submission_id=$1", "DELETE FROM registration_submission_candidates WHERE submission_id=$1"} {
		if _, err := f.db.Exec(t.Context(), query, id); err == nil {
			t.Fatal("audit mutation accepted")
		}
	}
	fresh := f.submit(registrationInput())
	f.must(f.app.Reviews.CreatePerson(t.Context(), f.approver, fresh))
	d, err = f.app.Reviews.GetDetails(t.Context(), f.approver, fresh)
	f.must(err)
	p, err := dbsqlc.New(f.db).GetPersonByID(t.Context(), d.Submission.ResolvedPersonID.Int32)
	f.must(err)
	if d.Submission.ResolutionType.String != "new_person" || p.ID == f.person || p.FirstName != "RÉMI" || p.LastName != "DUPONT" || p.Email.String != "New@example.test" || p.BirthDate != registrationInput().BirthDate {
		t.Fatal("explicit Person creation")
	}
	var users, memberships int
	f.must(f.db.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM users WHERE person_id=$1),(SELECT count(*) FROM memberships WHERE person_id=$1)", p.ID).Scan(&users, &memberships))
	if users != 0 || memberships != 0 || len(f.mail.messages) != 0 {
		t.Fatal("identity changed account/membership")
	}
}
func TestRegistrationResolutionRollbackAndConcurrency(t *testing.T) {
	f := newFixture(t)
	// Two independent administrators, one submission lock.
	p := f.id("INSERT INTO persons(first_name,last_name) VALUES ('Second','Admin') RETURNING id")
	actor := f.id("INSERT INTO users(person_id,username,password_hash,activated_at) VALUES ($1,'second','unchanged',now()) RETURNING id", p)
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", actor)
	for _, bothCreate := range []bool{true, false} {
		id := f.submit(registrationInput())
		var before int
		f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM persons").Scan(&before))
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i, who := range []int32{f.approver, actor} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if bothCreate || i == 0 {
					results <- f.app.Reviews.CreatePerson(t.Context(), who, id)
				} else {
					results <- f.app.Reviews.LinkPerson(t.Context(), who, id, f.person)
				}
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		successes, closed := 0, 0
		for err := range results {
			if err == nil {
				successes++
			} else if errors.Is(err, identityresolution.ErrClosed) {
				closed++
			} else {
				t.Fatal(err)
			}
		}
		if successes != 1 || closed != 1 {
			t.Fatal("double resolution")
		}
		d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
		f.must(err)
		var after int
		f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM persons").Scan(&after))
		delta := 0
		if d.Submission.ResolutionType.String == "new_person" {
			delta = 1
		}
		if after != before+delta {
			t.Fatal("orphan Person created")
		}
	}
	id := f.submit(registrationInput())
	snapshot := f.registrationSnapshot()
	before := f.personSnapshot()
	f.exec(`CREATE FUNCTION reject_registration_person_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private Person failure'; END $$`)
	f.exec(`CREATE TRIGGER reject_registration_person_test BEFORE INSERT ON persons FOR EACH ROW EXECUTE FUNCTION reject_registration_person_test()`)
	if err := f.app.Reviews.CreatePerson(t.Context(), f.approver, id); err == nil {
		t.Fatal("expected failure")
	}
	if snapshot != f.registrationSnapshot() || before != f.personSnapshot() {
		t.Fatal("partial resolution on Person failure")
	}
	f.exec("DROP TRIGGER reject_registration_person_test ON persons")
	// Failure after CreatePerson must roll the new Person back as well.
	f.exec(`CREATE FUNCTION reject_registration_resolution_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='resolved' THEN RAISE EXCEPTION 'private resolve failure'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER reject_registration_resolution_test BEFORE UPDATE ON registration_submissions FOR EACH ROW EXECUTE FUNCTION reject_registration_resolution_test()`)
	if err := f.app.Reviews.CreatePerson(t.Context(), f.approver, id); err == nil {
		t.Fatal("expected final write failure")
	}
	if snapshot != f.registrationSnapshot() || before != f.personSnapshot() {
		t.Fatal("orphan Person after final write failure")
	}
}
func TestRegistrationHTTPPermissionsAndCSRF(t *testing.T) {
	f := newFixture(t)
	id := f.submit(registrationInput())
	path := reviewPath(id)
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
			if home.Code != 200 || strings.Contains(home.Body.String(), `href="/registration-reviews"`) != allowed {
				t.Fatal("navigation")
			}
			for _, p := range []string{"/registration-reviews", path} {
				r := b.call("GET", p, nil)
				want := 403
				if role == "anonymous" {
					want = 303
				} else if allowed {
					want = 200
				}
				if r.Code != want || r.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("read access", r.Code, p)
				}
				if role == "anonymous" && r.Header().Get("Location") != "/login" {
					t.Fatal("login redirect")
				}
				if !allowed && (strings.Contains(r.Body.String(), "New@example.test") || strings.Contains(r.Body.String(), "remi@example.test") || strings.Contains(r.Body.String(), "RÉMI")) {
					t.Fatal("403 leaks identity")
				}
			}
			token := b.csrf(t, "/login")
			before := f.personSnapshot()
			snapshot := f.registrationSnapshot()
			for _, action := range []string{"link-person", "create-person"} {
				for _, csrf := range []string{"", "invalid", token} {
					if allowed && csrf == token {
						continue
					}
					r := b.call("POST", path+"/"+action, url.Values{"csrf_token": {csrf}, "person_id": {fmt.Sprint(f.person)}})
					want := 403
					if role == "anonymous" {
						want = 303
					}
					if r.Code != want {
						t.Fatal("mutation access", r.Code)
					}
					if before != f.personSnapshot() || snapshot != f.registrationSnapshot() {
						t.Fatal("unauthorized mutation")
					}
				}
			}
		})
	}
	b := f.membershipAdminBrowser()
	for _, p := range []string{"/registration-reviews/999999", "/registration-reviews/-1", "/registration-reviews/no"} {
		if r := b.call("GET", p, nil); r.Code != 404 {
			t.Fatal("404", r.Code)
		}
	}
	// Same session loses the permission immediately, including application entrypoints.
	f.exec("DELETE FROM user_roles WHERE user_id=$1", f.approver)
	if r := b.call("GET", path, nil); r.Code != 403 {
		t.Fatal("revocation")
	}
	for _, action := range []func() error{func() error { return f.app.Reviews.LinkPerson(t.Context(), f.approver, id, f.person) }, func() error { return f.app.Reviews.CreatePerson(t.Context(), f.approver, id) }} {
		if err := action(); !errors.Is(err, authorization.ErrForbidden) {
			t.Fatal("service lacks RBAC")
		}
	}
}
func TestRegistrationHTTPDetailAndResolution(t *testing.T) {
	f := newFixture(t)
	f.exec("UPDATE persons SET phone_number='+33 6 12 34 56 78',address='Adresse existante' WHERE id=$1", f.person)
	f.id("INSERT INTO users(person_id,username,password_hash,is_active) VALUES ($1,'unchanged','private-hash',false) RETURNING id", f.person)
	id := f.submit(registrationInput())
	no := registrationInput()
	no.FirstName = "Unique"
	other := f.submit(no)
	b := f.membershipAdminBrowser()
	list := b.call("GET", "/registration-reviews", nil)
	if !strings.Contains(list.Body.String(), "Vérifications (2)") || strings.Contains(list.Body.String(), "New@example.test") || strings.Index(list.Body.String(), reviewPath(id)) > strings.Index(list.Body.String(), reviewPath(other)) {
		t.Fatal("queue count/order/minimal PII")
	}
	page := b.call("GET", reviewPath(id), nil)
	body := html.UnescapeString(page.Body.String())
	for _, value := range []string{"RÉMI", "DUPONT", "01/01/1990", "New@example.test", "remi@example.test", "Adresse existante", "Différent", "Fort (strong)", "Téléphone normalisé", "User désactivé", "compte non activé", "Rattacher à cette personne", "Créer une nouvelle Person"} {
		if !strings.Contains(body, value) {
			t.Fatal("missing detail", value)
		}
	}
	if strings.Contains(page.Body.String(), "<script>alert(1)</script>") || strings.Contains(body, "private-hash") {
		t.Fatal("XSS or account leak")
	}
	f.assertNoDeliverySecrets(body)
	before := f.personSnapshot()
	token := b.csrf(t, reviewPath(id))
	r := b.call("POST", reviewPath(id)+"/link-person", url.Values{"csrf_token": {token}, "person_id": {fmt.Sprint(f.person)}, "actor_id": {"999999"}, "resolved_by_user_id": {"999999"}})
	if r.Code != 303 || r.Header().Get("Location") != reviewPath(id)+"?notice=resolved" {
		t.Fatal("PRG", r.Code)
	}
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Submission.ResolvedByUserID.Int32 != f.approver || f.personSnapshot() != before {
		t.Fatal("actor or Person overwritten")
	}
	page = b.call("GET", r.Header().Get("Location"), nil)
	if !strings.Contains(page.Body.String(), "Résolution enregistrée") || !strings.Contains(page.Body.String(), "admin") || !strings.Contains(page.Body.String(), "Vérifications (1)") || strings.Contains(page.Body.String(), "Rattacher à cette personne") || strings.Contains(page.Body.String(), "Créer une nouvelle Person") {
		t.Fatal("historical detail")
	}
	r = b.call("POST", reviewPath(id)+"/create-person", url.Values{"csrf_token": {token}})
	if r.Code != 303 || !strings.HasSuffix(r.Header().Get("Location"), "?notice=closed") || f.personSnapshot() != before {
		t.Fatal("double POST")
	}
	// No-candidate creation remains an explicit administrative decision.
	r = b.call("POST", reviewPath(other)+"/create-person", url.Values{"csrf_token": {token}})
	if r.Code != 303 {
		t.Fatal("create PRG", r.Code)
	}
	d, err = f.app.Reviews.GetDetails(t.Context(), f.approver, other)
	f.must(err)
	if d.Submission.ResolutionType.String != "new_person" {
		t.Fatal("new Person resolution")
	}
	var active bool
	f.must(f.db.QueryRow(t.Context(), "SELECT is_active FROM users WHERE person_id=$1", f.person).Scan(&active))
	if active || len(f.mail.messages) != 0 {
		t.Fatal("account reactivation")
	}
}
func TestRegistrationHTTPFailureIsGeneric(t *testing.T) {
	f := newFixture(t)
	id := f.submit(registrationInput())
	b := f.membershipAdminBrowser()
	token := b.csrf(t, reviewPath(id))
	f.exec(`CREATE FUNCTION reject_review_http_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private-db-detail'; END $$`)
	f.exec(`CREATE TRIGGER reject_review_http_test BEFORE INSERT ON persons FOR EACH ROW EXECUTE FUNCTION reject_review_http_test()`)
	r := b.call("POST", reviewPath(id)+"/create-person", url.Values{"csrf_token": {token}})
	if r.Code != 500 || strings.Contains(r.Body.String(), "private-db-detail") || strings.Contains(r.Body.String(), "SQLSTATE") {
		t.Fatal("unsafe internal error")
	}
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Submission.Status != "awaiting_identity_review" {
		t.Fatal("failed creation resolved submission")
	}
	// Read failures also fail closed and expose no raw PostgreSQL details.
	f.exec("ALTER TABLE registration_submission_candidates RENAME TO unavailable_candidates")
	for _, path := range []string{"/registration-reviews", reviewPath(id)} {
		r = b.call("GET", path, nil)
		if r.Code != 500 || strings.Contains(r.Body.String(), "registration_submission_candidates") {
			t.Fatal("unsafe read error")
		}
	}
}
func TestRegistrationSafeAcceptanceShape(t *testing.T) {
	// Guard the boundary against later additions of IDs, diagnostics or candidates.
	typ := reflect.TypeOf(identityresolution.Acceptance{})
	if typ.NumField() != 1 || typ.Field(0).Name != "Status" {
		t.Fatal("public response grew identifying fields")
	}
}

func TestRegistrationConcurrentHTTPCreate(t *testing.T) {
	f := newFixture(t)
	id := f.submit(registrationInput())
	first := f.membershipAdminBrowser()
	p := f.id("INSERT INTO persons(first_name,last_name) VALUES ('Other','Reviewer') RETURNING id")
	who := f.id("INSERT INTO users(person_id,username,password_hash,activated_at) SELECT $1,'other-reviewer',password_hash,now() FROM users WHERE id=$2 RETURNING id", p, f.approver)
	f.exec("INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name='secretary'", who)
	second := f.loginBrowser("other-reviewer")
	browsers := []*browser{first, second}
	tokens := []string{first.csrf(t, reviewPath(id)), second.csrf(t, reviewPath(id))}
	var before int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM persons").Scan(&before))
	type response struct {
		code     int
		location string
	}
	results := make(chan response, 2)
	start := make(chan struct{})
	for i, b := range browsers {
		go func() {
			<-start
			r := b.call("POST", reviewPath(id)+"/create-person", url.Values{"csrf_token": {tokens[i]}, "actor_id": {"999999"}})
			results <- response{r.Code, r.Header().Get("Location")}
		}()
	}
	close(start)
	notices := map[string]int{}
	for range 2 {
		r := <-results
		if r.code != 303 {
			t.Fatal("concurrent POST", r.code)
		}
		notices[r.location]++
	}
	if notices[reviewPath(id)+"?notice=resolved"] != 1 || notices[reviewPath(id)+"?notice=closed"] != 1 {
		t.Fatal("competing POST outcomes", notices)
	}
	var after int
	f.must(f.db.QueryRow(t.Context(), "SELECT count(*) FROM persons").Scan(&after))
	if after != before+1 {
		t.Fatal("double Person")
	}
	d, err := f.app.Reviews.GetDetails(t.Context(), f.approver, id)
	f.must(err)
	if d.Submission.ResolvedByUserID.Int32 != f.approver && d.Submission.ResolvedByUserID.Int32 != who {
		t.Fatal("untrusted actor")
	}
}
func TestRegistrationAdditionalInvariants(t *testing.T) {
	f := newFixture(t)
	snapshot := f.registrationSnapshot()
	if result, err := f.app.Submissions.CreateSubmission(t.Context(), identityresolution.SubmissionInput{FirstName: " ", LastName: "Name"}); !errors.Is(err, identityresolution.ErrInvalidSubmission) || result.Status != "" || snapshot != f.registrationSnapshot() {
		t.Fatal("invalid submission persisted")
	}
	id := f.submit(registrationInput())
	nonCandidate := f.id("SELECT person_id FROM users WHERE id=$1", f.approver)
	if err := f.app.Reviews.LinkPerson(t.Context(), f.approver, id, nonCandidate); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("arbitrary existing Person accepted")
	}
	// Names and submitted coordinates remain immutable even before resolution.
	if _, err := f.db.Exec(t.Context(), "UPDATE registration_submissions SET email='overwritten@example.test' WHERE id=$1", id); err == nil {
		t.Fatal("declared evidence overwritten")
	}
	if _, err := f.db.Exec(t.Context(), "UPDATE registration_submissions SET status='resolved' WHERE id=$1", id); err == nil {
		t.Fatal("incomplete audit accepted")
	}
	if _, err := f.db.Exec(t.Context(), "INSERT INTO registration_submission_candidates SELECT * FROM registration_submission_candidates WHERE submission_id=$1", id); err == nil {
		t.Fatal("duplicate candidate")
	}
	b := f.membershipAdminBrowser()
	for _, state := range []string{"absent", "unactivated", "activated", "disabled"} {
		switch state {
		case "unactivated":
			f.id("INSERT INTO users(person_id,username) VALUES ($1,'candidate-user') RETURNING id", f.person)
		case "activated":
			f.exec("UPDATE users SET activated_at=now(),password_hash='never-expose-this-hash' WHERE person_id=$1", f.person)
		case "disabled":
			f.exec("UPDATE users SET is_active=false WHERE person_id=$1", f.person)
		}
		r := b.call("GET", reviewPath(id), nil)
		body := html.UnescapeString(r.Body.String())
		want := map[string]string{"absent": "Aucun User", "unactivated": "User actif — compte non activé", "activated": "User actif — compte activé", "disabled": "User désactivé"}[state]
		if r.Code != 200 || !strings.Contains(body, want) || strings.Contains(body, "never-expose-this-hash") {
			t.Fatal("account state presentation", state)
		}
	}
	f.exec("UPDATE registration_submissions SET status='cancelled' WHERE id=$1", id)
	if err := f.app.Reviews.CreatePerson(t.Context(), f.approver, id); !errors.Is(err, identityresolution.ErrClosed) {
		t.Fatal("cancelled resolution")
	}
	if r := b.call("GET", reviewPath(id), nil); r.Code != 200 || strings.Contains(r.Body.String(), "Créer une nouvelle Person") {
		t.Fatal("cancelled UI")
	}
	// Application reads reject stale privileges independently of HTTP.
	f.exec("UPDATE users SET is_active=false WHERE id=$1", f.approver)
	if _, err := f.app.Reviews.List(t.Context(), f.approver); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatal("disabled reviewer")
	}
}
