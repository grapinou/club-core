package application

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/memberships"
	"github.com/pressly/goose/v3"
)

func p43NoTechnicalLabels(t *testing.T, body string) {
	t.Helper()
	// URLs and hidden fields are deliberately allowed.
	if regexp.MustCompile(`(?i)(fiche|person|compte|adhésion)\s*(#|n°)\s*\d+`).MatchString(body) {
		t.Fatal("technical label exposed")
	}
}
func p43Secretary(f *fixture) *browser {
	b := f.membershipAdminBrowser()
	f.exec(`DELETE FROM user_roles WHERE user_id=?1`, f.approver)
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name='secretary'`, f.approver)
	return b
}
func TestP43NavigationCategoriesAndPolicy(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	m := f.request()
	f.exec(`UPDATE memberships SET status='active' WHERE id=?1`, m.ID)
	prospect := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Camille','Homonyme','1990-01-01') RETURNING id`)
	contact := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Camille','Homonyme','1980-01-01') RETURNING id`)
	f.id(`INSERT INTO trial_registrations(person_id,activity_id,trial_date,status) VALUES(?1,?2,CURRENT_DATE,'attended') RETURNING id`, prospect, f.activity)
	child, parent := f.guardianPair()
	f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES(?1,?2,'guardian')`, child, f.person)
	for path, title := range map[string]string{"/admin": "Mon tableau de bord", "/trials": "Essais", "/memberships": "Adhésions", "/members": "Membres actuels", "/guardians": "Responsables", "/prospects": "Prospects après essai", "/persons": "Annuaire"} {
		body := officeOK(t, b, path, title, "Secrétaire", `href="/admin"`, `href="/members"`, `href="/guardians"`, `href="/prospects"`)
		if !strings.Contains(body, `href="`+path+`" aria-current="page"`) {
			t.Fatal("active category", path)
		}
		if strings.Contains(body, `>Personnes</a>`) || strings.Contains(body, `href="/admin/config"`) || strings.Contains(body, `href="/admin/users"`) {
			t.Fatal("navigation or permissions")
		}
		p43NoTechnicalLabels(t, body)
	}
	for _, tc := range []struct {
		path            string
		present, absent []int32
	}{
		{"/members", []int32{f.person}, []int32{prospect, parent, contact}},
		{"/guardians", []int32{f.person, parent}, []int32{prospect, contact}},
		{"/prospects", []int32{prospect}, []int32{f.person, parent, contact}},
		{"/persons", []int32{f.person, parent, prospect, contact}, nil},
	} {
		body := officeOK(t, b, tc.path)
		for _, id := range tc.present {
			if !strings.Contains(body, `href="`+officePerson(id)+`"`) {
				t.Fatal("missing", tc.path, id)
			}
		}
		for _, id := range tc.absent {
			if strings.Contains(body, `href="`+officePerson(id)+`"`) {
				t.Fatal("wrong category", tc.path, id)
			}
		}
	}
	body := officeOK(t, b, "/persons", "01/01/1990", "01/01/1980")
	token := hiddenValue(t, body, "csrf_token")
	for _, path := range []string{"/members", "/guardians", "/prospects"} {
		if b.call("POST", path, url.Values{"search": {"Camille"}}).Code != 403 {
			t.Fatal("category search CSRF", path)
		}
	}
	r := b.call("POST", "/prospects", url.Values{"csrf_token": {token}, "search": {"Camille"}, "category": {"guardians"}})
	if r.Code != 200 || !strings.Contains(r.Body.String(), officePerson(prospect)) || strings.Contains(r.Body.String(), officePerson(contact)) {
		t.Fatal("scoped search")
	}
	f.exec(`UPDATE memberships SET status='ended' WHERE id=?1`, m.ID)
	if strings.Contains(officeOK(t, b, "/members"), `href="`+officePerson(f.person)+`"`) {
		t.Fatal("historical member presented as current")
	}
	f.exec(`UPDATE memberships SET status='active' WHERE id=?1`, m.ID)
	f.exec(`UPDATE seasons SET starts_at='2024-09-01',ends_at='2025-08-31' WHERE id=?1`, f.season)
	if strings.Contains(officeOK(t, b, "/members"), `href="`+officePerson(f.person)+`"`) {
		t.Fatal("past season presented as current")
	}
	f.exec(`INSERT INTO organizations(name,is_active,max_trials_per_person_per_season) VALUES('Association',true,3)`)
	body = officeOK(t, b, "/trials", "3 séances maximum par personne et par saison")
	if strings.Contains(body, "Modifier la politique") {
		t.Fatal("secretary configure")
	}
	f.exec(`INSERT INTO user_roles(user_id,role_id) SELECT ?1,id FROM roles WHERE name='president'`, f.approver)
	officeOK(t, b, "/trials", "Président", "Secrétaire", "Modifier la politique")
	f.exec(`UPDATE organizations SET max_trials_per_person_per_season=NULL`)
	officeOK(t, b, "/trials", "Aucune limite configurée")
	_, ordinary := f.personalBrowser(parent, "ordinary.p43")
	body = f.personalOK(ordinary, "/dashboard", "Mon espace")
	if strings.Contains(body, "Mon tableau de bord") {
		t.Fatal("false office dashboard")
	}
	for _, path := range []string{"/members", "/guardians", "/prospects", "/admin/config"} {
		if ordinary.call("GET", path, nil).Code != 403 {
			t.Fatal("RBAC", path)
		}
	}
}

func TestP43FamilyAccessAndPendingRequests(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	child, parent := f.guardianPair()
	_, family := f.personalBrowser(parent, "family.p43")
	ctx := f.authenticatedContext(f.approver)
	m, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: child, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}})
	f.must(err)
	for _, path := range []string{officePerson(child), dossierPath(m.ID)} {
		officeOK(t, b, path, "Aucun responsable ne dispose actuellement d’un accès familial")
	}
	_, err = f.app.GuardianAccess.Grant(ctx, child, parent)
	f.must(err)
	second := f.id(`INSERT INTO persons(first_name,last_name,birth_date,email) VALUES('Second','Responsable','1980-01-01','second@example.test') RETURNING id`)
	f.personalBrowser(second, "second.p43")
	f.exec(`INSERT INTO person_guardians(child_person_id,guardian_person_id,relationship_type) VALUES(?1,?2,'guardian')`, child, second)
	_, err = f.app.GuardianAccess.Grant(ctx, child, second)
	f.must(err)
	for _, path := range []string{officePerson(child), dossierPath(m.ID)} {
		title := "Accès à l’espace de l’enfant"
		if path == officePerson(child) {
			title = "Accès familial"
		}
		body := officeOK(t, b, path, title, "Compte activé", "Claire", "Second", officePerson(parent), officePerson(second))
		if strings.Contains(body, "Aucun compte") || strings.Contains(body, "compte manquant") {
			t.Fatal("false missing child account")
		}
		p43NoTechnicalLabels(t, body)
	}
	f.personalBrowser(child, "historical.child.p43")
	for _, path := range []string{officePerson(child), dossierPath(m.ID)} {
		title := "Accès familiaux des responsables"
		if path == officePerson(child) {
			title = "Accès actuellement utilisables"
		}
		officeOK(t, b, path, "Compte personnel de l’enfant", "historical.child.p43", title)
	}
	f.must(f.app.GuardianAccess.Revoke(ctx, child, second))
	d, err := f.app.Administration.Person(ctx, child)
	f.must(err)
	if len(d.EffectiveGuardians) != 1 {
		t.Fatal("revoked guardian still effective")
	}
	v := f.familyForm(family, "/me/children/new")
	v.Set("first_name", "Nouvel enfant P43")
	v.Set("last_name", "Famille P43")
	if r := family.call("POST", "/me/children/new", v); r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	sub := f.id(`SELECT max(submission_id) FROM registration_applications`)
	body := f.personalOK(family, "/dashboard", "Nouvel enfant P43", "Demandes en cours", "en cours de traitement", "L’équipe du club")
	for _, term := range []string{"candidate", "strong match", "weak match", "guardian claim", "identity unresolved", "needs_review", "person_id", "submission_id"} {
		if strings.Contains(body, term) {
			t.Fatal("matching leaked", term)
		}
	}
	_, stranger := f.personalBrowser(f.person, "outsider.p43")
	if strings.Contains(f.personalOK(stranger, "/dashboard"), "Nouvel enfant P43") {
		t.Fatal("pending request leaked")
	}
	p43NoTechnicalLabels(t, officeOK(t, b, reviewPath(sub)))
	f.must(f.app.RegistrationApplications.ConfirmGuardian(ctx, sub))
	body = f.personalOK(family, "/dashboard", "Nouvel enfant P43")
	if strings.Contains(body, "Demandes en cours") {
		t.Fatal("completed still pending")
	}
}

func TestP43TrialGroupDefaultAndConcurrency(t *testing.T) {
	f := newFixture(t)
	b := p43Secretary(f)
	g, _ := f.officeGroup()
	ctx := f.authenticatedContext(f.approver)
	makeTrial := func(group any) (int32, int32) {
		p := f.id(`INSERT INTO persons(first_name,last_name,birth_date) VALUES('Conversion','Groupe','1990-01-01') RETURNING id`)
		tr := f.id(`INSERT INTO trial_registrations(person_id,activity_id,group_id,trial_date,status) VALUES(?1,?2,?3,CURRENT_DATE,'attended') RETURNING id`, p, f.activity, group)
		return p, tr
	}
	request := func(p int32) memberships.Request {
		return memberships.Request{PersonID: p, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}}
	}
	p, tr := makeTrial(g)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := f.app.Administration.RequestMembership(ctx, request(p), tr)
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		}
	}
	if successes != 1 || f.count(`SELECT count(*) FROM membership_groups mg JOIN memberships m ON m.id=mg.membership_id WHERE m.person_id=?1`, p) != 1 {
		t.Fatal("concurrent conversion")
	}
	id := f.id(`SELECT id FROM memberships WHERE person_id=?1`, p)
	officeOK(t, b, dossierPath(id), "Groupe repris de l’essai : Groupe adultes")
	if f.count(`SELECT count(*) FROM administrative_events WHERE action='membership_trial_group_assigned' AND resource_id=?1`, id) != 1 {
		t.Fatal("assignment audit")
	}
	assignment := f.id(`SELECT id FROM membership_groups WHERE membership_id=?1`, id)
	officeOK(t, b, dossierPath(id)+"/groups", "Clore l’affectation")
	officePost(t, b, fmt.Sprintf("/memberships/%d/groups/%d/close", id, assignment), url.Values{"left_at": {time.Now().UTC().Format("2006-01-02")}}, 303)
	officePost(t, b, dossierPath(id)+"/groups", url.Values{"group_id": {fmt.Sprint(g)}, "joined_at": {time.Now().UTC().Format("2006-01-02")}}, 303)
	if f.count(`SELECT count(*) FROM membership_groups WHERE membership_id=?1`, id) != 2 {
		t.Fatal("history modified")
	}
	p, tr = makeTrial(nil)
	id, err := f.app.Administration.RequestMembership(ctx, request(p), tr)
	f.must(err)
	if f.count(`SELECT count(*) FROM membership_groups WHERE membership_id=?1`, id) != 0 {
		t.Fatal("invented group")
	}
	p, tr = makeTrial(g)
	f.exec(`UPDATE groups SET is_active=false WHERE id=?1`, g)
	id, err = f.app.Administration.RequestMembership(ctx, request(p), tr)
	f.must(err)
	if f.count(`SELECT count(*) FROM membership_groups WHERE membership_id=?1`, id) != 0 {
		t.Fatal("invalid assignment")
	}
	officeOK(t, b, dossierPath(id), "Groupe de l’essai à vérifier")
	if f.count(`SELECT count(*) FROM administrative_events WHERE action='membership_trial_group_skipped' AND resource_id=?1`, id) != 1 {
		t.Fatal("skip audit")
	}
	// Future seasons begin at season opening, not on the historical trial date.
	f.exec(`UPDATE groups SET is_active=true WHERE id=?1`, g)
	future := f.id(`INSERT INTO seasons(name,starts_at,ends_at) VALUES('Future','2028-09-01','2029-08-31') RETURNING id`)
	p, tr = makeTrial(g)
	req := request(p)
	req.SeasonID = future
	id, err = f.app.Administration.RequestMembership(ctx, req, tr)
	f.must(err)
	if f.count(`SELECT count(*) FROM membership_groups WHERE membership_id=?1 AND joined_at='2028-09-01'`, id) != 1 {
		t.Fatal("future join date")
	}
	// Audit failure rolls membership and assignment back together.

	f.exec(`CREATE TRIGGER reject_p43_audit BEFORE INSERT ON administrative_events WHEN NEW.action='membership_trial_group_assigned' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END;`)
	p, tr = makeTrial(g)
	if _, err = f.app.Administration.RequestMembership(ctx, request(p), tr); err == nil {
		t.Fatal("audit failure ignored")
	}
	if f.count(`SELECT count(*) FROM memberships WHERE person_id=?1`, p) != 0 {
		t.Fatal("partial conversion")
	}
}

func TestP43GroupAuditMigrationPreservesHistory(t *testing.T) {
	f := newFixture(t)
	db, err := openTestConnection(t, f.db)
	f.must(err)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../../migrations"))
	f.must(err)
	_, err = provider.Up(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	group, _ := f.officeGroup()
	trial := f.id(`INSERT INTO trial_registrations(person_id,activity_id,group_id,trial_date,status) VALUES(?1,?2,?3,CURRENT_DATE,'attended') RETURNING id`, f.person, f.activity, group)
	old, err := f.memberships.CreateRequest(t.Context(), memberships.Request{PersonID: f.person, SeasonID: f.season, MembershipTypeID: f.kind, ActivityIDs: []int32{f.activity}, SourceTrialID: trial})
	f.must(err)
	_, err = provider.Up(t.Context())
	f.must(err)
	if f.count(`SELECT count(*) FROM membership_groups WHERE membership_id=?1`, old.ID) != 0 {
		t.Fatal("historical backfill")
	}
	office := f.membershipAdminBrowser()
	body := officeOK(t, office, dossierPath(old.ID))
	if strings.Contains(body, "Groupe repris de l’essai") || strings.Contains(body, "Groupe de l’essai à vérifier") {
		t.Fatal("invented historical outcome")
	}
	f.exec(`INSERT INTO administrative_events(actor_user_id,action,resource_type,resource_id) VALUES(?1,'membership_trial_group_skipped','membership',?2)`, f.approver, old.ID)
	if _, err = provider.DownTo(t.Context(), 0); err == nil {
		t.Fatal("lossy audit rollback")
	}
	if f.count(`SELECT count(*) FROM administrative_events WHERE resource_id=?1 AND action='membership_trial_group_skipped'`, old.ID) != 1 {
		t.Fatal("audit lost")
	}
}
