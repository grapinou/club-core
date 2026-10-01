package memberships

import (
	"context"
	"errors"
	"time"

	"database/sql"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

// AssignGroupTx preserves past intervals and prevents overlapping assignments.
func (s *Service) AssignGroupTx(ctx context.Context, tx *sql.Tx, p dbsqlc.AssignMembershipGroupParams) error {
	q := dbsqlc.New(tx)
	m, err := q.LockMembershipGroupTarget(ctx, p.MembershipID)
	if err != nil {
		return err
	}
	if (m.Status != "pending" && m.Status != "active") || !p.JoinedAt.Valid || !p.JoinedAt.IsFinite() || p.JoinedAt.Time.Before(m.StartsAt.Time) || p.JoinedAt.Time.After(m.EndsAt.Time) {
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
	compatible, err := q.MembershipGroupCompatible(ctx, dbsqlc.MembershipGroupCompatibleParams{MembershipID: p.MembershipID, GroupID: p.GroupID})
	if err != nil {
		return err
	}
	if !compatible {
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
func (s *Service) CloseGroupTx(ctx context.Context, tx *sql.Tx, membership, assignment int32, left dbtypes.Date) error {
	q := dbsqlc.New(tx)
	if _, err := q.LockMembershipGroupTarget(ctx, membership); err != nil {
		return err
	}
	a, err := q.LockMembershipGroupAssignment(ctx, dbsqlc.LockMembershipGroupAssignmentParams{ID: assignment, MembershipID: membership})
	if err != nil {
		return err
	}
	if a.LeftAt.Valid || !left.Valid || !left.IsFinite() || left.Time.Before(a.JoinedAt.Time) {
		return ErrInvalidRequest
	}
	_, err = q.CloseMembershipGroup(ctx, dbsqlc.CloseMembershipGroupParams{ID: assignment, LeftAt: left})
	return err
}

// AssignSourceTrialGroupTx is called exactly once by the office conversion,
// immediately after CreateRequestTx in the same transaction. The source trial,
// person and season are already locked by creation. No historical backfill.
// A domain refusal is a stale suggestion; a database failure aborts creation.
func (s *Service) AssignSourceTrialGroupTx(ctx context.Context, tx *sql.Tx, m dbsqlc.Membership) (attempted, assigned bool, err error) {
	if !m.SourceTrialID.Valid {
		return false, false, nil
	}
	q := dbsqlc.New(tx)
	trial, err := q.LockAdministrativeTrial(ctx, m.SourceTrialID.Int32)
	if err != nil {
		return false, false, err
	}
	if !trial.GroupID.Valid {
		return false, false, nil
	}
	season, err := q.LockMembershipGroupTarget(ctx, m.ID)
	if err != nil {
		return true, false, err
	}
	// Pending memberships have no joined_at yet. Start on the request's local
	// civil date, or the season opening when requested ahead of time. Never
	// backdate to a trial or force an expired season's last day.
	at := m.RequestedAt.Time.In(s.location)
	joined := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	if joined.Before(season.StartsAt.Time) {
		joined = season.StartsAt.Time
	}
	err = s.AssignGroupTx(ctx, tx, dbsqlc.AssignMembershipGroupParams{MembershipID: m.ID, GroupID: trial.GroupID.Int32, JoinedAt: dbtypes.Date{Time: joined, Valid: true}})
	if errors.Is(err, ErrInvalidRequest) {
		return true, false, nil
	}
	return true, err == nil, err
}
