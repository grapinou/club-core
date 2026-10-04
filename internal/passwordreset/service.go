// Package passwordreset owns the shared public and administrative reset lifecycle.
package passwordreset

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/mailer"
)

var (
	ErrInvalidToken = errors.New("invalid password reset link")
	ErrPassword     = errors.New("invalid password or confirmation")
	ErrIneligible   = errors.New("account cannot receive a password reset")
	ErrNoChannel    = errors.New("no usable recovery email")
	ErrLimited      = errors.New("password reset account budget exhausted")
	ErrDelivery     = errors.New("password reset delivery failed")
)

const AccountRequestsPerHour = 3

type Service struct {
	db            *sql.DB
	sender        mailer.Mailer
	sessions      *auth.Sessions
	from, baseURL string
	ttl           time.Duration
	publicSlots   chan struct{}
}

func New(db *sql.DB, sender mailer.Mailer, sessions *auth.Sessions, from, baseURL string, ttl time.Duration) *Service {
	if ttl <= 0 {
		panic("password reset TTL must be positive")
	}
	return &Service{db: db, sender: sender, sessions: sessions, from: from, baseURL: baseURL, ttl: ttl, publicSlots: make(chan struct{}, 16)}
}

// Submit returns before account lookup or SMTP. Every username follows the same
// HTTP path; neither eligibility nor relay latency is observable in the response.
// Work and plaintext tokens are bounded, process-local and never persisted.
func (s *Service) Submit(username string) {
	username = strings.TrimSpace(username)
	if len(username) > 256 {
		return
	}
	select {
	case s.publicSlots <- struct{}{}:
		go func() {
			defer func() { <-s.publicSlots }()
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			_ = s.RequestUsername(ctx, username)
		}()
	default:
	}
}

// RequestUsername is also the synchronous entry point for delivery integration.
// Its errors are operational only and must never be exposed on a public page.
func (s *Service) RequestUsername(ctx context.Context, username string) error {
	user, err := dbsqlc.New(s.db).GetUserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrIneligible
	}
	if err != nil {
		return err
	}
	return s.request(ctx, user.ID, 0)
}

func (s *Service) RequestForPerson(ctx context.Context, personID int32) error {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return authorization.ErrForbidden
	}
	allowed, err := authorization.New(dbsqlc.New(s.db)).HasPermission(ctx, actor, authorization.PasswordReset)
	if err != nil {
		return err
	}
	if !allowed {
		return authorization.ErrForbidden
	}
	userID, err := dbsqlc.New(s.db).PasswordResetUserForPerson(ctx, personID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrIneligible
	}
	if err != nil {
		return err
	}
	return s.request(ctx, userID, actor)
}

func (s *Service) request(ctx context.Context, userID, actor int32) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := dbsqlc.New(tx)
	// SQLite's immediate transaction serializes budget checks and invalidations.
	if actor != 0 {
		allowed, e := authorization.New(q).HasPermission(ctx, actor, authorization.PasswordReset)
		if e != nil {
			return e
		}
		if !allowed {
			return authorization.ErrForbidden
		}
	}
	user, err := q.PasswordResetUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrIneligible
	}
	if err != nil {
		return err
	}
	count, err := q.CountRecentPasswordResets(ctx, userID)
	if err != nil {
		return err
	}
	if count >= AccountRequestsPerHour {
		return ErrLimited
	}
	recipient, err := activation.RecipientTx(ctx, tx, userID, false)
	if err != nil {
		return err
	}
	if recipient.RecipientEmail == nil {
		return ErrNoChannel
	}
	token, err := auth.RandomToken()
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(token))
	if err = q.InvalidatePasswordResets(ctx, userID); err != nil {
		return err
	}
	id, err := q.CreatePasswordReset(ctx, dbsqlc.CreatePasswordResetParams{
		UserID: userID, RequestedByUserID: sql.NullInt32{Int32: actor, Valid: actor != 0}, TokenHash: hash[:], TtlSeconds: s.ttl.Seconds(),
	})
	if err != nil {
		return err
	}
	message, err := mailer.PasswordResetMessage(s.from, *recipient.RecipientEmail, user.Username, s.baseURL+"/password/reset?token="+token, s.ttl)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = s.sender.Send(ctx, message); err != nil {
		// Never leave an undelivered link active. Address/SMTP errors remain private.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := dbsqlc.New(s.db).InvalidateFailedPasswordReset(cleanup, id); e != nil {
			return e
		}
		return ErrDelivery
	}
	return nil
}

func tokenHash(token string) ([]byte, error) {
	if len(token) != 64 {
		return nil, ErrInvalidToken
	}
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return nil, ErrInvalidToken
	}
	hash := sha256.Sum256([]byte(token))
	return hash[:], nil
}

func (s *Service) Validate(ctx context.Context, token string) error {
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}
	_, err = dbsqlc.New(s.db).ActivePasswordReset(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidToken
	}
	return err
}

func (s *Service) Reset(ctx context.Context, token, password, confirmation string) error {
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := dbsqlc.New(tx)
	reset, err := q.ActivePasswordReset(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return err
	}
	if !auth.ValidPassword(password) || password != confirmation {
		return ErrPassword
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err = q.UpdateSelfServicePassword(ctx, dbsqlc.UpdateSelfServicePasswordParams{ID: reset.UserID, PasswordHash: sql.NullString{String: string(passwordHash), Valid: true}}); err != nil {
		return err
	}
	consumed, err := q.ConsumePasswordReset(ctx, reset.ID)
	if err != nil {
		return err
	}
	if consumed != 1 {
		return ErrInvalidToken
	}
	if err = q.InvalidatePasswordResets(ctx, reset.UserID); err != nil {
		return err
	}
	if err = q.InvalidateEmailChanges(ctx, reset.UserID); err != nil {
		return err
	}
	if err = q.CreateAccountSecurityEvent(ctx, dbsqlc.CreateAccountSecurityEventParams{UserID: reset.UserID, PersonID: reset.PersonID, Event: "password_changed"}); err != nil {
		return err
	}
	// Recipient lookup belongs to the best-effort notification as well.
	recipient, recipientErr := activation.RecipientTx(ctx, tx, reset.UserID, false)
	if err = tx.Commit(); err != nil {
		return err
	}
	s.sessions.RevokeAccount(reset.UserID)
	// Notification is best effort after commit. It cannot roll back the password.
	if recipientErr == nil && recipient.RecipientEmail != nil {
		notification, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = s.sender.Send(notification, mailer.PasswordChangedMessage(s.from, *recipient.RecipientEmail))
	}
	return nil
}
