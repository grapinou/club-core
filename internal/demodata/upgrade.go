package demodata

import (
	"context"
	_ "embed"

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
	if err = checkDemoDB(ctx, tx); err != nil {
		return err
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
