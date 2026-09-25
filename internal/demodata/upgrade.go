package demodata

import (
	"context"
	_ "embed"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed budokan_upgrade.sql
var budokanUpgradeSQL string

// UpgradeBudokan updates only known demonstration reference rows. Existing
// people, trials, memberships and custom non-empty editorial fields are kept.
func UpgradeBudokan(ctx context.Context, db *pgxpool.Pool) error {
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
	var match bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM organizations WHERE is_active AND name='Budokan Sud Oise' AND public_email='budokansud.oise@gmail.com') AND (SELECT count(*) FROM organizations)=1").Scan(&match); err != nil {
		return err
	}
	if !match {
		return ErrGuard
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(260914)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, budokanUpgradeSQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
