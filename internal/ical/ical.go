package ical

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apognu/gocal"
)

type Event struct {
	Summary     string
	Start       time.Time
	End         time.Time
	Location    string
	Description string
	Space       string // normalized: "sanctuary", "lower", "loft", "all", or ""
}

type Service struct {
	feedURL  string
	cache    map[string]*cacheEntry
	mu       sync.RWMutex
	cacheTTL time.Duration
}

type cacheEntry struct {
	events    []Event
	fetchedAt time.Time
}

func NewService(feedURL string) *Service {
	return &Service{
		feedURL:  feedURL,
		cache:    make(map[string]*cacheEntry),
		cacheTTL: 5 * time.Minute,
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

	// Fetch and parse iCal feed
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
	req, err := http.NewRequestWithContext(ctx, "GET", s.feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ical: create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ical: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ical: feed returned %d", resp.StatusCode)
	}

	loc := time.Now().Location()
	start := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)

	c := gocal.NewParser(resp.Body)
	c.Start, c.End = &start, &end
	if err := c.Parse(); err != nil {
		return nil, fmt.Errorf("ical: parse: %w", err)
	}

	events := make([]Event, 0, len(c.Events))
	for _, e := range c.Events {
		var eventStart, eventEnd time.Time
		if e.Start != nil {
			eventStart = *e.Start
		}
		if e.End != nil {
			eventEnd = *e.End
		}
		events = append(events, Event{
			Summary:     e.Summary,
			Start:       eventStart,
			End:         eventEnd,
			Location:    e.Location,
			Description: e.Description,
			Space:       normalizeSpace(e.Description, e.Location),
		})
	}

	return events, nil
}

// normalizeSpace extracts a space identifier from the description or location fields.
// Description is checked first; location is used as fallback.
func normalizeSpace(fields ...string) string {
	for _, field := range fields {
		s := strings.ToLower(strings.TrimSpace(field))
		switch {
		case strings.Contains(s, "sanctuary"):
			return "sanctuary"
		case strings.Contains(s, "lower"):
			return "lower"
		case strings.Contains(s, "loft"):
			return "loft"
		case strings.Contains(s, "entire") || strings.Contains(s, "all") || strings.Contains(s, "whole"):
			return "all"
		}
	}
	return ""
}
