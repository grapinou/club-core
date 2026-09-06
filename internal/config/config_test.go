package config

import "testing"

func TestLoad(t *testing.T) {

	cfg, err := Load("testdata/config.json")
	if err != nil {
		t.Fatalf(
			"le chargement de la configuration a échoué : %v",
			err,
		)
	}

	expected := Config{

		SiteName: "TCR Club Manager",

		Home: PageConfig{
			Title:       "Accueil test",
			Heading:     "Bienvenue !",
			Description: "La Team Cat Ride (TCR) vous accueille sur son site.",
		},

		Club: PageConfig{
			Title:       "Le Club test",
			Heading:     "Team Cat Ride",
			Description: "TCR est là pour t'accompagner et te faire progresser en vélo.",
		},

		Contact: ContactConfig{
			Title:        "Contact test",
			Heading:      "Contacter TCR",
			Description:  "Il est possible de nous contacter par email ou par téléphone.",
			EmailAddress: "teamcatride@miaoumail.com",
			PhoneNumber:  "00-01-02-03-04",
		},

		Rules: PageConfig{
			Title:       "Règlement test",
			Heading:     "Règlement intérieur",
			Description: "Chaque cat rider doit être poli et à l'heure.",
		},

		Where: PageConfig{
			Title:       "Où test",
			Heading:     "Où nous trouver ?",
			Description: "Nous sommes au 9 rue Chat Botté, à Catville",
		},

		When: PageConfig{
			Title:       "Quand test",
			Heading:     "Quand nous trouver ?",
			Description: "Nous sommes disponibles tous les soirs de 20h à 22h. Pour davantage d'informations, nous contacter.",
		}}

	if cfg != expected {

		t.Errorf(
			"configuration obtenue : %#v\nconfiguration attendue : %#v",
			cfg,
			expected,
		)

	}

}

func TestLoadReturnsErrorWhenFileDoesNotExist(t *testing.T) {
	_, err := Load("testdata/unknown.json")

	if err == nil {
		t.Fatal(
			"une erreur était attendue pour un fichier inexistant",
		)
	}
}
