package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"

	"github.com/dukerupert/south-hills-coc/internal/config"
	"github.com/dukerupert/south-hills-coc/internal/handlers"
	"github.com/dukerupert/south-hills-coc/internal/mailer"
	"github.com/dukerupert/south-hills-coc/internal/newsletter"
)

func main() {
	// Load .env file if present (does not override existing env vars)
	_ = godotenv.Load()

	cfg := config.Load()
	h := handlers.New(cfg)

	// Newsletter subscriber list. The database lives on a mounted volume; the
	// directory may not exist on a fresh deployment.
	if err := os.MkdirAll(filepath.Dir(cfg.NewsletterDBPath), 0o755); err != nil {
		log.Fatalf("failed to create newsletter data directory: %v", err)
	}
	store, err := newsletter.Open(cfg.NewsletterDBPath)
	if err != nil {
		log.Fatalf("failed to open newsletter database: %v", err)
	}
	defer store.Close()

	var m mailer.Mailer
	if cfg.PostmarkServerToken != "" && cfg.NewsletterFromAddress != "" {
		m = mailer.NewPostmark(cfg.PostmarkServerToken, cfg.NewsletterFromName, cfg.NewsletterFromAddress)
	} else {
		// No credentials: log the message (including the confirmation link)
		// instead of sending it. This is the development path.
		log.Printf("newsletter: Postmark not configured; confirmation emails will be logged, not sent")
		m = mailer.Logger{}
	}
	h.SetNewsletter(store, m)

	mux := http.NewServeMux()

	// Static files. Serves both the fingerprinted name a template emits and the
	// plain name, with Cache-Control set to match; see internal/handlers/assets.go.
	mux.Handle("GET /static/", h.StaticHandler())

	// Favicon and root-level static files. These are requested by their fixed
	// well-known paths, so they cannot be fingerprinted.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(cfg.StaticDir, "favicon.ico"))
	})
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(cfg.StaticDir, "robots.txt"))
	})
	mux.HandleFunc("GET /sitemap.xml", h.Sitemap)

	// Pages
	page(mux, "/", h.Home)
	page(mux, "/visit/", h.Visit)
	page(mux, "/about/", h.About)
	page(mux, "/about/leadership/", h.Leadership)
	page(mux, "/about/doctrine/", h.Doctrine)
	page(mux, "/ministries/", h.Ministries)
	page(mux, "/calendar/", h.Calendar)
	page(mux, "/reserve/", h.Reserve)
	page(mux, "/events/", h.Events)
	page(mux, "/contact/", h.Contact)

	// htmx endpoints
	mux.HandleFunc("GET /calendar/events", h.CalendarEvents)
	mux.HandleFunc("GET /calendar/feed", h.CalendarFeed)
	mux.HandleFunc("POST /reserve", h.HandleReserve)

	// Newsletter. The GET routes render a page and do not mutate; the POST
	// routes act. Mail-provider link scanners prefetch URLs found in email
	// bodies, so a GET that unsubscribed or confirmed would fire without a
	// human ever clicking.
	page(mux, "/newsletter/", h.Newsletter)
	mux.HandleFunc("POST /newsletter/subscribe", h.HandleNewsletterSubscribe)
	mux.HandleFunc("GET /newsletter/confirm", h.NewsletterConfirm)
	mux.HandleFunc("POST /newsletter/confirm", h.HandleNewsletterConfirm)
	mux.HandleFunc("GET /newsletter/unsubscribe", h.NewsletterUnsubscribe)
	mux.HandleFunc("POST /newsletter/unsubscribe", h.HandleNewsletterUnsubscribe)
	// RFC 8058. POSTed by the recipient's mail provider, never by a browser
	// with a session — it cannot carry a CSRF token.
	mux.HandleFunc("POST /newsletter/unsubscribe/one-click", h.HandleNewsletterOneClick)

	// API
	mux.HandleFunc("/api/contact", h.HandleContact)
	mux.HandleFunc("GET /api/health", h.HandleHealth)

	log.Printf("Server starting on :%s (env=%s)", cfg.Port, cfg.AppEnv)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatal(err)
	}
}

// page registers a GET handler for a path. For paths with trailing slashes,
// it also redirects the non-trailing-slash version.
func page(mux *http.ServeMux, path string, handler http.HandlerFunc) {
	if path == "/" {
		mux.HandleFunc("GET /{$}", handler)
		return
	}
	mux.HandleFunc("GET "+path, handler)
	if strings.HasSuffix(path, "/") {
		trimmed := strings.TrimSuffix(path, "/")
		mux.HandleFunc("GET "+trimmed, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, path, http.StatusMovedPermanently)
		})
	}
}
