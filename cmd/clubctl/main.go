package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/grapinou/club-core/internal/clubctl"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
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
	return clubctl.Run(ctx, dbsqlc.New(db), os.Args[1:], os.Stdout)
}
