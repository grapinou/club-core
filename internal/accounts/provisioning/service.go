// Package provisioning owns durable User creation, independently of Membership.
// These transaction primitives are for authorized backend workflows, not handlers.
package provisioning

import (
	"context"
	"errors"
	"strconv"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
)

// EnsureUserForPersonTx serializes on Person, reuses existing users unchanged,
// and preserves username collision handling. It does not activate or reactivate.
func EnsureUserForPersonTx(ctx context.Context, tx pgx.Tx, person int32) (dbsqlc.User, error) {
	var zero dbsqlc.User
	q := dbsqlc.New(tx)
	names, err := q.LockAccountPerson(ctx, person)
	if err != nil {
		return zero, err
	}
	user, err := q.GetUserByPerson(ctx, person)
	if !errors.Is(err, pgx.ErrNoRows) {
		return user, err
	}
	base := UsernameBase(names.FirstName, names.LastName)
	for suffix := 1; ; suffix++ {
		username := base
		if suffix > 1 {
			username += strconv.Itoa(suffix)
		}
		_, err = q.CreateUserForPersonUsername(ctx, dbsqlc.CreateUserForPersonUsernameParams{PersonID: person, Username: username})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return zero, err
		}
		return q.GetUserByPerson(ctx, person)
	}
}
