// Package clubconfig manages the one association in this database. All writes use
// the existing role policy and administrative audit in a single transaction.
package clubconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("invalid club configuration")
var ErrDependency = errors.New("complete the preceding configuration steps first")
var ErrHistorical = errors.New("this item has historical references")

type Option struct{ Value, Label string }
type Field struct {
	Name, Label, Type, Help string
	Required                bool
	Options                 []Option
}
type Record struct {
	ID     int32
	Name   string
	Values map[string]string
	Active bool
}
type Section struct {
	Key, Label, Hint, Resource string
	Fields                     []Field
	Records                    []Record
	Required                   bool
	Done                       bool
	Singleton                  bool
	Blocked                    string
}
type Snapshot struct {
	Sections []Section
	Ready    bool
}
type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

func sectionDefinitions() []Section {
	return []Section{
		{Key: "association", Label: "Association", Hint: "Nom et coordonnées affichés sur votre site.", Resource: "organization", Required: true, Singleton: true, Fields: []Field{{"name", "Nom de l’association", "text", "", true, nil}, {"short_name", "Nom court", "text", "Facultatif.", false, nil}, {"description", "Présentation", "textarea", "Quelques phrases pour l’accueil.", false, nil}, {"public_email", "Email public", "email", "", false, nil}, {"public_phone", "Téléphone public", "tel", "", false, nil}, {"public_phone_label", "Libellé du téléphone", "text", "Ex. permanence téléphonique.", false, nil}, {"correspondence_address", "Adresse de correspondance", "textarea", "", false, nil}, {"website_url", "Site externe", "url", "", false, nil}, {"max_trials_per_person_per_season", "Nombre maximal de séances d’essai", "number", "Laisser vide pour ne pas limiter le nombre d’essais. La limite s’applique par personne et par saison.", false, nil}}},
		{Key: "saisons", Label: "Saisons", Hint: "Définissez la période utilisée par les horaires et adhésions. Deux saisons actives ne doivent pas se chevaucher.", Resource: "season", Required: true, Fields: []Field{{"name", "Nom de la saison", "text", "Ex. 2026/2027.", true, nil}, {"starts_at", "Début", "date", "", true, nil}, {"ends_at", "Fin", "date", "", true, nil}}},
		{Key: "lieux", Label: "Lieux", Hint: "Indiquez où se déroulent les séances. Désactivez un lieu ancien pour conserver l’historique.", Resource: "location", Required: true, Fields: []Field{{"name", "Nom du lieu", "text", "", true, nil}, {"address", "Adresse", "textarea", "", true, nil}}},
		{Key: "activites", Label: "Activités", Hint: "Une activité peut être sportive, culturelle ou associative.", Resource: "activity", Required: true, Fields: []Field{{"name", "Nom de l’activité", "text", "Ex. Échecs, chorale, tennis.", true, nil}}},
		{Key: "groupes", Label: "Groupes", Hint: "Un groupe rassemble des participants dans la durée. Ajoutez ensuite un ou plusieurs horaires à ce groupe.", Resource: "group", Required: true, Fields: []Field{{"activity_id", "Activité", "select", "", true, nil}, {"name", "Nom du groupe", "text", "", true, nil}, {"description", "Description", "textarea", "", false, nil}, {"show_name_publicly", "Afficher le nom sur le site", "checkbox", "", false, nil}}},
		{Key: "horaires", Label: "Horaires", Hint: "Un horaire est le moment où un groupe se retrouve pendant une saison.", Resource: "group_slot", Required: true, Fields: []Field{{"group_id", "Groupe", "select", "", true, nil}, {"season_id", "Saison", "select", "", true, nil}, {"weekday", "Jour", "select", "", true, []Option{{"1", "Lundi"}, {"2", "Mardi"}, {"3", "Mercredi"}, {"4", "Jeudi"}, {"5", "Vendredi"}, {"6", "Samedi"}, {"7", "Dimanche"}}}, {"start_time", "Début", "time", "", true, nil}, {"end_time", "Fin", "time", "", true, nil}, {"location_id", "Lieu", "select", "", true, nil}, {"valid_from", "Valable du", "date", "Dans les dates de la saison.", true, nil}, {"valid_until", "Jusqu’au", "date", "Facultatif, sinon fin de saison.", false, nil}, {"practice_label", "Nom public de la séance", "text", "Facultatif.", false, nil}}},
		{Key: "tarifs", Label: "Adhésions et tarifs", Hint: "Un montant simple par type d’adhésion. Laissez le montant vide pour demander de contacter l’association.", Resource: "membership_type", Fields: []Field{{"name", "Type d’adhésion", "text", "", true, nil}, {"amount", "Montant", "number", "En euros, ex. 120,00.", false, nil}, {"currency", "Devise", "select", "", true, []Option{{"EUR", "EUR"}, {"USD", "USD"}, {"GBP", "GBP"}, {"CHF", "CHF"}}}, {"public_note", "Précision publique", "textarea", "", false, nil}}},
		{Key: "contenu", Label: "Contenu public", Hint: "Informations utiles pour les pages d’essai et de règlement intérieur.", Resource: "organization", Singleton: true, Fields: []Field{{"trial_session_description", "Déroulement de la première séance", "textarea", "", false, nil}, {"trial_items_to_bring", "À apporter", "textarea", "Un élément par ligne.", false, nil}, {"trial_equipment_offer", "Prêt de matériel", "textarea", "Laissez vide si vous n’en proposez pas.", false, nil}, {"trial_equipment_detail_prompt", "Précision sur le matériel", "text", "", false, nil}, {"public_rules_description", "Règlement intérieur — présentation", "textarea", "Présentez comment obtenir le règlement complet.", false, nil}}},
		{Key: "liens", Label: "Liens publics", Hint: "Réseaux sociaux et autres liens utiles.", Resource: "organization_link", Fields: []Field{{"kind", "Type de lien", "text", "Ex. instagram, facebook, site.", true, nil}, {"label", "Libellé", "text", "", true, nil}, {"url", "Adresse web", "url", "", true, nil}, {"position", "Ordre d’affichage", "number", "", true, nil}}},
		{Key: "images", Label: "Images publiques", Hint: "Référencez une image déjà installée dans /static/images. L’ajout de fichiers relève de la maintenance technique.", Resource: "organization_public_image", Fields: []Field{{"placement", "Emplacement", "select", "", true, []Option{{"hero", "Accueil"}, {"activity", "Activités"}, {"community", "Vie du club"}, {"schedule", "Horaires"}, {"trial", "Essai"}}}, {"src", "Chemin de l’image", "text", "Ex. /static/images/association/photo.jpg.", true, nil}, {"alt", "Description de l’image", "text", "", true, nil}, {"width", "Largeur en pixels", "number", "", true, nil}, {"height", "Hauteur en pixels", "number", "", true, nil}}},
	}
}
func readRecords(ctx context.Context, db *pgxpool.Pool, key string) ([]Record, error) {
	queries := map[string]string{
		"association": `SELECT id,name,jsonb_build_object('name',name,'short_name',coalesce(short_name,''),'description',coalesce(description,''),'public_email',coalesce(public_email,''),'public_phone',coalesce(public_phone,''),'public_phone_label',coalesce(public_phone_label,''),'correspondence_address',coalesce(correspondence_address,''),'website_url',coalesce(website_url,''),'max_trials_per_person_per_season',coalesce(max_trials_per_person_per_season::text,'')),is_active FROM organizations WHERE is_active ORDER BY id`,
		"contenu":     `SELECT id,name,jsonb_build_object('trial_session_description',coalesce(trial_session_description,''),'trial_items_to_bring',array_to_string(trial_items_to_bring,E'\n'),'trial_equipment_offer',coalesce(trial_equipment_offer,''),'trial_equipment_detail_prompt',coalesce(trial_equipment_detail_prompt,''),'public_rules_description',coalesce(public_rules_description,'')),is_active FROM organizations WHERE is_active ORDER BY id`,
		"saisons":     `SELECT id,name,jsonb_build_object('name',name,'starts_at',starts_at::text,'ends_at',ends_at::text),is_active FROM seasons ORDER BY starts_at DESC,id DESC`,
		"lieux":       `SELECT id,name,jsonb_build_object('name',name,'address',address),is_active FROM locations WHERE organization_id=(SELECT id FROM organizations WHERE is_active) ORDER BY name,id`,
		"activites":   `SELECT id,name,jsonb_build_object('name',name),is_active FROM activities ORDER BY name,id`,
		"groupes":     `SELECT id,name,jsonb_build_object('activity_id',activity_id::text,'name',name,'description',coalesce(description,''),'show_name_publicly',CASE WHEN show_name_publicly THEN 'yes' ELSE '' END),is_active FROM groups ORDER BY name,id`,
		"horaires":    `SELECT gs.id,g.name||' · '||gs.weekday||' · '||to_char(gs.start_time,'HH24:MI'),jsonb_build_object('group_id',gs.group_id::text,'season_id',gs.season_id::text,'weekday',gs.weekday::text,'start_time',to_char(gs.start_time,'HH24:MI'),'end_time',to_char(gs.end_time,'HH24:MI'),'location_id',coalesce(gs.location_id::text,''),'valid_from',gs.valid_from::text,'valid_until',coalesce(gs.valid_until::text,''),'practice_label',coalesce(gs.practice_label,'')),gs.is_active FROM group_slots gs JOIN groups g ON g.id=gs.group_id ORDER BY gs.valid_from DESC,gs.id DESC`,
		"tarifs":      `SELECT id,name,jsonb_build_object('name',name,'amount',CASE WHEN amount_cents IS NULL THEN '' ELSE to_char(amount_cents/100.0,'FM99999990.00') END,'currency',currency,'public_note',coalesce(public_note,'')),is_active FROM membership_types ORDER BY name,id`,
		"liens":       `SELECT id,label,jsonb_build_object('kind',kind,'label',label,'url',url,'position',position::text),is_active FROM organization_links WHERE organization_id=(SELECT id FROM organizations WHERE is_active) ORDER BY position,id`,
		"images":      `SELECT CASE placement WHEN 'hero' THEN 1 WHEN 'activity' THEN 2 WHEN 'community' THEN 3 WHEN 'schedule' THEN 4 ELSE 5 END,placement,jsonb_build_object('placement',placement,'src',src,'alt',alt,'width',width::text,'height',height::text),true FROM organization_public_images WHERE organization_id=(SELECT id FROM organizations WHERE is_active) ORDER BY placement`,
	}
	rows, err := db.Query(ctx, queries[key])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Name, &raw, &r.Active); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &r.Values); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	var result Snapshot
	actor, ok := auth.UserID(ctx)
	if !ok {
		return result, authorization.ErrForbidden
	}
	q := dbsqlc.New(s.db)
	u, err := q.GetUserByID(ctx, actor)
	if err != nil || !u.IsActive || !u.ActivatedAt.Valid || !u.PasswordHash.Valid {
		return result, authorization.ErrForbidden
	}
	allowed, err := authorization.New(q).HasPermission(ctx, actor, authorization.ClubConfigure)
	if err != nil {
		return result, err
	}
	if !allowed {
		return result, authorization.ErrForbidden
	}
	sections := sectionDefinitions()
	for i := range sections {
		records, err := readRecords(ctx, s.db, sections[i].Key)
		if err != nil {
			return result, err
		}
		sections[i].Records = records
		if sections[i].Key == "association" {
			sections[i].Done = len(records) > 0
		} else if sections[i].Key == "contenu" {
			sections[i].Done = len(records) > 0 && (records[0].Values["trial_session_description"] != "" || records[0].Values["trial_items_to_bring"] != "" || records[0].Values["public_rules_description"] != "")
		} else {
			for _, r := range records {
				if r.Active {
					sections[i].Done = true
					break
				}
			}
		}
	}
	result.Ready = true
	for _, sec := range sections {
		if sec.Required && !sec.Done {
			result.Ready = false
		}
	}
	if len(sections[0].Records) == 0 {
		for i := 1; i < len(sections); i++ {
			sections[i].Blocked = "Commencez par renseigner votre association."
		}
	} else {
		if !sections[3].Done {
			sections[4].Blocked = "Créez d’abord une activité active."
		}
		if !sections[4].Done || !sections[1].Done || !sections[2].Done {
			sections[5].Blocked = "Créez une saison, un lieu et un groupe actifs avant d’ajouter un horaire."
		}
	}
	activeActivities := map[string]bool{}
	for _, r := range sections[3].Records {
		if r.Active {
			activeActivities[strconv.Itoa(int(r.ID))] = true
		}
	}
	options := func(key string) []Option {
		for _, sec := range sections {
			if sec.Key == key {
				var out []Option
				for _, r := range sec.Records {
					if r.Active && (key != "groupes" || activeActivities[r.Values["activity_id"]]) {
						out = append(out, Option{strconv.Itoa(int(r.ID)), r.Name})
					}
				}
				return out
			}
		}
		return nil
	}
	for i := range sections {
		for j := range sections[i].Fields {
			f := &sections[i].Fields[j]
			switch f.Name {
			case "activity_id":
				f.Options = options("activites")
			case "group_id":
				f.Options = options("groupes")
			case "season_id":
				f.Options = options("saisons")
			case "location_id":
				f.Options = options("lieux")
			}
		}
	}
	result.Sections = sections
	return result, nil
}
func validText(v string, required bool, max int) (string, error) {
	v = strings.TrimSpace(v)
	if (required && v == "") || len(v) > max || strings.ContainsRune(v, 0) {
		return "", ErrInvalid
	}
	return v, nil
}
func validURL(v string, required bool) (string, error) {
	v, e := validText(v, required, 2048)
	if e != nil || v == "" {
		return v, e
	}
	u, e := url.Parse(v)
	if e != nil || !(u.Scheme == "https" || u.Scheme == "http") || u.Host == "" || u.User != nil {
		return "", ErrInvalid
	}
	return v, nil
}
func validEmail(v string) (string, error) {
	v, e := validText(v, false, 254)
	if e != nil || v == "" {
		return v, e
	}
	a, e := mail.ParseAddress(v)
	if e != nil || a.Address != v {
		return "", ErrInvalid
	}
	return v, nil
}
func parseID(v string) (int32, error) {
	n, e := strconv.ParseInt(v, 10, 32)
	if e != nil || n <= 0 {
		return 0, ErrInvalid
	}
	return int32(n), nil
}
func date(v string) (time.Time, error) {
	t, e := time.Parse("2006-01-02", v)
	if e != nil {
		return t, ErrInvalid
	}
	return t, nil
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func imageID(v string) int32 {
	switch v {
	case "hero":
		return 1
	case "activity":
		return 2
	case "community":
		return 3
	case "schedule":
		return 4
	case "trial":
		return 5
	}
	return 0
}
func cleanStatic(v string) bool {
	if !strings.HasPrefix(v, "/static/images/") || path.Clean(v) != v || len(v) >= 500 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '/' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func amountCents(v string) (any, error) {
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", "."))
	if v == "" {
		return nil, nil
	}
	parts := strings.Split(v, ".")
	if len(parts) > 2 || len(parts[0]) > 7 {
		return nil, ErrInvalid
	}
	whole, e := strconv.Atoi(parts[0])
	if e != nil || whole < 0 {
		return nil, ErrInvalid
	}
	cents := 0
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return nil, ErrInvalid
		}
		cents, e = strconv.Atoi(parts[1])
		if e != nil || cents < 0 {
			return nil, ErrInvalid
		}
		if len(parts[1]) == 1 {
			cents *= 10
		}
	}
	return whole*100 + cents, nil
}
func requiredReference(ctx context.Context, tx pgx.Tx, query string, id int32) error {
	var ok bool
	if err := tx.QueryRow(ctx, query, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrInvalid
	}
	return nil
}
func sqlSave(ctx context.Context, tx pgx.Tx, id int32, insert, update string, args ...any) (int32, error) {
	var out int32
	query := update
	if id == 0 {
		query = insert
	} else {
		args = append([]any{id}, args...)
	}
	err := tx.QueryRow(ctx, query, args...).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrInvalid
	}
	return out, err
}
func (s *Service) Save(ctx context.Context, key string, id int32, f url.Values) (int32, error) {
	var section *Section
	defs := sectionDefinitions()
	for i := range defs {
		if defs[i].Key == key {
			section = &defs[i]
			break
		}
	}
	if section == nil || id < 0 {
		return 0, ErrInvalid
	}
	for _, field := range section.Fields {
		if field.Type != "checkbox" && !(key == "images" && f.Get("remove") == "yes") {
			if _, e := validText(f.Get(field.Name), field.Required, 10000); e != nil {
				return 0, e
			}
		}
	}
	actor, ok := auth.UserID(ctx)
	if !ok {
		return 0, authorization.ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	u, err := q.GetUserByID(ctx, actor)
	if err != nil || !u.IsActive || !u.ActivatedAt.Valid || !u.PasswordHash.Valid {
		return 0, authorization.ErrForbidden
	}
	allowed, err := authorization.New(q).HasPermission(ctx, actor, authorization.ClubConfigure)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, authorization.ErrForbidden
	}
	var orgID int32
	err = tx.QueryRow(ctx, "SELECT id FROM organizations WHERE is_active FOR UPDATE").Scan(&orgID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if orgID == 0 && key != "association" {
		return 0, ErrDependency
	}
	if section.Singleton && id != 0 && id != orgID {
		return 0, ErrInvalid
	}
	if section.Singleton && orgID != 0 && id == 0 {
		id = orgID
	}
	active := f.Get("is_active") == "yes"
	var saved int32
	switch key {
	case "association":
		name := f.Get("name")
		var limit any
		if value := strings.TrimSpace(f.Get("max_trials_per_person_per_season")); value != "" {
			n, e := parseID(value)
			if e != nil {
				return 0, e
			}
			limit = n
		}
		email, e := validEmail(f.Get("public_email"))
		if e != nil {
			return 0, e
		}
		website, e := validURL(f.Get("website_url"), false)
		if e != nil {
			return 0, e
		}
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO organizations(name,short_name,description,public_email,public_phone,public_phone_label,correspondence_address,website_url,max_trials_per_person_per_season) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, `UPDATE organizations SET name=$2,short_name=$3,description=$4,public_email=$5,public_phone=$6,public_phone_label=$7,correspondence_address=$8,website_url=$9,max_trials_per_person_per_season=$10,updated_at=now() WHERE id=$1 AND is_active RETURNING id`, name, nullable(f.Get("short_name")), nullable(f.Get("description")), nullable(email), nullable(f.Get("public_phone")), nullable(f.Get("public_phone_label")), nullable(f.Get("correspondence_address")), nullable(website), limit)
	case "contenu":
		if orgID == 0 {
			return 0, ErrDependency
		}
		items := []string{}
		for _, v := range strings.Split(f.Get("trial_items_to_bring"), "\n") {
			v = strings.TrimSpace(v)
			if v != "" {
				if len(v) > 300 || len(items) >= 20 {
					return 0, ErrInvalid
				}
				items = append(items, v)
			}
		}
		err = tx.QueryRow(ctx, `UPDATE organizations SET trial_session_description=$2,trial_items_to_bring=$3,trial_equipment_offer=$4,trial_equipment_detail_prompt=$5,public_rules_description=$6,updated_at=now() WHERE id=$1 RETURNING id`, orgID, nullable(f.Get("trial_session_description")), items, nullable(f.Get("trial_equipment_offer")), nullable(f.Get("trial_equipment_detail_prompt")), nullable(f.Get("public_rules_description"))).Scan(&saved)
	case "saisons":
		start, e := date(f.Get("starts_at"))
		if e != nil {
			return 0, e
		}
		end, e := date(f.Get("ends_at"))
		if e != nil || end.Before(start) {
			return 0, ErrInvalid
		}
		if active {
			var overlap bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM seasons WHERE is_active AND id<>$1 AND starts_at<=$3 AND ends_at>=$2)`, id, start, end).Scan(&overlap)
			if err != nil {
				return 0, err
			}
			if overlap {
				return 0, ErrInvalid
			}
		}
		if id != 0 {
			var outside bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_slots WHERE season_id=$1 AND (valid_from<$2 OR valid_from>$3 OR valid_until>$3))`, id, start, end).Scan(&outside); err != nil {
				return 0, err
			}
			if outside {
				return 0, ErrHistorical
			}
		}
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO seasons(name,starts_at,ends_at,is_active) VALUES($1,$2,$3,$4) RETURNING id`, `UPDATE seasons SET name=$2,starts_at=$3,ends_at=$4,is_active=$5 WHERE id=$1 RETURNING id`, f.Get("name"), start, end, active)
	case "lieux":
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO locations(organization_id,name,address,is_active) VALUES($1,$2,$3,$4) RETURNING id`, `UPDATE locations SET name=$3,address=$4,is_active=$5,updated_at=now() WHERE id=$1 AND organization_id=$2 RETURNING id`, orgID, f.Get("name"), f.Get("address"), active)
	case "activites":
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO activities(name,is_active) VALUES($1,$2) RETURNING id`, `UPDATE activities SET name=$2,is_active=$3 WHERE id=$1 RETURNING id`, f.Get("name"), active)
	case "groupes":
		activity, e := parseID(f.Get("activity_id"))
		if e != nil {
			return 0, e
		}
		if active {
			if e = requiredReference(ctx, tx, "SELECT EXISTS(SELECT 1 FROM activities WHERE id=$1 AND is_active)", activity); e != nil {
				return 0, e
			}
		}
		if id != 0 {
			var previous int32
			if err = tx.QueryRow(ctx, `SELECT activity_id FROM groups WHERE id=$1 FOR UPDATE`, id).Scan(&previous); err != nil {
				return 0, ErrInvalid
			}
			if previous != activity {
				var used bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM membership_groups WHERE group_id=$1 UNION ALL SELECT 1 FROM group_slots WHERE group_id=$1 UNION ALL SELECT 1 FROM trial_registrations WHERE group_id=$1)`, id).Scan(&used); err != nil {
					return 0, err
				}
				if used {
					return 0, ErrHistorical
				}
			}
		}
		public := f.Get("show_name_publicly") == "yes"
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO groups(activity_id,name,description,is_active,show_name_publicly) VALUES($1,$2,$3,$4,$5) RETURNING id`, `UPDATE groups SET activity_id=$2,name=$3,description=$4,is_active=$5,show_name_publicly=$6,updated_at=now() WHERE id=$1 RETURNING id`, activity, f.Get("name"), nullable(f.Get("description")), active, public)
	case "horaires":
		group, e := parseID(f.Get("group_id"))
		if e != nil {
			return 0, e
		}
		season, e := parseID(f.Get("season_id"))
		if e != nil {
			return 0, e
		}
		location, e := parseID(f.Get("location_id"))
		if e != nil {
			return 0, e
		}
		weekday, e := strconv.Atoi(f.Get("weekday"))
		if e != nil || weekday < 1 || weekday > 7 {
			return 0, ErrInvalid
		}
		start := f.Get("start_time")
		end := f.Get("end_time")
		if _, e = time.Parse("15:04", start); e != nil {
			return 0, ErrInvalid
		}
		if _, e = time.Parse("15:04", end); e != nil || end <= start {
			return 0, ErrInvalid
		}
		from, e := date(f.Get("valid_from"))
		if e != nil {
			return 0, e
		}
		var until any
		if f.Get("valid_until") != "" {
			t, e := date(f.Get("valid_until"))
			if e != nil || t.Before(from) {
				return 0, ErrInvalid
			}
			until = t
		}
		if active {
			for _, ref := range []struct {
				query string
				id    int32
			}{{"SELECT EXISTS(SELECT 1 FROM groups g JOIN activities a ON a.id=g.activity_id WHERE g.id=$1 AND g.is_active AND a.is_active)", group}, {"SELECT EXISTS(SELECT 1 FROM locations WHERE id=$1 AND is_active AND organization_id=(SELECT id FROM organizations WHERE is_active))", location}, {"SELECT EXISTS(SELECT 1 FROM seasons WHERE id=$1 AND is_active)", season}} {
				if e = requiredReference(ctx, tx, ref.query, ref.id); e != nil {
					return 0, e
				}
			}
		}
		var seasonStart, seasonEnd time.Time
		err = tx.QueryRow(ctx, "SELECT starts_at,ends_at FROM seasons WHERE id=$1", season).Scan(&seasonStart, &seasonEnd)
		if err != nil {
			return 0, ErrInvalid
		}
		if from.Before(seasonStart) || from.After(seasonEnd) || until != nil && until.(time.Time).After(seasonEnd) {
			return 0, ErrInvalid
		}
		if id != 0 {
			var used bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trial_registrations WHERE group_slot_id=$1)`, id).Scan(&used); err != nil {
				return 0, err
			}
			if used {
				var oldActive bool
				if err = tx.QueryRow(ctx, `SELECT is_active FROM group_slots WHERE id=$1 FOR UPDATE`, id).Scan(&oldActive); err != nil {
					return 0, ErrInvalid
				}
				if active || !oldActive {
					return 0, ErrHistorical
				}
				if err = tx.QueryRow(ctx, `UPDATE group_slots SET is_active=false,updated_at=now() WHERE id=$1 RETURNING id`, id).Scan(&saved); err != nil {
					return 0, err
				}
				break
			}
		}
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location_id,valid_from,valid_until,practice_label,is_active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, `UPDATE group_slots SET group_id=$2,season_id=$3,weekday=$4,start_time=$5,end_time=$6,location_id=$7,location=NULL,valid_from=$8,valid_until=$9,practice_label=$10,is_active=$11,updated_at=now() WHERE id=$1 RETURNING id`, group, season, weekday, start, end, location, from, until, nullable(f.Get("practice_label")), active)
	case "tarifs":
		amount, e := amountCents(f.Get("amount"))
		if e != nil {
			return 0, e
		}
		currency := f.Get("currency")
		if currency != "EUR" && currency != "USD" && currency != "GBP" && currency != "CHF" {
			return 0, ErrInvalid
		}
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO membership_types(name,is_active,amount_cents,currency,public_note) VALUES($1,$2,$3,$4,$5) RETURNING id`, `UPDATE membership_types SET name=$2,is_active=$3,amount_cents=$4,currency=$5,public_note=$6 WHERE id=$1 RETURNING id`, f.Get("name"), active, amount, currency, nullable(f.Get("public_note")))
	case "liens":
		link, e := validURL(f.Get("url"), true)
		if e != nil {
			return 0, e
		}
		kind := f.Get("kind")
		if len(kind) > 50 || kind == "" {
			return 0, ErrInvalid
		}
		for _, c := range kind {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
				return 0, ErrInvalid
			}
		}
		position, e := strconv.Atoi(f.Get("position"))
		if e != nil || position < 0 || position > 1000 {
			return 0, ErrInvalid
		}
		saved, err = sqlSave(ctx, tx, id, `INSERT INTO organization_links(organization_id,kind,label,url,position,is_active) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, `UPDATE organization_links SET kind=$3,label=$4,url=$5,position=$6,is_active=$7,updated_at=now() WHERE id=$1 AND organization_id=$2 RETURNING id`, orgID, kind, f.Get("label"), link, position, active)
	case "images":
		if f.Get("remove") == "yes" {
			if id < 1 || id > 5 {
				return 0, ErrInvalid
			}
			var placement string
			if err = tx.QueryRow(ctx, `DELETE FROM organization_public_images WHERE organization_id=$1 AND placement=CASE $2 WHEN 1 THEN 'hero' WHEN 2 THEN 'activity' WHEN 3 THEN 'community' WHEN 4 THEN 'schedule' ELSE 'trial' END RETURNING placement`, orgID, id).Scan(&placement); err != nil {
				return 0, ErrInvalid
			}
			saved = id
			break
		}
		placement := f.Get("placement")
		if imageID(placement) == 0 || (id != 0 && id != imageID(placement)) || !cleanStatic(f.Get("src")) {
			return 0, ErrInvalid
		}
		width, e := strconv.Atoi(f.Get("width"))
		if e != nil || width <= 0 || width > 10000 {
			return 0, ErrInvalid
		}
		height, e := strconv.Atoi(f.Get("height"))
		if e != nil || height <= 0 || height > 10000 {
			return 0, ErrInvalid
		}
		_, err = tx.Exec(ctx, `INSERT INTO organization_public_images(organization_id,placement,src,alt,width,height) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(organization_id,placement) DO UPDATE SET src=excluded.src,alt=excluded.alt,width=excluded.width,height=excluded.height,webp_srcset=''`, orgID, placement, f.Get("src"), f.Get("alt"), width, height)
		saved = imageID(placement)
	default:
		return 0, ErrInvalid
	}
	if err != nil {
		return 0, err
	}
	if err = q.CreateAdministrativeEvent(ctx, dbsqlc.CreateAdministrativeEventParams{ActorUserID: actor, Action: "club_configuration_saved", ResourceType: section.Resource, ResourceID: saved}); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return saved, nil
}
func (s *Service) Record(ctx context.Context, key string, id int32) (Section, Record, error) {
	snap, e := s.Snapshot(ctx)
	if e != nil {
		return Section{}, Record{}, e
	}
	for _, sec := range snap.Sections {
		if sec.Key == key {
			if id == 0 {
				defaults := map[string]string{}
				switch key {
				case "groupes":
					defaults["show_name_publicly"] = "yes"
				case "tarifs":
					defaults["currency"] = "EUR"
				case "liens":
					defaults["position"] = "0"
				}
				return sec, Record{Values: defaults}, nil
			}
			for _, r := range sec.Records {
				if r.ID == id {
					for i := range sec.Fields {
						field := &sec.Fields[i]
						lookup := map[string]string{"activity_id": "activites", "group_id": "groupes", "season_id": "saisons", "location_id": "lieux"}[field.Name]
						selected := r.Values[field.Name]
						if lookup == "" || selected == "" {
							continue
						}
						found := false
						for _, opt := range field.Options {
							if opt.Value == selected {
								found = true
								break
							}
						}
						if found {
							continue
						}
						for _, source := range snap.Sections {
							if source.Key != lookup {
								continue
							}
							for _, linked := range source.Records {
								if strconv.Itoa(int(linked.ID)) == selected {
									field.Options = append(field.Options, Option{selected, linked.Name + " (inactif)"})
								}
							}
						}
					}
					return sec, r, nil
				}
			}
			return sec, Record{}, fmt.Errorf("%w: record", pgx.ErrNoRows)
		}
	}
	return Section{}, Record{}, pgx.ErrNoRows
}
