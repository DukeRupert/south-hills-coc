package handlers

import (
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dukerupert/south-hills-coc/internal/config"
	"github.com/dukerupert/south-hills-coc/internal/data"
	"github.com/dukerupert/south-hills-coc/internal/ical"
)

type Handler struct {
	Config         *config.Config
	leadershipData *data.LeadershipData
	ministriesData *data.MinistriesData
	icalService    *ical.Service
	templates             map[string]*template.Template
	gridTemplate          *template.Template
	feedTemplate          *template.Template
	reserveResultTemplate *template.Template
	templateDir           string
	isDev                 bool
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
		templateDir:    "templates",
		isDev:          cfg.IsDev(),
	}

	// Initialize iCal service if configured
	if cfg.ICalFeedURL != "" {
		h.icalService = ical.NewService(cfg.ICalFeedURL)
	}

	h.templates = h.parseAllTemplates()
	h.gridTemplate = h.parseGridTemplate()
	h.feedTemplate = h.parseFeedTemplate()
	h.reserveResultTemplate = h.parseReserveResultTemplate()
	return h
}

func (h *Handler) parseAllTemplates() map[string]*template.Template {
	funcMap := template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"currentYear": func() int { return time.Now().Year() },
	}

	pages := []string{
		"home", "visit", "contact",
		"about", "about-leadership", "about-doctrine",
		"ministries", "events", "calendar", "reserve",
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
	funcMap := template.FuncMap{
		"safeHTML":    func(s string) template.HTML { return template.HTML(s) },
		"currentYear": func() int { return time.Now().Year() },
	}
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
	funcMap := template.FuncMap{
		"safeHTML":    func(s string) template.HTML { return template.HTML(s) },
		"currentYear": func() int { return time.Now().Year() },
	}
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
	funcMap := template.FuncMap{
		"safeHTML":    func(s string) template.HTML { return template.HTML(s) },
		"currentYear": func() int { return time.Now().Year() },
		"formatTime": func(t time.Time) string { return t.Format("3:04 PM") },
	}
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
	if err := tmpl.ExecuteTemplate(w, "base", pd); err != nil {
		log.Printf("template execution error for %s: %v", page, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
