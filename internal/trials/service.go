// Package trials owns creation and rescheduling rules. Backend callers must use
// Service for scheduling and status changes; dbsqlc writes are internal primitives.
package trials

import (
	"context"
	"errors"
	"fmt"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidSchedule = errors.New("invalid trial schedule")
var ErrConverted = errors.New("trial already converted to membership")

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

func (s *Service) Schedule(ctx context.Context, p dbsqlc.CreateTrialParams) (dbsqlc.TrialRegistration, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	defer tx.Rollback(ctx)
	result, err := s.ScheduleTx(ctx, tx, p)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// Reschedule preserves person, status, notes and the P3 origin protection.
func (s *Service) Reschedule(ctx context.Context, p dbsqlc.RescheduleTrialParams) (dbsqlc.TrialRegistration, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	defer tx.Rollback(ctx)
	result, err := s.RescheduleTx(ctx, tx, p)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
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

// ScheduleTx and RescheduleTx compose the same domain rules with an audit transaction.
func (s *Service) ScheduleTx(ctx context.Context, tx pgx.Tx, p dbsqlc.CreateTrialParams) (dbsqlc.TrialRegistration, error) {
	q := dbsqlc.New(tx)
	if _, err := lockPolicy(ctx, tx); err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	if _, err := q.LockAdministrativePerson(ctx, p.PersonID); err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	if err := validate(ctx, q, dbsqlc.RescheduleTrialParams{ActivityID: p.ActivityID, GroupID: p.GroupID, GroupSlotID: p.GroupSlotID, TrialDate: p.TrialDate}); err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	if err := checkQuota(ctx, tx, p.PersonID, p.GroupSlotID, p.TrialDate, nil); err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	return q.CreateTrial(ctx, p)
}
func (s *Service) RescheduleTx(ctx context.Context, tx pgx.Tx, p dbsqlc.RescheduleTrialParams) (dbsqlc.TrialRegistration, error) {
	q := dbsqlc.New(tx)
	old, err := s.LockForUpdate(ctx, tx, p.ID)
	if err != nil {
		return old, err
	}
	if err := checkReschedule(ctx, tx, p.ID); err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	if err := validate(ctx, q, p); err != nil {
		return dbsqlc.TrialRegistration{}, err
	}
	if consumes(old.Status) {
		if err := checkQuota(ctx, tx, old.PersonID, p.GroupSlotID, p.TrialDate, &old); err != nil {
			return dbsqlc.TrialRegistration{}, err
		}
	}
	return q.RescheduleTrial(ctx, p)
}

// Keep the session behind a durable origin stable. Notes and outcome corrections
// remain possible; a new session must be scheduled as a separate trial.
func checkReschedule(ctx context.Context, tx pgx.Tx, id int32) error {
	if _, err := dbsqlc.New(tx).LockAdministrativeTrial(ctx, id); err != nil {
		return err
	}
	var converted bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memberships WHERE source_trial_id=$1)", id).Scan(&converted); err != nil {
		return err
	}
	if converted {
		return ErrConverted
	}
	return nil
}
func ValidStatus(status string) bool {
	return status == "registered" || status == "attended" || status == "cancelled" || status == "no_show"
}
func (s *Service) UpdateStatusTx(ctx context.Context, tx pgx.Tx, p dbsqlc.UpdateTrialStatusParams) (dbsqlc.TrialRegistration, error) {
	if !ValidStatus(p.Status) {
		return dbsqlc.TrialRegistration{}, ErrInvalidSchedule
	}
	old, err := s.LockForUpdate(ctx, tx, p.ID)
	if err != nil {
		return old, err
	}
	if consumes(p.Status) && !consumes(old.Status) {
		if err := checkQuota(ctx, tx, old.PersonID, old.GroupSlotID, old.TrialDate, &old); err != nil {
			return dbsqlc.TrialRegistration{}, err
		}
	}
	return dbsqlc.New(tx).UpdateTrialStatus(ctx, p)
}
func (s *Service) UpdateNotesTx(ctx context.Context, tx pgx.Tx, p dbsqlc.UpdateTrialNotesParams) (dbsqlc.TrialRegistration, error) {
	return dbsqlc.New(tx).UpdateTrialNotes(ctx, p)
}
