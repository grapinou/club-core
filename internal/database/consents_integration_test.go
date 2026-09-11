package database

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/grapinou/club-core/internal/consents"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pressly/goose/v3"
)

func TestEmergencyContactsIntegration(t *testing.T) {
	db := newTestDatabase(t)
	ctx := t.Context()
	q := dbsqlc.New(db)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(name string) int32 {
		t.Helper()
		p, e := q.CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: name, LastName: "Test"})
		must(e)
		return p.ID
	}
	a, b, c, d := person("Arthur"), person("Claire"), person("Julie"), person("Paul")
	phone, email := pgtype.Text{String: "0600000000", Valid: true}, pgtype.Text{String: "claire@example.test", Valid: true}
	_, err := db.Exec(ctx, "UPDATE persons SET phone_number=$2,email=$3 WHERE id=$1", b, phone, email)
	must(err)
	create := func(owner, contact, priority int32) (dbsqlc.PersonEmergencyContact, error) {
		return q.CreatePersonEmergencyContact(ctx, dbsqlc.CreatePersonEmergencyContactParams{PersonID: owner, ContactPersonID: contact, Priority: priority})
	}
	second, err := create(a, c, 2)
	must(err)
	first, err := create(a, b, 1)
	must(err)
	_, err = create(d, b, 1)
	must(err)
	rows, err := q.ListPersonEmergencyContacts(ctx, a)
	must(err)
	if len(rows) != 2 || rows[0].EmergencyContactID != first.ID || rows[1].EmergencyContactID != second.ID || rows[0].FirstName != "Claire" || rows[0].LastName != "Test" || rows[0].ContactPersonID != b || rows[0].PhoneNumber != phone || rows[0].Email != email || rows[0].RelationshipLabel.Valid || rows[1].PhoneNumber.Valid {
		t.Fatal(rows)
	}
	reverse, err := q.ListEmergencyContactForPersons(ctx, b)
	must(err)
	if len(reverse) != 2 || reverse[0].PersonID != a || reverse[1].PersonID != d {
		t.Fatal(reverse)
	}
	for _, tc := range []struct {
		name                     string
		owner, contact, priority int32
		code                     string
	}{
		{"self", a, a, 3, "23514"}, {"duplicate", a, b, 3, "23505"}, {"zero", a, d, 0, "23514"}, {"negative", a, d, -1, "23514"}, {"priority collision", a, d, 1, "23505"}, {"missing person", -1, b, 1, "23503"}, {"missing contact", a, -1, 3, "23503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := create(tc.owner, tc.contact, tc.priority)
			requirePostgresCode(t, err, tc.code)
		})
	}
	label := pgtype.Text{String: "amie de la famille", Valid: true}
	updated, err := q.UpdatePersonEmergencyContact(ctx, dbsqlc.UpdatePersonEmergencyContactParams{ID: second.ID, RelationshipLabel: label, Priority: 3})
	must(err)
	if updated.Priority != 3 || updated.RelationshipLabel != label || !updated.UpdatedAt.Time.After(second.UpdatedAt.Time) {
		t.Fatal(updated)
	}
	_, err = q.UpdatePersonEmergencyContact(ctx, dbsqlc.UpdatePersonEmergencyContactParams{ID: second.ID, Priority: 1})
	requirePostgresCode(t, err, "23505")
	_, err = q.UpdatePersonEmergencyContact(ctx, dbsqlc.UpdatePersonEmergencyContactParams{ID: second.ID, Priority: 0})
	requirePostgresCode(t, err, "23514")
	must(q.DeletePersonEmergencyContact(ctx, first.ID))
	rows, err = q.ListPersonEmergencyContacts(ctx, a)
	must(err)
	if len(rows) != 1 || rows[0].EmergencyContactID != second.ID || rows[0].RelationshipLabel != label {
		t.Fatal(rows)
	}
}

func TestMembershipConsentsIntegration(t *testing.T) {
	db := newTestDatabase(t)
	ctx := t.Context()
	q := dbsqlc.New(db)
	svc := consents.New(db)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) int32 {
		t.Helper()
		var n int32
		must(db.QueryRow(ctx, sql, args...).Scan(&n))
		return n
	}
	member := id("INSERT INTO persons(first_name,last_name) VALUES ('Arthur','Dupont') RETURNING id")
	guardian := id("INSERT INTO persons(first_name,last_name) VALUES ('Claire','Dupont') RETURNING id")
	stranger := id("INSERT INTO persons(first_name,last_name) VALUES ('Other','Test') RETURNING id")
	_, err := q.CreatePersonGuardian(ctx, dbsqlc.CreatePersonGuardianParams{ChildPersonID: member, GuardianPersonID: guardian, RelationshipType: "guardian"})
	must(err)
	season := id("INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2026','2026-09-01','2027-08-31') RETURNING id")
	kind := id("INSERT INTO membership_types(name) VALUES ('Test') RETURNING id")
	membership := id("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES ($1,$2,$3,'pending') RETURNING id", member, season, kind)
	otherMembership := id("INSERT INTO memberships(person_id,season_id,membership_type_id,status) VALUES ($1,$2,$3,'pending') RETURNING id", stranger, season, kind)
	base := dbsqlc.CreateConsentDefinitionParams{Code: "test_authorization", Version: 1, Title: "Test title", Description: "Test wording", IsActive: true}
	v1, err := q.CreateConsentDefinition(ctx, base)
	must(err)
	_, err = q.CreateConsentDefinition(ctx, base)
	requirePostgresCode(t, err, "23505")
	for _, field := range []string{"code", "version", "title", "description"} {
		t.Run("invalid definition "+field, func(t *testing.T) {
			p := base
			p.Code = "invalid"
			switch field {
			case "code":
				p.Code = " "
			case "version":
				p.Version = 0
			case "title":
				p.Title = ""
			case "description":
				p.Description = " "
			}
			_, err := q.CreateConsentDefinition(ctx, p)
			requirePostgresCode(t, err, "23514")
		})
	}
	base.Version = 2
	_, err = q.CreateConsentDefinition(ctx, base)
	requirePostgresCode(t, err, "23505")
	base.IsActive = false
	v2, err := q.CreateConsentDefinition(ctx, base)
	must(err)
	all, err := q.ListConsentDefinitions(ctx)
	must(err)
	if len(all) != 2 {
		t.Fatal(all)
	}
	active, err := q.ListActiveConsentDefinitions(ctx)
	must(err)
	if len(active) != 1 || active[0].ID != v1.ID {
		t.Fatal(active)
	}
	record := func(def, giver int32, decision string) (dbsqlc.MembershipConsent, error) {
		return svc.RecordConsentDecision(ctx, dbsqlc.CreateMembershipConsentParams{MembershipID: membership, ConsentDefinitionID: def, GivenByPersonID: giver, Decision: decision})
	}
	for _, decision := range []string{"granted", "refused", "withdrawn"} {
		_, err = record(v1.ID, stranger, decision)
		if !errors.Is(err, consents.ErrUnauthorizedGiver) {
			t.Fatal(err)
		}
	}
	_, err = record(v1.ID, member, "invalid")
	if !errors.Is(err, consents.ErrInvalidDecision) {
		t.Fatal(err)
	}
	_, err = svc.WithdrawConsent(ctx, membership, v1.ID, member)
	if !errors.Is(err, consents.ErrWithdrawalWithoutGrant) {
		t.Fatal(err)
	}
	decisions := []string{"refused", "granted", "withdrawn", "granted", "refused", "granted"}
	for i, decision := range decisions {
		giver := member
		if i%2 == 1 {
			giver = guardian
		}
		var saved dbsqlc.MembershipConsent
		if decision == "withdrawn" {
			saved, err = svc.WithdrawConsent(ctx, membership, v1.ID, giver)
		} else {
			saved, err = record(v1.ID, giver, decision)
		}
		must(err)
		if decision != "granted" {
			_, err = svc.WithdrawConsent(ctx, membership, v1.ID, giver)
			if !errors.Is(err, consents.ErrWithdrawalWithoutGrant) {
				t.Fatal(err)
			}
		}
		current, e := q.ListCurrentMembershipConsents(ctx, membership)
		must(e)
		if len(current) != 1 || current[0].CurrentDecision.String != decision || !current[0].CurrentDecision.Valid || current[0].GivenByPersonID.Int32 != giver || current[0].DecisionRecordedAt != saved.RecordedAt || current[0].Title != base.Title || current[0].Description != base.Description || current[0].GivenByLastName.String != "Dupont" {
			t.Fatal(current)
		}
	}
	_, err = record(v2.ID, member, "withdrawn")
	if !errors.Is(err, consents.ErrWithdrawalWithoutGrant) {
		t.Fatal(err)
	}
	_, err = svc.WithdrawConsent(ctx, otherMembership, v1.ID, stranger)
	if !errors.Is(err, consents.ErrWithdrawalWithoutGrant) {
		t.Fatal(err)
	}
	history, err := q.ListMembershipConsentHistory(ctx, dbsqlc.ListMembershipConsentHistoryParams{MembershipID: membership, ConsentDefinitionID: v1.ID})
	must(err)
	if len(history) != len(decisions) {
		t.Fatal(history)
	}
	for i, row := range history {
		if row.Decision != decisions[i] || row.Version != 1 || row.Code != base.Code || (i > 0 && (row.ID <= history[i-1].ID || row.RecordedAt.Time.Before(history[i-1].RecordedAt.Time))) {
			t.Fatal(history)
		}
	}
	_, err = db.Exec(ctx, "UPDATE consent_definitions SET is_active=true WHERE id=$1", v2.ID)
	requirePostgresCode(t, err, "23505")
	deactivated, err := q.DeactivateConsentDefinition(ctx, v1.ID)
	must(err)
	if deactivated.IsActive {
		t.Fatal(deactivated)
	}
	for _, decision := range []string{"granted", "refused"} {
		_, err = record(v1.ID, member, decision)
		if !errors.Is(err, consents.ErrInactiveDefinition) {
			t.Fatal(err)
		}
	}
	_, err = svc.WithdrawConsent(ctx, membership, v1.ID, member)
	must(err)
	_, err = svc.WithdrawConsent(ctx, membership, v1.ID, member)
	if !errors.Is(err, consents.ErrWithdrawalWithoutGrant) {
		t.Fatal(err)
	}
	// Both historical versions can be inactive, with another inactive version too.
	base.Version = 3
	_, err = q.CreateConsentDefinition(ctx, base)
	must(err)
	_, err = db.Exec(ctx, "UPDATE consent_definitions SET is_active=true WHERE id=$1", v2.ID)
	must(err)
	unanswered, err := q.ListCurrentMembershipConsents(ctx, membership)
	must(err)
	if len(unanswered) != 2 || unanswered[1].Version != 2 || unanswered[1].CurrentDecision.Valid || unanswered[1].DecisionRecordedAt.Valid || unanswered[1].GivenByPersonID.Valid || unanswered[1].GivenByFirstName.Valid || unanswered[1].GivenByLastName.Valid {
		t.Fatal(unanswered)
	}
	_, err = record(v2.ID, member, "refused")
	must(err)
	full, err := q.ListMembershipConsentsHistory(ctx, membership)
	must(err)
	if len(full) != len(decisions)+2 || full[len(full)-1].ConsentDefinitionID != v2.ID || full[len(decisions)].Decision != "withdrawn" {
		t.Fatal(full)
	}
	for i, prior := range history {
		if full[i].ID != prior.ID || full[i].Decision != prior.Decision || full[i].RecordedAt != prior.RecordedAt {
			t.Fatal(full)
		}
	}
	active, err = q.ListActiveConsentDefinitions(ctx)
	must(err)
	if len(active) != 1 || active[0].ID != v2.ID {
		t.Fatal(active)
	}
	old, err := q.GetConsentDefinition(ctx, v1.ID)
	must(err)
	if old.Description != base.Description || old.IsActive {
		t.Fatal(old)
	}
	current, err := q.ListCurrentMembershipConsents(ctx, membership)
	must(err)
	if len(current) != 2 || current[0].DefinitionIsActive || current[0].CurrentDecision.String != "withdrawn" || current[1].CurrentDecision.String != "refused" {
		t.Fatal(current)
	}
	for _, sql := range []string{"UPDATE consent_definitions SET description='changed' WHERE id=$1", "DELETE FROM consent_definitions WHERE id=$1"} {
		_, err = db.Exec(ctx, sql, v1.ID)
		requirePostgresCode(t, err, "23514")
	}
	for _, sql := range []string{"UPDATE membership_consents SET decision='refused' WHERE id=$1", "DELETE FROM membership_consents WHERE id=$1"} {
		_, err = db.Exec(ctx, sql, history[0].ID)
		requirePostgresCode(t, err, "23514")
	}
	_, err = db.Exec(ctx, "DELETE FROM memberships WHERE id=$1", membership)
	requirePostgresCode(t, err, "23503")
	_, err = db.Exec(ctx, "INSERT INTO membership_consents(membership_id,consent_definition_id,given_by_person_id,decision) VALUES ($1,$2,$3,'invalid')", membership, v1.ID, member)
	requirePostgresCode(t, err, "23514")
	var status string
	must(db.QueryRow(ctx, "SELECT status FROM memberships WHERE id=$1", membership).Scan(&status))
	if status != "pending" {
		t.Fatal(status)
	}
}

// Exercise the rollback with actual data and verify that earlier tables survive.
func TestConsentMigrationRoundTrip(t *testing.T) {
	pool := newTestDatabase(t)
	ctx := t.Context()
	db, err := sql.Open("pgx", pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO consent_definitions(code,version,title,description) VALUES ('test',1,'Test','Test')"); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.DownTo(ctx, 14); err != nil {
		t.Fatal(err)
	}
	var remaining bool
	if err = db.QueryRowContext(ctx, "SELECT to_regclass('memberships') IS NOT NULL AND to_regclass('person_emergency_contacts') IS NULL AND to_regclass('consent_definitions') IS NULL AND to_regclass('membership_consents') IS NULL").Scan(&remaining); err != nil || !remaining {
		t.Fatalf("rollback: %v, %v", remaining, err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
}
