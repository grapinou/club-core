// Package demodata contains versioned, opt-in demonstration data, never startup configuration.
package demodata

import (
	"context"
	_ "embed"
	"errors"
	"path/filepath"
	"strings"

	"database/sql"
)

//go:embed budokan.sql
var budokanSQL string

var ErrGuard = errors.New("seed refusé : confirmation explicite et base portant le suffixe _demo requises")
var ErrNotEmpty = errors.New("seed refusé : la base contient déjà des données métier")

// SeedBudokan accepts only an explicitly designated, migrated, empty demo DB.
// It never resets or upserts existing business data. Repetition returns ErrNotEmpty.
func SeedBudokan(ctx context.Context, db *sql.DB, confirmed bool) error {
	if !confirmed {
		return ErrGuard
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var seq int
	var alias, name string
	if err = tx.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &alias, &name); err != nil {
		return err
	}
	if !strings.HasSuffix(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)), "_demo") {
		return ErrGuard
	}
	// BEGIN IMMEDIATE serializes the emptiness check and the complete seed.
	rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('goose_db_version','roles','installation_setup') ORDER BY name")
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
		identifier := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		var exists bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+identifier+")").Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrNotEmpty
		}
	}
	if _, err = tx.ExecContext(ctx, budokanSQL); err != nil {
		return err
	}
	return tx.Commit()
}
