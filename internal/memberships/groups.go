package memberships

import (
	"context"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AssignGroupTx preserves past intervals and prevents overlapping assignments.
func (s *Service) AssignGroupTx(ctx context.Context, tx pgx.Tx, p dbsqlc.AssignMembershipGroupParams) error {
	q := dbsqlc.New(tx)
	m, err := q.LockMembershipGroupTarget(ctx, p.MembershipID)
	if err != nil {
		return err
	}
	if (m.Status != "pending" && m.Status != "active") || !p.JoinedAt.Valid || p.JoinedAt.InfinityModifier != pgtype.Finite || p.JoinedAt.Time.Before(m.StartsAt.Time) || p.JoinedAt.Time.After(m.EndsAt.Time) {
		return ErrInvalidRequest
	}
	g, err := q.LockTrialGroup(ctx, p.GroupID)
	if err != nil {
		return err
	}
	if !g.IsActive {
		return ErrInvalidRequest
	}
	ok, err := q.MembershipHasGroupActivity(ctx, dbsqlc.MembershipHasGroupActivityParams{MembershipID: p.MembershipID, ActivityID: g.ActivityID})
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidRequest
	}
	overlap, err := q.MembershipGroupOverlaps(ctx, dbsqlc.MembershipGroupOverlapsParams{MembershipID: p.MembershipID, GroupID: p.GroupID, JoinedAt: p.JoinedAt})
	if err != nil {
		return err
	}
	if overlap {
		return ErrInvalidRequest
	}
	_, err = q.AssignMembershipGroup(ctx, p)
	return err
}
func (s *Service) CloseGroupTx(ctx context.Context, tx pgx.Tx, membership, assignment int32, left pgtype.Date) error {
	q := dbsqlc.New(tx)
	if _, err := q.LockMembershipGroupTarget(ctx, membership); err != nil {
		return err
	}
	a, err := q.LockMembershipGroupAssignment(ctx, dbsqlc.LockMembershipGroupAssignmentParams{ID: assignment, MembershipID: membership})
	if err != nil {
		return err
	}
	if a.LeftAt.Valid || !left.Valid || left.InfinityModifier != pgtype.Finite || left.Time.Before(a.JoinedAt.Time) {
		return ErrInvalidRequest
	}
	_, err = q.CloseMembershipGroup(ctx, dbsqlc.CloseMembershipGroupParams{ID: assignment, LeftAt: left})
	return err
}
