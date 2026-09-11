// Package clubctl implements local role administration using the operator's
// database credentials. It is not exposed through HTTP.
package clubctl

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
)

var ErrUsage = errors.New("usage: clubctl grant-role <username> <role> | revoke-role <username> <role> | list-roles <username>")
var ErrUnknownUser = errors.New("identifiant inconnu")
var ErrUnknownRole = errors.New("rôle inconnu")

type Queries interface {
	GetUserByUsername(context.Context, string) (dbsqlc.User, error)
	GetRoleByName(context.Context, string) (dbsqlc.Role, error)
	AssignUserRole(context.Context, dbsqlc.AssignUserRoleParams) error
	RevokeUserRole(context.Context, dbsqlc.RevokeUserRoleParams) error
	ListUserRoles(context.Context, int32) ([]dbsqlc.Role, error)
}

func Run(ctx context.Context, q Queries, args []string, out io.Writer) error {
	if len(args) < 2 {
		return ErrUsage
	}
	command := args[0]
	if (command == "list-roles" && len(args) != 2) || ((command == "grant-role" || command == "revoke-role") && len(args) != 3) || (command != "list-roles" && command != "grant-role" && command != "revoke-role") {
		return ErrUsage
	}
	user, err := q.GetUserByUsername(ctx, args[1])
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnknownUser
	}
	if err != nil {
		return errors.New("lecture du compte impossible")
	}
	if command == "list-roles" {
		roles, err := q.ListUserRoles(ctx, user.ID)
		if err != nil {
			return errors.New("lecture des rôles impossible")
		}
		if len(roles) == 0 {
			_, err = fmt.Fprintln(out, "Aucun rôle attribué.")
			return err
		}
		for _, r := range roles {
			if _, err = fmt.Fprintln(out, r.Name); err != nil {
				return err
			}
		}
		return nil
	}
	if !authorization.KnownRole(args[2]) {
		return ErrUnknownRole
	}
	role, err := q.GetRoleByName(ctx, args[2])
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnknownRole
	}
	if err != nil {
		return errors.New("lecture du rôle impossible")
	}
	message := "Rôle attribué (ou déjà présent)."
	if command == "grant-role" {
		err = q.AssignUserRole(ctx, dbsqlc.AssignUserRoleParams{UserID: user.ID, RoleID: role.ID})
	} else {
		err = q.RevokeUserRole(ctx, dbsqlc.RevokeUserRoleParams{UserID: user.ID, RoleID: role.ID})
		message = "Rôle retiré (ou déjà absent)."
	}
	if err != nil {
		return errors.New("modification du rôle impossible")
	}
	_, err = fmt.Fprintln(out, message)
	return err
}
