package database

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// newTestDatabase fournit une base isolée et migrée, nettoyée à la fin du test.
func newTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("club_manager_test"),
		postgres.WithUsername("club_manager"),
		postgres.WithPassword("test_password"),
		postgres.BasicWaitStrategies(),
	)
	// Enregistrer le nettoyage même si le démarrage échoue partiellement.
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("démarrage de PostgreSQL impossible : %v", err)
	}

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("récupération de l'URL PostgreSQL impossible : %v", err)
	}

	migrationDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("ouverture de la connexion de migration impossible : %v", err)
	}
	defer migrationDB.Close()

	// go test exécute les tests depuis le répertoire du package.
	provider, err := goose.NewProvider(goose.DialectPostgres, migrationDB, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatalf("initialisation des migrations impossible : %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("application des migrations impossible : %v", err)
	}

	db, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connexion à PostgreSQL impossible : %v", err)
	}
	t.Cleanup(db.Close)
	return db
}
