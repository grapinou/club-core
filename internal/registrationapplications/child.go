package registrationapplications

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type ChildInput struct {
	Guardian                  identityresolution.SubmissionInput
	RelationshipType          string
	EmergencyContactRequested bool
}

func (s *Service) SetGuardianServices(g *guardianaccess.Service, a *accounts.Service) {
	s.guardians = g
	s.accounts = a
}
func validEmail(e pgtype.Text, required bool) bool {
	value := strings.TrimSpace(e.String)
	if !e.Valid || value == "" {
		return !required
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && !strings.ContainsAny(value, "\r\n")
}
func (s *Service) validateChild(in Input, fields ValidationErrors) {
	if !s.memberships.IsEligibleMinor(in.Identity.BirthDate) {
		fields["birth_date"] = "Indiquez une date de naissance valide pour un enfant mineur. À partir de 18 ans, utilisez /join pour vous inscrire."
	}
	if !validEmail(in.Identity.Email, false) {
		fields["email"] = "Indiquez une adresse email valide ou laissez ce champ vide."
	}
	for _, key := range identityresolution.InvalidInputFields(in.Child.Guardian) {
		fields["guardian_"+key] = "Vérifiez ce champ."
	}
	if !validEmail(in.Child.Guardian.Email, true) {
		fields["guardian_email"] = "Indiquez votre adresse email valide."
	}
	switch in.Child.RelationshipType {
	case "mother", "father", "guardian", "other":
	default:
		fields["relationship_type"] = "Choisissez votre lien avec l’enfant."
	}
}

// prepareChild holds the resolved Persons stable, preserves existing family
// data, and uses the guardian access domain for grants. No SMTP occurs here.
func (s *Service) prepareChild(ctx context.Context, tx pgx.Tx, a dbsqlc.RegistrationApplication, sub dbsqlc.GetRegistrationSubmissionRow, c dbsqlc.ChildRegistrationApplication, confirm bool) (int32, string, error) {
	q := dbsqlc.New(tx)
	g, err := q.LockGuardianIdentityClaim(ctx, c.GuardianClaimID)
	if err != nil {
		return 0, "", err
	}
	if sub.Status != "resolved" {
		return 0, "child_identity_review", nil
	}
	if g.Status != "resolved" {
		return 0, "guardian_identity_review", nil
	}
	child, guardian := sub.ResolvedPersonID.Int32, g.ResolvedPersonID.Int32
	if child == guardian {
		return 0, "guardian_relation_invalid", nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM persons WHERE id=ANY($1::integer[]) ORDER BY id FOR UPDATE`, []int32{child, guardian})
	if err != nil {
		return 0, "", err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, "", err
	}
	var birth pgtype.Date
	var archived bool
	err = tx.QueryRow(ctx, `SELECT c.birth_date,c.archived_at IS NOT NULL OR g.archived_at IS NOT NULL FROM persons c JOIN persons g ON g.id=$2 WHERE c.id=$1`, child, guardian).Scan(&birth, &archived)
	if err != nil {
		return 0, "", err
	}
	if !s.memberships.IsEligibleMinor(birth) {
		return 0, "member_not_minor", nil
	}
	if archived {
		return 0, "guardian_relation_invalid", nil
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM person_guardians WHERE child_person_id=$1 AND guardian_person_id=$2)`, child, guardian).Scan(&exists); err != nil {
		return 0, "", err
	}
	if !exists && !confirm {
		return 0, "guardian_confirmation_required", nil
	}
	if !exists {
		_, err = tx.Exec(ctx, `INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact) SELECT $1,$2,$3,NOT EXISTS(SELECT 1 FROM person_guardians WHERE child_person_id=$1 AND is_primary_contact) ON CONFLICT(child_person_id,guardian_person_id) DO NOTHING`, child, guardian, c.RelationshipType)
		if err != nil {
			return 0, "", err
		}
	}
	if s.guardians == nil {
		return 0, "", guardianaccess.ErrIneligible
	}
	if _, err = s.guardians.GrantTx(ctx, tx, child, guardian); err != nil {
		return 0, "", err
	}
	if c.EmergencyContactRequested {
		_, err = tx.Exec(ctx, `INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority) SELECT $1,$2,$3,COALESCE(MAX(priority),0)+1 FROM person_emergency_contacts WHERE person_id=$1 ON CONFLICT(person_id,contact_person_id) DO NOTHING`, child, guardian, c.RelationshipType)
		if err != nil {
			return 0, "", err
		}
	}
	actor, _ := auth.UserID(ctx)
	if err = q.ConfirmChildRegistrationGuardian(ctx, dbsqlc.ConfirmChildRegistrationGuardianParams{ApplicationID: a.ID, GuardianConfirmedByUserID: pgtype.Int4{Int32: actor, Valid: confirm}}); err != nil {
		return 0, "", err
	}
	return guardian, "", nil
}

func (s *Service) ConfirmGuardian(ctx context.Context, id int32) error {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return authorization.ErrForbidden
	}
	if s.guardians == nil {
		return guardianaccess.ErrIneligible
	}
	if _, err := s.guardians.RequireAdministrator(ctx); err != nil {
		return err
	}
	allowed, err := authorization.New(dbsqlc.New(s.db)).HasPermission(ctx, actor, authorization.RegistrationsReview)
	if err != nil {
		return err
	}
	if !allowed {
		return authorization.ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = identityresolution.LockDeliveryDecision(ctx, tx, id); err != nil {
		return err
	}
	q := dbsqlc.New(tx)
	if _, err = q.LockRegistrationSubmission(ctx, id); err != nil {
		return err
	}
	a, err := q.LockRegistrationApplication(ctx, id)
	if err != nil {
		return err
	}
	if a.Status == "cancelled" {
		return identityresolution.ErrClosed
	}
	if !a.MembershipID.Valid {
		sub, e := q.GetRegistrationSubmission(ctx, id)
		if e != nil {
			return e
		}
		c, e := q.GetChildRegistrationApplication(ctx, a.ID)
		if e != nil {
			return e
		}
		_, reason, e := s.prepareChild(ctx, tx, a, sub, c, true)
		if e != nil {
			return e
		}
		if reason != "" {
			return identityresolution.ErrUnavailable
		}
		if err = s.finalize(ctx, tx, id, false); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.AfterResolution(ctx, id)
	return nil
}

// AfterResolution is deliberately after the business commit. Existing activation
// tooling can retry failures; neither SMTP nor provisioning can undo membership.
func (s *Service) AfterResolution(ctx context.Context, id int32) {
	if s.accounts == nil {
		return
	}
	var child, guardian int32
	err := s.db.QueryRow(ctx, `SELECT s.resolved_person_id,g.resolved_person_id FROM registration_applications a JOIN registration_submissions s ON s.id=a.submission_id JOIN child_registration_applications c ON c.application_id=a.id JOIN guardian_identity_claims g ON g.id=c.guardian_claim_id WHERE s.id=$1 AND c.guardian_confirmed_at IS NOT NULL`, id).Scan(&child, &guardian)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err == nil {
		_, err = s.accounts.EnsureGuardianUser(ctx, child, guardian, accounts.KeepPendingActivation)
	}
	if err != nil {
		slog.ErrorContext(ctx, "guardian activation requires administrative retry", "submission_id", id)
	}
}

// RetryGuardianActivation is an explicit administrative retry, preserving the
// existing account activation semantics and guardian's own delivery address.
func (s *Service) RetryGuardianActivation(ctx context.Context, id int32) error {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return authorization.ErrForbidden
	}
	allowed, err := authorization.New(dbsqlc.New(s.db)).HasPermission(ctx, actor, authorization.RegistrationsReview)
	if err != nil {
		return err
	}
	if !allowed {
		return authorization.ErrForbidden
	}
	var child, guardian int32
	err = s.db.QueryRow(ctx, `SELECT s.resolved_person_id,g.resolved_person_id FROM registration_applications a JOIN registration_submissions s ON s.id=a.submission_id JOIN child_registration_applications c ON c.application_id=a.id JOIN guardian_identity_claims g ON g.id=c.guardian_claim_id WHERE s.id=$1 AND c.guardian_confirmed_at IS NOT NULL`, id).Scan(&child, &guardian)
	if err != nil {
		return err
	}
	_, err = s.accounts.EnsureGuardianUser(ctx, child, guardian)
	return err
}
