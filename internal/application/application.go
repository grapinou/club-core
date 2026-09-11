// Package application wires HTTP and administrative use cases without exposing
// approval/resend as public endpoints.
package application

import (
	"net/http"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/router"
	"github.com/grapinou/club-core/internal/websecurity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Application struct {
	Handler  http.Handler
	Accounts *accounts.Service
}

func New(cfg config.Config, runtime config.Runtime, db *pgxpool.Pool) (*Application, error) {
	var sender mailer.Mailer = mailer.Disabled{}
	if runtime.EmailTransport == "smtp" {
		smtp, err := mailer.NewSMTP(runtime.SMTP)
		if err != nil {
			return nil, err
		}
		sender = smtp
	}
	return NewWithMailer(cfg, runtime, db, sender)
}

// NewWithMailer allows a local fake in integration tests.
func NewWithMailer(cfg config.Config, runtime config.Runtime, db *pgxpool.Pool, sender mailer.Mailer) (*Application, error) {
	a, err := activation.New(db, runtime.ActivationValidity)
	if err != nil {
		return nil, err
	}
	m, err := memberships.New(db, runtime.ActivationValidity, runtime.Location)
	if err != nil {
		return nil, err
	}
	queries := dbsqlc.New(db)
	login, err := auth.New(queries)
	if err != nil {
		return nil, err
	}
	sessions := auth.NewSessions(runtime.SecureCookies)
	permissions := authorization.New(queries)
	access := handlers.NewAccess(cfg.SiteName, permissions)
	mux := router.New(cfg, queries, access, websecurity.NewCSRF(runtime.SecureCookies))
	handlers.NewAuthHandler(cfg.SiteName, a, login, sessions, runtime.SecureCookies).Register(mux)
	return &Application{
		Handler:  sessions.Middleware(login, access.Navigation(mux)),
		Accounts: accounts.New(db, m, a, sender, runtime.SMTP.From, runtime.BaseURL, permissions),
	}, nil
}
