package router

import (
	"context"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

type FakeQueries struct {
	PersonByID            dbsqlc.Person
	GetPersonByIDReceived int32
}

func (f FakeQueries) CreatePerson(ctx context.Context, arg dbsqlc.CreatePersonParams) (dbsqlc.Person, error) {
	return dbsqlc.Person{}, nil
}

func (f FakeQueries) ListPersons(ctx context.Context) ([]dbsqlc.Person, error) {
	return []dbsqlc.Person{}, nil
}

func (f *FakeQueries) GetPersonByID(ctx context.Context, id int32) (dbsqlc.Person, error) {

	f.GetPersonByIDReceived = id
	return f.PersonByID, nil
}

func (f FakeQueries) UpdatePerson(ctx context.Context, arg dbsqlc.UpdatePersonParams) (dbsqlc.Person, error) {
	return dbsqlc.Person{}, nil
}

func (f FakeQueries) ArchivePerson(ctx context.Context, id int32) error {
	return nil
}

func (f FakeQueries) ListArchivedPersons(ctx context.Context) ([]dbsqlc.Person, error) {
	return []dbsqlc.Person{}, nil
}

func (f FakeQueries) RestorePerson(ctx context.Context, id int32) error {

	return nil
}
