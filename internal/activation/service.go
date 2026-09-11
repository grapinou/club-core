// Package activation prepares delivery values and activates durable accounts.
// No delivery is performed here. Never log Delivery or persist its plaintext code.
package activation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math/big"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCode = errors.New("invalid or unavailable activation code")
var ErrInvalidPassword = errors.New("password must contain 12 to 72 bytes")

type Delivery struct {
	UserID            int32
	Username          string
	PlaintextCode     string
	RecipientEmail    *string
	RecipientPersonID *int32
}

type Service struct {
	db       *pgxpool.Pool
	validity time.Duration
}

func New(db *pgxpool.Pool, validity time.Duration) (*Service, error) {
	if validity <= 0 {
		return nil, errors.New("activation validity must be positive")
	}
	return &Service{db: db, validity: validity}, nil
}

// PrepareTx joins the caller's transaction and locks the user before replacing codes.
// A delivery is usable only after the caller commits successfully.
func (s *Service) PrepareTx(ctx context.Context, tx pgx.Tx, userID int32) (*Delivery, error) {
	var d Delivery
	var person int32
	var activated *time.Time
	var active bool
	err := tx.QueryRow(ctx, "SELECT id,username,person_id,activated_at,is_active FROM users WHERE id=$1 FOR UPDATE", userID).Scan(&d.UserID, &d.Username, &person, &activated, &active)
	if err != nil {
		return nil, err
	}
	if activated != nil {
		return nil, nil
	}
	if !active {
		return nil, ErrInvalidCode
	}
	// 20 decimal digits provide over 66 bits of cryptographic entropy.
	code := make([]byte, 20)
	for i := range code {
		n, e := rand.Int(rand.Reader, big.NewInt(10))
		if e != nil {
			return nil, e
		}
		code[i] = byte(n.Int64()) + '0'
	}
	d.PlaintextCode = string(code)
	hash := sha256.Sum256(code)
	_, err = tx.Exec(ctx, "UPDATE user_activation_codes SET invalidated_at=clock_timestamp() WHERE user_id=$1 AND used_at IS NULL AND invalidated_at IS NULL", userID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO user_activation_codes(user_id,code_hash,created_at,expires_at) VALUES ($1,$2,clock_timestamp(),clock_timestamp()+make_interval(secs => $3))", userID, hash[:], s.validity.Seconds())
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT id,btrim(email) FROM (
 SELECT id,email,0 AS rank,0 AS position FROM persons WHERE id=$1
 UNION ALL
 SELECT p.id,p.email,CASE WHEN g.is_primary_contact THEN 1 ELSE 2 END,g.id
 FROM person_guardians g JOIN persons p ON p.id=g.guardian_person_id WHERE g.child_person_id=$1
 ) candidates WHERE nullif(btrim(email),'') IS NOT NULL ORDER BY rank,position,id LIMIT 1`, person).Scan(&d.RecipientPersonID, &d.RecipientEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Activate atomically consumes a code; passwords use bcrypt with the standard cost.
func (s *Service) Activate(ctx context.Context, username, code, password string) (dbsqlc.User, error) {
	var zero dbsqlc.User
	if len(password) < 12 || len(password) > 72 {
		return zero, ErrInvalidPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return zero, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	var userID int32
	var active bool
	var activated *time.Time
	err = tx.QueryRow(ctx, "SELECT id,is_active,activated_at FROM users WHERE username=$1 FOR UPDATE", username).Scan(&userID, &active, &activated)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrInvalidCode
	}
	if err != nil {
		return zero, err
	}
	if !active || activated != nil {
		return zero, ErrInvalidCode
	}
	var codeID int64
	var stored []byte
	err = tx.QueryRow(ctx, "SELECT id,code_hash FROM user_activation_codes WHERE user_id=$1 AND used_at IS NULL AND invalidated_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE", userID).Scan(&codeID, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrInvalidCode
	}
	if err != nil {
		return zero, err
	}
	candidate := sha256.Sum256([]byte(code))
	if subtle.ConstantTimeCompare(candidate[:], stored) != 1 {
		return zero, ErrInvalidCode
	}
	tag, err := tx.Exec(ctx, "UPDATE user_activation_codes SET used_at=clock_timestamp() WHERE id=$1 AND expires_at>clock_timestamp()", codeID)
	if err != nil {
		return zero, err
	}
	if tag.RowsAffected() != 1 {
		return zero, ErrInvalidCode
	}
	_, err = tx.Exec(ctx, "UPDATE users SET password_hash=$2,activated_at=clock_timestamp() WHERE id=$1", userID, string(hash))
	if err != nil {
		return zero, err
	}
	user, err := dbsqlc.New(tx).GetUserByUsername(ctx, username)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return user, nil
}
