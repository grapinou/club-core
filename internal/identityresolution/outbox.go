package identityresolution

import (
	"context"
	"crypto/sha256"

	"errors"

	"database/sql"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

const RecipientHourlyLimit = 3

// Enqueue uses the creation transaction. BEGIN IMMEDIATE serializes recipient
// quota decisions across submissions, including different family members.
func (s *EmailService) Enqueue(ctx context.Context, tx *sql.Tx, sub dbsqlc.RegistrationSubmission) error {
	_, _, err := s.eligible(ctx, tx, sub)
	if err != nil && !errors.Is(err, ErrNotEligible) && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	eligible := err == nil
	if eligible {
		hash := sha256.Sum256([]byte(normalizedEmail(sub.Email)))
		var count int
		// Count all recent intents, even failed ones, and older outstanding intents.
		// This is deliberately conservative and prevents backlog bypasses.
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM registration_verification_outbox WHERE recipient_hash=?1 AND (created_at>strftime('%Y-%m-%d %H:%M:%f','now','-1 hour') OR sent_at>strftime('%Y-%m-%d %H:%M:%f','now','-1 hour') OR status IN ('pending','processing'))`, hash[:]).Scan(&count)
		if err != nil {
			return err
		}
		eligible = count < RecipientHourlyLimit
		if eligible {
			_, err = tx.ExecContext(ctx, `INSERT INTO registration_verification_outbox(submission_id,recipient_hash) VALUES (?1,?2)`, sub.ID, hash[:])
			if err != nil {
				return err
			}
		}
	}
	status := "awaiting_identity_review"
	if eligible {
		status = "awaiting_email_verification"
	}
	_, err = tx.ExecContext(ctx, `UPDATE registration_submissions SET status=?2,updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=?1`, sub.ID, status)
	return err
}
