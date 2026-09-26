package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/clubctl"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/demodata"
	"github.com/grapinou/club-core/internal/initialsetup"
	"github.com/grapinou/club-core/internal/organization"
	"github.com/jackc/pgx/v5"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
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
	if os.Args[1] == "setup-secret" {
		if len(os.Args) != 2 && !(len(os.Args) == 3 && os.Args[2] == "--rotate") {
			return fmt.Errorf("usage: clubctl setup-secret [--rotate]")
		}
		secret, err := initialsetup.New(db).IssueSecret(ctx, len(os.Args) == 3)
		if err != nil {
			return fmt.Errorf("code de configuration indisponible : %w", err)
		}
		url := os.Getenv("APP_BASE_URL")
		if url == "" {
			url = "http://localhost:8080"
		}
		url = strings.TrimSuffix(url, "/")
		fmt.Fprintln(os.Stdout, "Club Core : configuration initiale")
		fmt.Fprintln(os.Stdout, "Code de configuration (affiché une seule fois) :", secret)
		fmt.Fprintln(os.Stdout, "Ouvrez :", url+"/setup")
		return nil
	}
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
	if os.Args[1] == "grant-role" {
		var output bytes.Buffer
		if err := initialsetup.New(db).WithLocalRoleGrant(ctx, func(tx pgx.Tx) error {
			return clubctl.Run(ctx, dbsqlc.New(tx), os.Args[1:], &output)
		}); err != nil {
			return err
		}
		_, err := output.WriteTo(os.Stdout)
		return err
	}
	return clubctl.Run(ctx, dbsqlc.New(db), os.Args[1:], os.Stdout)
}
