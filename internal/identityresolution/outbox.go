package identityresolution

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
)

const RecipientHourlyLimit = 3

// DeliveryLockNamespace is shared by admin/verification transactions and the
// worker's session lock. Never hold a SQL transaction during SMTP.
const DeliveryLockNamespace = 7210

func LockDeliveryDecision(ctx context.Context, tx pgx.Tx, id int32) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, int32(DeliveryLockNamespace), id)
	return err
}

// Enqueue uses the creation transaction. The recipient lock serializes quota
// decisions across different submissions, including different family members.
func (s *EmailService) Enqueue(ctx context.Context, tx pgx.Tx, sub dbsqlc.RegistrationSubmission) error {
	_, _, err := s.eligible(ctx, tx, sub)
	if err != nil && !errors.Is(err, ErrNotEligible) && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	eligible := err == nil
	if eligible {
		hash := sha256.Sum256([]byte(normalizedEmail(sub.Email)))
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7211,$1)`, int32(binary.BigEndian.Uint32(hash[:4]))); err != nil {
			return err
		}
		var count int
		// Count all recent intents, even failed ones, and older outstanding intents.
		// This is deliberately conservative and prevents backlog bypasses.
		err = tx.QueryRow(ctx, `SELECT count(*) FROM registration_verification_outbox WHERE recipient_hash=$1 AND (created_at>clock_timestamp()-interval '1 hour' OR sent_at>clock_timestamp()-interval '1 hour' OR status IN ('pending','processing'))`, hash[:]).Scan(&count)
		if err != nil {
			return err
		}
		eligible = count < RecipientHourlyLimit
		if eligible {
			_, err = tx.Exec(ctx, `INSERT INTO registration_verification_outbox(submission_id,recipient_hash) VALUES ($1,$2)`, sub.ID, hash[:])
			if err != nil {
				return err
			}
		}
	}
	status := "awaiting_identity_review"
	if eligible {
		status = "awaiting_email_verification"
	}
	_, err = tx.Exec(ctx, `UPDATE registration_submissions SET status=$2,updated_at=clock_timestamp() WHERE id=$1`, sub.ID, status)
	return err
}
