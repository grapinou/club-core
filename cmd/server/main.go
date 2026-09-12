package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grapinou/club-core/internal/application"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database"
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

	runtime, err := config.LoadRuntime()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	app, err := application.New(cfg, runtime, db)
	if err != nil {
		log.Fatal("impossible d'initialiser l'application")
	}

	log.Println("Serveur lancé sur http://localhost:8080")

	address := ":8080"
	if !runtime.SecureCookies {
		address = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: address, Handler: app.Handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); _ = app.VerificationOutbox.Run(ctx) }()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Print("HTTP server stopped unexpectedly")
		}
		stop()
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
	}
	select {
	case <-workerDone:
	case <-shutdown.Done():
		log.Print("outbox shutdown deadline reached")
	}

}
