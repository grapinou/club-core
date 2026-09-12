package accounts

import (
	"context"
	"net/mail"
	"strings"

	"github.com/grapinou/club-core/internal/accounts/provisioning"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
)

// EnsureUserForPerson is an administrative entry point, independent of Membership.
// It reuses existing users unchanged, including disabled users.
func (s *Service) EnsureUserForPerson(ctx context.Context, person int32) (dbsqlc.User, error) {
	var zero dbsqlc.User
	actor, ok := auth.UserID(ctx)
	if !ok {
		return zero, authorization.ErrForbidden
	}
	if err := s.require(ctx, actor, authorization.PersonsWrite); err != nil {
		return zero, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	u, err := provisioning.EnsureUserForPersonTx(ctx, tx, person)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return u, nil
}

// EnsureGuardianUser never creates a Membership or a child account. Resource IDs
// identify the relationship; the administrator actor comes only from auth context.
func (s *Service) EnsureGuardianUser(ctx context.Context, child, guardian int32, options ...GuardianUserOption) (ResendResult, error) {
	var result ResendResult
	if s.guardians == nil {
		return result, guardianaccess.ErrIneligible
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = s.guardians.AuthorizeProvisionTx(ctx, tx, child, guardian); err != nil {
		return result, err
	}
	u, err := provisioning.EnsureUserForPersonTx(ctx, tx, guardian)
	if err != nil {
		return result, err
	}
	result.UserID = u.ID
	if !u.IsActive {
		return result, ErrDisabled
	}
	// Automatic finalization reuses a pending activation; explicit existing admin
	// calls keep their resend semantics. Eligibility/person locks serialize this check.
	for _, option := range options {
		if option == KeepPendingActivation {
			var pending bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_activation_codes WHERE user_id=$1 AND used_at IS NULL AND invalidated_at IS NULL AND expires_at>clock_timestamp())`, u.ID).Scan(&pending)
			if err != nil {
				return result, err
			}
			if pending {
				return result, tx.Commit(ctx)
			}
		}
	}
	d, err := s.activation.PreparePersonOnlyTx(ctx, tx, u.ID)
	if err != nil {
		return result, err
	}
	// Only the guardian's own usable mailbox may receive this activation.
	if d != nil && d.RecipientEmail != nil {
		address := strings.TrimSpace(*d.RecipientEmail)
		parsed, e := mail.ParseAddress(address)
		if e != nil || parsed.Address != address {
			d.RecipientEmail = nil
			d.RecipientPersonID = nil
		}
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

// SetGuardianAccess wires the resource-scoped authorization service.
func (s *Service) SetGuardianAccess(g *guardianaccess.Service) { s.guardians = g }

type GuardianUserOption int

const KeepPendingActivation GuardianUserOption = 1
