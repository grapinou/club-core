# P2.2 — Administration d’une association vierge

## État initial

Après P2.1.2, `/setup` créait le premier administrateur et l’interface gérait les rôles, personnes, essais et adhésions. Les tables `organizations`, `organization_public_images`, `organization_links`, `locations`, `activities`, `groups`, `group_slots`, `seasons` et `membership_types` existaient. Le site public lisait déjà l’identité, les lieux, activités, horaires et types d’adhésion depuis PostgreSQL. Leur création ou modification restait essentiellement tributaire du seed Budokan, de SQL ou de commandes techniques. Les montants n’étaient pas modélisés et `/tarifs` affichait une limitation statique. Le texte de `/rules` venait de `config/config.json`. Le CLI `describe-club` lisait ces références, sans constituer une interface d’édition.

## Vision retenue

**Une association → une base dédiée → un Club Core métier.** P2.2 n’ajoute aucun `organization_id` aux domaines historiques et ne crée pas de tenant partagé. Une éventuelle Club Core Platform Hosted sera un provisionneur d’instances, hors de ce chantier.

## Architecture réutilisée

Le service `internal/clubconfig` écrit dans les tables métier existantes. Les lectures publiques continuent de passer par `internal/organization`, les requêtes sqlc existantes et les services d’essai. Les routes sont assemblées dans `internal/application`, protégées par le middleware d’accès et le CSRF commun. Les changements sont écrits dans `administrative_events`. Les anciennes commandes CLI et le seed Budokan sont préservés.

## Parcours de configuration

Après `/setup` et la connexion, le premier administrateur ouvre **Configurer le club** depuis le menu administratif ou **Mon espace**. La page `/admin/config` propose, dans l’ordre : association, saison, lieux, activités, groupes, horaires, types d’adhésion et tarifs, contenu public, liens et images. Chaque rubrique mène aux mêmes formulaires qui servent ensuite aux modifications courantes. Le site public est accessible directement depuis cette page. Aucun SQL ni seed métier n’est utilisé par le responsable.

## Progression / onboarding

L’état est calculé à partir des données actives : association, saison, lieu, activité, groupe et horaire sont les étapes structurantes. Une étape affiche **À faire**, **Renseigné** ou **Facultatif**. Les dépendances sont annoncées, par exemple une activité avant un groupe et une saison, un lieu et un groupe avant un horaire. Cette progression ne bloque pas le reste de l’administration et n’ajoute ni booléen global ni moteur de workflow.

## Organisation

Le nom, nom court, présentation, email public, téléphone, libellé du téléphone, adresse de correspondance et site externe sont éditables. La base conserve une seule organisation active. La présentation et les coordonnées alimentent l’accueil et le contact. Les champs facultatifs vides ne créent pas de contenu public artificiel.

## Lieux

Création, modification et désactivation avec nom et adresse. Les anciens lieux restent en base lorsqu’ils sont référencés par des horaires ; un lieu inactif cesse d’être publié. Aucune suppression destructive n’est proposée.

## Activités

Création, modification et désactivation. Les noms restent libres : sport, culture ou autres activités associatives. Une activité inactive ne figure plus dans l’offre publique.

## Groupes

Un groupe appartient à une activité et dure au-delà d’une saison. Son nom, sa description, son état et la visibilité publique de son nom sont éditables. Les horaires sont une rubrique distincte ; plusieurs horaires peuvent référencer le même groupe. Le service refuse de changer l’activité d’un groupe déjà utilisé par un horaire, un essai ou une affectation d’adhésion afin de conserver le sens de l’historique.

## Saisons

Création, modification et désactivation du nom et des dates. Les dates doivent être ordonnées. Le service sérialise les écritures de configuration par verrouillage de la ligne de l’organisation et refuse le chevauchement de deux saisons actives ; cela évite l’ambiguïté dans le planning public. Il refuse également de raccourcir une saison au-delà des horaires qu’elle contient. Les saisons anciennes restent disponibles pour l’historique.

## Créneaux

Le formulaire demande le groupe, la saison, un jour nommé en français, les heures, le lieu, la période de validité et un éventuel nom public de séance. Les heures et dates sont validées dans le service, ainsi que l’activité du groupe, la saison et le lieu actifs. La période doit rester dans la saison. Une séance déjà référencée par un essai peut être désactivée ; ses caractéristiques historiques ne peuvent plus être réécrites dans ce formulaire. Un nouvel horaire peut alors être créé.

## Types d’adhésion / tarifs

La migration `0031_club_configuration.sql` ajoute à `membership_types` un montant facultatif en centimes, une devise ISO à trois lettres (EUR par défaut) et une note publique. L’interface permet création, modification et désactivation. La conversion des euros saisis en centimes se fait sans arrondi flottant. `/tarifs` affiche le montant et la note lorsqu’ils existent, ou **Tarif sur demande**. Le prix V1 est attaché au type, sans saison, réduction ni paiement.

## Contenu public

La première séance, les éléments à apporter, le prêt de matériel et sa consigne sont éditables. La migration `0031` ajoute aussi un texte de présentation du règlement intérieur à l’organisation ; il alimente `/rules` lorsqu’il est renseigné. Le texte générique de configuration reste un repli pour les bases existantes. Les liens sociaux ou autres liens publics peuvent être créés, modifiés et désactivés. Aucune publication ou révision CMS n’a été ajoutée.

## Images

Les cinq emplacements existants (`hero`, `activity`, `community`, `schedule`, `trial`) sont configurables : chemin sous `/static/images/`, texte alternatif et dimensions. Une référence peut être remplacée ou retirée. Le chemin est limité à un sous-chemin statique canonique ; l’application ne téléverse pas de fichier et ne vérifie pas encore l’existence du fichier référencé. L’installation technique doit placer l’image dans `static/images` avant de la choisir. Une association sans image reste présentable.

## Permissions

La permission unique `club.configure` protège ces écrans et toutes les mutations. Elle est attribuée initialement au rôle `president`, en cohérence avec sa gestion des accès et le premier compte créé par `/setup`. `secretary` garde les droits existants de gestion des dossiers, sans pouvoir changer la structure du club ; `treasurer` et `coach` n’obtiennent pas ce droit. Les handlers vérifient l’accès et le service revérifie le compte actif, activé et la permission issue de la politique des rôles, sans tester le nom du rôle. Le CSRF commun protège les POST. Les champs sont bornés et validés avant écriture.

## Audit

Chaque enregistrement et retrait de référence d’image est transactionnel avec une entrée `administrative_events` : acteur, action `club_configuration_saved`, type de ressource, identifiant et date. La migration étend les contraintes de ce journal. Les anciens événements restent inchangés. Le journal ne stocke pas encore les anciennes et nouvelles valeurs ; pour une image, l’identifiant stable 1 à 5 correspond à son emplacement.

## Généricité

Aucune règle d’autorisation, validation, formulaire ou requête de configuration ne dépend du Budokan, du JJB ou d’une discipline sportive. Les jours sont nommés, les groupes sont indépendants des horaires et le site public emploie des termes utilisables pour une activité culturelle. Le seed Budokan reste une démonstration séparée.

## Association fictive non sportive

Sur la base isolée `clubcore_p22_final`, créée vide puis migrée, Chromium a suivi `/setup`, créé le premier administrateur et configuré **Club d’échecs de Senlis** : saison 2026/2027, Salle municipale, activité Échecs, groupe Jeunes, séance le mercredi de 14 h à 15 h 30, adhésion Jeunes à 85,50 EUR, contenu de première visite, présentation du règlement et lien public. Aucune donnée métier n’a été injectée par SQL ou seed pendant ce parcours navigateur. La génération du code et la migration étaient des opérations locales d’installation. Les bases de test isolées ont été supprimées après vérification ; `clubcore_demo` a été préservée.

## Site public obtenu

Chromium a vérifié `/`, `/horaires`, `/tarifs`, `/contact`, `/essai`, `/essai?activity=1` et `/rules`. Le planning affiche le créneau du mercredi et son lieu, les jours sans séance ne créent plus de cartes vides. Le type d’adhésion affiche son montant. Le formulaire d’essai propose l’activité et le groupe. Le contact et le règlement affichent les informations saisies. Un contrôle séparé a confirmé que `/setup` renvoie vers `/login` après initialisation et que le secret n’est plus présent en base.

## Desktop / mobile

Le parcours a été réellement exécuté dans Chromium à 1280 px et les captures des écrans principaux ont été examinées à 390 px : `/setup`, progression, association, activité, groupe, horaire, horaires publics et essai public. Aucun débordement horizontal n’a été mesuré. Les formulaires restent utilisables sur téléphone ; la navigation se répartit sur plusieurs lignes.

## Tests automatisés

Le test PostgreSQL/Testcontainers `TestBlankAssociationConfiguredInBrowser` couvre `/setup` puis l’administration HTTP d’une association non sportive : refus anonyme et sans rôle, CSRF, association, saison, lieu, activité, groupe, horaire, tarif, contenu, liens, résultat public, audit, validation d’horaire invalide, conservation d’un horaire après désactivation de son lieu et refus du déplacement d’un groupe historiquement utilisé. Il vérifie aussi la permission au niveau du service. Les tests publics existants ont été adaptés pour distinguer prix publié et tarif sur demande. `TestPublicChildValidation` a été rendu stable autour de minuit : son cas « adulte » utilise 19 ans révolus, puisque le formulaire était construit dans le fuseau local et le service de la fixture valide en UTC. Les suites P1/P2.1/P2.1.1/P2.1.2 restent exécutées dans la vérification globale.

## Vérifications

- `go test ./...` : réussi, y compris les suites PostgreSQL/Testcontainers (le package `internal/application` a terminé en 227,463 s).
- `go vet ./...` : réussi.
- `git diff --check` : réussi.
- Aucun script shell n’a été modifié.

## Limites restantes

- Pas d’envoi d’image depuis le navigateur ni de vérification de l’existence du fichier référencé.
- Prix simple global au type d’adhésion, sans historisation par saison, remise, encaissement ni facture.
- Le texte du règlement est une présentation, pas un éditeur de document ou de versions.
- Pas de récupération autonome du mot de passe du dernier administrateur.
- L’installation technique et la remise confidentielle du code `/setup` restent à la charge de l’opérateur.
- Les données métier peuvent encore être modifiées directement par un opérateur disposant de l’accès PostgreSQL ; les validations du service concernent le parcours web normal.

## Compatibilité future Hosted

Une future plateforme devra créer une base dédiée, appliquer les migrations, préparer le secret de setup ou fournir un premier compte via une primitive sûre, puis associer un slug et une URL à l’instance. Elle ne doit pas centraliser les groupes, adhésions ou permissions des clubs. Aucun provisionnement Hosted n’a été implémenté.

## Suite recommandée

Après l’installation technique et la remise du code initial, un responsable peut désormais prendre possession de son instance et bâtir l’association depuis le navigateur. La priorité recommandée est **P3 — cycle complet de l’adhésion**, car la configuration structurelle et le site public sont maintenant utilisables. Un prototype de surcouche Club Core Hosted pourra ensuite réutiliser ce moteur et son modèle à base dédiée. Aucun de ces deux chantiers n’a commencé ici.
