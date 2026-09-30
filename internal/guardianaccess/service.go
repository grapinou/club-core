// Package guardianaccess authorizes an authenticated guardian on a specific child.
// A family relationship alone grants no digital access. Administrative RBAC is separate.
package guardianaccess

import (
	"context"
	"errors"
	"time"

	"database/sql"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

var ErrIneligible = errors.New("guardian access unavailable")

type PermissionChecker interface {
	HasPermission(context.Context, int32, authorization.Permission) (bool, error)
}
type Service struct {
	db          *sql.DB
	permissions PermissionChecker
	location    *time.Location
	now         func() time.Time
}
type Option func(*Service)

func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

func New(db *sql.DB, p PermissionChecker, location *time.Location, options ...Option) *Service {
	if location == nil {
		panic("guardian access requires business location")
	}
	s := &Service{db: db, permissions: p, location: location, now: time.Now}
	for _, option := range options {
		option(s)
	}
	return s
}
func (s *Service) minor(birth dbtypes.Date) bool {
	return birth.Valid && birth.IsFinite() && civildate.IsMinor(birth.Time, s.now().In(s.location))
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

// lockEligible checks Persons and the relationship in an IMMEDIATE transaction,
// serializing provisioning with approval. Grants need not have an active User.
func (s *Service) lockEligible(ctx context.Context, tx *sql.Tx, child, guardian int32) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM persons WHERE id IN (SELECT value FROM json_each(?1)) ORDER BY id", dbtypes.IDs{child, guardian})
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
	if errors.Is(err, sql.ErrNoRows) {
		return ErrIneligible
	}
	if err != nil {
		return err
	}
	var birth dbtypes.Date
	err = tx.QueryRowContext(ctx, "SELECT c.birth_date FROM persons c JOIN persons g ON g.id=?2 WHERE c.id=?1 AND c.archived_at IS NULL AND g.archived_at IS NULL", child, guardian).Scan(&birth)
	if errors.Is(err, sql.ErrNoRows) {
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
func (s *Service) AuthorizeProvisionTx(ctx context.Context, tx *sql.Tx, child, guardian int32) error {
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
	if errors.Is(err, sql.ErrNoRows) {
		return ErrIneligible
	}
	return err
}

func (s *Service) Grant(ctx context.Context, child, guardian int32) (dbsqlc.GuardianAccessGrant, error) {
	var zero dbsqlc.GuardianAccessGrant
	_, err := s.RequireAdministrator(ctx)
	if err != nil {
		return zero, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	grant, err := s.GrantTx(ctx, tx, child, guardian)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := dbsqlc.New(tx)
	relation, err := q.LockGuardianRelation(ctx, dbsqlc.LockGuardianRelationParams{ChildPersonID: child, GuardianPersonID: guardian})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = q.RevokeGuardianAccessGrant(ctx, dbsqlc.RevokeGuardianAccessGrantParams{ChildPersonID: child, GuardianPersonID: guardian, RevokedByUserID: sql.NullInt32{Int32: actor, Valid: true}})
	if err != nil {
		return err
	}
	if remove {
		if err = q.DeletePersonGuardian(ctx, relation.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
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

// GrantTx is the transactional variant of Grant. It preserves actor, permission,
// relation and minority checks and never treats primary contact as digital access.
func (s *Service) GrantTx(ctx context.Context, tx *sql.Tx, child, guardian int32) (dbsqlc.GuardianAccessGrant, error) {
	q := dbsqlc.New(tx)
	actor, err := requireAdministrator(ctx, q, authorization.New(q))
	if err != nil {
		return dbsqlc.GuardianAccessGrant{}, err
	}
	if err = s.lockEligible(ctx, tx, child, guardian); err != nil {
		return dbsqlc.GuardianAccessGrant{}, err
	}
	grant, err := q.CreateGuardianAccessGrant(ctx, dbsqlc.CreateGuardianAccessGrantParams{ChildPersonID: child, GuardianPersonID: guardian, GrantedByUserID: sql.NullInt32{Int32: actor, Valid: true}})
	if errors.Is(err, sql.ErrNoRows) {
		return q.GetActiveGuardianAccess(ctx, dbsqlc.GetActiveGuardianAccessParams{ChildPersonID: child, GuardianPersonID: guardian})
	}
	return grant, err
}

// AuthorizeManagedChildTx keeps existing effective access stable for a family
// membership request. It does not create a relationship or grant any access.
func (s *Service) AuthorizeManagedChildTx(ctx context.Context, tx *sql.Tx, child int32) (int32, error) {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return 0, ErrIneligible
	}
	q := dbsqlc.New(tx)
	account, err := q.GetPersonalAccount(ctx, actor)
	if err != nil {
		return 0, ErrIneligible
	}
	if err = s.lockEligible(ctx, tx, child, account.PersonID); err != nil {
		return 0, err
	}
	var id int32
	err = tx.QueryRowContext(ctx, `SELECT id FROM guardian_access_grants WHERE child_person_id=?1 AND guardian_person_id=?2 AND revoked_at IS NULL`, child, account.PersonID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrIneligible
	}
	return account.PersonID, err
}
