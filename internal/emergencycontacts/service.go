// Package emergencycontacts manages Person relationships without creating digital rights.
package emergencycontacts

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/identityresolution"
)

var ErrUnavailable = errors.New("emergency contact unavailable")

type Fields map[string]string

func (Fields) Error() string { return "invalid emergency contact" }

type Input struct {
	FirstName, LastName, Phone, Email, Relationship string
	Priority                                        int32
}

func Validate(in Input) (Input, error) {
	in.FirstName, in.LastName = strings.TrimSpace(in.FirstName), strings.TrimSpace(in.LastName)
	in.Phone, in.Email, in.Relationship = strings.TrimSpace(in.Phone), strings.TrimSpace(in.Email), strings.TrimSpace(in.Relationship)
	fields := Fields{}
	for _, key := range identityresolution.InvalidInputFields(identityresolution.SubmissionInput{FirstName: in.FirstName, LastName: in.LastName, PhoneNumber: nullable(in.Phone), Email: nullable(in.Email)}) {
		fields[key] = "Vérifiez ce champ."
	}
	for key, value := range map[string]string{"first_name": in.FirstName, "last_name": in.LastName, "phone_number": in.Phone, "relationship": in.Relationship} {
		if value == "" {
			fields[key] = "Ce champ est obligatoire."
		}
	}
	if !utf8.ValidString(in.Relationship) || utf8.RuneCountInString(in.Relationship) > 200 || strings.ContainsAny(in.Relationship, "\x00\r\n") {
		fields["relationship"] = "Indiquez un lien de 200 caractères maximum."
	}
	if in.Email != "" {
		a, e := mail.ParseAddress(in.Email)
		if e != nil || a.Address != in.Email || strings.ContainsAny(in.Email, "\r\n") {
			fields["email"] = "Indiquez un email valide ou laissez ce champ vide."
		}
	}
	if in.Phone != "" {
		in.Phone = identityresolution.NormalizePhone(in.Phone)
		if in.Phone == "" {
			fields["phone_number"] = "Indiquez un téléphone valide."
		}
	}
	if len(fields) > 0 {
		return in, fields
	}
	return in, nil
}
func nullable(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }

// CreateTx creates a dedicated contact Person. No matching, User or family grant.
func CreateTx(ctx context.Context, tx *sql.Tx, owner int32, in Input) (int32, error) {
	in, err := Validate(in)
	if err != nil {
		return 0, err
	}
	var id int32
	err = tx.QueryRowContext(ctx, `INSERT INTO persons(first_name,last_name,phone_number,email) VALUES(?1,?2,?3,?4) RETURNING id`, in.FirstName, in.LastName, in.Phone, nullable(in.Email)).Scan(&id)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority) SELECT ?1,?2,?3,coalesce(max(priority),0)+1 FROM person_emergency_contacts WHERE person_id=?1`, owner, id, in.Relationship)
	return id, err
}

type Service struct {
	db        *sql.DB
	guardians *guardianaccess.Service
	location  *time.Location
}

func New(db *sql.DB, g *guardianaccess.Service, location *time.Location) *Service {
	if location == nil {
		panic("emergency contacts require business location")
	}
	return &Service{db, g, location}
}

type Scope struct {
	Office bool
	Child  int32
} // Self-service has no caller-selected owner.
type Contact struct {
	dbsqlc.ListPersonEmergencyContactsRow
	CanEditCoordinates bool
}
type Page struct {
	PersonID int32
	Name     string
	Contacts []Contact
}

func (s *Service) authorize(ctx context.Context, tx *sql.Tx, scope Scope) (owner, actor int32, err error) {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return 0, 0, ErrUnavailable
	}
	q := dbsqlc.New(tx)
	a, e := q.GetPersonalAccount(ctx, actor)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, 0, ErrUnavailable
	}
	if e != nil {
		return 0, 0, e
	}
	if scope.Office {
		allowed, e := authorization.New(q).HasPermission(ctx, actor, authorization.PersonsWrite)
		if e != nil {
			return 0, 0, e
		}
		if !allowed {
			return 0, 0, authorization.ErrForbidden
		}
		owner = scope.Child
	} else if scope.Child != 0 {
		if s.guardians == nil {
			return 0, 0, ErrUnavailable
		}
		if _, e = s.guardians.AuthorizeManagedChildTx(ctx, tx, scope.Child); e != nil {
			if errors.Is(e, guardianaccess.ErrIneligible) || errors.Is(e, authorization.ErrForbidden) {
				return 0, 0, ErrUnavailable
			}
			return 0, 0, e
		}
		owner = scope.Child
	} else {
		// Minor accounts use their guardian for emergency administration.
		if a.BirthDate.Valid && civildate.IsMinor(a.BirthDate.Time, time.Now().In(s.location)) {
			return 0, 0, ErrUnavailable
		}
		owner = a.PersonID
	}
	if _, e = q.LockAdministrativePerson(ctx, owner); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return 0, 0, ErrUnavailable
		}
		return 0, 0, e
	}
	return owner, actor, nil
}
func exclusive(ctx context.Context, tx *sql.Tx, owner, contact int32) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM users WHERE person_id=?2)
 AND NOT EXISTS(SELECT 1 FROM person_guardians WHERE child_person_id=?2 OR guardian_person_id=?2)
 AND NOT EXISTS(SELECT 1 FROM memberships WHERE person_id=?2)
 AND NOT EXISTS(SELECT 1 FROM trial_registrations WHERE person_id=?2)
 AND NOT EXISTS(SELECT 1 FROM person_emergency_contacts WHERE contact_person_id=?2 AND person_id<>?1)
 AND NOT EXISTS(SELECT 1 FROM person_emergency_contacts WHERE person_id=?2)
 AND NOT EXISTS(SELECT 1 FROM registration_submissions WHERE resolved_person_id=?2)
 AND NOT EXISTS(SELECT 1 FROM guardian_identity_claims WHERE resolved_person_id=?2)`, owner, contact).Scan(&ok)
	return ok, err
}
func (s *Service) Page(ctx context.Context, scope Scope) (Page, error) {
	var p Page
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	owner, _, err := s.authorize(ctx, tx, scope)
	if err != nil {
		return p, err
	}
	person, err := dbsqlc.New(tx).GetPersonByID(ctx, owner)
	if err != nil {
		return p, err
	}
	p.PersonID, p.Name = owner, person.FirstName+" "+person.LastName
	contacts, err := dbsqlc.New(tx).ListPersonEmergencyContacts(ctx, owner)
	if err != nil {
		return p, err
	}
	for _, c := range contacts {
		editable := scope.Office
		if !editable {
			editable, err = exclusive(ctx, tx, owner, c.ContactPersonID)
			if err != nil {
				return p, err
			}
		}
		p.Contacts = append(p.Contacts, Contact{c, editable})
	}
	return p, tx.Commit()
}
func (s *Service) Mutate(ctx context.Context, scope Scope, action string, relation int32, in Input) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	owner, actor, err := s.authorize(ctx, tx, scope)
	if err != nil {
		return err
	}
	if action == "add" {
		_, err = CreateTx(ctx, tx, owner, in)
	} else {
		var contact int32
		err = tx.QueryRowContext(ctx, `SELECT contact_person_id FROM person_emergency_contacts WHERE id=?1 AND person_id=?2`, relation, owner).Scan(&contact)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnavailable
		}
		if err != nil {
			return err
		}
		switch action {
		case "remove":
			_, err = tx.ExecContext(ctx, `DELETE FROM person_emergency_contacts WHERE id=?1 AND person_id=?2`, relation, owner)
			if err == nil {
				rows, e := dbsqlc.New(tx).ListPersonEmergencyContacts(ctx, owner)
				err = e
				if err == nil && len(rows) > 0 {
					err = reorder(ctx, tx, owner, rows[0].EmergencyContactID, 1)
				}
			}
		case "update":
			editable := scope.Office
			if !editable {
				editable, err = exclusive(ctx, tx, owner, contact)
				if err != nil {
					return err
				}
			}
			if !editable { // Never edit another account or shared Person through this relation.
				p, e := dbsqlc.New(tx).GetPersonByID(ctx, contact)
				if e != nil {
					return e
				}
				if in.FirstName != p.FirstName || in.LastName != p.LastName || in.Phone != p.PhoneNumber.String || in.Email != p.Email.String {
					return ErrUnavailable
				}
			}
			if editable {
				in, err = Validate(in)
			} else {
				// Shared coordinates may be incomplete; validate only the editable label.
				in.Relationship = strings.TrimSpace(in.Relationship)
				if in.Relationship == "" || !utf8.ValidString(in.Relationship) || utf8.RuneCountInString(in.Relationship) > 200 || strings.ContainsAny(in.Relationship, "\x00\r\n") {
					err = Fields{"relationship": "Indiquez un lien de 200 caractères maximum."}
				}
			}
			if err != nil {
				return err
			}
			if editable {
				_, err = tx.ExecContext(ctx, `UPDATE persons SET first_name=?2,last_name=?3,phone_number=?4,email=?5,updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=?1 AND archived_at IS NULL`, contact, in.FirstName, in.LastName, in.Phone, nullable(in.Email))
				if err != nil {
					return err
				}
			}
			_, err = tx.ExecContext(ctx, `UPDATE person_emergency_contacts SET relationship_label=?2,updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=?1`, relation, in.Relationship)
			if err == nil {
				err = reorder(ctx, tx, owner, relation, in.Priority)
			}
		default:
			return Fields{"form": "Choisissez une action valide."}
		}
	}
	if err != nil {
		return err
	}
	if err = dbsqlc.New(tx).CreateAdministrativeEvent(ctx, dbsqlc.CreateAdministrativeEventParams{ActorUserID: actor, Action: map[string]string{"add": "emergency_contact_added", "update": "emergency_contact_updated", "remove": "emergency_contact_removed"}[action], ResourceType: "person", ResourceID: owner}); err != nil {
		return err
	}
	return tx.Commit()
}

// Allocate temporary ranks above the current maximum, then compact in requested order.
// This respects the unique (Person,priority) constraint throughout the transaction.
func reorder(ctx context.Context, tx *sql.Tx, owner, relation, priority int32) error {
	rows, err := dbsqlc.New(tx).ListPersonEmergencyContacts(ctx, owner)
	if err != nil {
		return err
	}
	if priority < 1 || priority > int32(len(rows)) {
		return Fields{"priority": "Choisissez une priorité dans la liste."}
	}
	var target dbsqlc.ListPersonEmergencyContactsRow
	out := make([]dbsqlc.ListPersonEmergencyContactsRow, 0, len(rows))
	for _, r := range rows {
		if r.EmergencyContactID == relation {
			target = r
		} else {
			out = append(out, r)
		}
	}
	out = append(out, dbsqlc.ListPersonEmergencyContactsRow{})
	copy(out[priority:], out[priority-1:])
	out[priority-1] = target
	var offset int32
	for _, r := range rows {
		if r.Priority > offset {
			offset = r.Priority
		}
	}
	offset++
	if _, err = tx.ExecContext(ctx, `UPDATE person_emergency_contacts SET priority=priority+?2 WHERE person_id=?1`, owner, offset); err != nil {
		return err
	}
	for i, r := range out {
		if _, err = tx.ExecContext(ctx, `UPDATE person_emergency_contacts SET priority=?2 WHERE id=?1`, r.EmergencyContactID, i+1); err != nil {
			return err
		}
	}
	return nil
}
