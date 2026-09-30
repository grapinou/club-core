// Package useraccess manages the existing users, roles and user_roles tables.
package useraccess

import (
	"context"
	"errors"
	"strings"

	"github.com/grapinou/club-core/internal/database/dbtypes"

	"database/sql"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
)

var ErrInvalid = errors.New("invalid user or role")
var ErrLastManager = errors.New("last access manager")

type User struct {
	ID                                   int32
	PersonID                             int32
	FirstName, LastName, Username, Email string
	Active, Activated                    bool
	Roles                                dbtypes.Strings
}

func (u User) HasRole(name string) bool {
	for _, role := range u.Roles {
		if role == name {
			return true
		}
	}
	return false
}

type Service struct {
	db          *sql.DB
	permissions *authorization.Service
}

func New(db *sql.DB, permissions *authorization.Service) *Service {
	return &Service{db: db, permissions: permissions}
}

func (s *Service) require(ctx context.Context, permission authorization.Permission) (int32, error) {
	id, ok := auth.UserID(ctx)
	if !ok {
		return 0, authorization.ErrForbidden
	}
	var eligible bool
	if err := s.db.QueryRowContext(ctx, `SELECT is_active AND activated_at IS NOT NULL AND password_hash IS NOT NULL FROM users WHERE id=?1`, id).Scan(&eligible); err != nil || !eligible {
		return 0, authorization.ErrForbidden
	}
	allowed, err := s.permissions.HasPermission(ctx, id, permission)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, authorization.ErrForbidden
	}
	return id, nil
}

func (s *Service) List(ctx context.Context, search string, page int32) ([]User, error) {
	if _, err := s.require(ctx, authorization.RolesRead); err != nil {
		return nil, err
	}
	search = strings.TrimSpace(search)
	if len(search) > 120 || page < 0 || page > 10000 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.person_id,p.first_name,p.last_name,u.username,coalesce(u.login_email,''),u.is_active,u.activated_at IS NOT NULL,
	 coalesce(json_group_array(r.name ORDER BY r.name) FILTER (WHERE r.name IS NOT NULL), json_array())
	 FROM users u JOIN persons p ON p.id=u.person_id
	 LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id
	 WHERE ?1='' OR unicode_lower(p.first_name) LIKE '%'||unicode_lower(?1)||'%' OR unicode_lower(p.last_name) LIKE '%'||unicode_lower(?1)||'%' OR unicode_lower(u.username) LIKE '%'||unicode_lower(?1)||'%' OR unicode_lower(u.login_email) LIKE '%'||unicode_lower(?1)||'%'
	 GROUP BY u.id,p.id ORDER BY p.last_name,p.first_name,u.id LIMIT 51 OFFSET ?2`, search, page*50)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.PersonID, &u.FirstName, &u.LastName, &u.Username, &u.Email, &u.Active, &u.Activated, &u.Roles); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Service) Get(ctx context.Context, id int32) (User, error) {
	var u User
	if _, err := s.require(ctx, authorization.RolesRead); err != nil {
		return u, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.person_id,p.first_name,p.last_name,u.username,coalesce(u.login_email,''),u.is_active,u.activated_at IS NOT NULL,
	 coalesce(json_group_array(r.name ORDER BY r.name) FILTER (WHERE r.name IS NOT NULL), json_array())
	 FROM users u JOIN persons p ON p.id=u.person_id
	 LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id
	 WHERE u.id=?1 GROUP BY u.id,p.id`, id).Scan(&u.ID, &u.PersonID, &u.FirstName, &u.LastName, &u.Username, &u.Email, &u.Active, &u.Activated, &u.Roles)
	return u, err
}

// Change serializes all web role mutations and checks the actor in the same
// transaction. Advisory locking prevents two removals from both seeing a
// second manager. Eligibility is counted from current account state.
func (s *Service) Change(ctx context.Context, target int32, role string, grant bool) error {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return authorization.ErrForbidden
	}
	if target <= 0 || !authorization.KnownRole(role) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var eligible bool
	err = tx.QueryRowContext(ctx, `SELECT is_active AND activated_at IS NOT NULL AND password_hash IS NOT NULL FROM users WHERE id=?1`, actor).Scan(&eligible)
	if err != nil || !eligible {
		return authorization.ErrForbidden
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=?1 AND r.name IN (SELECT value FROM json_each(?2)))`, actor, dbtypes.Strings(authorization.RolesWith(authorization.RolesManage))).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return authorization.ErrForbidden
	}
	var roleID int32
	err = tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name=?1`, role).Scan(&roleID)
	if err != nil {
		return err
	}
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=?1)`, target).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	var changed int64
	if grant {
		result, err := tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES (?1,?2) ON CONFLICT DO NOTHING`, target, roleID)
		if err != nil {
			return err
		}
		changed, err = result.RowsAffected()
	} else {
		result, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=?1 AND role_id=?2`, target, roleID)
		if err != nil {
			return err
		}
		changed, err = result.RowsAffected()
	}
	if changed == 0 {
		return tx.Commit()
	}
	if !grant {
		var count int64
		err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT u.id) FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id
		 WHERE u.is_active AND u.activated_at IS NOT NULL AND u.password_hash IS NOT NULL AND r.name IN (SELECT value FROM json_each(?1))`, dbtypes.Strings(authorization.RolesWith(authorization.RolesManage))).Scan(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrLastManager
		}
	}
	action := "role_revoked"
	if grant {
		action = "role_granted"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO administrative_events(actor_user_id,action,resource_type,resource_id,role_name) VALUES (?1,?2,'user',?3,?4)`, actor, action, target, role)
	if err != nil {
		return err
	}
	return tx.Commit()
}
