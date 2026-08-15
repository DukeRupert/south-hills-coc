// Package newsletter implements the subscriber list behind the church
// newsletter: double opt-in signup, confirmation, and unsubscribe.
package newsletter

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"
)

type Status string

const (
	StatusPending      Status = "pending"
	StatusSubscribed   Status = "subscribed"
	StatusUnsubscribed Status = "unsubscribed"
	StatusBounced      Status = "bounced"
	StatusComplained   Status = "complained"
)

// ConfirmTTL is how long a confirmation link stays valid. Stated in the
// confirmation email body.
const ConfirmTTL = 48 * time.Hour

// ResendInterval is the floor between confirmation emails aimed at a single
// address. It doubles as the per-address rate limit.
const ResendInterval = 10 * time.Minute

var (
	// ErrInvalidToken covers an unknown, malformed, or already-used token.
	// Callers must not distinguish these from one another in responses.
	ErrInvalidToken = errors.New("newsletter: invalid token")
	// ErrExpiredToken is a well-formed confirmation token past its expiry.
	ErrExpiredToken = errors.New("newsletter: token expired")
	// ErrInvalidEmail is returned for syntactically invalid addresses.
	ErrInvalidEmail = errors.New("newsletter: invalid email address")
)

type Subscriber struct {
	ID               int64
	Email            string
	Status           Status
	ConfirmSentAt    time.Time
	ConfirmExpiresAt time.Time
	ConfirmedAt      time.Time
	UnsubscribeToken string
	UnsubscribedAt   time.Time
	Source           string
	SignupIP         string
	SignupUserAgent  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type SignupInput struct {
	Email     string // caller normalizes: trim + lowercase
	Source    string // "web", "qr", etc.
	IP        string
	UserAgent string
}

type SignupResult struct {
	Subscriber   Subscriber
	ConfirmToken string // plaintext, returned once, never persisted
	// SendConfirmation reports whether the caller should actually send a
	// confirmation email. False for already-subscribed and complained
	// addresses, and for a pending address throttled by ResendInterval.
	SendConfirmation bool
}

type Store interface {
	StartSignup(ctx context.Context, in SignupInput) (SignupResult, error)
	Confirm(ctx context.Context, tokenHash []byte, unsubToken string) (Subscriber, error)
	UnsubscribeByToken(ctx context.Context, token string) error
	ListSubscribed(ctx context.Context) ([]Subscriber, error)
	SetStatusByEmail(ctx context.Context, email string, s Status) error
}

// NormalizeEmail trims surrounding whitespace and lowercases the address. The
// database CHECK constraint rejects anything this misses.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ValidateEmail checks syntax only. No MX lookup, no disposable-domain
// blocklist: a false positive on a congregation list costs more than it saves.
func ValidateEmail(s string) error {
	if s == "" || len(s) > 254 {
		return ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return ErrInvalidEmail
	}
	// mail.ParseAddress accepts a bare "user@host"; require a dotted domain so
	// obvious typos ("someone@gmail") do not become permanently pending rows.
	at := strings.LastIndex(s, "@")
	if at < 1 || !strings.Contains(s[at+1:], ".") {
		return ErrInvalidEmail
	}
	return nil
}

// NewToken mints a 256-bit URL-safe random token.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the SHA-256 of a confirmation token. Only the hash is
// persisted, so a database dump cannot be used to confirm arbitrary addresses.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
