// Package demodata contains versioned, opt-in demonstration data, never startup configuration.
package demodata

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed budokan.sql
var budokanSQL string

var ErrGuard = errors.New("seed refusé : confirmation explicite et base portant le suffixe _demo requises")
var ErrNotEmpty = errors.New("seed refusé : la base contient déjà des données métier")

// SeedBudokan accepts only an explicitly designated, migrated, empty demo DB.
// It never resets or upserts existing business data. Repetition returns ErrNotEmpty.
func SeedBudokan(ctx context.Context, db *pgxpool.Pool, confirmed bool) error {
	if !confirmed {
		return ErrGuard
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var name string
	if err = tx.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return err
	}
	if !strings.HasSuffix(name, "_demo") {
		return ErrGuard
	}
	// Serialize concurrent seeds, then lock every application table against writes.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(260914)"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT IN ('goose_db_version','roles','installation_setup') ORDER BY tablename")
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, table := range tables {
		identifier := pgx.Identifier{"public", table}.Sanitize()
		if _, err = tx.Exec(ctx, "LOCK TABLE "+identifier+" IN EXCLUSIVE MODE"); err != nil {
			return err
		}
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+identifier+")").Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrNotEmpty
		}
	}
	if _, err = tx.Exec(ctx, budokanSQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
