package config

import (
	"os"
	"strconv"
	"strings"
)

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

	ICalFeedURL string

	// SiteBaseURL is absolute and has no trailing slash. Every link placed in
	// an email is built from it.
	SiteBaseURL string
	// TrustedProxyCount is how many reverse proxies sit in front of the app.
	// It selects which X-Forwarded-For entry is the real client; see
	// handlers.clientIP.
	//
	// Two, not one: Cloudflare proxies this domain and Caddy runs on the host,
	// so the chain is client -> Cloudflare -> Caddy -> app and X-Forwarded-For
	// arrives as "<visitor>, <cloudflare-edge>". Counting one proxy keys every
	// rate-limit bucket on a Cloudflare edge IP shared by all visitors, which
	// turns the per-IP limit into a second global one.
	TrustedProxyCount int

	// TemplateDir is where html/template files are read from. Overridable so
	// tests can run from a package directory.
	TemplateDir string

	// StaticDir is where static assets are read from, both for serving and for
	// building the fingerprint index. Overridable for the same reason.
	StaticDir string

	NewsletterDBPath      string
	NewsletterFromName    string
	NewsletterFromAddress string
	// PostmarkServerToken defaults to PostmarkToken so an existing deployment
	// keeps working without a .env rename.
	PostmarkServerToken string
	StreamTransactional string
	StreamBroadcast     string
	FormHMACSecret      string
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

		ICalFeedURL: os.Getenv("ICAL_FEED_URL"),

		SiteBaseURL:       strings.TrimRight(envOr("SITE_BASE_URL", "https://www.southhillscoc.org"), "/"),
		TrustedProxyCount: intEnvOr("TRUSTED_PROXY_COUNT", 2),

		TemplateDir:           envOr("TEMPLATE_DIR", "templates"),
		StaticDir:             envOr("STATIC_DIR", "static"),
		NewsletterDBPath:      envOr("NEWSLETTER_DB_PATH", "data/newsletter.db"),
		NewsletterFromName:    envOr("NEWSLETTER_FROM_NAME", "South Hills Church of Christ"),
		NewsletterFromAddress: envOr("NEWSLETTER_FROM_ADDRESS", os.Getenv("FROM_EMAIL")),
		PostmarkServerToken:   envOr("POSTMARK_SERVER_TOKEN", os.Getenv("POSTMARK_TOKEN")),
		StreamTransactional:   envOr("POSTMARK_STREAM_TRANSACTIONAL", "outbound"),
		StreamBroadcast:       envOr("POSTMARK_STREAM_BROADCAST", "broadcast"),
		FormHMACSecret:        os.Getenv("FORM_HMAC_SECRET"),
	}
}

func (c *Config) IsDev() bool {
	return c.AppEnv == "development"
}

func intEnvOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return fallback
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
