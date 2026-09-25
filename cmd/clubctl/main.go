package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/grapinou/club-core/internal/clubctl"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/demodata"
	"github.com/grapinou/club-core/internal/organization"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 3 {
		return clubctl.ErrUsage
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL est requis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connexion PostgreSQL impossible")
	}
	defer db.Close()
	if os.Args[1] == "describe-club" {
		if len(os.Args) != 3 {
			return fmt.Errorf("usage: clubctl describe-club <saison>")
		}
		catalogue, err := organization.New(db).Catalogue(ctx, os.Args[2])
		if err != nil {
			return fmt.Errorf("lecture du club ou de la saison impossible")
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(catalogue)
	}
	if os.Args[1] == "upgrade-budokan-demo" {
		if len(os.Args) != 3 || os.Args[2] != "--confirm-demo" {
			return demodata.ErrGuard
		}
		if err := demodata.UpgradeBudokan(ctx, db); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Démonstration Budokan mise à jour sans supprimer les dossiers existants.")
		return nil
	}
	if os.Args[1] == "seed-budokan" {
		if len(os.Args) != 3 || os.Args[2] != "--confirm-empty-demo" {
			return demodata.ErrGuard
		}
		if err := demodata.SeedBudokan(ctx, db, true); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Démonstration Budokan créée : 1 organisation, 1 lieu, 3 activités, 5 groupes, 16 créneaux.")
		return nil
	}
	return clubctl.Run(ctx, dbsqlc.New(db), os.Args[1:], os.Stdout)
}
