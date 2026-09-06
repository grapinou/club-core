package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/database"
	"github.com/grapinou/club-manager/internal/database/dbsqlc"
	"github.com/grapinou/club-manager/internal/router"
)

const configPath = "config/config.json"

func main() {

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("impossible de charger la configuration depuis %q : %v",
			configPath,
			err,
		)
	}

	ctx := context.Background()

	db, err := database.New(
		ctx, os.Getenv("DATABASE_URL"),
	)
	if err != nil {
		log.Fatalf(
			"impossible de créer le pool PostgreSQL : %v",
			err,
		)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf(
			"impossible de se connecter à PostgreSQL : %v",
			err,
		)
	}

	log.Println("Connexion à PostgreSQL établie")

	queries := dbsqlc.New(db)

	mux := router.New(cfg, queries)

	log.Println("Serveur lancé sur http://localhost:8080")

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}

}
