package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"database/sql"

	"github.com/grapinou/club-core/internal/clubctl"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/demodata"
	"github.com/grapinou/club-core/internal/initialsetup"
	"github.com/grapinou/club-core/internal/organization"
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
	databasePath := os.Getenv("DATABASE_PATH")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.New(ctx, databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = database.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrations SQLite : %w", err)
	}
	if os.Args[1] == "migrate" {
		return nil
	}
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
	// Local bootstrap also handles a demo file already created by migrate.
	if os.Args[1] == "prepare-budokan-demo" {
		if len(os.Args) != 3 || os.Args[2] != "--confirm-demo" {
			return demodata.ErrGuard
		}
		err := demodata.SeedBudokan(ctx, db, true)
		if errors.Is(err, demodata.ErrNotEmpty) {
			err = demodata.UpgradeBudokan(ctx, db)
		}
		return err
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
		if err := initialsetup.New(db).WithLocalRoleGrant(ctx, func(tx *sql.Tx) error {
			return clubctl.Run(ctx, dbsqlc.New(tx), os.Args[1:], &output)
		}); err != nil {
			return err
		}
		_, err := output.WriteTo(os.Stdout)
		return err
	}
	return clubctl.Run(ctx, dbsqlc.New(db), os.Args[1:], os.Stdout)
}
