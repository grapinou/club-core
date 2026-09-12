package identityresolution

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotEligible = errors.New("registration email verification unavailable")
var ErrVerification = errors.New("invalid or unavailable registration verification")

// EmailDelivery is transient and internal. Never log it or return it to HTTP.
type EmailDelivery struct {
	SubmissionID, PersonID                         int32
	PublicReference, PlaintextCode, RecipientEmail string
}
type EmailService struct {
	db  *pgxpool.Pool
	ttl time.Duration
}

func NewEmailService(db *pgxpool.Pool, ttl time.Duration) (*EmailService, error) {
	if ttl <= 0 {
		return nil, errors.New("registration verification TTL must be positive")
	}
	return &EmailService{db, ttl}, nil
}
func openStatus(s string) bool {
	return s == "received" || s == "awaiting_identity_review" || s == "awaiting_email_verification"
}

// All writers lock the submission before touching its proof or candidate.
// A reference lookup never holds a proof lock while waiting for submission.
func (s *EmailService) eligible(ctx context.Context, tx pgx.Tx, sub dbsqlc.RegistrationSubmission) (int32, string, error) {
	q := dbsqlc.New(tx)
	candidates, err := q.ListRegistrationCandidates(ctx, sub.ID)
	if err != nil {
		return 0, "", err
	}
	// V1 treats every additional candidate, including weak homonyms, as ambiguity.
	if len(candidates) != 1 || candidates[0].Confidence != "strong" {
		return 0, "", ErrNotEligible
	}
	var p dbsqlc.ListIdentityMatchingPersonsRow
	var archived bool
	err = tx.QueryRow(ctx, `SELECT id,first_name,last_name,birth_date,email,phone_number,archived_at IS NOT NULL FROM persons WHERE id=$1 FOR SHARE`, candidates[0].PersonID).Scan(&p.ID, &p.FirstName, &p.LastName, &p.BirthDate, &p.Email, &p.PhoneNumber, &archived)
	if err != nil {
		return 0, "", err
	}
	if archived {
		return 0, "", ErrNotEligible
	}
	in := SubmissionInput{FirstName: sub.FirstName, LastName: sub.LastName, BirthDate: sub.BirthDate, Email: sub.Email, PhoneNumber: sub.PhoneNumber, Address: sub.Address}
	evidence, ok := match(in, p) // Exactly 0018's algorithm, including normalization.
	recipient := normalizedEmail(p.Email)
	address, parseErr := mail.ParseAddress(recipient)
	if !ok || evidence.Confidence != "strong" || !evidence.MatchedEmail || recipient == "" || parseErr != nil || address.Address != recipient {
		return 0, "", ErrNotEligible
	}
	// Detect new conflicts since the snapshot without changing historical evidence.
	persons, err := q.ListIdentityMatchingPersons(ctx)
	if err != nil {
		return 0, "", err
	}
	for _, other := range persons {
		if other.ID != p.ID {
			if _, matches := match(in, other); matches {
				return 0, "", ErrNotEligible
			}
		}
	}
	return p.ID, strings.TrimSpace(p.Email.String), nil
}
func (s *EmailService) PrepareEmailVerification(ctx context.Context, id int32) (*EmailDelivery, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = LockDeliveryDecision(ctx, tx, id); err != nil {
		return nil, err
	}
	d, err := s.PrepareInTransaction(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotEligible) {
			if cleanupErr := returnToReview(ctx, tx, id); cleanupErr != nil {
				return nil, cleanupErr
			}
			if cleanupErr := tx.Commit(ctx); cleanupErr != nil {
				return nil, cleanupErr
			}
		}
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

// PrepareInTransaction is used by the specialized outbox under its delivery lock.
// The caller commits before using the transient plaintext delivery.
func (s *EmailService) PrepareInTransaction(ctx context.Context, tx pgx.Tx, id int32) (*EmailDelivery, error) {
	sub, err := dbsqlc.New(tx).LockRegistrationSubmission(ctx, id)
	if err != nil {
		return nil, err
	}
	if !openStatus(sub.Status) {
		return nil, ErrNotEligible
	}
	person, recipient, err := s.eligible(ctx, tx, sub)
	if err != nil {
		return nil, err
	}
	reference := make([]byte, 32)
	if _, err = rand.Read(reference); err != nil {
		return nil, err
	}
	code := make([]byte, 20)
	for i := range code {
		n, e := rand.Int(rand.Reader, big.NewInt(10))
		if e != nil {
			return nil, e
		}
		code[i] = '0' + byte(n.Int64())
	}
	d := &EmailDelivery{SubmissionID: id, PersonID: person, PublicReference: hex.EncodeToString(reference), PlaintextCode: string(code), RecipientEmail: recipient}
	hash := sha256.Sum256(code)
	recipientHash := sha256.Sum256([]byte(normalizedEmail(sub.Email)))
	if err = invalidateProofs(ctx, tx, id); err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO registration_email_verifications(submission_id,person_id,public_reference,code_hash,recipient_hash,created_at,expires_at) VALUES ($1,$2,$3,$4,$5,clock_timestamp(),clock_timestamp()+make_interval(secs=>$6))`, id, person, d.PublicReference, hash[:], recipientHash[:], s.ttl.Seconds())
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `UPDATE registration_submissions SET status='awaiting_email_verification',updated_at=clock_timestamp() WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	return d, nil
}
func invalidateProofs(ctx context.Context, tx pgx.Tx, id int32) error {
	_, err := tx.Exec(ctx, `UPDATE registration_email_verifications SET invalidated_at=clock_timestamp() WHERE submission_id=$1 AND used_at IS NULL AND invalidated_at IS NULL`, id)
	return err
}
func returnToReview(ctx context.Context, tx pgx.Tx, id int32) error {
	if err := invalidateProofs(ctx, tx, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE registration_submissions SET status='awaiting_identity_review',updated_at=clock_timestamp() WHERE id=$1 AND status IN ('received','awaiting_identity_review','awaiting_email_verification')`, id)
	return err
}

// InvalidateDeliveryAttempt preserves history and only invalidates this delivery.
// The caller must hold the submission lock; retries own their status transition.
func InvalidateDeliveryAttempt(ctx context.Context, tx pgx.Tx, d *EmailDelivery) error {
	var associated bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM registration_email_verifications WHERE public_reference=$1 AND submission_id=$2)`, d.PublicReference, d.SubmissionID).Scan(&associated); err != nil {
		return err
	}
	if !associated {
		return ErrNotEligible
	}
	_, err := tx.Exec(ctx, `UPDATE registration_email_verifications SET invalidated_at=clock_timestamp() WHERE public_reference=$1 AND submission_id=$2 AND used_at IS NULL AND invalidated_at IS NULL`, d.PublicReference, d.SubmissionID)
	return err
}

func (s *EmailService) VerifyEmail(ctx context.Context, reference, code string) error {
	// The public boundary deliberately collapses all rejection/DB details.
	if len(reference) != 64 || len(code) != 20 {
		return ErrVerification
	}
	if err := s.verify(ctx, reference, code); err != nil {
		return ErrVerification
	}
	return nil
}
func (s *EmailService) verify(ctx context.Context, reference, code string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int32
	if err = tx.QueryRow(ctx, `SELECT submission_id FROM registration_email_verifications WHERE public_reference=$1`, reference).Scan(&id); err != nil {
		return err
	}
	if err = LockDeliveryDecision(ctx, tx, id); err != nil {
		return err
	}
	sub, err := dbsqlc.New(tx).LockRegistrationSubmission(ctx, id)
	if err != nil {
		return err
	}
	var person int32
	var stored, recipientHash []byte
	var usable, unexpired bool
	err = tx.QueryRow(ctx, `SELECT person_id,code_hash,recipient_hash,used_at IS NULL AND invalidated_at IS NULL,expires_at>clock_timestamp() FROM registration_email_verifications WHERE public_reference=$1 FOR UPDATE`, reference).Scan(&person, &stored, &recipientHash, &usable, &unexpired)
	if err != nil {
		return err
	}
	if !usable || sub.Status != "awaiting_email_verification" {
		return ErrVerification
	}
	if !unexpired {
		if err = returnToReview(ctx, tx, id); err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return ErrVerification
	}
	hash := sha256.Sum256([]byte(code))
	if subtle.ConstantTimeCompare(hash[:], stored) != 1 {
		return ErrVerification
	}
	expected, recipient, err := s.eligible(ctx, tx, sub)
	if err != nil || expected != person {
		if err != nil && !errors.Is(err, ErrNotEligible) && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err = returnToReview(ctx, tx, id); err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return ErrVerification
	}
	currentHash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(recipient))))
	if subtle.ConstantTimeCompare(currentHash[:], recipientHash) != 1 {
		return ErrVerification
	}
	tag, err := tx.Exec(ctx, `UPDATE registration_email_verifications SET used_at=clock_timestamp() WHERE public_reference=$1 AND expires_at>clock_timestamp() AND used_at IS NULL AND invalidated_at IS NULL`, reference)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrVerification
	}
	tag, err = tx.Exec(ctx, `UPDATE registration_submissions SET status='resolved',resolution_type='existing_person',resolved_person_id=$2,resolved_by_user_id=NULL,resolved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND status='awaiting_email_verification'`, id, person)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrVerification
	}
	return tx.Commit(ctx)
}

// ExpirePendingVerifications is called by administrative reads. Open outbox jobs
// remain awaiting verification while queued, leased or waiting for retry.
func ExpirePendingVerifications(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT s.id FROM registration_submissions s WHERE s.status='awaiting_email_verification' AND NOT EXISTS(SELECT 1 FROM registration_verification_outbox o WHERE o.submission_id=s.id AND o.status IN ('pending','processing')) AND NOT EXISTS(SELECT 1 FROM registration_email_verifications v WHERE v.submission_id=s.id AND v.used_at IS NULL AND v.invalidated_at IS NULL AND v.expires_at>clock_timestamp()) ORDER BY s.id FOR UPDATE OF s SKIP LOCKED`)
	if err != nil {
		return err
	}
	var ids []int32
	for rows.Next() {
		var id int32
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		// Recheck after acquiring the submission lock with a fresh statement snapshot.
		// A concurrent preparation may have replaced the previously expired proof.
		var live bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM registration_email_verifications WHERE submission_id=$1 AND used_at IS NULL AND invalidated_at IS NULL AND expires_at>clock_timestamp()) OR EXISTS(SELECT 1 FROM registration_verification_outbox WHERE submission_id=$1 AND status IN ('pending','processing'))`, id).Scan(&live); err != nil {
			return err
		}
		if live {
			continue
		}
		if err = returnToReview(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
