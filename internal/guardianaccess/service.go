// Package guardianaccess authorizes an authenticated guardian on a specific child.
// A family relationship alone grants no digital access. Administrative RBAC is separate.
package guardianaccess

import (
	"context"
	"errors"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrIneligible = errors.New("guardian access unavailable")

type PermissionChecker interface {
	HasPermission(context.Context, int32, authorization.Permission) (bool, error)
}
type Service struct {
	db          *pgxpool.Pool
	permissions PermissionChecker
	location    *time.Location
	now         func() time.Time
}
type Option func(*Service)

func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

func New(db *pgxpool.Pool, p PermissionChecker, location *time.Location, options ...Option) *Service {
	if location == nil {
		panic("guardian access requires business location")
	}
	s := &Service{db: db, permissions: p, location: location, now: time.Now}
	for _, option := range options {
		option(s)
	}
	return s
}
func (s *Service) minor(birth pgtype.Date) bool {
	return birth.Valid && birth.InfinityModifier == pgtype.Finite && civildate.IsMinor(birth.Time, s.now().In(s.location))
}

// RequireAdministrator derives the actor from authentication, never from resource IDs.
func (s *Service) RequireAdministrator(ctx context.Context) (int32, error) {
	return requireAdministrator(ctx, dbsqlc.New(s.db), s.permissions)
}
func requireAdministrator(ctx context.Context, q *dbsqlc.Queries, permissions PermissionChecker) (int32, error) {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return 0, authorization.ErrForbidden
	}
	u, err := q.GetUserByID(ctx, actor)
	if err != nil {
		return 0, authorization.ErrForbidden
	}
	if !u.IsActive || !u.ActivatedAt.Valid || !u.PasswordHash.Valid {
		return 0, authorization.ErrForbidden
	}
	allowed, err := permissions.HasPermission(ctx, actor, authorization.PersonsWrite)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, authorization.ErrForbidden
	}
	return actor, nil
}

// lockEligible locks Persons in ID order before the relationship, also serializing
// account provisioning with approval. Grants need not already have an active User.
func (s *Service) lockEligible(ctx context.Context, tx pgx.Tx, child, guardian int32) error {
	rows, err := tx.Query(ctx, "SELECT id FROM persons WHERE id=ANY($1::integer[]) ORDER BY id FOR UPDATE", []int32{child, guardian})
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	_, err = dbsqlc.New(tx).LockGuardianRelation(ctx, dbsqlc.LockGuardianRelationParams{ChildPersonID: child, GuardianPersonID: guardian})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIneligible
	}
	if err != nil {
		return err
	}
	var birth pgtype.Date
	err = tx.QueryRow(ctx, "SELECT c.birth_date FROM persons c JOIN persons g ON g.id=$2 WHERE c.id=$1 AND c.archived_at IS NULL AND g.archived_at IS NULL", child, guardian).Scan(&birth)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIneligible
	}
	if err != nil {
		return err
	}
	if !s.minor(birth) {
		return ErrIneligible
	}
	return nil
}

// AuthorizeProvisionTx holds eligibility stable until account preparation commits.
// The caller must commit promptly and deliver activation only afterwards.
func (s *Service) AuthorizeProvisionTx(ctx context.Context, tx pgx.Tx, child, guardian int32) error {
	// Use this transaction for authentication and RBAC reads too; acquiring another
	// pool connection here can deadlock concurrent provisioning on a small pool.
	q := dbsqlc.New(tx)
	if _, err := requireAdministrator(ctx, q, authorization.New(q)); err != nil {
		return err
	}
	if err := s.lockEligible(ctx, tx, child, guardian); err != nil {
		return err
	}
	_, err := dbsqlc.New(tx).GetActiveGuardianAccess(ctx, dbsqlc.GetActiveGuardianAccessParams{ChildPersonID: child, GuardianPersonID: guardian})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIneligible
	}
	return err
}

func (s *Service) Grant(ctx context.Context, child, guardian int32) (dbsqlc.GuardianAccessGrant, error) {
	var zero dbsqlc.GuardianAccessGrant
	actor, err := s.RequireAdministrator(ctx)
	if err != nil {
		return zero, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	if err = s.lockEligible(ctx, tx, child, guardian); err != nil {
		return zero, err
	}
	q := dbsqlc.New(tx)
	grant, err := q.CreateGuardianAccessGrant(ctx, dbsqlc.CreateGuardianAccessGrantParams{ChildPersonID: child, GuardianPersonID: guardian, GrantedByUserID: pgtype.Int4{Int32: actor, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		grant, err = q.GetActiveGuardianAccess(ctx, dbsqlc.GetActiveGuardianAccessParams{ChildPersonID: child, GuardianPersonID: guardian})
	}
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return grant, nil
}
func (s *Service) Revoke(ctx context.Context, child, guardian int32) error {
	return s.revoke(ctx, child, guardian, false)
}

// RemoveRelationship explicitly revokes before deleting the relationship, preserving grants.
func (s *Service) RemoveRelationship(ctx context.Context, child, guardian int32) error {
	return s.revoke(ctx, child, guardian, true)
}
func (s *Service) revoke(ctx context.Context, child, guardian int32, remove bool) error {
	actor, err := s.RequireAdministrator(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	relation, err := q.LockGuardianRelation(ctx, dbsqlc.LockGuardianRelationParams{ChildPersonID: child, GuardianPersonID: guardian})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = q.RevokeGuardianAccessGrant(ctx, dbsqlc.RevokeGuardianAccessGrantParams{ChildPersonID: child, GuardianPersonID: guardian, RevokedByUserID: pgtype.Int4{Int32: actor, Valid: true}})
	if err != nil {
		return err
	}
	if remove {
		if err = q.DeletePersonGuardian(ctx, relation.ID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type RelatedPerson struct {
	PersonID                          int32
	FirstName, LastName, Relationship string
	GrantedAt                         time.Time
}

func (s *Service) ListManagedChildren(ctx context.Context) ([]RelatedPerson, error) {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return nil, authorization.ErrForbidden
	}
	rows, err := dbsqlc.New(s.db).ListManagedChildrenForGuardian(ctx, actor)
	if err != nil {
		return nil, err
	}
	result := []RelatedPerson{}
	for _, r := range rows {
		if s.minor(r.BirthDate) {
			result = append(result, RelatedPerson{r.ID, r.FirstName, r.LastName, r.RelationshipType, r.GrantedAt.Time})
		}
	}
	return result, nil
}
func (s *Service) CanManageChild(ctx context.Context, child int32) (bool, error) {
	if _, ok := auth.UserID(ctx); !ok {
		return false, nil
	}
	children, err := s.ListManagedChildren(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range children {
		if c.PersonID == child {
			return true, nil
		}
	}
	return false, nil
}

// ListActiveGuardiansForChild exposes minimal effective access to the child themself
// or a persons.write administrator. No guardian secrets or account data are returned.
func (s *Service) ListActiveGuardiansForChild(ctx context.Context, child int32) ([]RelatedPerson, error) {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return nil, authorization.ErrForbidden
	}
	participants, err := dbsqlc.New(s.db).ListInteractionParticipants(ctx, []int32{actor})
	if err != nil {
		return nil, err
	}
	if len(participants) != 1 {
		return nil, authorization.ErrForbidden
	}
	if participants[0].PersonID != child {
		if _, err = s.RequireAdministrator(ctx); err != nil {
			return nil, err
		}
	}
	rows, err := dbsqlc.New(s.db).ListActiveGuardiansForChild(ctx, child)
	if err != nil {
		return nil, err
	}
	result := []RelatedPerson{}
	for _, r := range rows {
		if s.minor(r.BirthDate) {
			result = append(result, RelatedPerson{r.ID, r.FirstName, r.LastName, r.RelationshipType, r.GrantedAt.Time})
		}
	}
	return result, nil
}
