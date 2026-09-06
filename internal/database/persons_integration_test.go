package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/grapinou/club-manager/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPersonsIntegration(t *testing.T) {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")

	if databaseURL == "" {
		t.Fatal("DATABASE_URL doit être définie pour le test d'intégration")
	}

	db, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connexion à PostgreSQL impossible : %v", err)
	}
	defer db.Close()

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("impossible de démarrer la transaction : %v", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	queries := dbsqlc.New(db)
	queries = queries.WithTx(tx)

	params := dbsqlc.CreatePersonParams{
		FirstName: "Robin",
		LastName:  "Des Bois",
		BirthDate: pgtype.Date{
			Time:  time.Date(1990, 5, 12, 0, 0, 0, 0, time.UTC),
			Valid: true,
		},
		PhoneNumber: pgtype.Text{
			String: "00 01 02 03 04",
			Valid:  true,
		},
		Email: pgtype.Text{
			String: "robin.desbois@example.com",
			Valid:  true,
		},
		Address: pgtype.Text{
			String: "forêt de Sherwood",
			Valid:  true,
		},
	}

	createdPerson, err := queries.CreatePerson(ctx, params)
	if err != nil {
		t.Fatalf("création de la personne impossible : %v", err)
	}

	err = queries.ArchivePerson(ctx, createdPerson.ID)
	if err != nil {
		t.Fatalf(
			"archivage de la personne impossible : %v",
			err,
		)
	}

	archivedPerson, err := queries.GetPersonByID(
		ctx,
		createdPerson.ID,
	)
	if err != nil {
		t.Fatalf(
			"lecture de la personne archivée impossible : %v",
			err,
		)
	}

	if !archivedPerson.ArchivedAt.Valid {
		t.Error("la personne devrait être archivée")
	}

	persons, err := queries.ListPersons(ctx)
	if err != nil {
		t.Fatalf(
			"lecture de la liste des personnes impossible : %v",
			err,
		)
	}

	for _, person := range persons {
		if person.ID == createdPerson.ID {
			t.Error(
				"la personne archivée ne devrait pas apparaître dans ListPersons",
			)
		}
	}

	archivedPersons, err := queries.ListArchivedPersons(ctx)
	if err != nil {
		t.Fatalf(
			"lecture de la liste des personnes archivées impossible : %v",
			err,
		)
	}

	found := false

	for _, person := range archivedPersons {
		if person.ID == createdPerson.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error(
			"la personne archivée devrait apparaître dans ListArchivedPersons",
		)
	}

	err = queries.RestorePerson(ctx, createdPerson.ID)
	if err != nil {
		t.Fatalf(
			"restauration de la personne impossible : %v",
			err,
		)
	}

	restoredPerson, err := queries.GetPersonByID(
		ctx,
		createdPerson.ID,
	)
	if err != nil {
		t.Fatalf(
			"lecture de la personne restaurée impossible : %v",
			err,
		)
	}

	if restoredPerson.ArchivedAt.Valid {
		t.Error("la personne restaurée ne devrait plus être archivée")
	}

	persons, err = queries.ListPersons(ctx)
	if err != nil {
		t.Fatalf(
			"lecture de la liste après restauration impossible : %v",
			err,
		)
	}

	found = false

	for _, person := range persons {
		if person.ID == createdPerson.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error(
			"la personne restaurée devrait apparaître dans ListPersons",
		)
	}

	archivedPersons, err = queries.ListArchivedPersons(ctx)

	if err != nil {
		t.Fatalf(
			"lecture de la liste des personnes archivées après restauration impossible : %v",
			err,
		)
	}

	for _, person := range archivedPersons {
		if person.ID == createdPerson.ID {
			t.Error(
				"la personne restaurée ne devrait plus apparaître dans ListArchivedPersons",
			)
		}
	}

	savedPerson, err := queries.GetPersonByID(
		ctx,
		createdPerson.ID,
	)
	if err != nil {
		t.Fatalf("lecture de la personne impossible : %v", err)
	}

	if savedPerson.FirstName != params.FirstName {
		t.Errorf(
			"prénom obtenu : %q, attendu : %q",
			savedPerson.FirstName,
			params.FirstName,
		)
	}

	if savedPerson.LastName != params.LastName {
		t.Errorf(
			"nom obtenu : %q, attendu : %q",
			savedPerson.LastName,
			params.LastName,
		)
	}

}
