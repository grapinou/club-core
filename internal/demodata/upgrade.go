package demodata

import (
	"context"
	_ "embed"
	"path/filepath"
	"strings"

	"database/sql"
)

//go:embed budokan_upgrade.sql
var budokanUpgradeSQL string

// UpgradeBudokan updates only known demonstration reference rows. Existing
// people, trials, memberships and custom non-empty editorial fields are kept.
func UpgradeBudokan(ctx context.Context, db *sql.DB) error {
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
	var match bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM organizations WHERE is_active AND name='Budokan Sud Oise' AND public_email='budokansud.oise@gmail.com') AND (SELECT count(*) FROM organizations)=1").Scan(&match); err != nil {
		return err
	}
	if !match {
		return ErrGuard
	}
	if _, err = tx.ExecContext(ctx, budokanUpgradeSQL); err != nil {
		return err
	}
	return tx.Commit()
}
