# Contacts d’urgence et consentements d’adhésion — 9 septembre 2026

## Contexte et diagnostic

Préparer les données du dossier d’adhésion après le parcours Personne → contact → essai → adhésion. Dépôt initial propre, HEAD `6148c4a`; `go test ./...` initial réussi (cache). Aucun commit ni push. Les migrations 0001 à 0014 sont inchangées.

## Migration 0015

- `person_emergency_contacts` relie deux Person, sans copie des coordonnées ni dépendance aux guardians. Relation libre nullable, priorité positive, dates de création/modification. CHECK anti-auto-relation, unicités du couple et de la priorité par personne, index inverse.
- `consent_definitions` porte code, version positive, titre, description, activation et date. Textes obligatoires non vides, unicité `(code, version)` et index unique partiel `consent_definitions_one_active_code` sur `code WHERE is_active` : une seule version active par code. Aucun contenu juridique seedé. Un trigger interdit de modifier le contenu/identité/date et de supprimer une définition ; seule l’activation peut changer. Une nouvelle rédaction exige une nouvelle ligne/version.
- `membership_consents` conserve chaque décision (`granted`, `refused`, `withdrawn`) avec adhésion, définition précise, auteur Person et date serveur. Un trigger interdit UPDATE/DELETE. Index pour les historiques et les références inverses.

Toutes les références utilisent des clés étrangères sans cascade. Le Down supprime décisions, définitions puis contacts, ainsi que les fonctions de protection. Un rollback explicite supprime les données de ces nouvelles tables ; il ne touche pas aux tables précédentes.

## Service métier

`internal/consents.Service` expose `RecordConsentDecision` et `WithdrawConsent`. Chaque écriture utilise une transaction READ COMMITTED et verrouille l’adhésion FOR UPDATE, sérialisant ses décisions. L’auteur doit être la Person de l’adhésion ou son guardian explicite ; la relation guardian est verrouillée FOR SHARE jusqu’au commit. La définition est verrouillée FOR SHARE pour empêcher une désactivation concurrente entre validation et insertion. Aucune déduction par âge.

Un retrait exige que la décision **courante** pour cette adhésion et cet identifiant de définition soit `granted`, selon `recorded_at DESC, id DESC`. Un retrait sans réponse, après un refus ou après un retrait est rejeté, même si un accord plus ancien existe. Refus puis accord, accord puis retrait puis nouvel accord restent possibles. Les erreurs n’ajoutent aucune décision. La requête sqlc d’insertion est une primitive interne : les appelants backend doivent passer par le service pour les règles transversales.

Les décisions sont append-only. L’état courant utilise `recorded_at DESC, id DESC` ; les historiques utilisent l’ordre inverse. `clock_timestamp()` date l’insertion après acquisition du verrou, plutôt que le début de transaction. L’identifiant départage les dates identiques.

## Requêtes sqlc et frontend

- Contacts : `CreatePersonEmergencyContact`, `ListPersonEmergencyContacts`, `UpdatePersonEmergencyContact`, `DeletePersonEmergencyContact`, `ListEmergencyContactForPersons`.
- Définitions : `CreateConsentDefinition`, `GetConsentDefinition`, `ListConsentDefinitions`, `ListActiveConsentDefinitions`, `DeactivateConsentDefinition`.
- Décisions : `CreateMembershipConsent`, `ListCurrentMembershipConsents`, `ListMembershipConsentHistory`, `ListMembershipConsentsHistory`.
- Validation transactionnelle : `LockConsentMembership`, `LockConsentGuardian`, `LockConsentDefinition`, `GetCurrentMembershipConsentDecision`.

Les contacts sont triés par priorité et exposent identité, téléphone/email nullable, libellé et identifiants. La lecture inverse expose les personnes bénéficiaires.

L’état courant expose l’adhésion, la définition complète/version/activation, la dernière décision/date et l’identité de son auteur. Il inclut les définitions actives sans réponse (champs de décision/auteur NULL) et les définitions inactives ayant un historique. Les deux historiques joignent définition et auteur. Une réponse à v1 ne couvre jamais v2.

## Tests et validation

Tests PostgreSQL 16/Testcontainers dans `internal/database/consents_integration_test.go` : création et lecture riche des contacts, multiplicité, partage, tri, contraintes, modification et suppression ; création/versionnement/activation des définitions et conservation des textes ; auteurs autorisés/interdits, décisions et transitions, retraits interdits, isolation par version et adhésion, historique ordonné, état courant et absence de réponse ; protections UPDATE/DELETE et absence de cascade ; statut pending préservé. Cas finaux explicites : unicité active à la création et à la réactivation, coexistence de versions inactives, refus des accords/refus sur définition inactive, retrait sur définition inactive, double retrait interdit, retrait après refus interdit (y compris avec accord plus ancien), nouvel accord après retrait et historique intégral conservé. Test Down/Up de 0015 avec conservation des tables antérieures.

Validation finale : `sqlc generate`, gofmt des fichiers concernés, `go test ./...` et `git diff --check` réussis. Le statut final contient uniquement les nouveaux fichiers de ce chantier et la mise à jour des modèles sqlc générés.

## Choix et hors périmètre

Une seule version par code peut être active. Plusieurs versions inactives restent autorisées et lisibles. Aucune désactivation automatique : activer une autre version alors qu’une version est active échoue explicitement ; la future administration pourra gérer cette transition dans une transaction. Le contenu juridique et la politique de réactivation restent à définir. Le service refuse `granted` et `refused` sur une définition inactive ; `withdrawn` y reste autorisé uniquement si la décision courante est `granted`. Les lectures historiques ne filtrent pas sur l’activation.

Coordonnées lues depuis Person (pas de copie historique). La relation guardian est vérifiée au moment de la décision, sans créer un historique des guardians. Aucune obligation de téléphone ni de consentement positif.

Aucune UI, administration, validation pending → active, création User, email/activation, espace membre, paiement/cotisation, licence, niveau, boutique, intégration WhatsApp, gestion de photos ou règle d’âge. Aucun refactor général et aucun docs/Jalon.
