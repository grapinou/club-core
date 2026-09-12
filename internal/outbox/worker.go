// Package outbox dispatches registration verification intentions from PostgreSQL.
// Plaintext challenges exist only in the processing call's memory.
package outbox

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MaxAttempts   = 3
	LeaseDuration = 60 * time.Second
	SendTimeout   = 30 * time.Second
	PollInterval  = 2 * time.Second
)

func RetryDelay(attempt int32) time.Duration {
	if attempt <= 1 {
		return time.Minute
	}
	return 5 * time.Minute
}

type Worker struct {
	db            *pgxpool.Pool
	email         *identityresolution.EmailService
	sender        mailer.Mailer
	from, baseURL string
}

func New(db *pgxpool.Pool, email *identityresolution.EmailService, sender mailer.Mailer, from, baseURL string) *Worker {
	return &Worker{db: db, email: email, sender: sender, from: from, baseURL: baseURL}
}
func (w *Worker) Run(ctx context.Context) error {
	return run(ctx, w.ProcessOne, waitPoll)
}

// Kept separate so idle/error polling can be tested without wall-clock sleeps.
func run(ctx context.Context, process func(context.Context) (bool, error), wait func(context.Context) bool) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		processed, err := process(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			slog.Error("outbox processing failed", "error_code", "database_unavailable")
		}
		if processed && err == nil {
			continue
		}
		if !wait(ctx) {
			return nil
		}
	}
}
func waitPoll(ctx context.Context) bool {
	timer := time.NewTimer(PollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// ProcessOne claims with SKIP LOCKED in a short autocommit statement. The lease
// generation fences late completions. A session advisory lock also excludes a
// still-running former owner and serializes administrative/verification decisions.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	job, err := dbsqlc.New(w.db).ClaimRegistrationVerificationJob(ctx, LeaseDuration.Seconds())
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	conn, err := w.db.Acquire(ctx)
	if err != nil {
		return true, err
	}
	defer conn.Release()
	var locked bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1,$2)`, int32(identityresolution.DeliveryLockNamespace), job.SubmissionID).Scan(&locked)
	if err != nil {
		// A cancelled/lost response can leave acquisition ambiguous. Destroy
		// the session rather than returning a possibly locked connection.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = conn.Conn().Close(cleanup)
		cancel()
		return true, err
	}
	if !locked {
		return true, nil
	} // Reclaimable again after this lease; no concurrent SMTP.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(cleanup, `SELECT pg_advisory_unlock($1,$2)`, int32(identityresolution.DeliveryLockNamespace), job.SubmissionID); e != nil {
			// Never return a connection with a session lock to the pool.
			_ = conn.Conn().Close(cleanup)
		}
	}()
	delivery, attempt, err := w.prepare(ctx, conn, job)
	if err != nil || delivery == nil {
		return true, err
	}
	// Re-read immediately before SMTP. The session lock prevents an admin/verify
	// decision from slipping between this check and Send. No SQL transaction here.
	var live bool
	err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM registration_verification_outbox o JOIN registration_submissions s ON s.id=o.submission_id JOIN registration_email_verifications v ON v.submission_id=s.id WHERE o.id=$1 AND o.status='processing' AND o.lease_version=$2 AND o.lease_until>clock_timestamp() AND s.status='awaiting_email_verification' AND v.public_reference=$3 AND v.used_at IS NULL AND v.invalidated_at IS NULL AND v.expires_at>clock_timestamp())`, job.ID, job.LeaseVersion, delivery.PublicReference).Scan(&live)
	if err != nil || !live {
		return true, err
	}
	message, sendErr := mailer.RegistrationMessage(w.from, delivery.RecipientEmail, strings.TrimRight(w.baseURL, "/")+"/registration/verify", delivery.PublicReference, delivery.PlaintextCode)
	if sendErr == nil {
		sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)
		sendErr = w.sender.Send(sendCtx, message)
		cancel()
	}
	// Finish this attempt even when shutdown cancelled its SMTP context.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err = w.finish(cleanup, conn, job, delivery, attempt, sendErr)
	return true, err
}

func (w *Worker) prepare(ctx context.Context, conn *pgxpool.Conn, job dbsqlc.RegistrationVerificationOutbox) (*identityresolution.EmailDelivery, int32, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	sub, err := dbsqlc.New(tx).LockRegistrationSubmission(ctx, job.SubmissionID)
	if err != nil {
		return nil, 0, err
	}
	var attempts int32
	err = tx.QueryRow(ctx, `SELECT attempt_count FROM registration_verification_outbox WHERE id=$1 AND status='processing' AND lease_version=$2 AND lease_until>clock_timestamp() FOR UPDATE`, job.ID, job.LeaseVersion).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	reason := ""
	if sub.Status == "resolved" || sub.Status == "cancelled" {
		reason = "submission_closed"
	}
	if reason == "" && attempts >= MaxAttempts {
		reason = "attempts_exhausted"
	}
	if reason == "" {
		switch w.sender.(type) {
		case mailer.Disabled, *mailer.Disabled:
			reason = "mailer_disabled"
		}
	}
	var delivery *identityresolution.EmailDelivery
	if reason == "" {
		delivery, err = w.email.PrepareInTransaction(ctx, tx, sub.ID)
		if errors.Is(err, identityresolution.ErrNotEligible) || errors.Is(err, pgx.ErrNoRows) {
			reason = "ineligible"
		} else if err != nil {
			return nil, 0, err
		}
	}
	if reason != "" {
		if err = terminate(ctx, tx, job, reason); err != nil {
			return nil, 0, err
		}
		return nil, attempts, tx.Commit(ctx)
	}
	attempts++
	_, err = tx.Exec(ctx, `UPDATE registration_verification_outbox SET attempt_count=$2,updated_at=clock_timestamp() WHERE id=$1`, job.ID, attempts)
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return delivery, attempts, nil
}

func terminate(ctx context.Context, tx pgx.Tx, job dbsqlc.RegistrationVerificationOutbox, reason string) error {
	status := "dead"
	if reason == "submission_closed" {
		status = "cancelled"
	} else {
		if _, err := tx.Exec(ctx, `UPDATE registration_email_verifications SET invalidated_at=clock_timestamp() WHERE submission_id=$1 AND used_at IS NULL AND invalidated_at IS NULL`, job.SubmissionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE registration_submissions SET status='awaiting_identity_review',updated_at=clock_timestamp() WHERE id=$1 AND status IN ('received','awaiting_email_verification','awaiting_identity_review')`, job.SubmissionID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE registration_verification_outbox SET status=$3,lease_until=NULL,finished_at=clock_timestamp(),updated_at=clock_timestamp(),last_error_code=$4 WHERE id=$1 AND status='processing' AND lease_version=$2`, job.ID, job.LeaseVersion, status, reason)
	return err
}
func (w *Worker) finish(ctx context.Context, conn *pgxpool.Conn, job dbsqlc.RegistrationVerificationOutbox, d *identityresolution.EmailDelivery, attempt int32, sendErr error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = dbsqlc.New(tx).LockRegistrationSubmission(ctx, job.SubmissionID); err != nil {
		return err
	}
	var owned int64
	err = tx.QueryRow(ctx, `SELECT id FROM registration_verification_outbox WHERE id=$1 AND status='processing' AND lease_version=$2 FOR UPDATE`, job.ID, job.LeaseVersion).Scan(&owned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if sendErr == nil {
		_, err = tx.Exec(ctx, `UPDATE registration_verification_outbox SET status='sent',lease_until=NULL,sent_at=clock_timestamp(),finished_at=clock_timestamp(),updated_at=clock_timestamp(),last_error_code=NULL WHERE id=$1 AND status='processing' AND lease_version=$2`, job.ID, job.LeaseVersion)
	} else {
		// Only invalidate the proof associated with this attempt.
		err = identityresolution.InvalidateDeliveryAttempt(ctx, tx, d)
		if err != nil {
			return err
		}
		reason := "smtp_failed"
		if errors.Is(sendErr, mailer.ErrDisabled) {
			reason = "mailer_disabled"
		} else if attempt >= MaxAttempts {
			reason = "attempts_exhausted"
		}
		if reason != "smtp_failed" {
			err = terminate(ctx, tx, job, reason)
		} else {
			_, err = tx.Exec(ctx, `UPDATE registration_verification_outbox SET status='pending',lease_until=NULL,available_at=clock_timestamp()+make_interval(secs=>$3),updated_at=clock_timestamp(),last_error_code='smtp_failed' WHERE id=$1 AND status='processing' AND lease_version=$2`, job.ID, job.LeaseVersion, RetryDelay(attempt).Seconds())
		}
		// Categories only: never forward the SMTP error, message, or job contents.
		if err == nil {
			slog.Warn("outbox delivery failed", "error_code", reason)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
