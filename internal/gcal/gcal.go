package gcal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Summary  string
	Start    time.Time
	End      time.Time
	Location string // raw location from GCal
	Space    string // normalized: "sanctuary", "lower", "loft", "all", or ""
}

type Service struct {
	apiKey     string
	calendarID string
	cache      map[string]*cacheEntry
	mu         sync.RWMutex
	cacheTTL   time.Duration
}

type cacheEntry struct {
	events    []Event
	fetchedAt time.Time
}

func NewService(apiKey, calendarID string) *Service {
	return &Service{
		apiKey:     apiKey,
		calendarID: calendarID,
		cache:      make(map[string]*cacheEntry),
		cacheTTL:   5 * time.Minute,
	}
}

// EventsForMonth returns events for the given year/month.
// Results are cached in-memory for 5 minutes per month.
func (s *Service) EventsForMonth(ctx context.Context, year int, month time.Month) ([]Event, error) {
	key := fmt.Sprintf("%d-%02d", year, month)

	// Check cache
	s.mu.RLock()
	if entry, ok := s.cache[key]; ok && time.Since(entry.fetchedAt) < s.cacheTTL {
		events := entry.events
		s.mu.RUnlock()
		return events, nil
	}
	s.mu.RUnlock()

	// Fetch from GCal API
	events, err := s.fetchEvents(ctx, year, month)
	if err != nil {
		return nil, err
	}

	// Store in cache
	s.mu.Lock()
	s.cache[key] = &cacheEntry{events: events, fetchedAt: time.Now()}
	s.mu.Unlock()

	return events, nil
}

func (s *Service) fetchEvents(ctx context.Context, year int, month time.Month) ([]Event, error) {
	loc := time.Now().Location()
	timeMin := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	timeMax := timeMin.AddDate(0, 1, 0)

	u := fmt.Sprintf(
		"https://www.googleapis.com/calendar/v3/calendars/%s/events?%s",
		url.PathEscape(s.calendarID),
		url.Values{
			"key":          {s.apiKey},
			"timeMin":      {timeMin.Format(time.RFC3339)},
			"timeMax":      {timeMax.Format(time.RFC3339)},
			"singleEvents": {"true"},
			"orderBy":      {"startTime"},
			"maxResults":   {"250"},
		}.Encode(),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("gcal: create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gcal: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gcal: API returned %d", resp.StatusCode)
	}

	var result gcalResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("gcal: decode: %w", err)
	}

	events := make([]Event, 0, len(result.Items))
	for _, item := range result.Items {
		start, end := parseEventTimes(item)
		if start.IsZero() {
			continue
		}
		events = append(events, Event{
			Summary:  item.Summary,
			Start:    start,
			End:      end,
			Location: item.Location,
			Space:    normalizeSpace(item.Location),
		})
	}

	return events, nil
}

// normalizeSpace maps a GCal location string to a space identifier.
func normalizeSpace(location string) string {
	loc := strings.ToLower(strings.TrimSpace(location))
	switch {
	case strings.Contains(loc, "sanctuary"):
		return "sanctuary"
	case strings.Contains(loc, "lower"):
		return "lower"
	case strings.Contains(loc, "loft"):
		return "loft"
	case strings.Contains(loc, "entire") || strings.Contains(loc, "all") || strings.Contains(loc, "whole"):
		return "all"
	default:
		return ""
	}
}

// GCal API response types (minimal)

type gcalResponse struct {
	Items []gcalItem `json:"items"`
}

type gcalItem struct {
	Summary  string        `json:"summary"`
	Location string        `json:"location"`
	Start    gcalDateTime  `json:"start"`
	End      gcalDateTime  `json:"end"`
}

type gcalDateTime struct {
	DateTime string `json:"dateTime"` // RFC3339
	Date     string `json:"date"`     // YYYY-MM-DD (all-day events)
}

func parseEventTimes(item gcalItem) (time.Time, time.Time) {
	start := parseGCalDateTime(item.Start)
	end := parseGCalDateTime(item.End)
	return start, end
}

func parseGCalDateTime(dt gcalDateTime) time.Time {
	if dt.DateTime != "" {
		t, _ := time.Parse(time.RFC3339, dt.DateTime)
		return t
	}
	if dt.Date != "" {
		t, _ := time.Parse("2006-01-02", dt.Date)
		return t
	}
	return time.Time{}
}
