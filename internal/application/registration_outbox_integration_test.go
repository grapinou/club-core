package application

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/outbox"
	"github.com/pressly/goose/v3"
)

type controlledMailer struct {
	entered chan mailer.Message
	release chan struct{}
}

func (m *controlledMailer) Send(ctx context.Context, message mailer.Message) error {
	select {
	case m.entered <- message:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-m.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *fixture) jobState(id int32) (string, int32) {
	f.t.Helper()
	var state string
	var attempts int32
	f.must(f.db.QueryRow(f.t.Context(), `SELECT status,attempt_count FROM registration_verification_outbox WHERE submission_id=$1`, id).Scan(&state, &attempts))
	return state, attempts
}
func (f *fixture) drive() {
	f.t.Helper()
	ok, err := f.app.VerificationOutbox.ProcessOne(f.t.Context())
	f.must(err)
	if !ok {
		f.t.Fatal("expected job")
	}
}
func (f *fixture) retryNow(id int32) {
	f.t.Helper()
	f.exec(`UPDATE registration_verification_outbox SET available_at=clock_timestamp()-interval '1 second' WHERE submission_id=$1 AND status='pending'`, id)
}
func waitResult(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("operation blocked")
	}
}

func TestOutboxSubmissionNeverWaitsForSMTP(t *testing.T) {
	f := newFixture(t)
	sender := &controlledMailer{make(chan mailer.Message, 1), make(chan struct{})}
	defer close(sender.release)
	worker := outbox.New(f.db, f.app.Verifications, sender, "club@example.test", "https://club.example.test")
	// Install the blocking sender in the complete application wiring as well.
	var err error
	f.app, err = NewWithMailer(config.Config{SiteName: "Club Core"}, config.Runtime{RegistrationVerificationTTL: time.Hour, ActivationValidity: time.Hour, Location: time.UTC, SMTP: mailer.SMTPConfig{From: "club@example.test"}, BaseURL: "https://club.example.test"}, f.db, sender)
	f.must(err)
	// Submit with no worker running. A regression to inline SMTP would block.
	result := make(chan error, 1)
	go func() {
		a, err := f.app.Submissions.CreateSubmission(t.Context(), emailInput())
		if err == nil && a.Status != "submission accepted" {
			err = errors.New("public disclosure")
		}
		result <- err
	}()
	waitResult(t, result)
	select {
	case <-sender.entered:
		t.Fatal("submission performed SMTP")
	default:
	}
	id := f.id(`SELECT max(id) FROM registration_submissions`)
	state, attempt := f.jobState(id)
	if state != "pending" || attempt != 0 || f.emailState(id) != "awaiting_email_verification" {
		t.Fatal("enqueue state")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_email_verifications`); n != 0 {
		t.Fatal("challenge prepared in request")
	}
	count, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
	f.must(err)
	if count != 0 {
		t.Fatal("queued job expired by admin read")
	}
	done := make(chan error, 1)
	go func() { _, err := worker.ProcessOne(t.Context()); done <- err }()
	var message mailer.Message
	select {
	case message = <-sender.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not send")
	}
	// Sender is now definitely blocked. A second eligible submission still returns.
	go func() { _, err := f.app.Submissions.CreateSubmission(t.Context(), emailInput()); result <- err }()
	waitResult(t, result)
	ref, _ := verificationFrom(t, message)
	var usable bool
	f.must(f.db.QueryRow(t.Context(), `SELECT invalidated_at IS NULL FROM registration_email_verifications WHERE public_reference=$1`, ref).Scan(&usable))
	if !usable {
		t.Fatal("SMTP before challenge commit")
	}
	var transactions int
	f.must(f.db.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`).Scan(&transactions))
	if transactions != 0 {
		t.Fatal("SQL transaction held during SMTP")
	}
	sender.release <- struct{}{}
	waitResult(t, done)
	state, attempt = f.jobState(id)
	if state != "sent" || attempt != 1 {
		t.Fatal("delivery not finalized")
	}
}

func TestOutboxEnqueueAtomicityAndPrivacy(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	if _, err := f.db.Exec(t.Context(), `INSERT INTO registration_verification_outbox(submission_id,recipient_hash) SELECT submission_id,recipient_hash FROM registration_verification_outbox WHERE submission_id=$1`, id); err == nil {
		t.Fatal("duplicate active job")
	}
	noMatch := emailInput()
	noMatch.FirstName = "Unknown"
	other := f.submit(noMatch)
	if n := f.id(`SELECT count(*)::integer FROM registration_verification_outbox WHERE submission_id=$1`, other); n != 0 {
		t.Fatal("ineligible enqueued")
	}
	if f.emailState(other) != "awaiting_identity_review" {
		t.Fatal("ineligible not reviewed")
	}
	before := f.registrationSnapshot()
	f.exec(`CREATE FUNCTION fail_outbox_insert_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'sensitive insertion failure'; END $$`)
	f.exec(`CREATE TRIGGER fail_outbox_insert_test BEFORE INSERT ON registration_verification_outbox FOR EACH ROW EXECUTE FUNCTION fail_outbox_insert_test()`)
	_, err := f.app.Submissions.CreateSubmission(t.Context(), emailInput())
	if !errors.Is(err, identityresolution.ErrUnavailable) || before != f.registrationSnapshot() {
		t.Fatal("partial creation")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_submission_candidates WHERE submission_id NOT IN (SELECT id FROM registration_submissions)`); n != 0 {
		t.Fatal("orphan candidates")
	}
	f.exec(`DROP TRIGGER fail_outbox_insert_test ON registration_verification_outbox`)
	f.drive()
	ref, code := verificationFrom(t, f.mail.messages[0])
	var persisted string
	f.must(f.db.QueryRow(t.Context(), `SELECT to_jsonb(o)::text FROM registration_verification_outbox o WHERE submission_id=$1`, id).Scan(&persisted))
	for _, secret := range []string{ref, code, "remi@example.test", "Rémi", "Plaintext", "code_hash", "public_reference", "Subject", "Text"} {
		if strings.Contains(persisted, secret) {
			t.Fatal("outbox contains delivery material")
		}
	}
	// The table schema itself has no payload or address field.
	var columns string
	f.must(f.db.QueryRow(t.Context(), `SELECT string_agg(column_name,',') FROM information_schema.columns WHERE table_name='registration_verification_outbox'`).Scan(&columns))
	for _, field := range []string{"plaintext", "body", "email", "code_hash"} {
		if strings.Contains(columns, field) {
			t.Fatal("unsafe schema")
		}
	}
	f.must(f.app.Verifications.VerifyEmail(t.Context(), ref, code))
	if state, _ := f.jobState(id); state != "sent" {
		t.Fatal("verification rewrote sent history")
	}
	counts, err := dbsqlc.New(f.db).CountRegistrationVerificationJobs(t.Context())
	f.must(err)
	if len(counts) != 1 || counts[0].Status != "sent" || counts[0].JobCount != 1 {
		t.Fatal("diagnostics")
	}
}

func TestOutboxRetriesExhaustionAndLogs(t *testing.T) {
	f := newFixture(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	f.mail.err = errors.New("private SMTP remi@example.test secret")
	id := f.submit(emailInput())
	var oldCodes, oldReferences []string
	for attempt := int32(1); attempt <= outbox.MaxAttempts; attempt++ {
		f.drive()
		ref, code := verificationFrom(t, f.mail.messages[len(f.mail.messages)-1])
		oldCodes = append(oldCodes, code)
		oldReferences = append(oldReferences, ref)
		var valid bool
		f.must(f.db.QueryRow(t.Context(), `SELECT invalidated_at IS NULL FROM registration_email_verifications WHERE public_reference=$1`, ref).Scan(&valid))
		if valid {
			t.Fatal("failed attempt remains usable")
		}
		state, n := f.jobState(id)
		if n != attempt {
			t.Fatal("attempt count")
		}
		if attempt < outbox.MaxAttempts {
			if state != "pending" || f.emailState(id) != "awaiting_email_verification" {
				t.Fatal("retry transition")
			}
			var delay float64
			f.must(f.db.QueryRow(t.Context(), `SELECT extract(epoch FROM available_at-updated_at)::double precision FROM registration_verification_outbox WHERE submission_id=$1`, id).Scan(&delay))
			if delay < outbox.RetryDelay(attempt).Seconds()-1 || delay > outbox.RetryDelay(attempt).Seconds()+1 {
				t.Fatal("wrong backoff")
			}
			ok, err := f.app.VerificationOutbox.ProcessOne(t.Context())
			f.must(err)
			if ok {
				t.Fatal("retry too early")
			}
			count, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
			f.must(err)
			if count != 0 {
				t.Fatal("retry expired by read")
			}
			f.retryNow(id)
		} else if state != "dead" || f.emailState(id) != "awaiting_identity_review" {
			t.Fatal("exhausted dossier stranded")
		}
	}
	count, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
	f.must(err)
	if count != 1 {
		t.Fatal("review badge")
	}
	for i, code := range oldCodes {
		for j, other := range oldCodes {
			if i != j && code == other {
				t.Fatal("reused code")
			}
		}
		if err := f.app.Verifications.VerifyEmail(t.Context(), oldReferences[i], code); !errors.Is(err, identityresolution.ErrVerification) {
			t.Fatal("failed code accepted")
		}
		if strings.Contains(logs.String(), code) || strings.Contains(logs.String(), oldReferences[i]) {
			t.Fatal("secret logged")
		}
	}
	for _, secret := range []string{"private SMTP", "remi@example.test", "Rémi", "recipient_hash", "code_hash"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("PII log")
		}
	}
	if !strings.Contains(logs.String(), "smtp_failed") || !strings.Contains(logs.String(), "attempts_exhausted") {
		t.Fatal("missing safe diagnostics")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_email_verifications WHERE submission_id=$1`, id); n != 3 {
		t.Fatal("history lost")
	}
	if ok, err := f.app.VerificationOutbox.ProcessOne(t.Context()); err != nil || ok {
		t.Fatal("dead job retried")
	}
}

func TestOutboxRetrySuccessAndDisabled(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	f.mail.err = errors.New("failure")
	f.drive()
	oldRef, oldCode := verificationFrom(t, f.mail.messages[0])
	f.mail.err = nil
	f.retryNow(id)
	f.drive()
	ref, code := verificationFrom(t, f.mail.messages[1])
	if ref == oldRef || code == oldCode {
		t.Fatal("retry reused proof")
	}
	if err := f.app.Verifications.VerifyEmail(t.Context(), oldRef, oldCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("old proof usable")
	}
	f.must(f.app.Verifications.VerifyEmail(t.Context(), ref, code))
	if state, n := f.jobState(id); state != "sent" || n != 2 {
		t.Fatal("retry not delivered")
	}
	disabled := f.submit(emailInput())
	worker := outbox.New(f.db, f.app.Verifications, mailer.Disabled{}, "club@example.test", "https://club.example.test")
	_, err := worker.ProcessOne(t.Context())
	f.must(err)
	if state, n := f.jobState(disabled); state != "dead" || n != 0 || f.emailState(disabled) != "awaiting_identity_review" {
		t.Fatal("disabled retries")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_email_verifications WHERE submission_id=$1`, disabled); n != 0 {
		t.Fatal("disabled prepared challenge")
	}
	var reason string
	f.must(f.db.QueryRow(t.Context(), `SELECT last_error_code FROM registration_verification_outbox WHERE submission_id=$1`, disabled).Scan(&reason))
	if reason != "mailer_disabled" {
		t.Fatal("disabled category")
	}
}

func TestOutboxRevalidatesEveryAttempt(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	f.exec(`UPDATE persons SET archived_at=clock_timestamp() WHERE id=$1`, f.person)
	f.drive()
	if state, _ := f.jobState(id); state != "dead" || f.emailState(id) != "awaiting_identity_review" || len(f.mail.messages) != 0 {
		t.Fatal("stale eligibility sent")
	}
	f.exec(`UPDATE persons SET archived_at=NULL WHERE id=$1`, f.person)
	id = f.submit(emailInput())
	f.mail.err = errors.New("failure")
	f.drive()
	f.retryNow(id)
	f.id(`INSERT INTO persons(first_name,last_name) VALUES ('Rémi','Dupont') RETURNING id`)
	f.drive()
	if state, _ := f.jobState(id); state != "dead" || len(f.mail.messages) != 1 {
		t.Fatal("retry ignored new ambiguity")
	}
}

func TestOutboxClaimLeaseAndRecovery(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	job, err := dbsqlc.New(f.db).ClaimRegistrationVerificationJob(t.Context(), outbox.LeaseDuration.Seconds())
	f.must(err)
	if job.SubmissionID != id || job.Status != "processing" || job.LeaseVersion != 1 {
		t.Fatal("claim")
	}
	if job.LeaseUntil.Time.Sub(job.UpdatedAt.Time) < outbox.LeaseDuration-time.Second {
		t.Fatal("lease duration")
	}
	if ok, err := f.app.VerificationOutbox.ProcessOne(t.Context()); err != nil || ok {
		t.Fatal("live lease reclaimed")
	}
	// Simulate a process dying after committing preparation, either before SMTP,
	// or just after DATA was accepted: the database state is identical.
	delivery, err := f.app.Verifications.PrepareEmailVerification(t.Context(), id)
	f.must(err)
	f.exec(`UPDATE registration_verification_outbox SET attempt_count=1,lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID)
	f.drive()
	ref, code := verificationFrom(t, f.mail.messages[0])
	if ref == delivery.PublicReference || code == delivery.PlaintextCode {
		t.Fatal("reclaim reused code")
	}
	if err = f.app.Verifications.VerifyEmail(t.Context(), delivery.PublicReference, delivery.PlaintextCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("crashed challenge usable")
	}
	if state, n := f.jobState(id); state != "sent" || n != 2 {
		t.Fatal("reclaim finalization")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_email_verifications WHERE submission_id=$1`, id); n != 2 {
		t.Fatal("crash history")
	}
	f.must(f.app.Verifications.VerifyEmail(t.Context(), ref, code))
	// A crash on the last attempt exhausts the budget even without an SMTP error.
	id = f.submit(emailInput())
	job, err = dbsqlc.New(f.db).ClaimRegistrationVerificationJob(t.Context(), 60)
	f.must(err)
	delivery, err = f.app.Verifications.PrepareEmailVerification(t.Context(), id)
	f.must(err)
	f.exec(`UPDATE registration_verification_outbox SET attempt_count=3,lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID)
	f.drive()
	if state, _ := f.jobState(id); state != "dead" || len(f.mail.messages) != 1 {
		t.Fatal("crash bypassed max attempts")
	}
	if err = f.app.Verifications.VerifyEmail(t.Context(), delivery.PublicReference, delivery.PlaintextCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("exhausted crash proof valid")
	}
}

func TestOutboxSkipLockedAndTwoWorkers(t *testing.T) {
	f := newFixture(t)
	first := f.submit(emailInput())
	second := f.submit(emailInput())
	tx, err := f.db.Begin(t.Context())
	f.must(err)
	defer tx.Rollback(t.Context())
	_, err = tx.Exec(t.Context(), `SELECT id FROM registration_verification_outbox WHERE submission_id=$1 FOR UPDATE`, first)
	f.must(err)
	job, err := dbsqlc.New(f.db).ClaimRegistrationVerificationJob(t.Context(), 60)
	f.must(err)
	if job.SubmissionID != second {
		t.Fatal("did not skip locked")
	}
	f.must(tx.Rollback(t.Context()))
	f.exec(`UPDATE registration_verification_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID)
	sender := &controlledMailer{make(chan mailer.Message, 2), make(chan struct{})}
	worker := outbox.New(f.db, f.app.Verifications, sender, "club@example.test", "https://club.example.test")
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := worker.ProcessOne(t.Context()); done <- err }()
	}
	refs := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case message := <-sender.entered:
			ref, _ := verificationFrom(t, message)
			refs[ref] = true
		case <-time.After(5 * time.Second):
			close(sender.release)
			t.Fatal("workers did not progress independently")
		}
	}
	close(sender.release)
	waitResult(t, done)
	waitResult(t, done)
	if len(refs) != 2 {
		t.Fatal("duplicate delivery")
	}
	for _, id := range []int32{first, second} {
		if state, n := f.jobState(id); state != "sent" || n != 1 {
			t.Fatal("double processing")
		}
	}
}

func TestOutboxAdminWinsBeforeSend(t *testing.T) {
	f := newFixture(t)
	for _, claim := range []bool{false, true} {
		id := f.submit(emailInput())
		if claim {
			_, err := dbsqlc.New(f.db).ClaimRegistrationVerificationJob(t.Context(), 60)
			f.must(err)
		}
		f.must(f.app.Reviews.LinkPerson(t.Context(), f.approver, id, f.person))
		if state, _ := f.jobState(id); state != "cancelled" {
			t.Fatal("admin did not cancel intent")
		}
		if ok, err := f.app.VerificationOutbox.ProcessOne(t.Context()); err != nil || ok {
			t.Fatal("resolved job dispatched")
		}
	}
	if len(f.mail.messages) != 0 {
		t.Fatal("admin lost race")
	}
}

func TestOutboxAdminAndVerifyWaitForStartedSend(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	sender := &controlledMailer{make(chan mailer.Message, 1), make(chan struct{})}
	worker := outbox.New(f.db, f.app.Verifications, sender, "club@example.test", "https://club.example.test")
	workerDone := make(chan error, 1)
	go func() { _, err := worker.ProcessOne(t.Context()); workerDone <- err }()
	var message mailer.Message
	select {
	case message = <-sender.entered:
	case <-time.After(5 * time.Second):
		close(sender.release)
		t.Fatal("no SMTP")
	}
	ref, code := verificationFrom(t, message)
	// Both decision services must acquire the same advisory lock as the sender.
	adminDone := make(chan error, 1)
	verifyDone := make(chan error, 1)
	go func() { adminDone <- f.app.Reviews.LinkPerson(t.Context(), f.approver, id, f.person) }()
	go func() { verifyDone <- f.app.Verifications.VerifyEmail(t.Context(), ref, code) }()
	// Inspect PostgreSQL lock waiters rather than sleeping to guess scheduling.
	deadline, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		var waiting int
		err := f.db.QueryRow(deadline, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted AND classid=$1 AND objid=$2`, identityresolution.DeliveryLockNamespace, id).Scan(&waiting)
		if err != nil {
			close(sender.release)
			t.Fatal(err)
		}
		if waiting == 2 {
			break
		}
	}
	close(sender.release)
	waitResult(t, workerDone)
	a, b := <-adminDone, <-verifyDone
	if (a == nil) == (b == nil) {
		t.Fatal("expected one resolution winner")
	}
	if a != nil && !errors.Is(a, identityresolution.ErrClosed) {
		t.Fatal(a)
	}
	if b != nil && !errors.Is(b, identityresolution.ErrVerification) {
		t.Fatal(b)
	}
	if f.emailState(id) != "resolved" {
		t.Fatal("resolution missing")
	}
	if state, _ := f.jobState(id); state != "sent" {
		t.Fatal("accepted DATA history lost")
	}
}

func TestOutboxRecipientQuotaConcurrentAndFamily(t *testing.T) {
	f := newFixture(t)
	// Distinct names avoid matching ambiguity; one parent address may verify siblings.
	inputs := make([]identityresolution.SubmissionInput, 6)
	for i := range inputs {
		name := fmt.Sprintf("Child%d", i)
		f.id(`INSERT INTO persons(first_name,last_name,birth_date,email) VALUES ($1,'Family','1990-01-01','remi@example.test') RETURNING id`, name)
		inputs[i] = emailInput()
		inputs[i].FirstName = name
		inputs[i].LastName = "Family"
	}
	var wg sync.WaitGroup
	results := make(chan error, len(inputs))
	for _, in := range inputs {
		wg.Add(1)
		go func(in identityresolution.SubmissionInput) {
			defer wg.Done()
			a, err := f.app.Submissions.CreateSubmission(t.Context(), in)
			data, _ := json.Marshal(a)
			if err == nil && string(data) != `{"status":"submission accepted"}` {
				err = errors.New("quota disclosure")
			}
			results <- err
		}(in)
	}
	wg.Wait()
	close(results)
	for err := range results {
		f.must(err)
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_verification_outbox`); n != 3 {
		t.Fatal("recipient quota not serialized")
	}
	if n := f.id(`SELECT count(DISTINCT c.person_id)::integer FROM registration_verification_outbox o JOIN registration_submission_candidates c ON c.submission_id=o.submission_id`); n != 3 {
		t.Fatal("shared email treated as identity")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_submissions WHERE status='awaiting_identity_review'`); n != 3 {
		t.Fatal("quota rejected submission")
	}
	for i := 0; i < 3; i++ {
		f.drive()
	}
	f.submit(emailInput())
	if n := f.id(`SELECT count(*)::integer FROM registration_verification_outbox`); n != 3 {
		t.Fatal("sent jobs not counted")
	}
}

func TestOutboxRecipientWindowAndBacklog(t *testing.T) {
	for _, status := range []string{"sent", "pending"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			// Backdate at insertion; production history remains immutable thereafter.
			for i := 0; i < 3; i++ {
				id := f.stage(emailInput())
				if status == "sent" {
					f.exec(`INSERT INTO registration_verification_outbox(submission_id,recipient_hash,status,created_at,sent_at,finished_at) VALUES ($1,sha256('remi@example.test'::bytea),'sent',clock_timestamp()-interval '2 hours',clock_timestamp()-interval '2 hours',clock_timestamp()-interval '2 hours')`, id)
				} else {
					f.exec(`INSERT INTO registration_verification_outbox(submission_id,recipient_hash,created_at) VALUES ($1,sha256('remi@example.test'::bytea),clock_timestamp()-interval '2 hours')`, id)
				}
			}
			id := f.submit(emailInput())
			want := "awaiting_email_verification"
			if status == "pending" {
				want = "awaiting_identity_review"
			}
			if f.emailState(id) != want {
				t.Fatal("recipient window/backlog policy")
			}
		})
	}
}

func TestOutboxMigrationDown(t *testing.T) {
	f := newFixture(t)
	db, err := sql.Open("pgx", f.db.Config().ConnString())
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.DownTo(t.Context(), 19)
	f.must(err)
	_, err = provider.Up(t.Context())
	f.must(err)
	id := f.submit(emailInput())
	// Refuse for pending as well as processing and terminal history.
	for _, state := range []string{"pending", "processing", "sent"} {
		if state == "processing" {
			_, err = dbsqlc.New(f.db).ClaimRegistrationVerificationJob(t.Context(), 60)
			f.must(err)
		}
		if state == "sent" {
			f.exec(`UPDATE registration_verification_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE submission_id=$1`, id)
			f.drive()
		}
		if _, err = provider.DownTo(t.Context(), 19); err == nil {
			t.Fatal("rollback discarded " + state)
		}
		if actual, _ := f.jobState(id); actual != state {
			t.Fatal("rollback changed history")
		}
	}
	if _, err = f.db.Exec(t.Context(), `DELETE FROM registration_verification_outbox WHERE submission_id=$1`, id); err == nil {
		t.Fatal("history deleted")
	}
}

func TestOutboxLateWorkerIsFenced(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	sender := &controlledMailer{make(chan mailer.Message, 1), make(chan struct{})}
	worker := outbox.New(f.db, f.app.Verifications, sender, "club@example.test", "https://club.example.test")
	done := make(chan error, 1)
	go func() { _, err := worker.ProcessOne(t.Context()); done <- err }()
	var original mailer.Message
	select {
	case original = <-sender.entered:
	case <-time.After(5 * time.Second):
		close(sender.release)
		t.Fatal("no SMTP")
	}
	f.exec(`UPDATE registration_verification_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE submission_id=$1`, id)
	// A new lease cannot create a second concurrent delivery while the old
	// process still owns its session lock, even if its Send ignores the deadline.
	f.drive()
	if len(f.mail.messages) != 0 {
		close(sender.release)
		t.Fatal("concurrent SMTP after reclaim")
	}
	close(sender.release)
	waitResult(t, done)
	if state, _ := f.jobState(id); state != "processing" {
		t.Fatal("late owner finalized newer lease")
	}
	f.exec(`UPDATE registration_verification_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE submission_id=$1`, id)
	f.drive()
	oldRef, oldCode := verificationFrom(t, original)
	if err := f.app.Verifications.VerifyEmail(t.Context(), oldRef, oldCode); !errors.Is(err, identityresolution.ErrVerification) {
		t.Fatal("old accepted email still valid")
	}
	ref, code := verificationFrom(t, f.mail.messages[0])
	f.must(f.app.Verifications.VerifyEmail(t.Context(), ref, code))
}

func TestOutboxRunShutdownDuringSMTP(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	sender := &controlledMailer{make(chan mailer.Message, 1), make(chan struct{})}
	defer close(sender.release)
	worker := outbox.New(f.db, f.app.Verifications, sender, "club@example.test", "https://club.example.test")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-sender.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no SMTP")
	}
	cancel()
	waitResult(t, done)
	if state, n := f.jobState(id); state != "pending" || n != 1 {
		t.Fatal("shutdown did not compensate current attempt")
	}
	// No session advisory lock may leak into the connection pool.
	tx, err := f.db.Begin(t.Context())
	f.must(err)
	defer tx.Rollback(t.Context())
	var locked bool
	f.must(tx.QueryRow(t.Context(), `SELECT pg_try_advisory_xact_lock($1,$2)`, identityresolution.DeliveryLockNamespace, id).Scan(&locked))
	if !locked {
		t.Fatal("session lock leaked")
	}
}

func TestOutboxPreparationRollbackAndSentExpiry(t *testing.T) {
	f := newFixture(t)
	id := f.submit(emailInput())
	f.exec(`CREATE FUNCTION fail_outbox_preparation_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='awaiting_email_verification' THEN RAISE EXCEPTION 'preparation unavailable'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER fail_outbox_preparation_test BEFORE UPDATE ON registration_submissions FOR EACH ROW EXECUTE FUNCTION fail_outbox_preparation_test()`)
	if _, err := f.app.VerificationOutbox.ProcessOne(t.Context()); err == nil {
		t.Fatal("preparation error missing")
	}
	if n := f.id(`SELECT count(*)::integer FROM registration_email_verifications WHERE submission_id=$1`, id); n != 0 || len(f.mail.messages) != 0 {
		t.Fatal("partial preparation")
	}
	if state, n := f.jobState(id); state != "processing" || n != 0 {
		t.Fatal("failed transaction counted as prepared attempt")
	}
	f.exec(`DROP TRIGGER fail_outbox_preparation_test ON registration_submissions`)
	f.exec(`UPDATE registration_verification_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE submission_id=$1`, id)
	service, err := identityresolution.NewEmailService(f.db, time.Second)
	f.must(err)
	worker := outbox.New(f.db, service, f.mail, "club@example.test", "https://club.example.test")
	_, err = worker.ProcessOne(t.Context())
	f.must(err)
	ref, _ := verificationFrom(t, f.mail.messages[0])
	// Wait exactly until the persisted expiration, not an arbitrary scheduling sleep.
	f.exec(`SELECT pg_sleep(GREATEST(0,extract(epoch FROM (expires_at-clock_timestamp())))+0.01) FROM registration_email_verifications WHERE public_reference=$1`, ref)
	count, err := f.app.Reviews.CountOpen(t.Context(), f.approver)
	f.must(err)
	if count != 1 || f.emailState(id) != "awaiting_identity_review" {
		t.Fatal("sent challenge did not expire into review")
	}
	if state, _ := f.jobState(id); state != "sent" {
		t.Fatal("expiry rewrote delivery history")
	}
	if ok, err := worker.ProcessOne(t.Context()); err != nil || ok {
		t.Fatal("expiration resent email")
	}
}
