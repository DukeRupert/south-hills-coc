package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/dukerupert/south-hills-coc/internal/gcal"
)

type ReserveRequest struct {
	Name    string
	Email   string
	Phone   string
	Space   string
	Date    string // YYYY-MM-DD
	Start   string // HH:MM (24h)
	End     string // HH:MM (24h)
	Purpose string
}

var validSpaces = map[string]string{
	"sanctuary": "Sanctuary",
	"lower":     "Lower Hall",
	"loft":      "Loft",
	"all":       "Entire Building",
}

// Reserve serves the reservation page.
func (h *Handler) Reserve(w http.ResponseWriter, r *http.Request) {
	h.render(w, "reserve", PageData{
		Title:       "Reserve the Building",
		Description: "Request to use the South Hills Church of Christ building for your event.",
		CurrentPath: "/reserve/",
	})
}

// HandleReserve processes the reservation form via htmx.
func (h *Handler) HandleReserve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderReserveResult(w, "error", "Invalid form submission.", nil)
		return
	}

	req := ReserveRequest{
		Name:    strings.TrimSpace(r.FormValue("name")),
		Email:   strings.TrimSpace(r.FormValue("email")),
		Phone:   strings.TrimSpace(r.FormValue("phone")),
		Space:   strings.TrimSpace(r.FormValue("space")),
		Date:    strings.TrimSpace(r.FormValue("date")),
		Start:   strings.TrimSpace(r.FormValue("start")),
		End:     strings.TrimSpace(r.FormValue("end")),
		Purpose: strings.TrimSpace(r.FormValue("purpose")),
	}

	// Validate required fields
	if req.Name == "" || req.Email == "" || req.Space == "" ||
		req.Date == "" || req.Start == "" || req.End == "" || req.Purpose == "" {
		h.renderReserveResult(w, "error", "All required fields must be filled out.", nil)
		return
	}

	// Validate email
	if _, err := mail.ParseAddress(req.Email); err != nil {
		h.renderReserveResult(w, "error", "Please enter a valid email address.", nil)
		return
	}

	// Validate space
	spaceLabel, ok := validSpaces[req.Space]
	if !ok {
		h.renderReserveResult(w, "error", "Please select a valid space.", nil)
		return
	}

	// Parse date
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		h.renderReserveResult(w, "error", "Please enter a valid date.", nil)
		return
	}

	// Date must be in the future
	today := time.Now().Truncate(24 * time.Hour)
	if date.Before(today) {
		h.renderReserveResult(w, "error", "The date must be in the future.", nil)
		return
	}

	// Parse times
	startTime, err := time.Parse("15:04", req.Start)
	if err != nil {
		h.renderReserveResult(w, "error", "Please enter a valid start time.", nil)
		return
	}
	endTime, err := time.Parse("15:04", req.End)
	if err != nil {
		h.renderReserveResult(w, "error", "Please enter a valid end time.", nil)
		return
	}

	// End must be after start
	if !endTime.After(startTime) {
		h.renderReserveResult(w, "error", "End time must be after start time.", nil)
		return
	}

	// Build full start/end datetimes for conflict checking
	loc := time.Now().Location()
	reqStart := time.Date(date.Year(), date.Month(), date.Day(),
		startTime.Hour(), startTime.Minute(), 0, 0, loc)
	reqEnd := time.Date(date.Year(), date.Month(), date.Day(),
		endTime.Hour(), endTime.Minute(), 0, 0, loc)

	// Check for conflicts if GCal is configured
	if h.gcalService != nil {
		events, err := h.gcalService.EventsForMonth(r.Context(), date.Year(), date.Month())
		if err != nil {
			log.Printf("gcal error during reservation: %v", err)
			// Continue without conflict check rather than blocking the reservation
		} else {
			if conflict, evt := hasConflict(events, req.Space, date, reqStart, reqEnd); conflict {
				h.renderReserveResult(w, "conflict", "", evt)
				return
			}
		}
	}

	// Format for emails
	dateFormatted := date.Format("Monday, January 2, 2006")
	timeRange := fmt.Sprintf("%s – %s", reqStart.Format("3:04 PM"), reqEnd.Format("3:04 PM"))

	// Send admin notification email
	if err := h.sendReservationAdminEmail(req, spaceLabel, dateFormatted, timeRange, date); err != nil {
		log.Printf("failed to send admin reservation email: %v", err)
		// Don't block the user — send confirmation anyway
	}

	// Send requester confirmation email
	if err := h.sendReservationConfirmEmail(req, spaceLabel, dateFormatted, timeRange); err != nil {
		log.Printf("failed to send reservation confirmation email: %v", err)
	}

	h.renderReserveResult(w, "success", "", nil)
}

func hasConflict(events []gcal.Event, space string, date time.Time, start, end time.Time) (bool, *gcal.Event) {
	for _, e := range events {
		if !sameDay(e.Start, date) {
			continue
		}
		if !spacesOverlap(e.Space, space) {
			continue
		}
		if !timesOverlap(e.Start, e.End, start, end) {
			continue
		}
		return true, &e
	}
	return false, nil
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func spacesOverlap(existingSpace, requestedSpace string) bool {
	existing := strings.ToLower(existingSpace)
	requested := strings.ToLower(requestedSpace)

	// "all" conflicts with everything
	if requested == "all" || existing == "all" {
		return true
	}
	// Same space conflicts
	return existing == requested
}

func timesOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	// All-day events overlap with everything
	if aStart.Hour() == 0 && aStart.Minute() == 0 && aEnd.Hour() == 0 && aEnd.Minute() == 0 {
		return true
	}
	return aStart.Before(bEnd) && aEnd.After(bStart)
}

type reserveResultData struct {
	Status   string      // "success", "error", "conflict"
	Message  string      // for error messages
	Conflict *gcal.Event // for conflict details
}

func (h *Handler) renderReserveResult(w http.ResponseWriter, status, message string, conflict *gcal.Event) {
	tmpl := h.getReserveResultTemplate()
	if tmpl == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := reserveResultData{
		Status:   status,
		Message:  message,
		Conflict: conflict,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "reserve-result", data); err != nil {
		log.Printf("reserve-result template error: %v", err)
	}
}

func (h *Handler) sendReservationAdminEmail(req ReserveRequest, spaceLabel, dateFormatted, timeRange string, date time.Time) error {
	if h.Config.PostmarkToken == "" || h.Config.FromEmail == "" || h.Config.AdminEmail == "" {
		return fmt.Errorf("email configuration not set")
	}

	calLink := fmt.Sprintf("https://calendar.google.com/calendar/r/day/%d/%d/%d",
		date.Year(), date.Month(), date.Day())

	body := fmt.Sprintf(`New building reservation request

Name:    %s
Email:   %s
Phone:   %s
Space:   %s
Date:    %s
Time:    %s
Purpose: %s

---
To approve: add this event to Google Calendar.
%s

To decline: reply directly to %s.
`, req.Name, req.Email, req.Phone, spaceLabel, dateFormatted, timeRange, req.Purpose, calLink, req.Email)

	return h.sendPostmarkEmail(
		h.Config.AdminEmail,
		fmt.Sprintf("Building Request: %s — %s", spaceLabel, dateFormatted),
		body,
	)
}

func (h *Handler) sendReservationConfirmEmail(req ReserveRequest, spaceLabel, dateFormatted, timeRange string) error {
	if h.Config.PostmarkToken == "" || h.Config.FromEmail == "" {
		return fmt.Errorf("email configuration not set")
	}

	firstName := strings.SplitN(req.Name, " ", 2)[0]

	body := fmt.Sprintf(`Hi %s,

We received your request to use the %s on %s from %s.

Chanté will follow up with you to confirm availability.

South Hills Church of Christ
`, firstName, spaceLabel, dateFormatted, timeRange)

	return h.sendPostmarkEmail(
		req.Email,
		"Your building request has been received",
		body,
	)
}

func (h *Handler) sendPostmarkEmail(to, subject, textBody string) error {
	email := PostmarkEmail{
		From:     h.Config.FromEmail,
		To:       to,
		Subject:  subject,
		TextBody: textBody,
	}

	body, err := json.Marshal(email)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", "https://api.postmarkapp.com/email", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Postmark-Server-Token", h.Config.PostmarkToken)

	resp, err := (&http.Client{}).Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("postmark error: %s", string(respBody))
	}
	return nil
}
