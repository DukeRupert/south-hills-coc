package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/joho/godotenv"

	"github.com/dukerupert/south-hills-coc/internal/config"
	"github.com/dukerupert/south-hills-coc/internal/handlers"
)

func main() {
	// Load .env file if present (does not override existing env vars)
	_ = godotenv.Load()

	cfg := config.Load()
	h := handlers.New(cfg)

	mux := http.NewServeMux()

	// Static files
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Favicon and root-level static files
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/favicon.ico")
	})
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/robots.txt")
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
