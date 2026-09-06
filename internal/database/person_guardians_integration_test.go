package database

import (
	"errors"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPersonContactModelIntegration(t *testing.T) {
	ctx := t.Context()
	db := newTestDatabase(t)
	q := dbsqlc.New(db)
	createPerson := func(name string) dbsqlc.Person {
		t.Helper()
		p, err := q.CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: name, LastName: "Test"})
		if err != nil {
			t.Fatal(err)
		}
		if p.BirthDate.Valid || p.Notes.Valid || p.PhoneNumber.Valid || p.Email.Valid || p.Address.Valid {
			t.Fatalf("optional fields should be NULL: %+v", p)
		}
		return p
	}
	child := createPerson("Child")
	sibling := createPerson("Sibling")
	mother := createPerson("Mother")
	father := createPerson("Father")
	other := createPerson("Other")

	t.Run("person notes and optional birth date", func(t *testing.T) {
		note := pgtype.Text{String: "Information durable", Valid: true}
		p, err := q.CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: "Contact", LastName: "Test", Notes: note, BirthDate: pgtype.Date{Time: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}})
		if err != nil {
			t.Fatal(err)
		}
		saved, err := q.GetPersonByID(ctx, p.ID)
		if err != nil || saved.Notes != note {
			t.Fatalf("create/read notes: %+v, %v", saved, err)
		}
		note.String = "Information mise à jour"
		_, err = q.UpdatePerson(ctx, dbsqlc.UpdatePersonParams{ID: p.ID, FirstName: p.FirstName, LastName: p.LastName, Notes: note, UpdateNotes: true})
		if err != nil {
			t.Fatal(err)
		}
		saved, err = q.GetPersonByID(ctx, p.ID)
		if err != nil || saved.Notes != note || saved.BirthDate.Valid {
			t.Fatalf("update/read: %+v, %v", saved, err)
		}
		_, err = q.UpdatePerson(ctx, dbsqlc.UpdatePersonParams{ID: p.ID, FirstName: p.FirstName, LastName: p.LastName})
		if err != nil {
			t.Fatal(err)
		}
		saved, err = q.GetPersonByID(ctx, p.ID)
		if err != nil || saved.Notes != note {
			t.Fatalf("omitted notes must be preserved: %+v, %v", saved, err)
		}
		_, err = q.UpdatePerson(ctx, dbsqlc.UpdatePersonParams{ID: p.ID, FirstName: p.FirstName, LastName: p.LastName, UpdateNotes: true})
		if err != nil {
			t.Fatal(err)
		}
		saved, err = q.GetPersonByID(ctx, p.ID)
		if err != nil || saved.Notes.Valid {
			t.Fatalf("clear notes: %+v, %v", saved, err)
		}
	})

	createRelation := func(childID, guardianID int32, kind string, primary bool) dbsqlc.PersonGuardian {
		t.Helper()
		r, err := q.CreatePersonGuardian(ctx, dbsqlc.CreatePersonGuardianParams{ChildPersonID: childID, GuardianPersonID: guardianID, RelationshipType: kind, IsPrimaryContact: primary})
		if err != nil {
			t.Fatal(err)
		}
		if !r.CreatedAt.Valid {
			t.Fatal("missing creation timestamp")
		}
		return r
	}
	m := createRelation(child.ID, mother.ID, "mother", false)
	f := createRelation(child.ID, father.ID, "father", false)
	createRelation(sibling.ID, mother.ID, "guardian", false)
	guardians, err := q.ListPersonGuardians(ctx, child.ID)
	if err != nil || len(guardians) != 2 {
		t.Fatalf("guardians: %+v, %v", guardians, err)
	}
	if guardians[0].GuardianPersonID != mother.ID || guardians[1].GuardianPersonID != father.ID || guardians[0].IsPrimaryContact || guardians[1].IsPrimaryContact {
		t.Fatalf("unexpected guardians: %+v", guardians)
	}
	children, err := q.ListGuardianChildren(ctx, mother.ID)
	if err != nil || len(children) != 2 {
		t.Fatalf("children: %+v, %v", children, err)
	}
	if children[0].ChildPersonID != child.ID || children[1].ChildPersonID != sibling.ID {
		t.Fatalf("unexpected children: %+v", children)
	}

	for _, tc := range []struct {
		name            string
		child, guardian int32
		kind, code      string
	}{
		{"self relation", child.ID, child.ID, "guardian", "23514"},
		{"duplicate", child.ID, mother.ID, "other", "23505"},
		{"invalid relationship", child.ID, other.ID, "invalid", "23514"},
		{"missing child", -1, mother.ID, "guardian", "23503"},
		{"missing guardian", child.ID, -1, "guardian", "23503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := q.CreatePersonGuardian(ctx, dbsqlc.CreatePersonGuardianParams{ChildPersonID: tc.child, GuardianPersonID: tc.guardian, RelationshipType: tc.kind})
			requirePostgresCode(t, err, tc.code)
		})
	}
	updated, err := q.UpdatePersonGuardian(ctx, dbsqlc.UpdatePersonGuardianParams{ID: m.ID, RelationshipType: "other", IsPrimaryContact: true})
	if err != nil || updated.RelationshipType != "other" || !updated.IsPrimaryContact {
		t.Fatalf("update: %+v, %v", updated, err)
	}
	t.Run("two primary contacts", func(t *testing.T) {
		_, err := q.CreatePersonGuardian(ctx, dbsqlc.CreatePersonGuardianParams{ChildPersonID: child.ID, GuardianPersonID: other.ID, RelationshipType: "other", IsPrimaryContact: true})
		requirePostgresCode(t, err, "23505")
		_, err = q.UpdatePersonGuardian(ctx, dbsqlc.UpdatePersonGuardianParams{ID: f.ID, RelationshipType: "father", IsPrimaryContact: true})
		requirePostgresCode(t, err, "23505")
	})
	_, err = q.UpdatePersonGuardian(ctx, dbsqlc.UpdatePersonGuardianParams{ID: m.ID, RelationshipType: "mother", IsPrimaryContact: false})
	if err != nil {
		t.Fatal(err)
	}
	_, err = q.UpdatePersonGuardian(ctx, dbsqlc.UpdatePersonGuardianParams{ID: f.ID, RelationshipType: "father", IsPrimaryContact: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.DeletePersonGuardian(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	guardians, err = q.ListPersonGuardians(ctx, child.ID)
	if err != nil || len(guardians) != 1 || guardians[0].ID != m.ID || guardians[0].IsPrimaryContact {
		t.Fatalf("delete / no primary: %+v, %v", guardians, err)
	}

	t.Run("trial notes are independent", func(t *testing.T) {
		_, err := db.Exec(ctx, "UPDATE persons SET notes = 'Durable' WHERE id = $1", child.ID)
		if err != nil {
			t.Fatal(err)
		}
		var activityID int32
		if err := db.QueryRow(ctx, "INSERT INTO activities (name) VALUES ('Test') RETURNING id").Scan(&activityID); err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"registered", "attended", "cancelled", "no_show"} {
			var trialID int32
			if err := db.QueryRow(ctx, "INSERT INTO trial_registrations (person_id, activity_id, trial_date, status, notes) VALUES ($1,$2,CURRENT_DATE,$3,'Essai uniquement') RETURNING id", child.ID, activityID, status).Scan(&trialID); err != nil {
				t.Fatal(err)
			}
			var note pgtype.Text
			if err := db.QueryRow(ctx, "SELECT notes FROM trial_registrations WHERE id=$1", trialID).Scan(&note); err != nil || !note.Valid || note.String != "Essai uniquement" {
				t.Fatalf("trial note: %+v, %v", note, err)
			}
			if _, err := db.Exec(ctx, "UPDATE trial_registrations SET notes=NULL WHERE id=$1", trialID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(ctx, "SELECT notes FROM trial_registrations WHERE id=$1", trialID).Scan(&note); err != nil || note.Valid {
				t.Fatalf("nullable trial note: %+v, %v", note, err)
			}
		}
		p, err := q.GetPersonByID(ctx, child.ID)
		if err != nil || p.Notes.String != "Durable" || !p.Notes.Valid {
			t.Fatalf("person note changed: %+v, %v", p, err)
		}
	})
}

func requirePostgresCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("expected PostgreSQL %s, got %v", code, err)
	}
}
