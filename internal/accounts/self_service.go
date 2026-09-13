package accounts

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/mailer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrSelfServiceUnavailable = errors.New("account unavailable")
var ErrEmailChangeLimited = errors.New("email change request limit reached")
var ErrEmailChangeDelivery = errors.New("email change delivery unavailable")

type AccountFields map[string]string

func (e AccountFields) Error() string { return "invalid account fields" }

type SelfService struct {
	db   *pgxpool.Pool
	mail mailer.Mailer
	from string
	ttl  time.Duration
}

func NewSelfService(db *pgxpool.Pool, sender mailer.Mailer, from string, ttl time.Duration) *SelfService {
	if ttl <= 0 {
		panic("email change TTL must be positive")
	}
	return &SelfService{db: db, mail: sender, from: from, ttl: ttl}
}
func optionalContact(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != ""}
}

// The only selector is the authenticated context, never a browser-supplied ID.
func (s *SelfService) locked(ctx context.Context) (pgx.Tx, *dbsqlc.Queries, dbsqlc.LockSelfServiceAccountRow, error) {
	var zero dbsqlc.LockSelfServiceAccountRow
	id, ok := auth.UserID(ctx)
	if !ok {
		return nil, nil, zero, ErrSelfServiceUnavailable
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, nil, zero, err
	}
	q := dbsqlc.New(tx)
	u, err := q.LockSelfServiceAccount(ctx, id)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrSelfServiceUnavailable
		}
		return nil, nil, zero, err
	}
	return tx, q, u, nil
}
func accountEvent(ctx context.Context, q *dbsqlc.Queries, u dbsqlc.LockSelfServiceAccountRow, event string) error {
	return q.CreateAccountSecurityEvent(ctx, dbsqlc.CreateAccountSecurityEventParams{UserID: u.ID, PersonID: u.PersonID, Event: event})
}
func checkCurrent(u dbsqlc.LockSelfServiceAccountRow, password string) error {
	if len(password) > 72 || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash.String), []byte(password)) != nil {
		return AccountFields{"current_password": "Le mot de passe actuel est incorrect."}
	}
	return nil
}
func (s *SelfService) UpdateContact(ctx context.Context, phone, address string) error {
	p, a := optionalContact(phone), optionalContact(address)
	fields := AccountFields{}
	// Reuse the existing contact size/encoding validation and phone normalization.
	for _, key := range identityresolution.InvalidInputFields(identityresolution.SubmissionInput{FirstName: "valid", LastName: "valid", PhoneNumber: p, Address: a}) {
		fields[key] = "Cette valeur est invalide ou trop longue."
	}
	if p.Valid {
		p.String = identityresolution.NormalizePhone(p.String)
		if p.String == "" {
			fields["phone_number"] = "Saisissez un numéro de téléphone valide."
		}
	}
	if len(fields) > 0 {
		return fields
	}
	tx, q, u, err := s.locked(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = q.UpdateSelfServiceContact(ctx, dbsqlc.UpdateSelfServiceContactParams{ID: u.PersonID, PhoneNumber: p, Address: a}); err != nil {
		return err
	}
	if err = accountEvent(ctx, q, u, "profile_contact_updated"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *SelfService) RequestEmail(ctx context.Context, email, password string) error {
	email = strings.TrimSpace(email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || strings.ContainsAny(email, "\r\n") || len(email) > 254 {
		return AccountFields{"new_email": "Saisissez une adresse email valide."}
	}
	tx, q, u, err := s.locked(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = checkCurrent(u, password); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(u.Email.String), email) {
		return AccountFields{"new_email": "Saisissez une adresse différente de votre adresse actuelle."}
	}
	count, err := q.CountRecentEmailChanges(ctx, u.ID)
	if err != nil {
		return err
	}
	if count >= 3 {
		return ErrEmailChangeLimited
	}
	code, err := activation.GenerateCode()
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(code))
	normalized := strings.ToLower(email)
	emailHash := sha256.Sum256([]byte(normalized))
	if err = q.InvalidateEmailChanges(ctx, u.ID); err != nil {
		return err
	}
	if err = q.CreateEmailChange(ctx, dbsqlc.CreateEmailChangeParams{UserID: u.ID, PersonID: u.PersonID, NewEmail: email, NewEmailNormalized: normalized, NewEmailHash: emailHash[:], CodeHash: digest[:], TtlSeconds: s.ttl.Seconds()}); err != nil {
		return err
	}
	if err = accountEvent(ctx, q, u, "email_change_requested"); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	// Delivery follows commit. Never expose its error, recipient or plaintext code.
	if err = s.mail.Send(ctx, mailer.Message{From: s.from, To: email, Subject: "Vérifiez votre nouvelle adresse email", Text: "Code de vérification : " + code + "\nSaisissez ce code dans votre espace personnel Club Core. Si vous n’êtes pas à l’origine de cette demande, ignorez ce message."}); err != nil {
		return ErrEmailChangeDelivery
	}
	return nil
}
func (s *SelfService) VerifyEmail(ctx context.Context, code string) error {
	invalid := AccountFields{"code": "Ce code est incorrect, expiré ou déjà utilisé."}
	tx, q, u, err := s.locked(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	req, err := q.LockActiveEmailChange(ctx, dbsqlc.LockActiveEmailChangeParams{UserID: u.ID, PersonID: u.PersonID})
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid
	}
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(code)))
	if subtle.ConstantTimeCompare(hash[:], req.CodeHash) != 1 {
		return invalid
	}
	n, err := q.ConsumeEmailChange(ctx, req.ID)
	if err != nil {
		return err
	}
	if n != 1 {
		return invalid
	}
	if err = q.UpdateSelfServiceEmail(ctx, dbsqlc.UpdateSelfServiceEmailParams{ID: u.PersonID, Email: optionalContact(req.NewEmail)}); err != nil {
		return err
	}
	if err = accountEvent(ctx, q, u, "email_changed"); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	// Best-effort security notification; never roll back or expose delivery details.
	if u.Email.Valid {
		_ = s.mail.Send(ctx, mailer.Message{From: s.from, To: u.Email.String, Subject: "Votre adresse email Club Core a été modifiée", Text: "Votre adresse email Club Core a été modifiée. Si vous n’êtes pas à l’origine de cette opération, contactez le club."})
	}
	return nil
}

// ChangePassword rechecks the old bcrypt hash under the User lock. The result is
// only a credential fingerprint for session binding, never an HTML view model.
func (s *SelfService) ChangePassword(ctx context.Context, current, password, confirmation string) ([32]byte, error) {
	var zero [32]byte
	fields := AccountFields{}
	if !auth.ValidPassword(password) {
		fields["new_password"] = "Le mot de passe doit contenir entre 12 et 72 octets."
	}
	if password != confirmation {
		fields["confirmation"] = "La confirmation ne correspond pas au nouveau mot de passe."
	}
	if len(fields) > 0 {
		return zero, fields
	}
	tx, q, u, err := s.locked(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	if err = checkCurrent(u, current); err != nil {
		return zero, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash.String), []byte(password)) == nil {
		return zero, AccountFields{"new_password": "Choisissez un mot de passe différent du mot de passe actuel."}
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return zero, err
	}
	if err = q.UpdateSelfServicePassword(ctx, dbsqlc.UpdateSelfServicePasswordParams{ID: u.ID, PasswordHash: pgtype.Text{String: string(hash), Valid: true}}); err != nil {
		return zero, err
	}
	// A pending channel replacement must not survive a password security change.
	if err = q.InvalidateEmailChanges(ctx, u.ID); err != nil {
		return zero, err
	}
	if err = accountEvent(ctx, q, u, "password_changed"); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return sha256.Sum256(hash), nil
}
