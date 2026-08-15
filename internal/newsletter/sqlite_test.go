package newsletter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// setClock pins the store's clock so the ResendInterval and ConfirmTTL
// branches are testable without sleeping.
func (s *SQLiteStore) setClock(t time.Time) {
	s.now = func() time.Time { return t }
}

func mustSignup(t *testing.T, s *SQLiteStore, email string) SignupResult {
	t.Helper()
	res, err := s.StartSignup(context.Background(), SignupInput{Email: email, Source: "web"})
	if err != nil {
		t.Fatalf("StartSignup(%s): %v", email, err)
	}
	return res
}

func mustConfirm(t *testing.T, s *SQLiteStore, token string) Subscriber {
	t.Helper()
	unsub, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Confirm(context.Background(), HashToken(token), unsub)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	return sub
}

// TestStartSignupDecisionTable covers one case per row of the spec's decision
// table: the state a prior row is in determines both the resulting state and
// whether a confirmation email goes out.
func TestStartSignupDecisionTable(t *testing.T) {
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		// setup leaves the address in the state under test.
		setup        func(t *testing.T, s *SQLiteStore, email string)
		at           time.Time // clock for the signup under test
		wantSend     bool
		wantStatus   Status
		wantNewToken bool
	}{
		{
			name:         "no existing row inserts pending and mints token",
			setup:        func(*testing.T, *SQLiteStore, string) {},
			at:           base,
			wantSend:     true,
			wantStatus:   StatusPending,
			wantNewToken: true,
		},
		{
			name: "pending within resend interval is throttled",
			setup: func(t *testing.T, s *SQLiteStore, email string) {
				s.setClock(base)
				mustSignup(t, s, email)
			},
			at:         base.Add(ResendInterval - time.Second),
			wantSend:   false,
			wantStatus: StatusPending,
		},
		{
			name: "pending past resend interval re-sends",
			setup: func(t *testing.T, s *SQLiteStore, email string) {
				s.setClock(base)
				mustSignup(t, s, email)
			},
			at:           base.Add(ResendInterval + time.Second),
			wantSend:     true,
			wantStatus:   StatusPending,
			wantNewToken: true,
		},
		{
			name: "subscribed does nothing",
			setup: func(t *testing.T, s *SQLiteStore, email string) {
				s.setClock(base)
				mustConfirm(t, s, mustSignup(t, s, email).ConfirmToken)
			},
			at:         base.Add(time.Hour),
			wantSend:   false,
			wantStatus: StatusSubscribed,
		},
		{
			name: "unsubscribed resets to pending",
			setup: func(t *testing.T, s *SQLiteStore, email string) {
				s.setClock(base)
				sub := mustConfirm(t, s, mustSignup(t, s, email).ConfirmToken)
				if err := s.UnsubscribeByToken(context.Background(), sub.UnsubscribeToken); err != nil {
					t.Fatal(err)
				}
			},
			at:           base.Add(time.Hour),
			wantSend:     true,
			wantStatus:   StatusPending,
			wantNewToken: true,
		},
		{
			name: "bounced resets to pending",
			setup: func(t *testing.T, s *SQLiteStore, email string) {
				s.setClock(base)
				mustSignup(t, s, email)
				if err := s.SetStatusByEmail(context.Background(), email, StatusBounced); err != nil {
					t.Fatal(err)
				}
			},
			at:           base.Add(time.Hour),
			wantSend:     true,
			wantStatus:   StatusPending,
			wantNewToken: true,
		},
		{
			name: "complained stays dead",
			setup: func(t *testing.T, s *SQLiteStore, email string) {
				s.setClock(base)
				mustSignup(t, s, email)
				if err := s.SetStatusByEmail(context.Background(), email, StatusComplained); err != nil {
					t.Fatal(err)
				}
			},
			at:         base.Add(time.Hour),
			wantSend:   false,
			wantStatus: StatusComplained,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			const email = "someone@example.com"
			tc.setup(t, s, email)

			s.setClock(tc.at)
			res, err := s.StartSignup(context.Background(), SignupInput{
				Email: email, Source: "qr", IP: "203.0.113.7", UserAgent: "test-agent",
			})
			if err != nil {
				t.Fatalf("StartSignup: %v", err)
			}

			if res.SendConfirmation != tc.wantSend {
				t.Errorf("SendConfirmation = %v, want %v", res.SendConfirmation, tc.wantSend)
			}
			if res.Subscriber.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", res.Subscriber.Status, tc.wantStatus)
			}
			if got := res.ConfirmToken != ""; got != tc.wantNewToken {
				t.Errorf("minted token = %v, want %v", got, tc.wantNewToken)
			}
			// The consent record is only meaningful when we act on the signup.
			if tc.wantSend && res.Subscriber.SignupIP != "203.0.113.7" {
				t.Errorf("signup_ip = %q, want the submitting IP", res.Subscriber.SignupIP)
			}
		})
	}
}

// A throttled re-submission must not rotate the token, or the link in the
// message the person already received would stop working.
func TestStartSignupThrottledKeepsExistingTokenValid(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	s.setClock(base)

	first := mustSignup(t, s, "someone@example.com")

	s.setClock(base.Add(time.Minute))
	second := mustSignup(t, s, "someone@example.com")
	if second.SendConfirmation || second.ConfirmToken != "" {
		t.Fatalf("expected throttled signup, got send=%v minted=%v",
			second.SendConfirmation, second.ConfirmToken != "")
	}

	if _, err := s.Confirm(context.Background(), HashToken(first.ConfirmToken), "unsub-1"); err != nil {
		t.Fatalf("original token should still confirm: %v", err)
	}
}

func TestConfirmRejectsExpiredToken(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	s.setClock(base)

	res := mustSignup(t, s, "someone@example.com")

	s.setClock(base.Add(ConfirmTTL + time.Minute))
	_, err := s.Confirm(context.Background(), HashToken(res.ConfirmToken), "unsub-1")
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("err = %v, want ErrExpiredToken", err)
	}
}

func TestConfirmRejectsUnknownToken(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Confirm(context.Background(), HashToken("never-issued"), "unsub-1")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestConfirmIsSingleUse(t *testing.T) {
	s := newTestStore(t)
	res := mustSignup(t, s, "someone@example.com")

	sub := mustConfirm(t, s, res.ConfirmToken)
	if sub.Status != StatusSubscribed {
		t.Fatalf("status = %q, want subscribed", sub.Status)
	}
	if sub.UnsubscribeToken == "" {
		t.Fatal("unsubscribe token must be minted at confirmation")
	}
	if sub.ConfirmedAt.IsZero() {
		t.Fatal("confirmed_at must be set")
	}

	_, err := s.Confirm(context.Background(), HashToken(res.ConfirmToken), "unsub-2")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("second confirm err = %v, want ErrInvalidToken", err)
	}
}

func TestUnsubscribe(t *testing.T) {
	s := newTestStore(t)
	sub := mustConfirm(t, s, mustSignup(t, s, "someone@example.com").ConfirmToken)

	if err := s.UnsubscribeByToken(context.Background(), sub.UnsubscribeToken); err != nil {
		t.Fatalf("UnsubscribeByToken: %v", err)
	}
	// Idempotent: a second click, or a mail scanner replaying the POST, is
	// not an error.
	if err := s.UnsubscribeByToken(context.Background(), sub.UnsubscribeToken); err != nil {
		t.Fatalf("second UnsubscribeByToken: %v", err)
	}

	subs, err := s.ListSubscribed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Fatalf("ListSubscribed returned %d rows after unsubscribe", len(subs))
	}

	if err := s.UnsubscribeByToken(context.Background(), "not-a-real-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown token err = %v, want ErrInvalidToken", err)
	}
}

// ListSubscribed is what the send path reads. Anything other than a confirmed
// subscriber appearing here would mail someone who never opted in.
func TestListSubscribedExcludesNonSubscribed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// One of each non-subscribed state.
	mustSignup(t, s, "pending@example.com")

	unsubbed := mustConfirm(t, s, mustSignup(t, s, "unsubbed@example.com").ConfirmToken)
	if err := s.UnsubscribeByToken(ctx, unsubbed.UnsubscribeToken); err != nil {
		t.Fatal(err)
	}

	mustSignup(t, s, "bounced@example.com")
	if err := s.SetStatusByEmail(ctx, "bounced@example.com", StatusBounced); err != nil {
		t.Fatal(err)
	}

	mustSignup(t, s, "complained@example.com")
	if err := s.SetStatusByEmail(ctx, "complained@example.com", StatusComplained); err != nil {
		t.Fatal(err)
	}

	// ...and one that is actually subscribed.
	mustConfirm(t, s, mustSignup(t, s, "active@example.com").ConfirmToken)

	subs, err := s.ListSubscribed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Email != "active@example.com" {
		got := make([]string, len(subs))
		for i, s := range subs {
			got[i] = s.Email
		}
		t.Fatalf("ListSubscribed = %v, want [active@example.com]", got)
	}
}

func TestStartSignupNormalizesAndValidates(t *testing.T) {
	s := newTestStore(t)

	res, err := s.StartSignup(context.Background(), SignupInput{Email: "  Someone@Example.COM \t"})
	if err != nil {
		t.Fatalf("StartSignup: %v", err)
	}
	if res.Subscriber.Email != "someone@example.com" {
		t.Errorf("email = %q, want normalized", res.Subscriber.Email)
	}
	if res.Subscriber.Source != "web" {
		t.Errorf("source = %q, want default %q", res.Subscriber.Source, "web")
	}

	for _, bad := range []string{"", "not-an-email", "someone@gmail", "a b@example.com", "<a@b.com>"} {
		if _, err := s.StartSignup(context.Background(), SignupInput{Email: bad}); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("StartSignup(%q) err = %v, want ErrInvalidEmail", bad, err)
		}
	}
}
