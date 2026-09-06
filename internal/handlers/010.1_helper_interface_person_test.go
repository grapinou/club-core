package handlers

import (
	"context"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

// recordingPersonQueries enregistre les paramètres reçus par une requête,
// afin de vérifier ce que le handler transmet à la couche base de données.
type recordingPersonQueries struct {
	CreatePersonParams dbsqlc.CreatePersonParams
	CreatePersonCalled bool
	CreatePersonError  error
	PersonsList        []dbsqlc.Person

	PersonByID            dbsqlc.Person
	GetPersonByIDReceived int32
	GetPersonByIDError    error

	UpdatePersonParams dbsqlc.UpdatePersonParams
	UpdatePersonCalled bool
	UpdatePersonError  error

	ArchivePersonReceived int32
	ArchivePersonCalled   bool
	ArchivePersonError    error

	RestorePersonReceived int32
	RestorePersonCalled   bool
	RestorePersonError    error
	ArchivedPersonsList   []dbsqlc.Person
}

func (q *recordingPersonQueries) CreatePerson(ctx context.Context, arg dbsqlc.CreatePersonParams) (dbsqlc.Person, error) {

	q.CreatePersonParams = arg
	q.CreatePersonCalled = true
	return dbsqlc.Person{}, q.CreatePersonError
}

func (q *recordingPersonQueries) ListPersons(ctx context.Context) ([]dbsqlc.Person, error) {
	return q.PersonsList, nil
}

func (q *recordingPersonQueries) GetPersonByID(ctx context.Context, id int32) (dbsqlc.Person, error) {
	q.GetPersonByIDReceived = id
	return q.PersonByID, q.GetPersonByIDError
}

func (q *recordingPersonQueries) UpdatePerson(ctx context.Context, arg dbsqlc.UpdatePersonParams) (dbsqlc.Person, error) {
	q.UpdatePersonParams = arg
	q.UpdatePersonCalled = true
	return dbsqlc.Person{}, q.UpdatePersonError
}

func (q *recordingPersonQueries) ArchivePerson(ctx context.Context, id int32) error {

	q.ArchivePersonReceived = id
	q.ArchivePersonCalled = true

	return q.ArchivePersonError
}

func (q *recordingPersonQueries) ListArchivedPersons(ctx context.Context) ([]dbsqlc.Person, error) {
	return q.ArchivedPersonsList, nil
}

func (q *recordingPersonQueries) RestorePerson(ctx context.Context, id int32) error {

	q.RestorePersonReceived = id
	q.RestorePersonCalled = true

	return q.RestorePersonError
}
