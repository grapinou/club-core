// multi-user is local campaign tooling, never wired into the production server.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/grapinou/club-core/internal/accounts/provisioning"
	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/application"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/clubctl"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/moby/moby/api/types/container"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const root = "runtime/multi-user"
const input = root + "/identities.json"
const mailpitImage = "axllent/mailpit@sha256:e22dce5b36f93c77082e204a3942fb6b283b7896e057458400a4c88344c3df68"

type identities struct {
	Emails map[string]string
	// An explicit local file setting is required to use an external SMTP transport.
	SMTP *mailer.SMTPConfig
}
type account struct {
	Person, User       int32
	Username, Password string
}
type state struct {
	Database, Run, BaseURL, Mailbox                                                                                   string
	Accounts                                                                                                          map[string]account
	Child, Season, NextSeason, Kind, Activity, Group, Slot, Trial, MembershipA, MembershipC, MembershipChild, Consent int32
	Today                                                                                                             string
}

func privateJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
func readPrivate(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("fichier local requis absent")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("le fichier local doit être régulier et privé (chmod 600)")
	}
	f, err := os.Open(path)
	if err != nil {
		return errors.New("lecture locale impossible")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64*1024))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return errors.New("fichier JSON local invalide")
	}
	return nil
}
func loadIdentities() (identities, error) {
	var v identities
	if err := readPrivate(input, &v); err != nil {
		return v, err
	}
	for _, alias := range []string{"A", "B", "C"} {
		s := v.Emails[alias]
		a, err := mail.ParseAddress(s)
		if err != nil || a.Address != s || strings.ContainsAny(s, "\r\n") || len(s) > 254 {
			return v, fmt.Errorf("adresse de User %s absente ou invalide", alias)
		}
	}
	if v.SMTP != nil {
		if err := v.SMTP.Validate(); err != nil {
			return v, errors.New("configuration SMTP locale invalide")
		}
	}
	return v, nil
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	if len(args) == 1 && args[0] == "init" {
		if err := os.MkdirAll(root, 0700); err != nil {
			return errors.New("création du répertoire local impossible")
		}
		if err := privateJSON(input, identities{Emails: map[string]string{"A": "user-a@example.test", "B": "user-b@example.test", "C": "user-c@example.test"}}); err != nil {
			return errors.New("fichier local déjà présent ou création impossible ; aucun écrasement")
		}
		fmt.Println("Identités fictives créées dans", input, "; permissions 600. Modifier localement si souhaité.")
		return nil
	}
	if len(args) == 1 && args[0] == "start" {
		v, err := loadIdentities()
		if err != nil {
			return err
		}
		return start(ctx, v)
	}
	if len(args) == 2 {
		switch args[0] {
		case "grant", "revoke", "grant-role", "revoke-role", "audit":
			return control(ctx, args[0], args[1])
		}
	}
	return errors.New("usage depuis la racine : multi-user init | start | grant/revoke/grant-role/revoke-role/audit <state.json>")
}
func loopback(h *container.HostConfig) {
	for port, bindings := range h.PortBindings {
		for i := range bindings {
			bindings[i].HostIP = netip.MustParseAddr("127.0.0.1")
		}
		h.PortBindings[port] = bindings
	}
}
func stage(label string, err error) error {
	if err != nil {
		return fmt.Errorf("campagne : %s impossible (détails sensibles masqués)", label)
	}
	return nil
}
func start(ctx context.Context, ids identities) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return stage("répertoire", err)
	}
	dir, err := os.MkdirTemp(root, "run-")
	if err != nil {
		return stage("répertoire", err)
	}
	runID, err := auth.RandomToken()
	if err != nil {
		return stage("aléa", err)
	}
	dbPassword, err := auth.RandomToken()
	if err != nil {
		return stage("aléa", err)
	}
	logger := log.New(io.Discard, "", 0)
	pg, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("club_campaign"), postgres.WithUsername("club"), postgres.WithPassword(dbPassword), postgres.BasicWaitStrategies(), testcontainers.WithHostConfigModifier(loopback), testcontainers.WithLogger(logger))
	if err != nil {
		return stage("PostgreSQL jetable", err)
	}
	defer pg.Terminate(context.Background())
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return stage("connexion isolée", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return stage("pool", err)
	}
	defer pool.Close()
	migrationDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return stage("migration", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, migrationDB, os.DirFS("migrations"))
	if err == nil {
		_, err = provider.Up(ctx)
	}
	migrationDB.Close()
	if err != nil {
		return stage("migration", err)
	}
	_, err = pool.Exec(ctx, "CREATE TABLE campaign_fixture_guard(run_id TEXT PRIMARY KEY)")
	if err == nil {
		_, err = pool.Exec(ctx, "INSERT INTO campaign_fixture_guard VALUES($1)", runID)
	}
	if err != nil {
		return stage("marqueur isolé", err)
	}
	smtp := ids.SMTP
	mailbox := ""
	if smtp == nil {
		mp, e := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: mailpitImage, ExposedPorts: []string{"1025/tcp", "8025/tcp"}, HostConfigModifier: loopback, WaitingFor: wait.ForListeningPort("1025/tcp")}, Started: true, Logger: logger})
		if e != nil {
			return stage("boîte SMTP locale", e)
		}
		defer mp.Terminate(context.Background())
		port, e := mp.MappedPort(ctx, "1025/tcp")
		if e != nil {
			return stage("port SMTP", e)
		}
		ui, e := mp.MappedPort(ctx, "8025/tcp")
		if e != nil {
			return stage("port boîte mail", e)
		}
		smtp = &mailer.SMTPConfig{Host: "127.0.0.1", Port: int(port.Num()), From: "club@example.test"}
		mailbox = "http://127.0.0.1:" + ui.Port()
	}
	sender, err := mailer.NewSMTP(*smtp)
	if err != nil {
		return stage("transport", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8090")
	if err != nil {
		return errors.New("port local 8090 occupé ; aucun serveur existant arrêté")
	}
	defer listener.Close()
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return stage("fuseau", err)
	}
	runtime := config.Runtime{BaseURL: "http://localhost:8090", Location: loc, ActivationValidity: time.Hour, RegistrationVerificationTTL: time.Hour, SMTP: *smtp}
	app, err := application.NewWithMailer(config.Config{SiteName: "Club Core — Campagne"}, runtime, pool, sender)
	if err != nil {
		return stage("application", err)
	}
	s := state{Database: dsn, Run: runID, BaseURL: runtime.BaseURL, Mailbox: mailbox, Accounts: map[string]account{}}
	if err = seed(ctx, pool, app, loc, ids, &s); err != nil {
		return stage("jeu de données", err)
	}
	statePath := filepath.Join(dir, "state.json")
	if err = privateJSON(statePath, s); err != nil {
		return stage("état privé", err)
	}
	fmt.Println("Campagne disponible :", s.BaseURL)
	fmt.Println("État et accès privés :", statePath)
	if mailbox != "" {
		fmt.Println("Boîte SMTP locale :", mailbox)
	}
	fmt.Println("Trois profils navigateur distincts. Ctrl+C arrête le serveur et détruit uniquement ses conteneurs jetables.")
	server := &http.Server{Handler: app.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); _ = app.VerificationOutbox.Run(workerCtx) }()
	select {
	case <-ctx.Done():
	case <-done:
	}
	cancel()
	shutdown, finish := context.WithTimeout(context.Background(), 10*time.Second)
	defer finish()
	_ = server.Shutdown(shutdown)
	_ = server.Close()
	<-workerDone
	return nil
}
func seed(ctx context.Context, db *pgxpool.Pool, app *application.Application, loc *time.Location, ids identities, s *state) error {
	id := func(query string, args ...any) (int32, error) {
		var n int32
		err := db.QueryRow(ctx, query, args...).Scan(&n)
		return n, err
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	s.Today = today.Format("2006-01-02")
	year := today.Year()
	if today.Month() < 9 {
		year--
	}
	begin := time.Date(year, 9, 1, 0, 0, 0, 0, time.UTC)
	end := begin.AddDate(1, 0, -1)
	var err error
	for _, alias := range []string{"A", "B", "C"} {
		person, e := id("INSERT INTO persons(first_name,last_name,birth_date,email) VALUES('User',$1,$2,$3) RETURNING id", alias, today.AddDate(-35, 0, 0), ids.Emails[alias])
		if e != nil {
			return e
		}
		tx, e := db.Begin(ctx)
		if e != nil {
			return e
		}
		u, e := provisioning.EnsureUserForPersonTx(ctx, tx, person)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		activationService, e := activation.New(db, time.Hour)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		delivery, e := activationService.PrepareTx(ctx, tx, u.ID)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		password, e := auth.RandomToken()
		if e != nil {
			return e
		}
		if _, e = activationService.Activate(ctx, u.Username, delivery.PlaintextCode, password); e != nil {
			return e
		}
		s.Accounts[alias] = account{person, u.ID, u.Username, password}
	}
	q := dbsqlc.New(db)
	if err = clubctl.Run(ctx, q, []string{"grant-role", s.Accounts["B"].Username, "secretary"}, io.Discard); err != nil {
		return err
	}
	if s.Child, err = id("INSERT INTO persons(first_name,last_name,birth_date) VALUES('Enfant','Campagne',$1) RETURNING id", today.AddDate(-10, 0, 0)); err != nil {
		return err
	}
	if _, err = db.Exec(ctx, "INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) VALUES($1,$2,'guardian',true)", s.Child, s.Accounts["C"].Person); err != nil {
		return err
	}
	if _, err = db.Exec(ctx, "INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority) VALUES($1,$2,1)", s.Child, s.Accounts["C"].Person); err != nil {
		return err
	}
	if s.Season, err = id("INSERT INTO seasons(name,starts_at,ends_at) VALUES('Saison campagne',$1,$2) RETURNING id", begin, end); err != nil {
		return err
	}
	if s.NextSeason, err = id("INSERT INTO seasons(name,starts_at,ends_at) VALUES('Saison suivante',$1,$2) RETURNING id", begin.AddDate(1, 0, 0), end.AddDate(1, 0, 0)); err != nil {
		return err
	}
	if s.Activity, err = id("INSERT INTO activities(name) VALUES('Pratique campagne') RETURNING id"); err != nil {
		return err
	}
	if s.Kind, err = id("INSERT INTO membership_types(name) VALUES('Adhésion campagne') RETURNING id"); err != nil {
		return err
	}
	if s.Group, err = id("INSERT INTO groups(activity_id,name) VALUES($1,'Groupe campagne') RETURNING id", s.Activity); err != nil {
		return err
	}
	weekday := int(today.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	if s.Slot, err = id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location,valid_from,valid_until) VALUES($1,$2,$3,'18:30','20:00','Salle de campagne',$4,$5) RETURNING id", s.Group, s.Season, weekday, begin, end); err != nil {
		return err
	}
	if s.Consent, err = id("INSERT INTO consent_definitions(code,version,title,description) VALUES('campaign_photo',1,'Photographies','Décision facultative pour la campagne.') RETURNING id"); err != nil {
		return err
	}
	m, err := memberships.New(db, time.Hour, loc)
	if err != nil {
		return err
	}
	for _, alias := range []string{"A", "C", "child"} {
		person, giver := s.Accounts[alias].Person, s.Accounts[alias].Person
		if alias == "child" {
			person = s.Child
			giver = s.Accounts["C"].Person
		}
		membership, e := m.CreateRequest(ctx, memberships.Request{PersonID: person, SeasonID: s.Season, MembershipTypeID: s.Kind, ActivityIDs: []int32{s.Activity}, Consents: []memberships.Decision{{ConsentDefinitionID: s.Consent, GivenByPersonID: giver, Decision: "refused"}}})
		if e != nil {
			return e
		}
		switch alias {
		case "A":
			s.MembershipA = membership.ID
		case "C":
			s.MembershipC = membership.ID
		case "child":
			s.MembershipChild = membership.ID
		}
	}
	if _, err = app.Accounts.ApproveMembership(ctx, s.MembershipC, s.Accounts["B"].User, nil); err != nil {
		return err
	}
	trial, err := trials.New(db).Schedule(ctx, dbsqlc.CreateTrialParams{PersonID: s.Accounts["A"].Person, ActivityID: s.Activity, GroupID: pgtype.Int4{Int32: s.Group, Valid: true}, GroupSlotID: pgtype.Int4{Int32: s.Slot, Valid: true}, TrialDate: pgtype.Date{Time: today, Valid: true}})
	if err != nil {
		return err
	}
	s.Trial = trial.ID
	return nil
}

// Local domain commands authenticate B using B's own current password. They do
// not create a login-as endpoint or alter any of the three browser sessions.
func actorContext(ctx context.Context, db *pgxpool.Pool, b account) (context.Context, error) {
	login, err := auth.New(dbsqlc.New(db))
	if err != nil {
		return nil, err
	}
	user, credential, err := login.AuthenticateSession(ctx, b.Username, b.Password)
	if err != nil {
		return nil, err
	}
	sessions := auth.NewSessions(false)
	r := httptest.NewRequest("GET", "http://localhost/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	if err = sessions.CreateAuthenticated(w, r, user, credential); err != nil {
		return nil, err
	}
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	var result context.Context
	sessions.Middleware(login, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { result = r.Context() })).ServeHTTP(httptest.NewRecorder(), r)
	if result == nil {
		return nil, errors.New("authentification indisponible")
	}
	return result, nil
}
func control(ctx context.Context, command, path string) error {
	var s state
	if err := readPrivate(path, &s); err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig(s.Database)
	if err != nil {
		return stage("état isolé", err)
	}
	host := cfg.ConnConfig.Host
	if (host != "localhost" && !net.ParseIP(host).IsLoopback()) || cfg.ConnConfig.Database != "club_campaign" {
		return errors.New("connexion hors campagne refusée")
	}
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return stage("connexion campagne", err)
	}
	defer db.Close()
	var match bool
	if err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM campaign_fixture_guard WHERE run_id=$1)", s.Run).Scan(&match); err != nil || !match {
		return errors.New("base de campagne absente ou différente")
	}
	q := dbsqlc.New(db)
	switch command {
	case "grant-role", "revoke-role":
		return clubctl.Run(ctx, q, []string{command, s.Accounts["B"].Username, "secretary"}, os.Stdout)
	case "grant", "revoke":
		actor, e := actorContext(ctx, db, s.Accounts["B"])
		if e != nil {
			return stage("authentification User B", e)
		}
		loc, e := time.LoadLocation("Europe/Paris")
		if e != nil {
			return e
		}
		g := guardianaccess.New(db, authorization.New(q), loc)
		if command == "grant" {
			_, err = g.Grant(actor, s.Child, s.Accounts["C"].Person)
		} else {
			err = g.Revoke(actor, s.Child, s.Accounts["C"].Person)
		}
		if err != nil {
			return stage("décision GuardianAccess", err)
		}
		fmt.Println("Décision GuardianAccess enregistrée pour User C.")
	case "audit":
		var result []byte
		err = db.QueryRow(ctx, `SELECT jsonb_build_object(
   'persons',(SELECT count(*) FROM persons),
   'users',(SELECT count(*) FROM users),
   'child_users',(SELECT count(*) FROM users WHERE person_id=$1),
   'trial_status',(SELECT status FROM trial_registrations WHERE id=$2),
   'events',coalesce((SELECT jsonb_agg(jsonb_build_object('actor',actor_user_id,'action',action,'resource',resource_type,'id',resource_id,'at',created_at) ORDER BY id) FROM administrative_events),'[]'::jsonb))`, s.Child, s.Trial).Scan(&result)
		if err != nil {
			return stage("audit", err)
		}
		fmt.Println(string(result))
	}
	return nil
}
