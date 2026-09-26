// Package initialsetup owns the one-time takeover of a new installation.
package initialsetup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInitialized = errors.New("installation already configured")
var ErrSecretExists = errors.New("setup secret already issued")
var ErrSecretUnavailable = errors.New("setup secret not issued")
var ErrInvalidSecret = errors.New("invalid setup secret")
var ErrInvalidInput = errors.New("invalid first administrator details")

const initialRole = "president"

type State struct {
	Initialized bool
	Ready       bool
}

type Input struct {
	Secret, FirstName, LastName, Username, Email, Password, Confirmation string
}

type Result struct{ PersonID, UserID int32 }

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

func initialRoleValid() bool {
	for _, name := range authorization.RolesWith(authorization.RolesManage) {
		if name == initialRole {
			return true
		}
	}
	return false
}

func hasManager(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE r.name=ANY($1))`, authorization.RolesWith(authorization.RolesManage)).Scan(&exists)
	return exists, err
}

func (s *Service) Status(ctx context.Context) (State, error) {
	var st State
	var initialized *time.Time
	var hash []byte
	err := s.db.QueryRow(ctx, `SELECT initialized_at,secret_hash FROM installation_setup WHERE id=TRUE`).Scan(&initialized, &hash)
	if err != nil {
		return st, err
	}
	manager, err := hasManager(ctx, s.db)
	if err != nil {
		return st, err
	}
	st.Initialized = initialized != nil || manager
	st.Ready = !st.Initialized && len(hash) == 32
	return st, nil
}

// IssueSecret is an installer-only operation. Rotation invalidates the previous
// code, and both issue and completion lock the same database row.
func (s *Service) IssueSecret(ctx context.Context, rotate bool) (string, error) {
	if !initialRoleValid() {
		return "", ErrInitialized
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var initialized *time.Time
	var oldHash []byte
	err = tx.QueryRow(ctx, `SELECT initialized_at,secret_hash FROM installation_setup WHERE id=TRUE FOR UPDATE`).Scan(&initialized, &oldHash)
	if err != nil {
		return "", err
	}
	manager, err := hasManager(ctx, tx)
	if err != nil {
		return "", err
	}
	if initialized != nil || manager {
		return "", ErrInitialized
	}
	if len(oldHash) > 0 && !rotate {
		return "", ErrSecretExists
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(random[:])
	digest := sha256.Sum256([]byte(secret))
	_, err = tx.Exec(ctx, `UPDATE installation_setup SET secret_hash=$1,secret_issued_at=clock_timestamp(),secret_generation=secret_generation+1 WHERE id=TRUE`, digest[:])
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return secret, nil
}

// WithLocalRoleGrant serializes a privileged CLI grant with web setup. The
// callback must use the supplied transaction for its role write.
func (s *Service) WithLocalRoleGrant(ctx context.Context, grant func(pgx.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var initialized *time.Time
	if err = tx.QueryRow(ctx, `SELECT initialized_at FROM installation_setup WHERE id=TRUE FOR UPDATE`).Scan(&initialized); err != nil {
		return err
	}
	if err = grant(tx); err != nil {
		return err
	}
	if initialized != nil {
		return tx.Commit(ctx)
	}
	manager, err := hasManager(ctx, tx)
	if err != nil {
		return err
	}
	if manager {
		if _, err = tx.Exec(ctx, `UPDATE installation_setup SET initialized_at=clock_timestamp(),secret_hash=NULL,secret_issued_at=NULL WHERE id=TRUE`); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

func validName(value string) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 200 && !strings.ContainsRune(value, 0)
}
func validate(in *Input) error {
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)
	in.Username = strings.TrimSpace(in.Username)
	in.Email = strings.TrimSpace(in.Email)
	in.Secret = strings.TrimSpace(in.Secret)
	if !validName(in.FirstName) || !validName(in.LastName) || !usernamePattern.MatchString(in.Username) || len(in.Email) > 254 || !utf8.ValidString(in.Email) || strings.ContainsAny(in.Email, "\r\n\x00") {
		return ErrInvalidInput
	}
	address, err := mail.ParseAddress(in.Email)
	if err != nil || address.Address != in.Email {
		return ErrInvalidInput
	}
	if in.Password != in.Confirmation || !auth.ValidPassword(in.Password) {
		return ErrInvalidInput
	}
	return nil
}

// Complete checks the secret and absence of a manager under a row lock, then
// creates the normal Person/User/role and consumes the secret in one commit.
func (s *Service) Complete(ctx context.Context, input Input) (Result, error) {
	var result Result
	if err := validate(&input); err != nil {
		return result, err
	}
	if !initialRoleValid() {
		return result, ErrInitialized
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var initialized *time.Time
	var stored []byte
	err = tx.QueryRow(ctx, `SELECT initialized_at,secret_hash FROM installation_setup WHERE id=TRUE FOR UPDATE`).Scan(&initialized, &stored)
	if err != nil {
		return result, err
	}
	manager, err := hasManager(ctx, tx)
	if err != nil {
		return result, err
	}
	if initialized != nil || manager {
		return result, ErrInitialized
	}
	if len(stored) != 32 {
		return result, ErrSecretUnavailable
	}
	digest := sha256.Sum256([]byte(input.Secret))
	if len(input.Secret) != 64 || subtle.ConstantTimeCompare(stored, digest[:]) != 1 {
		return result, ErrInvalidSecret
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		return result, ErrInvalidInput
	}
	err = tx.QueryRow(ctx, `INSERT INTO persons(first_name,last_name,email) VALUES ($1,$2,$3) RETURNING id`, input.FirstName, input.LastName, input.Email).Scan(&result.PersonID)
	if err != nil {
		return Result{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO users(person_id,username,login_email,password_hash,is_active,activated_at) VALUES ($1,$2,$3,$4,TRUE,clock_timestamp()) RETURNING id`, result.PersonID, input.Username, input.Email, string(hash)).Scan(&result.UserID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Result{}, ErrInvalidInput
		}
		return Result{}, err
	}
	command, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role_id) SELECT $1,id FROM roles WHERE name=$2`, result.UserID, initialRole)
	if err != nil {
		return Result{}, err
	}
	if command.RowsAffected() != 1 {
		return Result{}, ErrInitialized
	}
	_, err = tx.Exec(ctx, `UPDATE installation_setup SET initialized_at=clock_timestamp(),first_admin_user_id=$1,secret_hash=NULL,secret_issued_at=NULL WHERE id=TRUE`, result.UserID)
	if err != nil {
		return Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return result, nil
}
