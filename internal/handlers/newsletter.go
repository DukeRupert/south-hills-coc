package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dukerupert/south-hills-coc/internal/newsletter"
)

// Form-token bounds. A submission faster than minFormAge or older than
// maxFormAge did not come from a person who just loaded the page.
const (
	minFormAge = 3 * time.Second
	maxFormAge = 2 * time.Hour
)

// NewsletterData carries per-request state to the newsletter templates.
type NewsletterData struct {
	// FormTS is the signed timestamp embedded in the signup form.
	FormTS string
	// Source is the ?src= attribution value, echoed into the form.
	Source string
	// Token is the confirm or unsubscribe token, echoed into the POST form.
	Token string
	// Notice is a user-actionable message shown above the form.
	Notice string
	// State is "", "expired", or "invalid" on the confirm page.
	State string
}

// Newsletter serves the signup form. This is the QR code target, so the URL
// stays short and typeable.
func (h *Handler) Newsletter(w http.ResponseWriter, r *http.Request) {
	h.renderNewsletterForm(w, r, "")
}

func (h *Handler) renderNewsletterForm(w http.ResponseWriter, r *http.Request, notice string) {
	src := r.URL.Query().Get("src")
	if src == "" {
		src = r.FormValue("src")
	}
	h.render(w, "newsletter", PageData{
		Title:       "Newsletter",
		Description: "Get the South Hills Church of Christ weekly newsletter by email.",
		CurrentPath: "/newsletter/",
		Newsletter: &NewsletterData{
			FormTS: h.signFormTimestamp(time.Now()),
			Source: sanitizeSource(src),
			Notice: notice,
		},
	})
}

// HandleNewsletterSubscribe applies the bot mitigations and creates a pending
// row. It renders the same "check your email" page for a new address, an
// already-subscribed address, an unsubscribed address, and a honeypot-tripped
// bot — any variation would turn the form into an address-enumeration oracle.
func (h *Handler) HandleNewsletterSubscribe(w http.ResponseWriter, r *http.Request) {
	if h.newsletterStore == nil {
		http.Error(w, "Newsletter is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderNewsletterForm(w, r, "That submission could not be read. Please try again.")
		return
	}

	// 2. Honeypot. Return the standard success page and discard; never signal
	// rejection, or the bot learns to leave the field alone.
	if strings.TrimSpace(r.PostFormValue("phone2")) != "" {
		h.renderNewsletterSent(w)
		return
	}

	// 3. Signed timestamp.
	switch age, err := h.verifyFormTimestamp(r.PostFormValue("ts"), time.Now()); {
	case err != nil:
		// Forged or missing signature. This is not a person; look like success.
		h.renderNewsletterSent(w)
		return
	case age < minFormAge:
		// Plausibly an autofilling browser rather than a bot, so give a path
		// forward instead of a silent drop.
		h.renderNewsletterForm(w, r, "That was quick — please submit once more to confirm.")
		return
	case age > maxFormAge:
		h.renderNewsletterForm(w, r, "This page had been open a while. Please submit again.")
		return
	}

	// 4. Rate limits. A tripped limit looks like success for the same reason
	// the honeypot does.
	ip := clientIP(r, h.Config.TrustedProxyCount)
	if !h.newsletterIPLimit.Allow(ip) || !h.newsletterGlobalLimit.Allow("global") {
		log.Printf("newsletter: rate limit tripped for %s", ip)
		h.renderNewsletterSent(w)
		return
	}

	email := newsletter.NormalizeEmail(r.PostFormValue("email"))
	if err := newsletter.ValidateEmail(email); err != nil {
		// A syntax error is the user's own typo, not a fact about the list.
		h.renderNewsletterForm(w, r, "That doesn't look like a valid email address. Please check it and try again.")
		return
	}

	// 5. Per-address throttling happens inside the store.
	res, err := h.newsletterStore.StartSignup(r.Context(), newsletter.SignupInput{
		Email:     email,
		Source:    sanitizeSource(r.PostFormValue("src")),
		IP:        ip,
		UserAgent: truncate(r.UserAgent(), 500),
	})
	if err != nil {
		if errors.Is(err, newsletter.ErrInvalidEmail) {
			h.renderNewsletterForm(w, r, "That doesn't look like a valid email address. Please check it and try again.")
			return
		}
		log.Printf("newsletter: StartSignup(%s): %v", email, err)
		h.renderNewsletterForm(w, r, "Something went wrong on our end. Please try again in a moment.")
		return
	}

	if res.SendConfirmation {
		// Send failures are logged, not surfaced: the response must not vary,
		// and the address can retry after the resend interval.
		if err := h.sendConfirmationEmail(r.Context(), res.Subscriber.Email, res.ConfirmToken); err != nil {
			log.Printf("newsletter: confirmation email to %s: %v", res.Subscriber.Email, err)
		}
	}

	h.renderNewsletterSent(w)
}

func (h *Handler) renderNewsletterSent(w http.ResponseWriter) {
	h.render(w, "newsletter-sent", PageData{
		Title:       "Check Your Email",
		Description: "Confirm your newsletter subscription.",
		CurrentPath: "/newsletter/",
	})
}

// NewsletterConfirm renders the confirmation page. It deliberately does not
// mutate: Outlook and Gmail link scanners prefetch URLs found in email bodies,
// and a prefetched GET confirm would defeat double opt-in entirely by
// confirming an address without a human ever acting.
func (h *Handler) NewsletterConfirm(w http.ResponseWriter, r *http.Request) {
	h.render(w, "newsletter-confirm", PageData{
		Title:       "Confirm Your Subscription",
		Description: "Confirm your newsletter subscription.",
		CurrentPath: "/newsletter/confirm",
		Newsletter:  &NewsletterData{Token: r.URL.Query().Get("token")},
	})
}

// HandleNewsletterConfirm performs the confirmation.
func (h *Handler) HandleNewsletterConfirm(w http.ResponseWriter, r *http.Request) {
	if h.newsletterStore == nil {
		http.Error(w, "Newsletter is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderConfirmState(w, "invalid", "")
		return
	}

	token := r.PostFormValue("token")
	unsubToken, err := newsletter.NewToken()
	if err != nil {
		log.Printf("newsletter: mint unsubscribe token: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	sub, err := h.newsletterStore.Confirm(r.Context(), newsletter.HashToken(token), unsubToken)
	switch {
	case errors.Is(err, newsletter.ErrExpiredToken):
		h.renderConfirmState(w, "expired", token)
		return
	case errors.Is(err, newsletter.ErrInvalidToken):
		h.renderConfirmState(w, "invalid", token)
		return
	case err != nil:
		log.Printf("newsletter: Confirm: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := h.sendWelcomeEmail(r.Context(), sub); err != nil {
		log.Printf("newsletter: welcome email to %s: %v", sub.Email, err)
	}

	h.render(w, "newsletter-confirmed", PageData{
		Title:       "You're Subscribed",
		Description: "You're on the South Hills Church of Christ newsletter list.",
		CurrentPath: "/newsletter/confirm",
	})
}

func (h *Handler) renderConfirmState(w http.ResponseWriter, state, token string) {
	h.render(w, "newsletter-confirm", PageData{
		Title:       "Confirm Your Subscription",
		Description: "Confirm your newsletter subscription.",
		CurrentPath: "/newsletter/confirm",
		Newsletter:  &NewsletterData{Token: token, State: state},
	})
}

// NewsletterUnsubscribe renders the unsubscribe page. Like the confirm page it
// does not mutate — a scanner prefetching this link would otherwise silently
// unsubscribe people who never clicked.
func (h *Handler) NewsletterUnsubscribe(w http.ResponseWriter, r *http.Request) {
	h.render(w, "newsletter-unsubscribe", PageData{
		Title:       "Unsubscribe",
		Description: "Unsubscribe from the South Hills Church of Christ newsletter.",
		CurrentPath: "/newsletter/unsubscribe",
		Newsletter:  &NewsletterData{Token: r.URL.Query().Get("token")},
	})
}

// HandleNewsletterUnsubscribe performs the unsubscribe. An unknown token
// renders the same confirmation page as a successful one, so the endpoint
// cannot be used to test whether an address is on the list.
func (h *Handler) HandleNewsletterUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if h.newsletterStore == nil {
		http.Error(w, "Newsletter is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err == nil {
		h.unsubscribe(r.Context(), r.PostFormValue("token"))
	}
	h.render(w, "newsletter-unsubscribed", PageData{
		Title:       "Unsubscribed",
		Description: "You have been removed from the newsletter list.",
		CurrentPath: "/newsletter/unsubscribe",
	})
}

// HandleNewsletterOneClick implements RFC 8058. It is POSTed by the
// recipient's mail provider, not by a browser with a session, so it carries no
// CSRF token and must never be gated on one. It always returns 200.
func (h *Handler) HandleNewsletterOneClick(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		_ = r.ParseForm()
		token = r.PostFormValue("token")
	}
	h.unsubscribe(r.Context(), token)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Unsubscribed")
}

func (h *Handler) unsubscribe(ctx context.Context, token string) {
	if h.newsletterStore == nil || token == "" {
		return
	}
	if err := h.newsletterStore.UnsubscribeByToken(ctx, token); err != nil &&
		!errors.Is(err, newsletter.ErrInvalidToken) {
		log.Printf("newsletter: UnsubscribeByToken: %v", err)
	}
}

// --- signed form timestamp ---

// signFormTimestamp returns "<unix>.<hmac>", embedded as a hidden field. It
// kills scripted posts and replayed scraped forms.
func (h *Handler) signFormTimestamp(t time.Time) string {
	unix := strconv.FormatInt(t.Unix(), 10)
	return unix + "." + hex.EncodeToString(h.formMAC(unix))
}

// verifyFormTimestamp returns the age of a valid token, or an error if the
// signature does not check out.
func (h *Handler) verifyFormTimestamp(v string, now time.Time) (time.Duration, error) {
	unix, sig, ok := strings.Cut(v, ".")
	if !ok {
		return 0, errors.New("malformed form timestamp")
	}
	want, err := hex.DecodeString(sig)
	if err != nil {
		return 0, errors.New("malformed form signature")
	}
	if subtle.ConstantTimeCompare(want, h.formMAC(unix)) != 1 {
		return 0, errors.New("bad form signature")
	}
	secs, err := strconv.ParseInt(unix, 10, 64)
	if err != nil {
		return 0, errors.New("malformed form timestamp")
	}
	return now.Sub(time.Unix(secs, 0)), nil
}

func (h *Handler) formMAC(msg string) []byte {
	mac := hmac.New(sha256.New, h.formHMACSecret)
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}

// --- helpers ---

// sanitizeSource keeps attribution values to a short, printable slug so a
// crafted ?src= cannot inject anything into the consent record.
func sanitizeSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if len(b.String()) >= 32 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "web"
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
