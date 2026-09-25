# Site public et réservation d’un essai — 25 septembre 2026

## État initial

Le site public lisait déjà l’identité, les activités, les lieux et le planning depuis PostgreSQL. La page `/essai` conseillait de contacter le club, sans créer d’essai. L’administration possédait déjà les listes, détails, statuts et outils de reprogrammation des `trial_registrations`. Le service `trials.Service` validait les liens activité/groupe/créneau et les dates ; le modèle `persons` et `person_guardians` permettait déjà de représenter les mineurs et responsables.

## Architecture réutilisée

Le parcours s’appuie sur l’Application et son routeur, le handler public, `html/template`, les protections CSRF et la limite de fréquence des demandes publiques. `trials.PublicService` présente les créneaux possibles et compose la création des personnes avec `trials.Service.ScheduleTx` dans une transaction PostgreSQL unique. Les tables existantes suffisent : aucune migration ni nouveau modèle. La requête de validation de `trials.Service` a été corrigée pour vérifier également `seasons.is_active` lors de la programmation.

Les offres publiques viennent des activités, groupes, créneaux, saisons et lieux actifs. Le groupe dont `show_name_publicly` est faux reste exclu de la réservation ; ce marqueur existant évite de proposer le groupe technique du lundi Budokan. La sélection présente les dates des 21 prochains jours compatibles avec le jour hebdomadaire, la saison et la période du créneau. L’écriture revérifie activité, groupe, créneau, date, saison et lieu en transaction ; la liste affichée ne fait pas autorité.

## Choix UX et contenu

Le tunnel tient sur une page : activité, groupe/horaire, date, puis coordonnées du pratiquant. Deux petits formulaires GET successifs servent à affiner la liste sans JavaScript ni calendrier complexe. Le formulaire POST montre les champs adulte ou responsable selon le choix « mineur ». La confirmation HTML rappelle activité, groupe, pratique éventuelle, date, heures et lieu, ainsi qu’un conseil d’arrivée. Les erreurs de sélection et les champs manquants sont affichés près du champ ; les erreurs de validation générale sont affichées en haut du formulaire. Le parcours reste utilisable sur mobile avec la grille responsive existante.

L’accueil comporte maintenant une explication en trois points de la première venue. La description Budokan est stockée dans son seed `organizations.description` plutôt que dans un template. Les illustrations déjà présentes restent réutilisées sur l’accueil, les horaires et l’essai. Les âges des groupes sont présentés comme repères pédagogiques, sans règle d’éligibilité inventée.

## Parcours adulte et mineur

Pour un adulte, prénom, nom, date de naissance, email et téléphone sont demandés. La date de naissance sert à distinguer un majeur d’un mineur sans compte. Une `person` prospect et un `trial_registration` au statut `registered` sont créés.

Pour un mineur, prénom, nom et date de naissance du pratiquant, puis nom, prénom, email, téléphone et lien du responsable sont demandés. Deux `persons` distinctes et une relation `person_guardians` de contact principal sont créées, suivies de l’essai. Aucune donnée de compte, adresse postale, paiement, consentement d’adhésion ou document n’est demandée. Sans preuve d’identité, le parcours crée un nouveau prospect à chaque réservation ; il ne fusionne pas automatiquement avec une personne existante.

## Routes, administration et sécurité

`GET /essai` affiche le parcours ; `POST /essai` enregistre et rend une confirmation. Les routes d’administration existantes `/trials`, `/trials/{id}` et `/persons/{id}` retrouvent le nouvel essai et les coordonnées, y compris le responsable via la relation familiale. Le POST public passe par le CSRF existant et un limiteur de fréquence partagé avec les autres demandes publiques. Aucune authentification n’est nécessaire.

## Tests et vérifications

`public_trial_integration_test.go` utilise le helper PostgreSQL/Testcontainers existant. Il couvre une réservation adulte, une réservation mineur et son responsable, les valeurs stockées, la projection administrative, les pages du bureau, le formulaire HTTP avec CSRF et la confirmation. Les cas refusés couvrent activité/groupe/créneau invalides, date sur le mauvais jour, créneau, groupe, activité ou saison inactifs, date hors période, email adulte et email du responsable invalides. Après chaque refus, les nombres de personnes, relations et essais sont identiques à l’état initial.

Vérifications finales réussies : `go test ./...` (package application : 209,902 s ; package database : résultat en cache après un passage complet réussi à 33,409 s), `go vet ./...` et `git diff --check`. `go test ./internal/application -run TestPublicTrial -count=1` réussit aussi sur le parcours ciblé.

## Limites connues

Les dates possibles sont calculées sur trois semaines de séances hebdomadaires. Toutes les activités visibles sur l’accueil ne possèdent pas nécessairement un créneau d’essai réservable : dans le seed Budokan, la préparation physique est aussi une activité de référence, mais aucun groupe/créneau propre n’y est rattaché. Le groupe technique du lundi est volontairement masqué de la réservation. Le modèle ne contient pas encore d’exceptions pour vacances, annulations ponctuelles ou jauges ; le formulaire ne prétend pas gérer ces cas. La confirmation est rendue après le POST ; une actualisation du navigateur peut reproposer l’envoi. Il n’y a pas d’email de confirmation ni de rapprochement automatique avec une personne préexistante, car aucun mécanisme de preuve d’identité n’est intégré à ce parcours.

## Prochain test manuel

Vérifier sur mobile et ordinateur : compréhension de l’activité et du public, choix du bon groupe, lisibilité des dates/heures/lieux, bascule adulte/mineur, erreurs de saisie, confirmation, puis recherche du prospect et de l’essai par un membre du bureau. Vérifier aussi le vocabulaire et la tenue à prévoir avec le club.

## Pistes futures, hors chantier

Ajouter un calendrier d’exceptions aux créneaux si le club en a besoin ; envoyer une confirmation après vérification de l’email ; envisager un rapprochement de prospect avec identité vérifiée ; préciser les contenus pratiques et la tarification dans des champs éditoriaux adaptés. Ces pistes ne sont pas implémentées ici.
