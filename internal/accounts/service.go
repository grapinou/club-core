// Package accounts orchestrates committed business operations and email delivery.
// Administrative capabilities are checked here, outside the membership domain.
package accounts

import (
	"context"
	"errors"

	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DeliveryStatus string

const (
	NotRequired DeliveryStatus = "not_required"
	Sent        DeliveryStatus = "sent"
	NoChannel   DeliveryStatus = "no_email_channel"
	SendFailed  DeliveryStatus = "send_failed"
)

var ErrAlreadyActivated = errors.New("account is already activated")
var ErrDisabled = errors.New("account is disabled")

// DeliveryError exposes a safe operational message; Unwrap preserves the transport
// error for classification. The SMTP implementation sanitizes relay diagnostics.
type DeliveryError struct {
	ApprovalCommitted bool
	cause             error
}

func (e *DeliveryError) Unwrap() error { return e.cause }

func (e *DeliveryError) Error() string {
	if e.ApprovalCommitted {
		return "Adhésion validée, mais email d'activation non envoyé."
	}
	return "Nouveau code préparé, mais email d'activation non envoyé."
}

type ApprovalResult struct {
	Membership     dbsqlc.Membership
	UserID         int32
	Completeness   memberships.Completeness
	DeliveryStatus DeliveryStatus
}
type ResendResult struct {
	UserID         int32
	DeliveryStatus DeliveryStatus
}
type PermissionChecker interface {
	HasPermission(context.Context, int32, authorization.Permission) (bool, error)
}
type Service struct {
	permissions         PermissionChecker
	db                  *pgxpool.Pool
	memberships         *memberships.Service
	activation          *activation.Service
	mailer              mailer.Mailer
	from, activationURL string
}

func New(db *pgxpool.Pool, m *memberships.Service, a *activation.Service, sender mailer.Mailer, from, baseURL string, permissions PermissionChecker) *Service {
	return &Service{permissions: permissions, db: db, memberships: m, activation: a, mailer: sender, from: from, activationURL: baseURL + "/activate"}
}
func (s *Service) deliver(ctx context.Context, d *activation.Delivery) (DeliveryStatus, error) {
	if d == nil {
		return NotRequired, nil
	}
	if d.RecipientEmail == nil {
		return NoChannel, nil
	}
	message, err := mailer.ActivationMessage(s.from, s.activationURL, *d)
	if err != nil {
		return SendFailed, err
	}
	if err = s.mailer.Send(ctx, message); err != nil {
		return SendFailed, err
	}
	return Sent, nil
}
func (s *Service) ApproveMembership(ctx context.Context, id, approver int32, note *string) (ApprovalResult, error) {
	if err := s.require(ctx, approver, authorization.MembershipsApprove); err != nil {
		return ApprovalResult{}, err
	}
	a, err := s.memberships.ApproveMembership(ctx, id, approver, note)
	if err != nil {
		return ApprovalResult{}, err
	}
	// The existing service returns successfully only after commit.
	result := ApprovalResult{Membership: a.Membership, UserID: a.User.ID, Completeness: a.Completeness}
	result.DeliveryStatus, err = s.deliver(ctx, a.ActivationDelivery)
	if result.DeliveryStatus == SendFailed {
		return result, &DeliveryError{ApprovalCommitted: true, cause: err}
	}
	return result, nil
}
func (s *Service) ResendActivation(ctx context.Context, actorID, userID int32) (ResendResult, error) {
	result := ResendResult{UserID: userID}
	if err := s.require(ctx, actorID, authorization.ActivationResend); err != nil {
		return result, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var active, activated bool
	err = tx.QueryRow(ctx, "SELECT is_active,activated_at IS NOT NULL FROM users WHERE id=$1 FOR UPDATE", userID).Scan(&active, &activated)
	if err != nil {
		return result, err
	}
	if !active {
		return result, ErrDisabled
	}
	if activated {
		return result, ErrAlreadyActivated
	}
	d, err := s.activation.PrepareTx(ctx, tx, userID)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.DeliveryStatus, err = s.deliver(ctx, d)
	if result.DeliveryStatus == SendFailed {
		return result, &DeliveryError{cause: err}
	}
	return result, nil
}

// actorID must come from the authenticated caller, never a browser form field.
func (s *Service) require(ctx context.Context, actorID int32, permission authorization.Permission) error {
	user, err := dbsqlc.New(s.db).GetUserByID(ctx, actorID)
	if err != nil {
		return authorization.ErrForbidden
	}
	if !user.IsActive || !user.ActivatedAt.Valid || !user.PasswordHash.Valid {
		return authorization.ErrForbidden
	}
	allowed, err := s.permissions.HasPermission(ctx, actorID, permission)
	if err != nil {
		return err
	}
	if !allowed {
		return authorization.ErrForbidden
	}
	return nil
}
