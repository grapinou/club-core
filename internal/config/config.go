package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config contient les paramètres généraux
// chargés au démarrage de l'application.
type Config struct {
	SiteName string        `json:"site_name"`
	Home     PageConfig    `json:"home"`
	Club     PageConfig    `json:"club"`
	Contact  ContactConfig `json:"contact"`
	Rules    PageConfig    `json:"rules"`
	Where    PageConfig    `json:"where"`
	When     PageConfig    `json:"when"`
}

type PageConfig struct {
	Title       string `json:"title"`
	Heading     string `json:"heading"`
	Description string `json:"description"`
	Image       string `json:"image"`
	ImageAlt    string `json:"image_alt"`
}

type ContactConfig struct {
	Title        string `json:"title"`
	Heading      string `json:"heading"`
	Description  string `json:"description"`
	EmailAddress string `json:"email_address"`
	PhoneNumber  string `json:"phone_number"`
}

// Load ouvre le fichier situé au chemin reçu,
// décode son contenu JSON et retourne la configuration.
func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf(
			"ouvrir le fichier de configuration : %w",
			err,
		)
	}
	defer file.Close()

	var cfg Config

	err = json.NewDecoder(file).Decode(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf(
			"décoder le fichier de configuration : %w",
			err,
		)
	}

	return cfg, nil

}
