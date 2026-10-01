package demodata

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CheckDemoPath rejects defaults, links and ambiguous/non-demo filenames before
// a CLI opens (and potentially migrates) a database. This is a development guard,
// not proof that a deliberately renamed production database contains fake data.
func CheckDemoPath(path string) error {
	if strings.HasSuffix(path, string(filepath.Separator)) || strings.ContainsAny(path, ":?\r\n") {
		return ErrGuard
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if strings.HasSuffix(base, "_demo") {
		ext = ""
	}
	if ext != "" && ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
		return ErrGuard
	}
	if !strings.HasSuffix(strings.TrimSuffix(base, ext), "_demo") {
		return ErrGuard
	}
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !info.Mode().IsRegular() {
		return ErrGuard
	}
	return nil
}

func checkDemoDB(ctx context.Context, tx *sql.Tx) error {
	var seq int
	var alias, path string
	if err := tx.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &alias, &path); err != nil {
		return err
	}
	return CheckDemoPath(path)
}

// Every non-reference table must be empty, including future business tables.
// The three office accounts are checked separately, never treated as members.
func checkNoBusinessData(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table'
 AND name NOT LIKE 'sqlite_%' AND name NOT IN (
 'goose_db_version','installation_setup','roles','persons','users','user_roles',
 'organizations','organization_links','organization_public_images','locations',
 'seasons','activities','groups','group_slots','membership_types','membership_type_groups','consent_definitions')`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, table := range tables {
		var exists bool
		identifier := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+identifier+")").Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrNotEmpty
		}
	}
	return nil
}
