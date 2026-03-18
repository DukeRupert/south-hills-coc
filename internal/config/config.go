package config

import "os"

type Config struct {
	Port             string
	AppEnv           string
	BaseURL          string
	Title            string
	Description      string
	Tagline          string
	Phone            string
	Email            string
	OfficeEmail      string
	Address          string
	Latitude         string
	Longitude        string
	TurnstileSiteKey string
	YouTubeChannel   string

	Street     string
	City       string
	Region     string
	PostalCode string
	Country    string

	BibleClass string
	Worship    string

	YouTubeURL string
	GivingURL  string

	PostmarkToken   string
	FromEmail       string
	ToEmail         string
	AdminEmail      string
	AllowedOrigin   string
	TurnstileSecret string

	GCalAPIKey     string
	GCalCalendarID string
}

func Load() *Config {
	return &Config{
		Port:             envOr("PORT", "8080"),
		AppEnv:           envOr("APP_ENV", "production"),
		BaseURL:          "https://www.southhillscoc.org/",
		Title:            "South Hills Church of Christ",
		Description:      "A welcoming Christian community in Helena, Montana. Revealing God, Renewing Lives, and Rejoicing Together.",
		Tagline:          "Revealing God, Renewing Lives, Rejoicing Together",
		Phone:            "(406) 442-8950",
		Email:            "southhillschurchofchrist@gmail.com",
		OfficeEmail:      "office@southhillscoc.org",
		Address:          "2294 Deerfield Ln, Helena, MT 59601",
		Latitude:         "46.570233",
		Longitude:        "-111.9714227",
		TurnstileSiteKey: envOr("TURNSTILE_SITE_KEY", "0x4AAAAAACHhuM2lhHmWLq04"),
		YouTubeChannel:   "@southhillscoc",

		Street:     "2294 Deerfield Ln",
		City:       "Helena",
		Region:     "MT",
		PostalCode: "59601",
		Country:    "US",

		BibleClass: "9:30 AM",
		Worship:    "10:30 AM",

		YouTubeURL: "https://www.youtube.com/@southhillscoc",
		GivingURL:  "https://give.tithe.ly/?formId=cd988f16-9c1f-4906-a76e-76e42be71c80",

		PostmarkToken:   os.Getenv("POSTMARK_TOKEN"),
		FromEmail:       os.Getenv("FROM_EMAIL"),
		ToEmail:         os.Getenv("TO_EMAIL"),
		AdminEmail:      envOr("ADMIN_EMAIL", os.Getenv("TO_EMAIL")),
		AllowedOrigin:   envOr("ALLOWED_ORIGIN", "http://localhost:8080"),
		TurnstileSecret: os.Getenv("TURNSTILE_SECRET"),

		GCalAPIKey:     os.Getenv("GCAL_API_KEY"),
		GCalCalendarID: os.Getenv("GCAL_CALENDAR_ID"),
	}
}

func (c *Config) IsDev() bool {
	return c.AppEnv == "development"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
