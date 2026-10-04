package demodata_test

import (
	"errors"
	"testing"

	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/demodata"
)

func TestShowcasePreparationAndShiftedIDs(t *testing.T) {
	db := demoDB(t)
	// Do not depend on the sequence values produced by the standard demo seed.
	if _, err := db.ExecContext(t.Context(), `INSERT INTO sqlite_sequence(name,seq) SELECT 'persons',1000 WHERE NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='persons'); INSERT INTO sqlite_sequence(name,seq) SELECT 'users',1000 WHERE NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='users'); UPDATE sqlite_sequence SET seq=seq+1000 WHERE name IN ('seasons','activities','groups','membership_types','consent_definitions')`); err != nil {
		t.Fatal(err)
	}
	if err := demodata.PrepareDemoOffice(t.Context(), db, demoPassword); err != nil {
		t.Fatal(err)
	}
	if err := demodata.PrepareShowcase(t.Context(), db, demoPassword); err != nil {
		t.Fatal(err)
	}
	if err := demodata.VerifyShowcase(t.Context(), db, demoPassword); err != nil {
		t.Fatal(err)
	}
	if err := demodata.VerifyShowcase(t.Context(), db, "incorrect password"); err == nil {
		t.Fatal("wrong credentials accepted")
	}
	if err := demodata.PrepareShowcase(t.Context(), db, demoPassword); err == nil {
		t.Fatal("repeat accepted")
	}
	if err := demodata.VerifyShowcase(t.Context(), db, demoPassword); err != nil {
		t.Fatal("repeat changed demo", err)
	}
	if err := demodata.VerifyDemo(t.Context(), db, demoPassword); err == nil {
		t.Fatal("standard verifier accepted business data")
	}
	var actual int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM persons WHERE id>1000`).Scan(&actual); err != nil || actual != 14 {
		t.Fatal("fixed person IDs", actual, err)
	}
}

func TestShowcaseTransactionRollbackAndPrerequisites(t *testing.T) {
	for _, mutation := range []string{
		`CREATE TRIGGER fail_showcase BEFORE INSERT ON membership_consents BEGIN SELECT RAISE(ABORT,'injected failure'); END`,
		`INSERT INTO persons(first_name,last_name) VALUES('Unexpected','Person')`,
		`INSERT INTO registration_submissions(first_name,last_name,email,birth_date,status) VALUES('Unexpected','Submission','demo@clubcore.invalid','2000-01-01','received')`,
		`UPDATE users SET is_active=false WHERE username='secretary.demo'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			db := demoDB(t)
			if err := demodata.PrepareDemoOffice(t.Context(), db, demoPassword); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			var before int
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM persons").Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err := demodata.PrepareShowcase(t.Context(), db, demoPassword); err == nil {
				t.Fatal("unsafe preparation accepted")
			}
			var persons, seasons, memberships int
			if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM persons),(SELECT count(*) FROM seasons),(SELECT count(*) FROM memberships)`).Scan(&persons, &seasons, &memberships); err != nil || persons != before || seasons != 1 || memberships != 0 {
				t.Fatal("partial seed", persons, seasons, memberships, err)
			}
		})
	}
	db := demoDB(t)
	if err := demodata.PrepareShowcase(t.Context(), db, demoPassword); err == nil {
		t.Fatal("missing office accepted")
	}
	if err := demodata.PrepareDemoOffice(t.Context(), db, demoPassword); err != nil {
		t.Fatal(err)
	}
	if err := demodata.PrepareShowcase(t.Context(), db, "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := demodata.VerifyDemo(t.Context(), db, demoPassword); err != nil {
		t.Fatal("invalid password changed database", err)
	}
}

func TestShowcaseVerifierRejectsScenarioCorruption(t *testing.T) {
	db := demoDB(t)
	if err := demodata.PrepareDemoOffice(t.Context(), db, demoPassword); err != nil {
		t.Fatal(err)
	}
	if err := demodata.PrepareShowcase(t.Context(), db, demoPassword); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		`UPDATE memberships SET status='cancelled' WHERE person_id=(SELECT person_id FROM users WHERE username='member.demo') AND status='active'`,
		`INSERT INTO user_roles(user_id,role_id) SELECT u.id,r.id FROM users u CROSS JOIN roles r WHERE u.username='empty.demo' AND r.name='coach'`,
		`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) SELECT p.id,u.person_id,'other' FROM persons p CROSS JOIN users u WHERE p.first_name='Arthur' AND u.username='secretary.member.demo'; INSERT INTO guardian_access_grants(child_person_id,guardian_person_id) SELECT p.id,u.person_id FROM persons p CROSS JOIN users u WHERE p.first_name='Arthur' AND u.username='secretary.member.demo'`,
		`INSERT INTO membership_consents(membership_id,consent_definition_id,decision,given_by_person_id) SELECT membership_id,consent_definition_id,'granted',given_by_person_id FROM membership_consents WHERE decision='refused'`,
		`UPDATE person_emergency_contacts SET priority=3 WHERE person_id=(SELECT person_id FROM users WHERE username='member.demo') AND priority=2`,
		`UPDATE group_slots SET is_active=false WHERE practice_label='Préparation physique / Jiu-Jitsu Brésilien'`,
		`INSERT INTO account_security_events(user_id,person_id,event) SELECT id,person_id,'password_changed' FROM users WHERE username='member.demo'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			// A separate SQLite copy keeps the baseline intact for each corruption.
			path := t.TempDir() + "/corrupt_demo.db"
			if _, err := db.ExecContext(t.Context(), "VACUUM INTO ?", path); err != nil {
				t.Fatal(err)
			}
			copyDB, err := database.New(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer copyDB.Close()
			if _, err := copyDB.ExecContext(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			if err := demodata.VerifyShowcase(t.Context(), copyDB, demoPassword); !errors.Is(err, demodata.ErrDemoInvalid) {
				t.Fatal("corruption accepted", err)
			}
		})
	}
}
