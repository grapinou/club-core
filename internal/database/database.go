package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func New(
	ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf(
			"créer le pool PostgreSQL : %w",
			err,
		)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf(
			"tester la connexion PostgreSQL : %w",
			err,
		)
	}

	return pool, nil
}
