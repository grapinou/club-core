// Package registrationapplications stages public membership intent separately
// from declared identity, and finalizes through the existing membership domain.
package registrationapplications

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const PresentationValidity = time.Hour

type Service struct {
	guardians   *guardianaccess.Service
	accounts    *accounts.Service
	db          *pgxpool.Pool
	identities  *identityresolution.Submitter
	memberships *memberships.Service
	key         [32]byte
}

func New(db *pgxpool.Pool, identities *identityresolution.Submitter, m *memberships.Service) (*Service, error) {
	s := &Service{db: db, identities: identities, memberships: m}
	if _, err := rand.Read(s.key[:]); err != nil {
		return nil, err
	}
	return s, nil
}

type Catalog struct {
	Seasons    []dbsqlc.Season
	Types      []dbsqlc.MembershipType
	Activities []dbsqlc.Activity
	Consents   []dbsqlc.ConsentDefinition
}

func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	return catalog(ctx, dbsqlc.New(s.db))
}
func catalog(ctx context.Context, q *dbsqlc.Queries) (c Catalog, err error) {
	if c.Seasons, err = q.ListRegistrationSeasons(ctx); err != nil {
		return
	}
	if c.Types, err = q.ListRegistrationMembershipTypes(ctx); err != nil {
		return
	}
	if c.Activities, err = q.ListRegistrationActivities(ctx); err != nil {
		return
	}
	c.Consents, err = q.ListActiveConsentDefinitions(ctx)
	return
}

// Presentation contains no PII. Its MAC authenticates the exact versions shown,
// binds them to this browser's CSRF cookie, and supplies a retry/idempotency key.
type presentation struct {
	IDs   []int32 `json:"ids"`
	At    int64   `json:"at"`
	Nonce string  `json:"nonce"`
	CSRF  string  `json:"csrf"`
}

func (s *Service) Present(c Catalog, csrf string) (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	p := presentation{At: time.Now().Unix(), Nonce: base64.RawURLEncoding.EncodeToString(nonce), CSRF: csrf, IDs: []int32{}}
	for _, d := range c.Consents {
		p.IDs = append(p.IDs, d.ID)
	}
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *Service) presentation(token, csrf string) (presentation, error) {
	var p presentation
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 6000 {
		return p, identityresolution.ErrInvalidSubmission
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return p, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return p, err
	}
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return p, identityresolution.ErrInvalidSubmission
	}
	if err = json.Unmarshal(body, &p); err != nil {
		return p, err
	}
	now := time.Now().Unix()
	if p.CSRF != csrf || p.At > now || now-p.At > int64(PresentationValidity.Seconds()) || len(p.Nonce) != 43 {
		return p, identityresolution.ErrInvalidSubmission
	}
	return p, nil
}

// PresentedCatalog re-reads immutable wording for authenticated versions, even
// if they were deactivated after display. Injected unpresented IDs cannot enter.
func (s *Service) PresentedCatalog(ctx context.Context, c Catalog, token, csrf string) (Catalog, error) {
	p, err := s.presentation(token, csrf)
	if err != nil {
		return c, err
	}
	c.Consents, err = dbsqlc.New(s.db).ListPresentedRegistrationConsents(ctx, p.IDs)
	if err == nil && len(c.Consents) != len(p.IDs) {
		err = identityresolution.ErrInvalidSubmission
	}
	return c, err
}

type Input struct {
	Child                      *ChildInput
	Identity                   identityresolution.SubmissionInput
	SeasonID, MembershipTypeID int32
	ActivityIDs                []int32
	Consents                   []memberships.Decision
	Presentation               string
}
type ValidationErrors map[string]string

func (e ValidationErrors) Error() string { return "invalid public registration form" }
func (s *Service) validate(ctx context.Context, q *dbsqlc.Queries, in Input, csrf string) (presentation, error) {
	fields := ValidationErrors{}
	in.Identity.FirstName = strings.TrimSpace(in.Identity.FirstName)
	in.Identity.LastName = strings.TrimSpace(in.Identity.LastName)
	if in.Identity.FirstName == "" {
		fields["first_name"] = "Indiquez votre prénom."
	}
	if in.Identity.LastName == "" {
		fields["last_name"] = "Indiquez votre nom."
	}
	for _, key := range identityresolution.InvalidInputFields(in.Identity) {
		if fields[key] == "" {
			fields[key] = "Vérifiez la longueur et le format de ce champ."
		}
	}
	if in.Child != nil {
		s.validateChild(in, fields)
	} else {
		if !in.Identity.BirthDate.Valid || in.Identity.BirthDate.InfinityModifier != pgtype.Finite {
			fields["birth_date"] = "Indiquez une date de naissance valide."
		} else if !s.memberships.IsAdult(in.Identity.BirthDate) {
			fields["birth_date"] = "Ce formulaire est réservé aux personnes majeures. Utilisez le parcours « J’inscris mon enfant »."
		}
		if !validEmail(in.Identity.Email, true) {
			fields["email"] = "Indiquez une adresse email valide."
		}
	}
	c, err := catalog(ctx, q)
	if err != nil {
		return presentation{}, err
	}
	seasonOK, typeOK := false, false
	for _, v := range c.Seasons {
		if v.ID == in.SeasonID {
			seasonOK = true
		}
	}
	for _, v := range c.Types {
		if v.ID == in.MembershipTypeID {
			typeOK = true
		}
	}
	if !seasonOK {
		fields["season_id"] = "Choisissez une saison disponible."
	}
	if !typeOK {
		fields["membership_type_id"] = "Choisissez un type d’adhésion disponible."
	}
	active := map[int32]bool{}
	for _, v := range c.Activities {
		active[v.ID] = true
	}
	seen := map[int32]bool{}
	for _, id := range in.ActivityIDs {
		if !active[id] || seen[id] {
			fields["activities"] = "Choisissez uniquement des activités disponibles, sans doublon."
		}
		seen[id] = true
	}
	if len(in.ActivityIDs) == 0 {
		fields["activities"] = "Choisissez au moins une activité."
	}
	p, err := s.presentation(in.Presentation, csrf)
	if err != nil {
		fields["form"] = "Le formulaire a expiré ou a changé. Relisez les consentements avant de continuer."
	} else {
		defs, e := q.ListPresentedRegistrationConsents(ctx, p.IDs)
		if e != nil {
			return p, e
		}
		expected := map[int32]bool{}
		for _, d := range defs {
			expected[d.ID] = true
		}
		if len(defs) != len(p.IDs) {
			fields["form"] = "Les consentements présentés ne sont plus disponibles."
		}
		answers := map[int32]bool{}
		for _, d := range in.Consents {
			key := consentField(d.ConsentDefinitionID)
			if !expected[d.ConsentDefinitionID] || answers[d.ConsentDefinitionID] {
				fields["consents"] = "Répondez uniquement aux consentements présentés, une fois chacun."
			}
			if d.Decision != "granted" && d.Decision != "refused" {
				fields[key] = "Choisissez « J’accepte » ou « Je refuse »."
			}
			answers[d.ConsentDefinitionID] = true
		}
		for id := range expected {
			if !answers[id] {
				fields[consentField(id)] = "Répondez à ce consentement."
			}
		}
	}
	if len(fields) > 0 {
		return p, fields
	}
	return p, nil
}
func (s *Service) Validate(ctx context.Context, in Input, csrf string) error {
	_, err := s.validate(ctx, dbsqlc.New(s.db), in, csrf)
	return err
}

func (s *Service) Submit(ctx context.Context, in Input, csrf string) (identityresolution.Acceptance, error) {
	zero := identityresolution.Acceptance{}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	p, err := s.presentation(in.Presentation, csrf)
	if err != nil {
		return zero, ValidationErrors{"form": "Le formulaire a expiré ou a changé. Relisez les consentements avant de continuer."}
	}
	key := sha256.Sum256([]byte(p.Nonce))
	// Same browser presentation can be retried safely, including concurrent POSTs.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7212,$1)`, int32(binary.BigEndian.Uint32(key[:4]))); err != nil {
		return zero, err
	}
	if _, err = q.GetRegistrationApplicationByRequest(ctx, key[:]); err == nil {
		return identityresolution.Acceptance{Status: "submission accepted"}, tx.Commit(ctx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}
	if _, err = s.validate(ctx, q, in, csrf); err != nil {
		return zero, err
	}
	in.Identity.FirstName = strings.TrimSpace(in.Identity.FirstName)
	in.Identity.LastName = strings.TrimSpace(in.Identity.LastName)
	for _, v := range []*pgtype.Text{&in.Identity.Email, &in.Identity.PhoneNumber, &in.Identity.Address} {
		v.String = strings.TrimSpace(v.String)
		v.Valid = v.String != ""
	}
	var claim dbsqlc.GuardianIdentityClaim
	submitter := s.identities
	if in.Child != nil {
		claim, err = identityresolution.CreateGuardianClaim(ctx, tx, in.Child.Guardian)
		if err != nil {
			return zero, err
		}
		submitter = identityresolution.NewSubmitter(s.db)
	}
	sub, err := submitter.CreateInTransaction(ctx, tx, in.Identity)
	if err != nil {
		return zero, err
	}
	app, err := q.CreateRegistrationApplication(ctx, dbsqlc.CreateRegistrationApplicationParams{SubmissionID: sub.ID, RequestKey: key[:], SeasonID: in.SeasonID, MembershipTypeID: in.MembershipTypeID})
	if err != nil {
		return zero, err
	}
	if in.Child != nil {
		err = q.CreateChildRegistrationApplication(ctx, dbsqlc.CreateChildRegistrationApplicationParams{ApplicationID: app.ID, GuardianClaimID: claim.ID, RelationshipType: in.Child.RelationshipType, EmergencyContactRequested: in.Child.EmergencyContactRequested})
		if err != nil {
			return zero, err
		}
	}
	for _, id := range in.ActivityIDs {
		if err = q.CreateRegistrationApplicationActivity(ctx, dbsqlc.CreateRegistrationApplicationActivityParams{ApplicationID: app.ID, ActivityID: id}); err != nil {
			return zero, err
		}
	}
	for _, d := range in.Consents {
		if err = q.CreateRegistrationApplicationConsent(ctx, dbsqlc.CreateRegistrationApplicationConsentParams{ApplicationID: app.ID, ConsentDefinitionID: d.ConsentDefinitionID, Decision: d.Decision, PresentedAt: pgtype.Timestamptz{Time: time.Unix(p.At, 0), Valid: true}}); err != nil {
			return zero, err
		}
	}
	candidates, err := q.ListRegistrationCandidates(ctx, sub.ID)
	if err != nil {
		return zero, err
	}
	if len(candidates) == 0 {
		if err = identityresolution.ResolveUnmatchedApplication(ctx, tx, sub.ID); err != nil {
			return zero, err
		}
		// A new Person and membership are one atomic creation, including unexpected
		// SQL failure. Existing identity resolutions use a recoverable savepoint below.
		if err = s.finalize(ctx, tx, sub.ID, in.Child == nil); err != nil {
			return zero, err
		}
	}
	if in.Child != nil && len(candidates) > 0 {
		if err = s.finalize(ctx, tx, sub.ID, false); err != nil {
			return zero, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return identityresolution.Acceptance{Status: "submission accepted"}, nil
}

// Finalize is idempotent and also permits retry of a needs_review application
// after the club restores the availability of its original choices.
func (s *Service) Finalize(ctx context.Context, submissionID int32) (dbsqlc.RegistrationApplication, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dbsqlc.RegistrationApplication{}, err
	}
	defer tx.Rollback(ctx)
	if err = s.FinalizeSubmission(ctx, tx, submissionID); err != nil {
		return dbsqlc.RegistrationApplication{}, err
	}
	a, err := dbsqlc.New(tx).LockRegistrationApplication(ctx, submissionID)
	if err != nil {
		return a, err
	}
	if err = tx.Commit(ctx); err != nil {
		return a, err
	}
	s.AfterResolution(ctx, submissionID)
	return a, nil
}
func (s *Service) FinalizeSubmission(ctx context.Context, tx pgx.Tx, id int32) error {
	if err := identityresolution.LockDeliveryDecision(ctx, tx, id); err != nil {
		return err
	}
	if _, err := dbsqlc.New(tx).LockRegistrationSubmission(ctx, id); err != nil {
		return err
	}
	return s.finalize(ctx, tx, id, false)
}
func (s *Service) finalize(ctx context.Context, tx pgx.Tx, id int32, strict bool) error {
	q := dbsqlc.New(tx)
	a, err := q.LockRegistrationApplication(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.MembershipID.Valid || a.Status == "cancelled" {
		return nil
	}
	sub, err := q.GetRegistrationSubmission(ctx, id)
	if err != nil {
		return err
	}
	child, childErr := q.GetChildRegistrationApplication(ctx, a.ID)
	isChild := childErr == nil
	if childErr != nil && !errors.Is(childErr, pgx.ErrNoRows) {
		return childErr
	}
	giver := sub.ResolvedPersonID.Int32
	if isChild {
		var reason string
		giver, reason, err = s.prepareChild(ctx, tx, a, sub, child, false)
		if err != nil {
			return err
		}
		if reason != "" {
			return q.MarkRegistrationApplicationReview(ctx, dbsqlc.MarkRegistrationApplicationReviewParams{ID: a.ID, LastErrorCode: pgtype.Text{String: reason, Valid: true}})
		}
	} else if sub.Status != "resolved" {
		return nil
	}
	activities, err := q.ListRegistrationApplicationActivities(ctx, a.ID)
	if err != nil {
		return err
	}
	decisions, err := q.ListRegistrationApplicationConsents(ctx, a.ID)
	if err != nil {
		return err
	}
	r := memberships.Request{PersonID: sub.ResolvedPersonID.Int32, SeasonID: a.SeasonID, MembershipTypeID: a.MembershipTypeID}
	presented := make([]memberships.PresentedConsent, 0, len(decisions))
	for _, v := range activities {
		r.ActivityIDs = append(r.ActivityIDs, v.ID)
	}
	for _, d := range decisions {
		r.Consents = append(r.Consents, memberships.Decision{ConsentDefinitionID: d.ConsentDefinitionID, Decision: d.Decision, GivenByPersonID: giver})
		presented = append(presented, memberships.PresentedConsent{ConsentDefinitionID: d.ConsentDefinitionID, PresentedAt: d.PresentedAt})
	}
	// Savepoint preserves a completed identity proof even when membership creation
	// fails (including a concurrent unique person/season conflict).
	nested, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	var birth pgtype.Date
	err = nested.QueryRow(ctx, `SELECT birth_date FROM persons WHERE id=$1 FOR NO KEY UPDATE`, r.PersonID).Scan(&birth)
	reason := ""
	if err == nil && !isChild && !s.memberships.IsAdult(birth) {
		reason = "member_not_adult"
	}
	var m dbsqlc.Membership
	if err == nil && reason == "" {
		m, err = s.memberships.CreateRequestWithPresentedConsentsTx(ctx, nested, r, presented)
		if err == nil {
			err = dbsqlc.New(nested).MarkRegistrationApplicationCreated(ctx, dbsqlc.MarkRegistrationApplicationCreatedParams{ID: a.ID, MembershipID: pgtype.Int4{Int32: m.ID, Valid: true}})
		}
	}
	if err != nil || reason != "" {
		if rollbackErr := nested.Rollback(ctx); rollbackErr != nil {
			return rollbackErr
		}
		if strict {
			if err != nil {
				return err
			}
			return memberships.ErrInvalidRequest
		}
		if reason == "" {
			reason = "membership_unavailable"
			var pgerr *pgconn.PgError
			if errors.As(err, &pgerr) && pgerr.Code == "23505" {
				reason = "membership_already_exists"
			} else if errors.Is(err, memberships.ErrInvalidRequest) || errors.Is(err, pgx.ErrNoRows) {
				reason = "choices_unavailable"
			}
		}
		return q.MarkRegistrationApplicationReview(ctx, dbsqlc.MarkRegistrationApplicationReviewParams{ID: a.ID, LastErrorCode: pgtype.Text{String: reason, Valid: true}})
	}
	return nested.Commit(ctx)
}

func consentField(id int32) string { return "consent_" + strconv.FormatInt(int64(id), 10) }
