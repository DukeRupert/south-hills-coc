package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dukerupert/south-hills-coc/internal/gcal"
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
	MonthLabel string
	PrevMonth  string
	NextMonth  string
	DayHeaders []string
	Days       []DayCell
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
	Summary   string
	TimeRange string // "2:00 PM – 5:00 PM"
	Space     string // "sanctuary", "lower", "loft", "all", ""
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

	var events []gcal.Event
	if h.gcalService != nil {
		var err error
		events, err = h.gcalService.EventsForMonth(r.Context(), year, time.Month(month))
		if err != nil {
			log.Printf("gcal error: %v", err)
			// Continue with empty events rather than failing
		}
	}

	grid := buildGrid(year, month, events)
	prev, next := adjacentMonths(year, month)
	label := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Format("January 2006")

	data := GridData{
		MonthLabel: label,
		PrevMonth:  prev,
		NextMonth:  next,
		DayHeaders: dayHeaders,
		Days:       grid,
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

func buildGrid(year, month int, events []gcal.Event) []DayCell {
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
		eventsByDay[day] = append(eventsByDay[day], EventView{
			Summary:   e.Summary,
			TimeRange: formatTimeRange(e.Start, e.End),
			Space:     e.Space,
		})
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
