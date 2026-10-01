package demodata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

var ErrOfficeNotEmpty = errors.New("bootstrap bureau refusé : comptes ou configuration déjà présents ; utiliser reset-demo.sh pour repartir à zéro")
var ErrDemoInvalid = errors.New("démonstration non conforme")

var officeAccounts = []struct{ username, firstName, role string }{
	{"president.demo", "Président", "president"},
	{"secretary.demo", "Secrétaire", "secretary"},
	{"treasurer.demo", "Trésorier", "treasurer"},
}

// PrepareDemoOffice is deliberately single-use. It never changes credentials or
// duplicates existing identities. IMMEDIATE transactions serialize checks and
// writes; any failure rolls back all three accounts and setup closure together.
func PrepareDemoOffice(ctx context.Context, db *sql.DB, password string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkDemoDB(ctx, tx); err != nil {
		return err
	}
	var occupied bool
	if err = tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM persons) OR EXISTS(SELECT 1 FROM users) OR EXISTS(SELECT 1 FROM user_roles)
 OR EXISTS(SELECT 1 FROM installation_setup WHERE initialized_at IS NOT NULL OR secret_hash IS NOT NULL)`).Scan(&occupied); err != nil {
		return err
	}
	if occupied {
		return ErrOfficeNotEmpty
	}
	if err = checkReferences(ctx, tx); err != nil {
		return err
	}
	if err = checkNoBusinessData(ctx, tx); err != nil {
		return err
	}
	var presidentID int32
	for _, account := range officeAccounts {
		// Independent salts, same intentionally shared local demo password.
		hash, err := auth.HashPassword(password)
		if err != nil {
			return fmt.Errorf("CLUBCORE_DEMO_PASSWORD : %w", err)
		}
		var personID, userID int32
		email := account.username + "@clubcore.invalid"
		if err = tx.QueryRowContext(ctx, `INSERT INTO persons(first_name,last_name,email) VALUES (?1,'Démo',?2) RETURNING id`, account.firstName, email).Scan(&personID); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `INSERT INTO users(person_id,username,login_email,password_hash,is_active,activated_at)
 VALUES (?1,?2,?3,?4,true,strftime('%Y-%m-%d %H:%M:%f','now')) RETURNING id`, personID, account.username, email, string(hash)).Scan(&userID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name=?2", userID, account.role)
		if err != nil {
			return err
		}
		if count, err := result.RowsAffected(); err != nil || count != 1 {
			return fmt.Errorf("%w : rôle %s absent", ErrDemoInvalid, account.role)
		}
		if account.role == "president" {
			presidentID = userID
		}
	}
	// Close takeover through the normal installation state, without invoking or
	// modifying /setup. This database already has its demo president.
	if _, err = tx.ExecContext(ctx, `UPDATE installation_setup SET initialized_at=strftime('%Y-%m-%d %H:%M:%f','now'),
 first_admin_user_id=?1,secret_hash=NULL,secret_issued_at=NULL WHERE id=true`, presidentID); err != nil {
		return err
	}
	if err = checkOffice(ctx, tx, password); err != nil {
		return err
	}
	return tx.Commit()
}

// VerifyDemo checks the complete reset result and authenticates each account
// using the same service as /login. It never writes business or security events.
func VerifyDemo(ctx context.Context, db *sql.DB, password string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkDemoDB(ctx, tx); err != nil {
		return err
	}
	if err = checkReferences(ctx, tx); err != nil {
		return err
	}
	if err = checkNoBusinessData(ctx, tx); err != nil {
		return err
	}
	return checkOffice(ctx, tx, password)
}

func checkReferences(ctx context.Context, tx *sql.Tx) error {
	for table, want := range map[string]int{
		"organizations": 1, "organization_public_images": 5, "organization_links": 1,
		"locations": 1, "seasons": 1, "activities": 2, "groups": 5,
		"group_slots": 16, "membership_type_groups": 6, "membership_types": 3, "consent_definitions": 1,
	} {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return err
		}
		if count != want {
			return fmt.Errorf("%w : %s (%d, attendu %d)", ErrDemoInvalid, table, count, want)
		}
	}
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM organizations WHERE name='Budokan Sud Oise' AND is_active)
 AND (SELECT count(*) FROM activities WHERE is_active AND name IN ('Jiu-Jitsu Brésilien','Jiu-Jitsu Traditionnel / Combat'))=2
 AND EXISTS(SELECT 1 FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN activities a ON a.id=g.activity_id
 WHERE gs.weekday=1 AND gs.start_time='20:15' AND gs.end_time='22:00'
 AND gs.practice_label='Préparation physique / Jiu-Jitsu Brésilien' AND gs.is_active
 AND g.name='JJB Adolescents et Adultes' AND a.name='Jiu-Jitsu Brésilien')
 AND (SELECT count(*) FROM membership_types WHERE is_active AND currency='EUR'
 AND public_note='Tarif fictif utilisé pour la démonstration.'
 AND ((name='Adulte' AND amount_cents=30000) OR (name='Adolescent' AND amount_cents=25000) OR (name='Enfant' AND amount_cents=20000)))=3
 AND EXISTS(SELECT 1 FROM consent_definitions WHERE code='image_rights' AND is_active)
 AND (SELECT count(*) FROM membership_type_groups c JOIN membership_types mt ON mt.id=c.membership_type_id JOIN groups g ON g.id=c.group_id WHERE (mt.name IN ('Adulte','Adolescent') AND g.name='JJB Adolescents et Adultes') OR (mt.name='Enfant' AND g.name IN ('JJB enfants 7–10 ans','JJB enfants 10–14 ans','Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans','Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans')))=6`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrDemoInvalid
	}
	return nil
}

func checkOffice(ctx context.Context, tx *sql.Tx, password string) error {
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM persons)=3 AND (SELECT count(*) FROM users)=3
 AND (SELECT count(*) FROM user_roles)=3
 AND EXISTS(SELECT 1 FROM installation_setup WHERE initialized_at IS NOT NULL AND secret_hash IS NULL
 AND first_admin_user_id=(SELECT id FROM users WHERE username='president.demo'))`).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrDemoInvalid
	}
	service, err := auth.New(dbsqlc.New(tx))
	if err != nil {
		return err
	}
	for _, account := range officeAccounts {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN persons p ON p.id=u.person_id
 JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id
 WHERE u.username=?1 AND p.first_name=?2 AND p.last_name='Démo' AND p.archived_at IS NULL
 AND u.is_active AND u.activated_at IS NOT NULL AND r.name=?3
 AND u.login_email=?4 AND p.email=?4)`, account.username, account.firstName, account.role, account.username+"@clubcore.invalid").Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrDemoInvalid
		}
		if _, err := service.Authenticate(ctx, account.username, password); err != nil {
			return fmt.Errorf("%w : authentification %s", ErrDemoInvalid, account.username)
		}
	}
	return nil
}
