package trials

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrQuotaSeason = errors.New("trial quota season cannot be determined uniquely")

type QuotaExceededError struct {
	Used  int64
	Limit int32
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("trial quota reached: %d/%d", e.Used, e.Limit)
}

type QuotaUsage struct {
	SeasonID                                                  int32
	SeasonName                                                string
	AttendedCount, RegisteredCount, UsedOrReserved, Remaining int64
	Limit                                                     pgtype.Int4
	Ambiguous                                                 bool
}

func consumes(status string) bool { return status == "registered" || status == "attended" }

// The order for every trial mutation is policy -> Person -> Trial -> calendar.
// The Person lock serializes all seasons for that person. The shared policy lock
// allows different people concurrently and excludes clubconfig changes until commit.
func lockPolicy(ctx context.Context, tx pgx.Tx) (pgtype.Int4, error) {
	limit, err := dbsqlc.New(tx).LockTrialQuotaPolicy(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.Int4{}, nil
	}
	return limit, err
}
func (s *Service) LockForUpdate(ctx context.Context, tx pgx.Tx, id int32) (dbsqlc.TrialRegistration, error) {
	var zero dbsqlc.TrialRegistration
	if _, err := lockPolicy(ctx, tx); err != nil {
		return zero, err
	}
	var person int32
	if err := tx.QueryRow(ctx, "SELECT person_id FROM trial_registrations WHERE id=$1", id).Scan(&person); err != nil {
		return zero, err
	}
	if _, err := dbsqlc.New(tx).LockAdministrativePerson(ctx, person); err != nil {
		return zero, err
	}
	trial, err := dbsqlc.New(tx).LockAdministrativeTrial(ctx, id)
	if err == nil && trial.PersonID != person {
		return zero, ErrInvalidSchedule
	}
	return trial, err
}

// old is excluded from counting. Operations that keep the same reservation may
// proceed even if the configured limit was subsequently lowered below usage.
func checkQuota(ctx context.Context, tx pgx.Tx, person int32, slot pgtype.Int4, date pgtype.Date, old *dbsqlc.TrialRegistration) error {
	q := dbsqlc.New(tx)
	limit, err := q.GetTrialQuotaLimit(ctx)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !limit.Valid {
		return nil
	}
	if err != nil {
		return err
	}
	target, err := q.ResolveTrialQuotaSeason(ctx, dbsqlc.ResolveTrialQuotaSeasonParams{SlotID: slot, TrialDate: date})
	if err != nil {
		return err
	}
	if len(target) != 1 {
		return ErrQuotaSeason
	}
	if old != nil && consumes(old.Status) {
		previous, err := q.ResolveTrialQuotaSeason(ctx, dbsqlc.ResolveTrialQuotaSeasonParams{SlotID: old.GroupSlotID, TrialDate: old.TrialDate})
		if err != nil {
			return err
		}
		if len(previous) == 1 && previous[0].ID == target[0].ID {
			return nil
		}
	}
	facts, err := q.ListPersonTrialQuotaFacts(ctx, person)
	if err != nil {
		return err
	}
	var used int64
	for _, f := range facts {
		if !consumes(f.Status) || old != nil && f.ID == old.ID {
			continue
		}
		if !slices.Contains(f.SeasonIds, target[0].ID) {
			continue
		}
		if len(f.SeasonIds) != 1 {
			return ErrQuotaSeason
		}
		used++
	}
	if used >= int64(limit.Int32) {
		return &QuotaExceededError{Used: used, Limit: limit.Int32}
	}
	return nil
}

// Quotas is a read projection for an already authorized Person. Counts are never
// stored. Ambiguous/unassigned history is visible and never assigned arbitrarily.
func (s *Service) Quotas(ctx context.Context, person int32) ([]QuotaUsage, error) {
	return s.quotaUsage(ctx, person, 0)
}

func (s *Service) quotaUsage(ctx context.Context, person, trialID int32) ([]QuotaUsage, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	limit, err := q.GetTrialQuotaLimit(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	seasons, err := q.ListTrialQuotaSeasons(ctx)
	if err != nil {
		return nil, err
	}
	facts, err := q.ListPersonTrialQuotaFacts(ctx, person)
	if err != nil {
		return nil, err
	}
	bySeason := map[int32]*QuotaUsage{}
	for _, season := range seasons {
		bySeason[season.ID] = &QuotaUsage{SeasonID: season.ID, SeasonName: season.Name, Limit: limit}
	}
	unknown := &QuotaUsage{SeasonName: "Saison indéterminée", Limit: limit, Ambiguous: true}
	hasUnknown := false
	var trialSeason int32
	for _, f := range facts {
		usage := unknown
		if len(f.SeasonIds) == 1 {
			usage = bySeason[f.SeasonIds[0]]
			if f.ID == trialID {
				trialSeason = f.SeasonIds[0]
			}
		} else {
			hasUnknown = true
			if consumes(f.Status) {
				for _, id := range f.SeasonIds {
					bySeason[id].Ambiguous = true
				}
			}
		}
		if f.Status == "attended" {
			usage.AttendedCount++
		}
		if f.Status == "registered" {
			usage.RegisteredCount++
		}
	}
	out := []QuotaUsage{}
	for _, season := range seasons {
		usage := bySeason[season.ID]
		usage.UsedOrReserved = usage.AttendedCount + usage.RegisteredCount
		usage.Remaining = max(int64(limit.Int32)-usage.UsedOrReserved, 0)
		if trialID != 0 && season.ID != trialSeason {
			continue
		}
		if trialID != 0 || season.IsActive || usage.UsedOrReserved > 0 || usage.Ambiguous {
			out = append(out, *usage)
		}
	}
	if hasUnknown && (trialID == 0 || trialSeason == 0) {
		unknown.UsedOrReserved = unknown.AttendedCount + unknown.RegisteredCount
		out = append(out, *unknown)
	}
	return out, tx.Commit(ctx)
}

func (s *Service) QuotaForTrial(ctx context.Context, trial dbsqlc.TrialRegistration) ([]QuotaUsage, error) {
	return s.quotaUsage(ctx, trial.PersonID, trial.ID)
}
