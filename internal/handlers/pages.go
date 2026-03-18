package handlers

import (
	"encoding/xml"
	"net/http"
	"time"
)

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	h.render(w, "home", PageData{
		Title:       "South Hills Church of Christ | Helena, MT | Sunday Worship 10:30 AM",
		IsHome:      true,
		CurrentPath: "/",
	})
}

func (h *Handler) Visit(w http.ResponseWriter, r *http.Request) {
	h.render(w, "visit", PageData{
		Title:       "Plan Your Visit",
		Description: "Visit South Hills Church of Christ in Helena, MT. Sunday Bible Class at 9:30 AM, Worship at 10:30 AM. Located at 2294 Deerfield Ln. Families welcome!",
		CurrentPath: "/visit/",
	})
}

func (h *Handler) Contact(w http.ResponseWriter, r *http.Request) {
	h.render(w, "contact", PageData{
		Title:       "Contact Us",
		Description: "Contact South Hills Church of Christ in Helena, Montana. Call (406) 442-8950 or visit us at 2294 Deerfield Ln, Helena, MT 59601.",
		CurrentPath: "/contact/",
	})
}

func (h *Handler) About(w http.ResponseWriter, r *http.Request) {
	h.render(w, "about", PageData{
		Title:       "About Us",
		Description: "Learn about South Hills Church of Christ, a Bible-based congregation in Helena, Montana. Established in 2011, we're focused on revealing God, renewing lives, and rejoicing together.",
		CurrentPath: "/about/",
		Leadership:  h.leadershipData,
	})
}

func (h *Handler) Leadership(w http.ResponseWriter, r *http.Request) {
	h.render(w, "about-leadership", PageData{
		Title:       "Church Leadership",
		Description: "Meet the ministers, elders, and deacons serving South Hills Church of Christ in Helena, Montana. Our leadership team is committed to shepherding our congregation.",
		CurrentPath: "/about/leadership/",
		Leadership:  h.leadershipData,
	})
}

func (h *Handler) Doctrine(w http.ResponseWriter, r *http.Request) {
	h.render(w, "about-doctrine", PageData{
		Title:       "What We Believe",
		Description: "Discover the beliefs and doctrine of South Hills Church of Christ in Helena, MT. We are a non-denominational, Bible-based congregation following New Testament Christianity.",
		CurrentPath: "/about/doctrine/",
	})
}

func (h *Handler) Ministries(w http.ResponseWriter, r *http.Request) {
	h.render(w, "ministries", PageData{
		Title:       "Ministries",
		Description: "Explore ministries at South Hills Church of Christ in Helena, Montana. Programs for women, men, youth, children, and community outreach opportunities.",
		CurrentPath: "/ministries/",
		Ministries:  h.ministriesData,
	})
}

func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	year, month := currentMonth()
	if m := r.URL.Query().Get("month"); m != "" {
		if y, mo, ok := parseMonth(m); ok {
			year, month = y, mo
		}
	}
	prev, next := adjacentMonths(year, month)
	label := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Format("January 2006")

	h.render(w, "events", PageData{
		Title:       "Events & Building Use",
		Description: "View the building calendar and request to use the South Hills Church of Christ building for your event.",
		CurrentPath: "/events/",
		Calendar: &CalendarData{
			MonthLabel: label,
			PrevMonth:  prev,
			NextMonth:  next,
			Year:       year,
			Month:      month,
		},
	})
}

type sitemapURL struct {
	Loc        string `xml:"loc"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (h *Handler) Sitemap(w http.ResponseWriter, r *http.Request) {
	pages := []struct {
		path     string
		freq     string
		priority string
	}{
		{"/", "weekly", "1.0"},
		{"/visit/", "monthly", "0.9"},
		{"/about/", "monthly", "0.7"},
		{"/about/leadership/", "monthly", "0.6"},
		{"/about/doctrine/", "monthly", "0.5"},
		{"/ministries/", "monthly", "0.7"},
		{"/events/", "weekly", "0.8"},
		{"/contact/", "monthly", "0.7"},
	}

	var urls []sitemapURL
	for _, p := range pages {
		urls = append(urls, sitemapURL{
			Loc:        h.Config.BaseURL + p.path[1:],
			ChangeFreq: p.freq,
			Priority:   p.priority,
		})
	}

	sitemap := sitemapURLSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(sitemap)
}
