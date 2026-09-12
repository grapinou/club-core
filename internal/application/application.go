// Package application wires public HTTP and permission-protected administrative use cases.
package application

import (
	"net/http"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/minorsafety"
	"github.com/grapinou/club-core/internal/outbox"
	"github.com/grapinou/club-core/internal/registrationapplications"
	"github.com/grapinou/club-core/internal/router"
	"github.com/grapinou/club-core/internal/websecurity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Application struct {
	GuardianAccess           *guardianaccess.Service
	MinorSafety              *minorsafety.Service
	Handler                  http.Handler
	Accounts                 *accounts.Service
	Submissions              *identityresolution.Submitter
	Reviews                  *identityresolution.ReviewService
	Verifications            *identityresolution.EmailService
	VerificationOutbox       *outbox.Worker
	SubmissionLimiter        *handlers.AttemptLimiter
	RegistrationApplications *registrationapplications.Service
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
	verification, err := identityresolution.NewEmailService(db, runtime.RegistrationVerificationTTL)
	if err != nil {
		return nil, err
	}
	m, err := memberships.New(db, runtime.ActivationValidity, runtime.Location)
	if err != nil {
		return nil, err
	}
	submissions := identityresolution.NewEmailSubmitter(db, verification)
	applications, err := registrationapplications.New(db, submissions, m)
	if err != nil {
		return nil, err
	}
	verification.SetFinalizer(applications)
	submissionLimiter := handlers.NewRegistrationSubmissionLimiter()
	queries := dbsqlc.New(db)
	login, err := auth.New(queries)
	if err != nil {
		return nil, err
	}
	sessions := auth.NewSessions(runtime.SecureCookies)
	permissions := authorization.New(queries)
	access := handlers.NewAccess(cfg.SiteName, permissions)
	reviews := identityresolution.NewReviewService(db, permissions)
	reviews.SetFinalizer(applications)
	access.SetRegistrationCounter(reviews)
	csrf := websecurity.NewCSRF(runtime.SecureCookies)
	mux := router.New(cfg, queries, access, csrf)
	handlers.NewJoinHandler(cfg.SiteName, applications, submissionLimiter).Register(mux, csrf)
	guardians := guardianaccess.New(db, permissions, runtime.Location)
	accountService := accounts.New(db, m, a, sender, runtime.SMTP.From, runtime.BaseURL, permissions)
	accountService.SetGuardianAccess(guardians)
	applications.SetGuardianServices(guardians, accountService)
	handlers.NewMembershipHandler(cfg.SiteName, runtime.Location, m, accountService, queries, permissions).Register(mux, access, csrf)
	handlers.NewRegistrationHandler(cfg.SiteName, runtime.Location, reviews, applications).Register(mux, access, csrf)
	authHandler := handlers.NewAuthHandler(cfg.SiteName, a, login, sessions, runtime.SecureCookies)
	authHandler.Register(mux)
	authHandler.RegisterRegistrationVerification(mux, verification)
	return &Application{
		GuardianAccess:           guardians,
		MinorSafety:              minorsafety.New(db, runtime.Location),
		Handler:                  sessions.Middleware(login, access.Navigation(mux)),
		Accounts:                 accountService,
		Submissions:              submissions,
		Reviews:                  reviews,
		Verifications:            verification,
		VerificationOutbox:       outbox.New(db, verification, sender, runtime.SMTP.From, runtime.BaseURL),
		SubmissionLimiter:        submissionLimiter,
		RegistrationApplications: applications,
	}, nil
}
