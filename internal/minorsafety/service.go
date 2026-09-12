// Package minorsafety provides backend safeguards for future interaction domains.
// It neither creates conversations nor grants permission to read child resources.
package minorsafety

import (
	"context"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Decision string

const (
	Allowed             Decision = "allowed"
	Denied              Decision = "denied"
	SupervisionRequired Decision = "supervision_required"
	NotApplicable       Decision = "not_applicable"
)

type RuleBasis string

const (
	RuleLegal                  RuleBasis = "legal"
	RuleOfficialRecommendation RuleBasis = "official_recommendation"
	RuleClubCoreSafety         RuleBasis = "club_core_safety"
)

type Result struct {
	Decision   Decision
	Basis      RuleBasis
	Supervised bool
}
type Service struct {
	db       *pgxpool.Pool
	location *time.Location
	now      func() time.Time
}

func New(db *pgxpool.Pool, location *time.Location) *Service {
	if location == nil {
		panic("minor safety requires business location")
	}
	return &Service{db, location, time.Now}
}

// EvaluatePrivateConversation requires the authenticated actor among participants.
// IDs identify requested resources only: age and guardian grants are read from DB.
// Future messaging must call this at every sensitive operation and when membership
// in a channel changes; an Allowed result is not a general messaging capability.
func (s *Service) EvaluatePrivateConversation(ctx context.Context, userIDs []int32) (Result, error) {
	deny := Result{Decision: Denied, Basis: RuleClubCoreSafety}
	actor, ok := auth.UserID(ctx)
	if !ok || len(userIDs) < 2 || len(userIDs) > 256 {
		return deny, nil
	}
	seen := map[int32]bool{}
	for _, id := range userIDs {
		if seen[id] {
			return deny, nil
		}
		seen[id] = true
	}
	if !seen[actor] {
		return deny, nil
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return deny, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	people, err := q.ListInteractionParticipants(ctx, userIDs)
	if err != nil {
		return deny, err
	}
	if len(people) != len(userIDs) {
		return deny, nil
	}
	ids := make([]int32, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.PersonID)
	}
	edges, err := q.ListInteractionGuardianEdges(ctx, ids)
	if err != nil {
		return deny, err
	}
	result := evaluate(people, edges, s.now().In(s.location))
	if err = tx.Commit(ctx); err != nil {
		return deny, err
	}
	return result, nil
}

// Club Core safety choice, not a claim of legal obligation. Every minor in a
// mixed channel needs their own authorized adult guardian among participants.
// Additional unrelated adults do not, by themselves, establish supervision.
func evaluate(people []dbsqlc.ListInteractionParticipantsRow, edges []dbsqlc.ListInteractionGuardianEdgesRow, at time.Time) Result {
	result := Result{Decision: NotApplicable, Basis: RuleClubCoreSafety}
	minors, adults := map[int32]bool{}, map[int32]bool{}
	for _, p := range people {
		if !p.BirthDate.Valid || p.BirthDate.InfinityModifier != pgtype.Finite {
			result.Decision = Denied
			return result
		}
		if civildate.IsMinor(p.BirthDate.Time, at) {
			minors[p.PersonID] = true
		} else {
			adults[p.PersonID] = true
		}
	}
	if len(minors) == 0 || len(adults) == 0 {
		return result
	}
	guardians := map[int32]map[int32]bool{}
	for _, e := range edges {
		if minors[e.ChildPersonID] && adults[e.GuardianPersonID] {
			if guardians[e.ChildPersonID] == nil {
				guardians[e.ChildPersonID] = map[int32]bool{}
			}
			guardians[e.ChildPersonID][e.GuardianPersonID] = true
		}
	}
	for child := range minors {
		if len(guardians[child]) == 0 {
			result.Decision = SupervisionRequired
			result.Supervised = false
			return result
		}
		if len(guardians[child]) < len(adults) {
			result.Supervised = true
		}
	}
	result.Decision = Allowed
	return result
}
