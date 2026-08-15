package handlers

import (
	"crypto/rand"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dukerupert/south-hills-coc/internal/config"
	"github.com/dukerupert/south-hills-coc/internal/data"
	"github.com/dukerupert/south-hills-coc/internal/ical"
	"github.com/dukerupert/south-hills-coc/internal/mailer"
	"github.com/dukerupert/south-hills-coc/internal/newsletter"
)

type Handler struct {
	Config                *config.Config
	leadershipData        *data.LeadershipData
	ministriesData        *data.MinistriesData
	icalService           *ical.Service
	templates             map[string]*template.Template
	gridTemplate          *template.Template
	feedTemplate          *template.Template
	reserveResultTemplate *template.Template
	templateDir           string
	staticDir             string
	assets                *assetIndex
	isDev                 bool

	newsletterStore       newsletter.Store
	newsletterMailer      mailer.Mailer
	newsletterIPLimit     *tokenBucket
	newsletterGlobalLimit *tokenBucket
	formHMACSecret        []byte
}

type PageData struct {
	Config      *config.Config
	Title       string
	Description string
	IsHome      bool
	CurrentPath string
	Year        int
	Content     template.HTML
	Leadership  *data.LeadershipData
	Ministries  *data.MinistriesData
	Calendar    *CalendarData
	Newsletter  *NewsletterData
	MenuItems   []MenuItem
}

type MenuItem struct {
	Name string
	URL  string
}

var mainMenu = []MenuItem{
	{Name: "Visit", URL: "/visit/"},
	{Name: "About", URL: "/about/"},
	{Name: "Ministries", URL: "/ministries/"},
	{Name: "Events", URL: "/events/"},
	{Name: "Contact", URL: "/contact/"},
}

func New(cfg *config.Config) *Handler {
	h := &Handler{
		Config:         cfg,
		leadershipData: data.LoadLeadership(),
		ministriesData: data.LoadMinistries(),
		templateDir:    templateDir(cfg),
		staticDir:      staticDir(cfg),
		isDev:          cfg.IsDev(),
	}

	// Fingerprint the static assets once. Development skips the index: the
	// files change under the running process and the URLs stay plain anyway.
	if !h.isDev {
		h.assets = newAssetIndex(h.staticDir)
	}

	// Initialize iCal service if configured
	if cfg.ICalFeedURL != "" {
		h.icalService = ical.NewService(cfg.ICalFeedURL)
	}

	// Per-IP: 5 submissions/hour. Global: 20/hour. In-memory is sufficient at
	// this scale; a restart resetting the buckets is acceptable.
	h.newsletterIPLimit = newTokenBucket(5, time.Hour)
	h.newsletterGlobalLimit = newTokenBucket(20, time.Hour)
	h.formHMACSecret = formHMACSecret(cfg)

	h.templates = h.parseAllTemplates()
	h.gridTemplate = h.parseGridTemplate()
	h.feedTemplate = h.parseFeedTemplate()
	h.reserveResultTemplate = h.parseReserveResultTemplate()
	return h
}

func templateDir(cfg *config.Config) string {
	if cfg.TemplateDir != "" {
		return cfg.TemplateDir
	}
	return "templates"
}

func staticDir(cfg *config.Config) string {
	if cfg.StaticDir != "" {
		return cfg.StaticDir
	}
	return "static"
}

// SetNewsletter wires the subscriber store and mailer. Until it is called the
// newsletter routes report themselves unavailable rather than half-working.
func (h *Handler) SetNewsletter(store newsletter.Store, m mailer.Mailer) {
	h.newsletterStore = store
	h.newsletterMailer = m
}

// formHMACSecret returns the configured signing key. If none is set the form
// tokens still work — they are just invalidated on restart — but an
// unconfigured production deployment is worth shouting about.
func formHMACSecret(cfg *config.Config) []byte {
	if cfg.FormHMACSecret != "" {
		return []byte(cfg.FormHMACSecret)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("failed to generate form HMAC secret: %v", err)
	}
	if !cfg.IsDev() {
		log.Printf("WARNING: FORM_HMAC_SECRET is not set; using an ephemeral key. " +
			"Open signup forms will stop validating after a restart.")
	}
	return b
}

// funcMap is shared by every template set. "asset" is a method value rather
// than a package function so it can see the fingerprint index built for this
// Handler.
func (h *Handler) funcMap() template.FuncMap {
	return template.FuncMap{
		"safeHTML":    func(s string) template.HTML { return template.HTML(s) },
		"currentYear": func() int { return time.Now().Year() },
		"asset":       h.AssetURL,
		"formatTime":  func(t time.Time) string { return t.Format("3:04 PM") },
	}
}

func (h *Handler) parseAllTemplates() map[string]*template.Template {
	funcMap := h.funcMap()

	pages := []string{
		"home", "visit", "contact",
		"about", "about-leadership", "about-doctrine",
		"ministries", "events", "calendar", "reserve",
		"newsletter", "newsletter-sent", "newsletter-confirm",
		"newsletter-confirmed", "newsletter-unsubscribe", "newsletter-unsubscribed",
	}

	sharedFiles := []string{
		filepath.Join(h.templateDir, "base.html"),
		filepath.Join(h.templateDir, "partials", "header.html"),
		filepath.Join(h.templateDir, "partials", "footer.html"),
		filepath.Join(h.templateDir, "partials", "schema.html"),
	}

	templates := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		files := make([]string, len(sharedFiles))
		copy(files, sharedFiles)
		files = append(files, filepath.Join(h.templateDir, "pages", page+".html"))

		tmpl, err := template.New("").Funcs(funcMap).ParseFiles(files...)
		if err != nil {
			log.Fatalf("failed to parse template %s: %v", page, err)
		}
		templates[page] = tmpl
	}
	return templates
}

func (h *Handler) parseGridTemplate() *template.Template {
	funcMap := h.funcMap()
	file := filepath.Join(h.templateDir, "partials", "calendar-grid.html")
	tmpl, err := template.New("").Funcs(funcMap).ParseFiles(file)
	if err != nil {
		log.Fatalf("failed to parse calendar-grid template: %v", err)
	}
	return tmpl
}

func (h *Handler) getGridTemplate() *template.Template {
	if h.isDev {
		return h.parseGridTemplate()
	}
	return h.gridTemplate
}

func (h *Handler) parseFeedTemplate() *template.Template {
	funcMap := h.funcMap()
	file := filepath.Join(h.templateDir, "partials", "events-feed.html")
	tmpl, err := template.New("").Funcs(funcMap).ParseFiles(file)
	if err != nil {
		log.Fatalf("failed to parse events-feed template: %v", err)
	}
	return tmpl
}

func (h *Handler) getFeedTemplate() *template.Template {
	if h.isDev {
		return h.parseFeedTemplate()
	}
	return h.feedTemplate
}

func (h *Handler) parseReserveResultTemplate() *template.Template {
	funcMap := h.funcMap()
	file := filepath.Join(h.templateDir, "partials", "reserve-result.html")
	tmpl, err := template.New("").Funcs(funcMap).ParseFiles(file)
	if err != nil {
		log.Fatalf("failed to parse reserve-result template: %v", err)
	}
	return tmpl
}

func (h *Handler) getReserveResultTemplate() *template.Template {
	if h.isDev {
		return h.parseReserveResultTemplate()
	}
	return h.reserveResultTemplate
}

func (h *Handler) render(w http.ResponseWriter, page string, pd PageData) {
	pd.Config = h.Config
	pd.Year = time.Now().Year()
	pd.MenuItems = mainMenu

	var tmpl *template.Template
	if h.isDev {
		// Re-parse templates on every request in development
		templates := h.parseAllTemplates()
		tmpl = templates[page]
	} else {
		tmpl = h.templates[page]
	}

	if tmpl == nil {
		log.Printf("template %q not found", page)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The HTML is what names the fingerprinted assets, so it has to stay fresh.
	// A cached page would keep pointing browsers at the previous build's URLs.
	// Revalidation is cheap; the page itself is not what we are caching.
	w.Header().Set("Cache-Control", "no-cache")
	if err := tmpl.ExecuteTemplate(w, "base", pd); err != nil {
		log.Printf("template execution error for %s: %v", page, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
