package memberships

import (
	"context"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
)

type ListEntry struct {
	Membership   dbsqlc.ListAdministrativeMembershipsRow
	Completeness Completeness
	Account      AccountState
}

// List uses two queries regardless of the number of memberships. All rows and
// completeness facts come from one read-only snapshot and one administrative date.
func (s *Service) List(ctx context.Context) ([]ListEntry, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	rows, err := q.ListAdministrativeMemberships(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int32, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Membership.ID)
	}
	facts, err := q.ListMembershipCompletenessFacts(ctx, ids)
	if err != nil {
		return nil, err
	}
	at := time.Now().In(s.location)
	completenessByID := map[int32]Completeness{}
	for _, fact := range facts {
		completenessByID[fact.ID] = evaluateCompleteness(fact, at)
	}
	result := make([]ListEntry, 0, len(rows))
	for _, row := range rows {
		result = append(result, ListEntry{Membership: row, Completeness: completenessByID[row.Membership.ID], Account: AccountState{
			Exists: row.UserID.Valid, IsActive: row.UserIsActive.Bool, IsActivated: row.ActivatedAt.Valid, NeedsActivation: row.UserID.Valid && !row.ActivatedAt.Valid,
		}})
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
