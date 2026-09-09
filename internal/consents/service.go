// Package consents validates and records append-only membership decisions.
// Backend callers must use Service for writes; dbsqlc supplies frontend reads.
package consents

import (
	"context"
	"errors"
	"fmt"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUnauthorizedGiver      = errors.New("consent giver must be the member or a guardian")
	ErrWithdrawalWithoutGrant = errors.New("withdrawal requires the current decision to be granted")
	ErrInactiveDefinition     = errors.New("inactive consent definition cannot receive grants or refusals")
	ErrInvalidDecision        = errors.New("invalid consent decision")
)

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

func (s *Service) RecordConsentDecision(ctx context.Context, p dbsqlc.CreateMembershipConsentParams) (dbsqlc.MembershipConsent, error) {
	var zero dbsqlc.MembershipConsent
	if p.Decision != "granted" && p.Decision != "refused" && p.Decision != "withdrawn" {
		return zero, ErrInvalidDecision
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	person, err := q.LockConsentMembership(ctx, p.MembershipID)
	if err != nil {
		return zero, fmt.Errorf("membership: %w", err)
	}
	if person != p.GivenByPersonID {
		_, err = q.LockConsentGuardian(ctx, dbsqlc.LockConsentGuardianParams{ChildPersonID: person, GuardianPersonID: p.GivenByPersonID})
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, ErrUnauthorizedGiver
		}
		if err != nil {
			return zero, err
		}
	}
	active, err := q.LockConsentDefinition(ctx, p.ConsentDefinitionID)
	if err != nil {
		return zero, fmt.Errorf("consent definition: %w", err)
	}
	if p.Decision != "withdrawn" && !active {
		return zero, ErrInactiveDefinition
	}
	if p.Decision == "withdrawn" {
		decision, err := q.GetCurrentMembershipConsentDecision(ctx, dbsqlc.GetCurrentMembershipConsentDecisionParams{MembershipID: p.MembershipID, ConsentDefinitionID: p.ConsentDefinitionID})
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, ErrWithdrawalWithoutGrant
		}
		if err != nil {
			return zero, err
		}
		if decision != "granted" {
			return zero, ErrWithdrawalWithoutGrant
		}
	}
	result, err := q.CreateMembershipConsent(ctx, p)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

// WithdrawConsent appends a withdrawal, preserving every previous decision.
func (s *Service) WithdrawConsent(ctx context.Context, membershipID, definitionID, givenByPersonID int32) (dbsqlc.MembershipConsent, error) {
	return s.RecordConsentDecision(ctx, dbsqlc.CreateMembershipConsentParams{MembershipID: membershipID, ConsentDefinitionID: definitionID, GivenByPersonID: givenByPersonID, Decision: "withdrawn"})
}
