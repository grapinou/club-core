// Package trials owns creation and rescheduling rules. Backend callers must use
// Service for scheduling; dbsqlc reads, status and note updates are independent.
package trials

import (
	"context"
	"errors"
	"fmt"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidSchedule = errors.New("invalid trial schedule")

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

func (s *Service) Schedule(ctx context.Context, p dbsqlc.CreateTrialParams) (dbsqlc.TrialRegistration, error) {
	return s.write(ctx, dbsqlc.RescheduleTrialParams{ActivityID: p.ActivityID, GroupID: p.GroupID, GroupSlotID: p.GroupSlotID, TrialDate: p.TrialDate}, func(q *dbsqlc.Queries) (dbsqlc.TrialRegistration, error) { return q.CreateTrial(ctx, p) })
}

// Reschedule preserves the person, status and notes, and validates the entire new target.
func (s *Service) Reschedule(ctx context.Context, p dbsqlc.RescheduleTrialParams) (dbsqlc.TrialRegistration, error) {
	return s.write(ctx, p, func(q *dbsqlc.Queries) (dbsqlc.TrialRegistration, error) { return q.RescheduleTrial(ctx, p) })
}

func (s *Service) write(ctx context.Context, p dbsqlc.RescheduleTrialParams, write func(*dbsqlc.Queries) (dbsqlc.TrialRegistration, error)) (dbsqlc.TrialRegistration, error) {
	var zero dbsqlc.TrialRegistration
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	if err = validate(ctx, q, p); err != nil {
		return zero, err
	}
	result, err := write(q)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

func validate(ctx context.Context, q *dbsqlc.Queries, p dbsqlc.RescheduleTrialParams) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidSchedule, reason) }
	if !p.TrialDate.Valid || p.TrialDate.InfinityModifier != pgtype.Finite {
		return invalid("a finite date is required")
	}
	if p.GroupSlotID.Valid && !p.GroupID.Valid {
		return invalid("slot requires group")
	}
	active, err := q.LockTrialActivity(ctx, p.ActivityID)
	if err != nil {
		return fmt.Errorf("activity: %w", err)
	}
	if !active {
		return invalid("inactive activity")
	}
	if p.GroupID.Valid {
		group, err := q.LockTrialGroup(ctx, p.GroupID.Int32)
		if err != nil {
			return fmt.Errorf("group: %w", err)
		}
		if group.ActivityID != p.ActivityID || !group.IsActive {
			return invalid("group must be active and belong to activity")
		}
	}
	if p.GroupSlotID.Valid {
		slot, err := q.LockTrialSlot(ctx, dbsqlc.LockTrialSlotParams{ID: p.GroupSlotID.Int32, TrialDate: p.TrialDate})
		if err != nil {
			return fmt.Errorf("slot: %w", err)
		}
		if slot.GroupID != p.GroupID.Int32 || !slot.IsActive {
			return invalid("slot must be active and belong to group")
		}
		if !slot.CalendarValid {
			return invalid("date outside slot weekday, validity or season")
		}
	}
	return nil
}
