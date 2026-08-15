package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dukerupert/south-hills-coc/internal/config"
	"github.com/dukerupert/south-hills-coc/internal/mailer"
	"github.com/dukerupert/south-hills-coc/internal/newsletter"
)

func newTestHandler(t *testing.T) (*Handler, *newsletter.SQLiteStore, *mailer.Fake) {
	t.Helper()

	cfg := &config.Config{
		AppEnv:              "test",
		TemplateDir:         filepath.Join("..", "..", "templates"),
		Title:               "South Hills Church of Christ",
		Address:             "2294 Deerfield Ln, Helena, MT 59601",
		OfficeEmail:         "office@example.test",
		Phone:               "(406) 442-8950",
		BibleClass:          "9:30 AM",
		Worship:             "10:30 AM",
		BaseURL:             "https://example.test/",
		SiteBaseURL:         "https://example.test",
		TrustedProxyCount:   1,
		StreamTransactional: "outbound",
		StreamBroadcast:     "broadcast",
		FormHMACSecret:      "test-secret",
	}

	store, err := newsletter.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	fake := &mailer.Fake{}
	h := New(cfg)
	h.SetNewsletter(store, fake)
	return h, store, fake
}

// subscribeForm posts a signup with a valid signed timestamp of the given age.
func (h *Handler) subscribeForm(t *testing.T, email string, age time.Duration, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"email": {email},
		"ts":    {h.signFormTimestamp(time.Now().Add(-age))},
	}
	for k, vs := range extra {
		form[k] = vs
	}
	req := httptest.NewRequest(http.MethodPost, "/newsletter/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.RemoteAddr = "10.0.0.1:12345"

	rec := httptest.NewRecorder()
	h.HandleNewsletterSubscribe(rec, req)
	return rec
}

func countSubscribers(t *testing.T, s *newsletter.SQLiteStore) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT count(*) FROM subscribers`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const validAge = 10 * time.Second

// The honeypot must look exactly like success and write nothing. Any signal of
// rejection teaches a bot to leave the field alone.
func TestSubscribeHoneypotSucceedsAndWritesNothing(t *testing.T) {
	h, store, fake := newTestHandler(t)

	rec := h.subscribeForm(t, "bot@example.com", validAge, url.Values{"phone2": {"555-1234"}})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if n := countSubscribers(t, store); n != 0 {
		t.Errorf("wrote %d subscribers, want 0", n)
	}
	if len(fake.Sent()) != 0 {
		t.Errorf("sent %d emails, want 0", len(fake.Sent()))
	}

	// ...and it must be byte-identical to a real signup.
	want := h.subscribeForm(t, "someone@example.com", validAge, nil)
	if rec.Body.String() != want.Body.String() {
		t.Error("honeypot response differs from a genuine signup response")
	}
}

func TestSubscribeFormTimestamp(t *testing.T) {
	cases := []struct {
		name       string
		ts         func(h *Handler) string
		wantStored bool
		wantBody   string // substring that must appear
	}{
		{
			name:       "valid age is accepted",
			ts:         func(h *Handler) string { return h.signFormTimestamp(time.Now().Add(-validAge)) },
			wantStored: true,
			wantBody:   "Check Your Email",
		},
		{
			name:       "submitted in under three seconds",
			ts:         func(h *Handler) string { return h.signFormTimestamp(time.Now().Add(-time.Second)) },
			wantStored: false,
			wantBody:   "please submit once more",
		},
		{
			name:       "older than two hours",
			ts:         func(h *Handler) string { return h.signFormTimestamp(time.Now().Add(-3 * time.Hour)) },
			wantStored: false,
			wantBody:   "open a while",
		},
		{
			// A forged signature is not a person, so it gets the success page.
			name:       "forged hmac",
			ts:         func(h *Handler) string { return "1753000000.deadbeef" },
			wantStored: false,
			wantBody:   "Check Your Email",
		},
		{
			name:       "missing entirely",
			ts:         func(h *Handler) string { return "" },
			wantStored: false,
			wantBody:   "Check Your Email",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, store, _ := newTestHandler(t)

			form := url.Values{"email": {"someone@example.com"}, "ts": {tc.ts(h)}}
			req := httptest.NewRequest(http.MethodPost, "/newsletter/subscribe", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			h.HandleNewsletterSubscribe(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body does not contain %q", tc.wantBody)
			}
			if got := countSubscribers(t, store) > 0; got != tc.wantStored {
				t.Errorf("stored = %v, want %v", got, tc.wantStored)
			}
		})
	}
}

// The form must not become an address-enumeration oracle: a new address, an
// already-subscribed one, and an unsubscribed one all produce the same bytes.
func TestSubscribeResponseIdenticalAcrossSubscriberStates(t *testing.T) {
	h, store, _ := newTestHandler(t)
	ctx := context.Background()

	// Pre-seed one subscribed and one unsubscribed address.
	res, err := store.StartSignup(ctx, newsletter.SignupInput{Email: "subscribed@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(ctx, newsletter.HashToken(res.ConfirmToken), "unsub-a"); err != nil {
		t.Fatal(err)
	}

	res, err = store.StartSignup(ctx, newsletter.SignupInput{Email: "unsubscribed@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(ctx, newsletter.HashToken(res.ConfirmToken), "unsub-b"); err != nil {
		t.Fatal(err)
	}
	if err := store.UnsubscribeByToken(ctx, "unsub-b"); err != nil {
		t.Fatal(err)
	}

	bodies := map[string]string{}
	for _, email := range []string{
		"brand-new@example.com",
		"subscribed@example.com",
		"unsubscribed@example.com",
	} {
		rec := h.subscribeForm(t, email, validAge, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", email, rec.Code)
		}
		bodies[email] = rec.Body.String()
	}

	first := bodies["brand-new@example.com"]
	for email, body := range bodies {
		if body != first {
			t.Errorf("%s produced a different response body", email)
		}
	}
}

func TestSubscribePerIPRateLimitTrips(t *testing.T) {
	h, store, _ := newTestHandler(t)

	// The bucket allows 5 per hour per IP; all six requests share an IP.
	for i := 0; i < 6; i++ {
		rec := h.subscribeForm(t, "person"+string(rune('a'+i))+"@example.com", validAge, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, rec.Code)
		}
	}

	// The sixth was dropped, silently.
	if n := countSubscribers(t, store); n != 5 {
		t.Errorf("stored %d subscribers, want 5", n)
	}
}

func TestSubscribeSendsAbsoluteConfirmationLink(t *testing.T) {
	h, _, fake := newTestHandler(t)

	h.subscribeForm(t, "someone@example.com", validAge, url.Values{"src": {"qr-slide"}})

	msg, ok := fake.Last()
	if !ok {
		t.Fatal("no confirmation email sent")
	}
	if msg.To != "someone@example.com" {
		t.Errorf("To = %q", msg.To)
	}
	// Confirmation is transactional; newsletters go on the broadcast stream.
	if string(msg.Stream) != "outbound" {
		t.Errorf("Stream = %q, want the transactional stream", msg.Stream)
	}
	if msg.HTMLBody == "" || msg.TextBody == "" {
		t.Error("both HTML and text bodies are required")
	}
	for _, body := range []string{msg.HTMLBody, msg.TextBody} {
		if !strings.Contains(body, "https://example.test/newsletter/confirm?token=") {
			t.Error("confirmation link must be absolute")
		}
	}
}

// A GET of the confirm link must not confirm: mail scanners prefetch it, and a
// mutating GET would defeat double opt-in entirely.
func TestConfirmGETDoesNotMutate(t *testing.T) {
	h, store, fake := newTestHandler(t)

	res, err := store.StartSignup(context.Background(), newsletter.SignupInput{Email: "someone@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/newsletter/confirm?token="+url.QueryEscape(res.ConfirmToken), nil)
	rec := httptest.NewRecorder()
	h.NewsletterConfirm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	subs, err := store.ListSubscribed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Error("GET confirm subscribed the address")
	}
	if len(fake.Sent()) != 0 {
		t.Error("GET confirm sent mail")
	}
	// The rendered page must carry the token forward into the POST form.
	if !strings.Contains(rec.Body.String(), res.ConfirmToken) {
		t.Error("confirm page does not carry the token into the form")
	}
}

func TestConfirmPOSTSubscribesAndSendsWelcome(t *testing.T) {
	h, store, fake := newTestHandler(t)

	res, err := store.StartSignup(context.Background(), newsletter.SignupInput{Email: "someone@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	rec := postConfirm(t, h, res.ConfirmToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "You're on the List") {
		t.Error("expected the confirmed page")
	}

	subs, err := store.ListSubscribed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("ListSubscribed = %d rows, want 1", len(subs))
	}

	msg, ok := fake.Last()
	if !ok || msg.To != "someone@example.com" {
		t.Fatal("no welcome email sent")
	}
	// The welcome message carries the visible unsubscribe link.
	if !strings.Contains(msg.TextBody, "https://example.test/newsletter/unsubscribe?token=") {
		t.Error("welcome email is missing an absolute unsubscribe link")
	}

	// A second POST with the same token must fail cleanly, not subscribe twice.
	again := postConfirm(t, h, res.ConfirmToken)
	if again.Code != http.StatusOK {
		t.Fatalf("second confirm status = %d, want 200", again.Code)
	}
	if !strings.Contains(again.Body.String(), "We Couldn't Use That Link") {
		t.Error("second confirm should render the invalid-link page")
	}
}

// An expired link is a dead end unless the page offers a way back.
func TestConfirmExpiredOffersResignup(t *testing.T) {
	h, store, _ := newTestHandler(t)
	ctx := context.Background()

	res, err := store.StartSignup(ctx, newsletter.SignupInput{Email: "someone@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Age the token past its expiry.
	if _, err := store.DB().Exec(
		`UPDATE subscribers SET confirm_expires_at = '2020-01-01T00:00:00.000Z'`); err != nil {
		t.Fatal(err)
	}

	rec := postConfirm(t, h, res.ConfirmToken)
	body := rec.Body.String()
	if !strings.Contains(body, "That Link Has Expired") {
		t.Error("expected the expired page")
	}
	if !strings.Contains(body, `href="/newsletter/"`) {
		t.Error("expired page must offer a path back to signup")
	}
}

func postConfirm(t *testing.T, h *Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/newsletter/confirm", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.HandleNewsletterConfirm(rec, req)
	return rec
}

// Same reasoning as confirm: a scanner prefetching the unsubscribe link would
// silently remove people who never clicked.
func TestUnsubscribeGETDoesNotMutate(t *testing.T) {
	h, store := handlerWithSubscriber(t)
	token := onlySubscriber(t, store).UnsubscribeToken

	req := httptest.NewRequest(http.MethodGet, "/newsletter/unsubscribe?token="+url.QueryEscape(token), nil)
	rec := httptest.NewRecorder()
	h.NewsletterUnsubscribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if subs, _ := store.ListSubscribed(context.Background()); len(subs) != 1 {
		t.Error("GET unsubscribe removed the subscriber")
	}
	if !strings.Contains(rec.Body.String(), token) {
		t.Error("unsubscribe page does not carry the token into the form")
	}
}

func TestUnsubscribePOST(t *testing.T) {
	h, store := handlerWithSubscriber(t)
	token := onlySubscriber(t, store).UnsubscribeToken

	form := url.Values{"token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.HandleNewsletterUnsubscribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if subs, _ := store.ListSubscribed(context.Background()); len(subs) != 0 {
		t.Error("subscriber was not removed")
	}
}

// An unknown token must produce the same page as a real one, or the endpoint
// becomes a way to test whether an address is on the list.
func TestUnsubscribeUnknownTokenLeaksNothing(t *testing.T) {
	h, store := handlerWithSubscriber(t)
	real := onlySubscriber(t, store).UnsubscribeToken

	bodyFor := func(token string) string {
		form := url.Values{"token": {token}}
		req := httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.HandleNewsletterUnsubscribe(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		return rec.Body.String()
	}

	if bodyFor("not-a-real-token") != bodyFor(real) {
		t.Error("unknown token produced a different response than a valid one")
	}
}

// RFC 8058: the recipient's mail provider POSTs this with no session and no
// CSRF token, and expects a 200.
func TestOneClickUnsubscribe(t *testing.T) {
	h, store := handlerWithSubscriber(t)
	token := onlySubscriber(t, store).UnsubscribeToken

	req := httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe/one-click?token="+url.QueryEscape(token),
		strings.NewReader("List-Unsubscribe=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.HandleNewsletterOneClick(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if subs, _ := store.ListSubscribed(context.Background()); len(subs) != 0 {
		t.Error("one-click did not unsubscribe")
	}

	// An unknown token still returns 200 — the provider is not the place to
	// report errors.
	req = httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe/one-click?token=bogus", nil)
	rec = httptest.NewRecorder()
	h.HandleNewsletterOneClick(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("unknown token status = %d, want 200", rec.Code)
	}
}

// Both RFC 8058 headers are required together; one without the other does not
// satisfy the spec.
func TestListUnsubscribeHeaders(t *testing.T) {
	h, _, _ := newTestHandler(t)

	got := h.ListUnsubscribeHeaders("tok-123")
	if want := "<https://example.test/newsletter/unsubscribe/one-click?token=tok-123>"; got["List-Unsubscribe"] != want {
		t.Errorf("List-Unsubscribe = %q, want %q", got["List-Unsubscribe"], want)
	}
	if want := "List-Unsubscribe=One-Click"; got["List-Unsubscribe-Post"] != want {
		t.Errorf("List-Unsubscribe-Post = %q, want %q", got["List-Unsubscribe-Post"], want)
	}
}

// RemoteAddr is Caddy, so a per-IP limit keyed on it would bucket everyone
// together. Only the entry Caddy appended is unforgeable.
func TestClientIP(t *testing.T) {
	cases := []struct {
		name           string
		xff            string
		remoteAddr     string
		trustedProxies int
		want           string
	}{
		{"no header falls back to peer", "", "203.0.113.5:4444", 1, "203.0.113.5"},
		{"single proxy uses the last entry", "203.0.113.9", "10.0.0.1:1", 1, "203.0.113.9"},
		{"forged left-hand entry is ignored", "1.2.3.4, 203.0.113.9", "10.0.0.1:1", 1, "203.0.113.9"},
		{"two trusted proxies step further left", "203.0.113.9, 10.0.0.7", "10.0.0.1:1", 2, "203.0.113.9"},
		// The shape this deployment actually sees: Cloudflare writes the visitor
		// and Caddy appends the edge IP it peered with. Keying on the edge IP
		// would put every visitor in one bucket.
		{"cloudflare then caddy resolves the visitor", "198.51.100.23, 172.71.0.9", "10.0.0.1:1", 2, "198.51.100.23"},
		// Cloudflare appends rather than replaces, so a client-supplied entry
		// survives to the left of the real one and must stay ignored.
		{"forged entry ahead of cloudflare is ignored", "1.2.3.4, 198.51.100.23, 172.71.0.9", "10.0.0.1:1", 2, "198.51.100.23"},
		{"more trust than entries clamps to the first", "203.0.113.9", "10.0.0.1:1", 5, "203.0.113.9"},
		{"no trusted proxy ignores the header", "1.2.3.4", "10.0.0.1:1", 0, "10.0.0.1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(req, tc.trustedProxies); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// The signup page must render a usable form with the honeypot and a fresh
// signed timestamp.
func TestNewsletterPageRendersForm(t *testing.T) {
	h, _, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/newsletter/?src=QR%20Slide!", nil)
	rec := httptest.NewRecorder()
	h.Newsletter(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for _, want := range []string{
		`action="/newsletter/subscribe"`,
		`name="phone2"`,
		`name="ts"`,
		`value="qrslide"`, // ?src= is sanitized to a short slug
	} {
		if !strings.Contains(body, want) {
			t.Errorf("signup page missing %q", want)
		}
	}
}

func TestSanitizeSource(t *testing.T) {
	cases := map[string]string{
		"":                          "web",
		"QR-Slide":                  "qr-slide",
		"  bulletin  ":              "bulletin",
		"<script>alert(1)</script>": "scriptalert1script",
		strings.Repeat("x", 80):     strings.Repeat("x", 32),
	}
	for in, want := range cases {
		if got := sanitizeSource(in); got != want {
			t.Errorf("sanitizeSource(%q) = %q, want %q", in, got, want)
		}
	}
}

// handlerWithSubscriber returns a handler whose store holds exactly one
// confirmed subscriber.
func handlerWithSubscriber(t *testing.T) (*Handler, *newsletter.SQLiteStore) {
	t.Helper()
	h, store, fake := newTestHandler(t)
	ctx := context.Background()

	res, err := store.StartSignup(ctx, newsletter.SignupInput{Email: "someone@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	unsub, err := newsletter.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(ctx, newsletter.HashToken(res.ConfirmToken), unsub); err != nil {
		t.Fatal(err)
	}
	fake.Reset()
	return h, store
}

func onlySubscriber(t *testing.T, store *newsletter.SQLiteStore) newsletter.Subscriber {
	t.Helper()
	subs, err := store.ListSubscribed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected exactly one subscriber, got %d", len(subs))
	}
	return subs[0]
}
