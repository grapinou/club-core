package demodata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

const showcaseDate = "2026-10-04 10:00:00"

type showcasePerson struct {
	key, first, last, birth, email, phone, address, role string
}

var showcasePeople = []showcasePerson{
	{"member.demo", "Marc", "Membre", "1985-03-12", "marc.membre@clubcore.invalid", "06 00 00 00 11", "Résidence des Jardins de la Mardelle\nBâtiment C, appartement 24\n18 avenue du Général de Gaulle, 60260 Lamorlaye", ""},
	{"parent.demo", "Claire", "Famille", "1987-06-21", "claire.famille@clubcore.invalid", "06 00 00 00 12", "7 allée des Tilleuls\n60260 Lamorlaye", ""},
	{"secretary.member.demo", "Camille", "Martin-Lefebvre", "1990-11-08", "camille.martin@clubcore.invalid", "06 00 00 00 13", "24 rue de la République, 60260 Lamorlaye", "secretary"},
	{"empty.demo", "Alex", "Découverte", "1992-02-15", "alex.decouverte@clubcore.invalid", "", "", ""},
	{"arthur", "Arthur", "Famille", "2015-09-12", "", "", "", ""},
	{"louise", "Louise", "Famille", "2018-01-23", "", "", "", ""},
	{"hugo", "Hugo", "Famille", "2019-04-17", "", "", "", ""},
	{"emma", "Emma-Lou", "Famille", "2012-12-05", "", "", "", ""},
	{"contact.marc.1", "Jean-Paul", "Membre", "1960-08-02", "jean.paul@clubcore.invalid", "06 00 00 00 21", "", ""},
	{"contact.marc.2", "Anne-Sophie", "de La Roche-Saint-Clair", "1986-05-14", "anne.sophie@clubcore.invalid", "06 00 00 00 22", "", ""},
	{"contact.famille", "Paul", "Famille", "1985-07-19", "paul.famille@clubcore.invalid", "06 00 00 00 23", "", ""},
}

type showcaseMembership struct {
	person, season, kind, status, decision, giver string
	groups                                        []string
	traditional                                   bool
}

var showcaseMemberships = []showcaseMembership{
	{person: "member.demo", season: "2026/2027", kind: "Adulte", status: "active", decision: "granted", giver: "member.demo", groups: []string{"JJB Adolescents et Adultes"}},
	{person: "member.demo", season: "2025/2026", kind: "Adulte", status: "ended", decision: "granted", giver: "member.demo", groups: []string{"JJB Adolescents et Adultes"}},
	{person: "arthur", season: "2026/2027", kind: "Enfant", status: "active", decision: "granted", giver: "parent.demo", groups: []string{"JJB enfants 10–14 ans", "Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans"}, traditional: true},
	{person: "arthur", season: "2025/2026", kind: "Enfant", status: "ended", decision: "granted", giver: "parent.demo", groups: []string{"JJB enfants 10–14 ans"}},
	{person: "louise", season: "2026/2027", kind: "Enfant", status: "pending"},
	{person: "emma", season: "2025/2026", kind: "Enfant", status: "ended", decision: "granted", giver: "parent.demo", groups: []string{"JJB enfants 10–14 ans"}},
	{person: "secretary.member.demo", season: "2026/2027", kind: "Adulte", status: "active", decision: "refused", giver: "secretary.member.demo"},
}

var showcaseContacts = []struct {
	owner, contact, relationship string
	priority                     int
}{
	{"member.demo", "contact.marc.1", "father", 1},
	{"member.demo", "contact.marc.2", "other", 2},
	{"parent.demo", "contact.famille", "other", 1},
	{"arthur", "parent.demo", "mother", 1},
	{"arthur", "contact.famille", "father", 2},
}

// PrepareShowcase only extends a pristine Budokan + office demo. No upserts:
// repetition or an unexpected business row is refused before any write.
func PrepareShowcase(ctx context.Context, db *sql.DB, password string) error {
	if !auth.ValidPassword(password) {
		return fmt.Errorf("CLUBCORE_DEMO_PASSWORD : 12 à 72 octets")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, check := range []func(context.Context, *sql.Tx) error{checkDemoDB, checkReferences, checkNoBusinessData} {
		if err := check(ctx, tx); err != nil {
			return err
		}
	}
	if err := checkOffice(ctx, tx, password); err != nil {
		return err
	}
	ids := map[string]int64{}
	var approver int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM users WHERE username='president.demo'").Scan(&approver); err != nil {
		return err
	}
	for _, p := range showcasePeople {
		var id int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO persons(first_name,last_name,birth_date,email,phone_number,address,created_at,updated_at)
 VALUES(?,?,?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),?,?) RETURNING id`, p.first, p.last, p.birth, p.email, p.phone, p.address, showcaseDate, showcaseDate).Scan(&id); err != nil {
			return err
		}
		ids[p.key] = id
		if p.key != "member.demo" && p.key != "parent.demo" && p.key != "secretary.member.demo" && p.key != "empty.demo" {
			continue
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		var uid int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO users(person_id,username,password_hash,is_active,activated_at,created_at)
 VALUES(?,?,?,true,?,?) RETURNING id`, id, p.key, string(hash), showcaseDate, showcaseDate).Scan(&uid); err != nil {
			return err
		}
		if p.role != "" {
			if _, err := tx.ExecContext(ctx, "INSERT INTO user_roles(user_id,role_id) SELECT ?,id FROM roles WHERE name=?", uid, p.role); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO seasons(name,starts_at,ends_at,is_active,created_at) VALUES('2025/2026','2025-09-01','2026-08-31',false,?)`, showcaseDate); err != nil {
		return err
	}
	for _, child := range []string{"arthur", "louise", "hugo", "emma"} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type,is_primary_contact,created_at) VALUES(?,?,'mother',true,?)`, ids[child], ids["parent.demo"], showcaseDate); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO guardian_access_grants(child_person_id,guardian_person_id,granted_by_user_id,granted_at,created_at) VALUES(?,?,?,?,?)`, ids[child], ids["parent.demo"], approver, showcaseDate, showcaseDate); err != nil {
			return err
		}
	}
	for _, c := range showcaseContacts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO person_emergency_contacts(person_id,contact_person_id,relationship_label,priority,created_at,updated_at) VALUES(?,?,?,?,?,?)`, ids[c.owner], ids[c.contact], c.relationship, c.priority, showcaseDate, showcaseDate); err != nil {
			return err
		}
	}
	for _, m := range showcaseMemberships {
		requested, joined, ended := showcaseDate, "2026-09-15", ""
		if m.status == "active" {
			requested = "2026-09-15 10:00:00"
		}
		if m.season == "2025/2026" {
			requested, joined, ended = "2025-09-15 10:00:00", "2025-09-15", "2026-08-31"
		}
		var mid int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO memberships(person_id,season_id,membership_type_id,status,requested_at,joined_at,ended_at,approved_at,approved_by_user_id,created_at,updated_at)
 VALUES(?,(SELECT id FROM seasons WHERE name=?),(SELECT id FROM membership_types WHERE name=?),?,?,CASE WHEN ?='pending' THEN NULL ELSE ? END,NULLIF(?,''),CASE WHEN ?='pending' THEN NULL ELSE ? END,CASE WHEN ?='pending' THEN NULL ELSE ? END,?,?) RETURNING id`, ids[m.person], m.season, m.kind, m.status, requested, m.status, joined, ended, m.status, requested, m.status, approver, requested, requested).Scan(&mid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO membership_activities(membership_id,activity_id) SELECT ?,id FROM activities WHERE name='Jiu-Jitsu Brésilien' OR (? AND name='Jiu-Jitsu Traditionnel / Combat')`, mid, m.traditional); err != nil {
			return err
		}
		for _, group := range m.groups {
			if _, err := tx.ExecContext(ctx, `INSERT INTO membership_groups(membership_id,group_id,joined_at,left_at,created_at) VALUES(?,(SELECT id FROM groups WHERE name=?),?,NULLIF(?,''),?)`, mid, group, joined, ended, requested); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO membership_consent_requirements(membership_id,consent_definition_id,presented_at) SELECT ?,id,? FROM consent_definitions WHERE code='image_rights' AND is_active`, mid, requested); err != nil {
			return err
		}
		if m.decision != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO membership_consents(membership_id,consent_definition_id,decision,given_by_person_id,recorded_at) SELECT ?,id,?,?,? FROM consent_definitions WHERE code='image_rights' AND is_active`, mid, m.decision, ids[m.giver], requested); err != nil {
				return err
			}
		}
	}
	if err := verifyShowcaseTx(ctx, tx, password); err != nil {
		return err
	}
	return tx.Commit()
}

// VerifyShowcase is read-only and checks the reproducible baseline, not a demo
// that has subsequently been edited through the application.
func VerifyShowcase(ctx context.Context, db *sql.DB, password string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return verifyShowcaseTx(ctx, tx, password)
}

func showcaseCheck(ctx context.Context, tx *sql.Tx, label, query string, args ...any) error {
	var valid bool
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w : showcase %s", ErrDemoInvalid, label)
	}
	return nil
}

func verifyShowcaseTx(ctx context.Context, tx *sql.Tx, password string) error {
	if err := checkDemoDB(ctx, tx); err != nil {
		return err
	}
	counts := map[string]int{"roles": 4, "persons": 14, "users": 7, "user_roles": 4, "seasons": 2, "activities": 2, "groups": 5, "group_slots": 16, "membership_types": 3, "membership_type_groups": 6, "consent_definitions": 1, "organizations": 1, "organization_links": 1, "organization_public_images": 5, "locations": 1, "memberships": 7, "membership_activities": 8, "membership_groups": 6, "membership_consent_requirements": 7, "membership_consents": 6, "person_guardians": 4, "guardian_access_grants": 4, "person_emergency_contacts": 5}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('goose_db_version','installation_setup') ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return rowErr
	}
	for _, table := range tables {
		want := counts[table] // Every other business table must remain empty.
		identifier := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		if err := showcaseCheck(ctx, tx, table, "SELECT count(*)=? FROM "+identifier, want); err != nil {
			return err
		}
	}
	if err := showcaseCheck(ctx, tx, "référentiel", `SELECT
 EXISTS(SELECT 1 FROM organizations WHERE name='Budokan Sud Oise' AND is_active)
 AND (SELECT count(*) FROM seasons WHERE (name='2026/2027' AND starts_at='2026-09-01' AND ends_at='2027-08-31' AND is_active) OR (name='2025/2026' AND starts_at='2025-09-01' AND ends_at='2026-08-31' AND NOT is_active))=2
 AND (SELECT count(*) FROM activities WHERE is_active AND name IN ('Jiu-Jitsu Brésilien','Jiu-Jitsu Traditionnel / Combat'))=2
 AND (SELECT count(*) FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN seasons s ON s.id=gs.season_id JOIN locations l ON l.id=gs.location_id WHERE s.name='2026/2027' AND gs.is_active AND g.is_active AND l.name='Gymnase La Mardelle' AND gs.valid_from=s.starts_at AND gs.valid_until=s.ends_at)=16
 AND EXISTS(SELECT 1 FROM group_slots gs JOIN groups g ON g.id=gs.group_id WHERE g.name='JJB Adolescents et Adultes' AND gs.practice_label='Préparation physique / Jiu-Jitsu Brésilien' AND gs.weekday=1 AND gs.start_time='20:15' AND gs.end_time='22:00')
 AND (SELECT count(*) FROM membership_types WHERE is_active AND currency='EUR' AND ((name='Adulte' AND amount_cents=30000) OR (name='Adolescent' AND amount_cents=25000) OR (name='Enfant' AND amount_cents=20000)))=3
 AND EXISTS(SELECT 1 FROM consent_definitions WHERE code='image_rights' AND version=1 AND is_active)
 AND EXISTS(SELECT 1 FROM installation_setup WHERE initialized_at IS NOT NULL AND secret_hash IS NULL AND first_admin_user_id=(SELECT id FROM users WHERE username='president.demo'))`); err != nil {
		return err
	}
	if err := checkShowcaseCatalogue(ctx, tx); err != nil {
		return err
	}
	service, err := auth.New(dbsqlc.New(tx))
	if err != nil {
		return err
	}
	var hashes []string
	accounts := map[string]string{"president.demo": "president", "secretary.demo": "secretary", "treasurer.demo": "treasurer", "member.demo": "", "parent.demo": "", "secretary.member.demo": "secretary", "empty.demo": ""}
	for _, account := range officeAccounts {
		if err := showcaseCheck(ctx, tx, "bureau", `SELECT EXISTS(SELECT 1 FROM users u JOIN persons p ON p.id=u.person_id WHERE u.username=? AND p.first_name=? AND p.last_name='Démo' AND u.login_email=? AND p.email=u.login_email)`, account.username, account.firstName, account.username+"@clubcore.invalid"); err != nil {
			return err
		}
	}
	for username, role := range accounts {
		if err := showcaseCheck(ctx, tx, "compte "+username, `SELECT EXISTS(SELECT 1 FROM users u JOIN persons p ON p.id=u.person_id WHERE u.username=? AND u.is_active AND u.activated_at IS NOT NULL AND p.archived_at IS NULL AND u.password_hash IS NOT NULL
 AND (SELECT count(*) FROM user_roles WHERE user_id=u.id)=CASE WHEN ?='' THEN 0 ELSE 1 END
 AND (?='' OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.name=?)))`, username, role, role, role); err != nil {
			return err
		}
		if _, err := service.Authenticate(ctx, username, password); err != nil {
			return fmt.Errorf("%w : authentification %s", ErrDemoInvalid, username)
		}
		var hash string
		if err := tx.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username=?", username).Scan(&hash); err != nil {
			return err
		}
		for _, previous := range hashes {
			if previous == hash {
				return fmt.Errorf("%w : sels bcrypt distincts requis", ErrDemoInvalid)
			}
		}
		hashes = append(hashes, hash)
	}
	ids := map[string]int64{}
	for _, p := range showcasePeople {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM persons WHERE first_name=? AND last_name=? AND birth_date=? AND coalesce(email,'')=? AND coalesce(phone_number,'')=? AND coalesce(address,'')=? AND archived_at IS NULL`, p.first, p.last, p.birth, p.email, p.phone, p.address).Scan(&id); err != nil {
			return fmt.Errorf("%w : personne %s", ErrDemoInvalid, p.key)
		}
		ids[p.key] = id
		if _, interactive := accounts[p.key]; interactive {
			if err := showcaseCheck(ctx, tx, "liaison compte", `SELECT EXISTS(SELECT 1 FROM users WHERE username=? AND person_id=?)`, p.key, id); err != nil {
				return err
			}
		}
	}
	for _, child := range []string{"arthur", "louise", "hugo", "emma"} {
		if err := showcaseCheck(ctx, tx, "famille "+child, `SELECT
 EXISTS(SELECT 1 FROM person_guardians WHERE child_person_id=? AND guardian_person_id=? AND relationship_type='mother' AND is_primary_contact)
 AND EXISTS(SELECT 1 FROM guardian_access_grants WHERE child_person_id=? AND guardian_person_id=? AND revoked_at IS NULL AND granted_by_user_id=(SELECT id FROM users WHERE username='president.demo'))`, ids[child], ids["parent.demo"], ids[child], ids["parent.demo"]); err != nil {
			return err
		}
	}
	for _, c := range showcaseContacts {
		if err := showcaseCheck(ctx, tx, "contact", `SELECT EXISTS(SELECT 1 FROM person_emergency_contacts WHERE person_id=? AND contact_person_id=? AND relationship_label=? AND priority=?)`, ids[c.owner], ids[c.contact], c.relationship, c.priority); err != nil {
			return err
		}
	}
	for _, m := range showcaseMemberships {
		var mid int64
		if err := tx.QueryRowContext(ctx, `SELECT m.id FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types mt ON mt.id=m.membership_type_id WHERE m.person_id=? AND s.name=? AND mt.name=? AND m.status=?`, ids[m.person], m.season, m.kind, m.status).Scan(&mid); err != nil {
			return fmt.Errorf("%w : adhésion %s %s", ErrDemoInvalid, m.person, m.season)
		}
		activities := 1
		if m.traditional {
			activities = 2
		}
		if err := showcaseCheck(ctx, tx, "pratique", `SELECT (SELECT count(*) FROM membership_groups WHERE membership_id=?)=?
 AND (SELECT count(*) FROM membership_activities ma JOIN activities a ON a.id=ma.activity_id WHERE ma.membership_id=? AND (a.name='Jiu-Jitsu Brésilien' OR (? AND a.name='Jiu-Jitsu Traditionnel / Combat')))=?
 AND EXISTS(SELECT 1 FROM membership_consent_requirements cr JOIN consent_definitions cd ON cd.id=cr.consent_definition_id WHERE cr.membership_id=? AND cd.code='image_rights')`, mid, len(m.groups), mid, m.traditional, activities, mid); err != nil {
			return err
		}
		for _, group := range m.groups {
			if err := showcaseCheck(ctx, tx, "groupe", `SELECT EXISTS(SELECT 1 FROM membership_groups mg JOIN groups g ON g.id=mg.group_id JOIN memberships m ON m.id=mg.membership_id JOIN membership_type_groups mtg ON mtg.group_id=g.id AND mtg.membership_type_id=m.membership_type_id JOIN membership_activities ma ON ma.membership_id=m.id AND ma.activity_id=g.activity_id WHERE mg.membership_id=? AND g.name=? AND ((m.status='active' AND mg.left_at IS NULL) OR (m.status='ended' AND mg.left_at=m.ended_at)))`, mid, group); err != nil {
				return err
			}
		}
		decisions := 0
		if m.decision != "" {
			decisions = 1
		}
		if err := showcaseCheck(ctx, tx, "décision", `SELECT (SELECT count(*) FROM membership_consents WHERE membership_id=?)=?`, mid, decisions); err != nil {
			return err
		}
		if m.decision != "" {
			if err := showcaseCheck(ctx, tx, "consentement", `SELECT EXISTS(SELECT 1 FROM membership_consents mc JOIN consent_definitions cd ON cd.id=mc.consent_definition_id WHERE mc.membership_id=? AND cd.code='image_rights' AND mc.decision=? AND mc.given_by_person_id=?)`, mid, m.decision, ids[m.giver]); err != nil {
				return err
			}
		}
	}
	if err := showcaseCheck(ctx, tx, "états vides", `SELECT
 NOT EXISTS(SELECT 1 FROM memberships WHERE person_id IN (?,?))
 AND NOT EXISTS(SELECT 1 FROM person_emergency_contacts WHERE person_id IN (?,?,?,?))
 AND NOT EXISTS(SELECT 1 FROM person_guardians WHERE guardian_person_id=?)
 AND NOT EXISTS(SELECT 1 FROM guardian_access_grants WHERE guardian_person_id IN (SELECT person_id FROM users WHERE username<>'parent.demo'))`, ids["hugo"], ids["empty.demo"], ids["louise"], ids["hugo"], ids["emma"], ids["empty.demo"], ids["empty.demo"]); err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	violated := rows.Next()
	rowErr = rows.Err()
	rows.Close()
	if rowErr != nil {
		return rowErr
	}
	if violated {
		return fmt.Errorf("%w : clé étrangère", ErrDemoInvalid)
	}
	var integrity string
	if err := tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("%w : intégrité SQLite", ErrDemoInvalid)
	}
	return nil
}

func checkShowcaseCatalogue(ctx context.Context, tx *sql.Tx) error {
	return showcaseCheck(ctx, tx, "créneaux et compatibilités Budokan", `WITH expected(group_name,weekday,start_time,end_time,practice) AS (VALUES
 ('Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans',1,'18:15','19:15',''),
 ('Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans',1,'19:15','20:15',''),
 ('JJB Adolescents et Adultes',1,'20:15','22:00','Préparation physique / Jiu-Jitsu Brésilien'),
 ('JJB enfants 7–10 ans',2,'18:15','19:15',''),
 ('JJB enfants 10–14 ans',2,'19:15','20:15',''),
 ('JJB Adolescents et Adultes',2,'20:15','22:00',''),
 ('JJB enfants 7–10 ans',3,'18:15','19:15',''),
 ('JJB enfants 10–14 ans',3,'19:15','20:15',''),
 ('JJB Adolescents et Adultes',3,'20:15','22:00',''),
 ('Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans',4,'18:15','19:15',''),
 ('Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans',4,'19:15','20:15',''),
 ('JJB Adolescents et Adultes',4,'20:15','22:00',''),
 ('JJB Adolescents et Adultes',5,'20:15','22:00','Préparation physique / JJB — Adolescents et Adultes'),
 ('JJB Adolescents et Adultes',6,'10:00','12:00','JJB No-Gi'),
 ('JJB Adolescents et Adultes',6,'16:00','18:00',''),
 ('JJB Adolescents et Adultes',7,'16:00','18:00','JJB libre'))
 SELECT NOT EXISTS(SELECT 1 FROM expected e WHERE (SELECT count(*) FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN activities a ON a.id=g.activity_id
 WHERE g.name=e.group_name AND a.name=CASE WHEN e.group_name LIKE 'JJB%' THEN 'Jiu-Jitsu Brésilien' ELSE 'Jiu-Jitsu Traditionnel / Combat' END
 AND gs.weekday=e.weekday AND gs.start_time=e.start_time AND gs.end_time=e.end_time AND coalesce(gs.practice_label,'')=e.practice)<>1)
 AND (SELECT count(*) FROM membership_type_groups mtg JOIN membership_types mt ON mt.id=mtg.membership_type_id JOIN groups g ON g.id=mtg.group_id
 WHERE (mt.name IN ('Adulte','Adolescent') AND g.name='JJB Adolescents et Adultes') OR (mt.name='Enfant' AND g.name IN ('JJB enfants 7–10 ans','JJB enfants 10–14 ans','Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans','Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans')))=6`)
}
