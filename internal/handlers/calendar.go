package handlers

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dukerupert/south-hills-coc/internal/ical"
)

// CalendarData is passed to the calendar page template.
type CalendarData struct {
	MonthLabel string // "March 2026"
	PrevMonth  string // "2026-02"
	NextMonth  string // "2026-04"
	Year       int
	Month      int
}

// GridData is passed to the calendar-grid partial (htmx fragment).
type GridData struct {
	MonthLabel   string
	PrevMonth    string
	NextMonth    string
	CurrentMonth string // "YYYY-MM" for toggle buttons
	ShowToggle   bool   // true when rendered from /events/ page
	DayHeaders   []string
	Days         []DayCell
}

// DayCell represents one cell in the calendar grid.
type DayCell struct {
	Day     int
	InMonth bool
	IsToday bool
	Events  []EventView
}

// EventView is a template-friendly event.
type EventView struct {
	Summary     string
	TimeRange   string // "2:00 PM – 5:00 PM"
	DateLabel   string // "Tuesday, March 17"
	Space       string // "sanctuary", "lower", "loft", "all", ""
	SpaceLabel  string // "Sanctuary", "Lower Hall", etc.
	Location    string
	Description string
	HasDetail   bool // true if any of SpaceLabel, Location, or Description is set
}

// FeedData is passed to the events-feed partial (htmx fragment).
type FeedData struct {
	MonthLabel   string
	PrevMonth    string
	NextMonth    string
	CurrentMonth string // "YYYY-MM" for toggle buttons
	Days         []FeedDay
	Empty        bool
}

// FeedDay groups events under a single date heading.
type FeedDay struct {
	DateLabel string // "Tuesday, March 17"
	IsToday   bool
	Events    []EventView
}

// spaceLabels maps space IDs to display names.
var spaceLabels = map[string]string{
	"sanctuary": "Sanctuary",
	"lower":     "Lower Hall",
	"loft":      "Loft",
	"all":       "Entire Building",
}

var dayHeaders = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// Calendar serves the calendar page shell.
func (h *Handler) Calendar(w http.ResponseWriter, r *http.Request) {
	year, month := currentMonth()
	if m := r.URL.Query().Get("month"); m != "" {
		if y, mo, ok := parseMonth(m); ok {
			year, month = y, mo
		}
	}
	prev, next := adjacentMonths(year, month)
	label := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Format("January 2006")

	h.render(w, "calendar", PageData{
		Title:       "Calendar",
		Description: "Building use calendar for South Hills Church of Christ.",
		CurrentPath: "/calendar/",
		Calendar: &CalendarData{
			MonthLabel: label,
			PrevMonth:  prev,
			NextMonth:  next,
			Year:       year,
			Month:      month,
		},
	})
}

// CalendarEvents serves the htmx calendar grid fragment.
func (h *Handler) CalendarEvents(w http.ResponseWriter, r *http.Request) {
	year, month := currentMonth()
	if m := r.URL.Query().Get("month"); m != "" {
		if y, mo, ok := parseMonth(m); ok {
			year, month = y, mo
		}
	}

	events := h.fetchEvents(r, year, month)
	grid := buildGrid(year, month, events)
	prev, next := adjacentMonths(year, month)
	label := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Format("January 2006")

	// Show toggle when request comes from the events page
	showToggle := r.URL.Query().Get("toggle") == "1"

	data := GridData{
		MonthLabel:   label,
		PrevMonth:    prev,
		NextMonth:    next,
		CurrentMonth: fmt.Sprintf("%d-%02d", year, month),
		ShowToggle:   showToggle,
		DayHeaders:   dayHeaders,
		Days:         grid,
	}

	tmpl := h.getGridTemplate()
	if tmpl == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "calendar-grid", data); err != nil {
		log.Printf("calendar grid template error: %v", err)
	}
}

// CalendarFeed serves the htmx events feed fragment.
func (h *Handler) CalendarFeed(w http.ResponseWriter, r *http.Request) {
	year, month := currentMonth()
	if m := r.URL.Query().Get("month"); m != "" {
		if y, mo, ok := parseMonth(m); ok {
			year, month = y, mo
		}
	}

	events := h.fetchEvents(r, year, month)
	prev, next := adjacentMonths(year, month)
	label := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Format("January 2006")

	data := buildFeed(year, month, events)
	data.MonthLabel = label
	data.PrevMonth = prev
	data.NextMonth = next
	data.CurrentMonth = fmt.Sprintf("%d-%02d", year, month)

	tmpl := h.getFeedTemplate()
	if tmpl == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "events-feed", data); err != nil {
		log.Printf("events feed template error: %v", err)
	}
}

// fetchEvents retrieves iCal events for a given month, logging errors gracefully.
func (h *Handler) fetchEvents(r *http.Request, year, month int) []ical.Event {
	if h.icalService == nil {
		return nil
	}
	events, err := h.icalService.EventsForMonth(r.Context(), year, time.Month(month))
	if err != nil {
		log.Printf("ical error: %v", err)
		return nil
	}
	return events
}

func toEventView(e ical.Event) EventView {
	sl := spaceLabels[e.Space]
	return EventView{
		Summary:     e.Summary,
		TimeRange:   formatTimeRange(e.Start, e.End),
		DateLabel:   e.Start.Format("Monday, January 2"),
		Space:       e.Space,
		SpaceLabel:  sl,
		Location:    e.Location,
		Description: e.Description,
		HasDetail:   sl != "" || e.Location != "" || e.Description != "",
	}
}

func buildGrid(year, month int, events []ical.Event) []DayCell {
	firstOfMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Now().Location())
	lastOfMonth := firstOfMonth.AddDate(0, 1, -1)
	today := time.Now()

	// Start grid on the Sunday before (or on) the 1st
	startDay := firstOfMonth
	for startDay.Weekday() != time.Sunday {
		startDay = startDay.AddDate(0, 0, -1)
	}

	// End grid on the Saturday after (or on) the last day
	endDay := lastOfMonth
	for endDay.Weekday() != time.Saturday {
		endDay = endDay.AddDate(0, 0, 1)
	}

	// Build event lookup by day
	eventsByDay := make(map[int][]EventView)
	for _, e := range events {
		day := e.Start.Day()
		eventsByDay[day] = append(eventsByDay[day], toEventView(e))
	}

	var cells []DayCell
	for d := startDay; !d.After(endDay); d = d.AddDate(0, 0, 1) {
		inMonth := d.Month() == time.Month(month)
		isToday := d.Year() == today.Year() && d.Month() == today.Month() && d.Day() == today.Day()

		var dayEvents []EventView
		if inMonth {
			dayEvents = eventsByDay[d.Day()]
		}

		cells = append(cells, DayCell{
			Day:     d.Day(),
			InMonth: inMonth,
			IsToday: isToday,
			Events:  dayEvents,
		})
	}

	return cells
}

func buildFeed(year, month int, events []ical.Event) FeedData {
	if len(events) == 0 {
		return FeedData{Empty: true}
	}

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Group events by day, filtering out past days
	dayMap := make(map[int][]EventView)
	for _, e := range events {
		if e.Start.Before(todayStart) {
			continue
		}
		day := e.Start.Day()
		dayMap[day] = append(dayMap[day], toEventView(e))
	}

	if len(dayMap) == 0 {
		return FeedData{Empty: true}
	}

	// Sort days
	dayNums := make([]int, 0, len(dayMap))
	for d := range dayMap {
		dayNums = append(dayNums, d)
	}
	sort.Ints(dayNums)

	loc := now.Location()
	var days []FeedDay
	for _, d := range dayNums {
		date := time.Date(year, time.Month(month), d, 0, 0, 0, 0, loc)
		isToday := date.Year() == now.Year() && date.Month() == now.Month() && date.Day() == now.Day()
		days = append(days, FeedDay{
			DateLabel: date.Format("Monday, January 2"),
			IsToday:   isToday,
			Events:    dayMap[d],
		})
	}

	return FeedData{Days: days}
}

func formatTimeRange(start, end time.Time) string {
	// All-day events have zero hour
	if start.Hour() == 0 && start.Minute() == 0 && end.Hour() == 0 && end.Minute() == 0 {
		return "All day"
	}
	return fmt.Sprintf("%s – %s", start.Format("3:04 PM"), end.Format("3:04 PM"))
}

func currentMonth() (int, int) {
	now := time.Now()
	return now.Year(), int(now.Month())
}

func adjacentMonths(year, month int) (string, string) {
	t := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	prev := t.AddDate(0, -1, 0)
	next := t.AddDate(0, 1, 0)
	return fmt.Sprintf("%d-%02d", prev.Year(), prev.Month()),
		fmt.Sprintf("%d-%02d", next.Year(), next.Month())
}

func parseMonth(s string) (int, int, bool) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil || year < 2000 || year > 2100 {
		return 0, 0, false
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 1 || month > 12 {
		return 0, 0, false
	}
	return year, month, true
}
