package newsletter

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// timeLayout matches the strftime format used by the column defaults.
const timeLayout = "2006-01-02T15:04:05.999Z"

// SQLiteStore is the Store implementation. It is safe for concurrent use.
type SQLiteStore struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the SQLite database at path and applies any
// pending migrations.
//
// The DSN sets _txlock=immediate so that every transaction takes the write
// lock up front. StartSignup does read-then-write, and a deferred transaction
// would let two concurrent signups for the same address both read "no row"
// before either inserts.
func Open(path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf(
		"file:%s?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		url.PathEscape(path),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("newsletter: open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("newsletter: open %s: %w", path, err)
	}
	s := &SQLiteStore{db: db, now: time.Now}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// DB exposes the underlying handle for later steps (admin compose, sending).
func (s *SQLiteStore) DB() *sql.DB { return s.db }

func (s *SQLiteStore) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`); err != nil {
		return fmt.Errorf("newsletter: create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		var applied int
		if err := s.db.QueryRow(
			`SELECT count(*) FROM schema_migrations WHERE name = ?`, name,
		).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("newsletter: migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("newsletter: migration %s: %w", name, err)
		}
	}
	return nil
}

// StartSignup implements the decision table in the spec. An address may hit a
// row in any prior state, so this reads the row inside the write transaction
// and branches explicitly rather than collapsing into an upsert.
func (s *SQLiteStore) StartSignup(ctx context.Context, in SignupInput) (SignupResult, error) {
	email := NormalizeEmail(in.Email)
	if err := ValidateEmail(email); err != nil {
		return SignupResult{}, err
	}
	source := in.Source
	if source == "" {
		source = "web"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SignupResult{}, err
	}
	defer tx.Rollback()

	now := s.now().UTC()

	var (
		id            int64
		status        Status
		confirmSentAt sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, status, confirm_sent_at FROM subscribers WHERE email = ?`, email,
	).Scan(&id, &status, &confirmSentAt)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		// No row: insert pending and mint a token.
		token, hash, expires, err := mintConfirm(now)
		if err != nil {
			return SignupResult{}, err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO subscribers
			   (email, status, confirm_token_hash, confirm_sent_at, confirm_expires_at,
			    source, signup_ip, signup_user_agent, created_at, updated_at)
			 VALUES (?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?)`,
			email, hash, formatTime(now), formatTime(expires),
			source, nullString(in.IP), nullString(in.UserAgent),
			formatTime(now), formatTime(now),
		)
		if err != nil {
			return SignupResult{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return SignupResult{}, err
		}
		sub, err := getByID(ctx, tx, id)
		if err != nil {
			return SignupResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return SignupResult{}, err
		}
		return SignupResult{Subscriber: sub, ConfirmToken: token, SendConfirmation: true}, nil

	case err != nil:
		return SignupResult{}, err
	}

	switch status {
	case StatusSubscribed, StatusComplained:
		// Subscribed: re-confirming an active member is noise.
		// Complained: permanently dead. That person marked a message as spam
		// and Postmark has suppressed them upstream; resurrecting them through
		// a public form puts the domain back in front of the same filter.
		// Re-adding is a deliberate administrator action, not a form action.
		sub, err := getByID(ctx, tx, id)
		if err != nil {
			return SignupResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return SignupResult{}, err
		}
		return SignupResult{Subscriber: sub, SendConfirmation: false}, nil

	case StatusPending:
		// Throttle: at most one confirmation email per ResendInterval, which
		// caps mail aimed at any single victim at six per hour.
		//
		// Note the token is only re-minted when we are actually going to send.
		// Rotating it on a throttled submission would invalidate the link in
		// the message the person already received.
		if confirmSentAt.Valid {
			sent, perr := parseTime(confirmSentAt.String)
			if perr == nil && now.Sub(sent) < ResendInterval {
				sub, err := getByID(ctx, tx, id)
				if err != nil {
					return SignupResult{}, err
				}
				if err := tx.Commit(); err != nil {
					return SignupResult{}, err
				}
				return SignupResult{Subscriber: sub, SendConfirmation: false}, nil
			}
		}
	}

	// pending (past the throttle), unsubscribed, or bounced: reset to pending
	// and mint a fresh token.
	token, hash, expires, err := mintConfirm(now)
	if err != nil {
		return SignupResult{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE subscribers
		    SET status = 'pending',
		        confirm_token_hash = ?,
		        confirm_sent_at = ?,
		        confirm_expires_at = ?,
		        confirmed_at = NULL,
		        unsubscribed_at = NULL,
		        source = ?,
		        signup_ip = ?,
		        signup_user_agent = ?,
		        updated_at = ?
		  WHERE id = ?`,
		hash, formatTime(now), formatTime(expires),
		source, nullString(in.IP), nullString(in.UserAgent), formatTime(now), id,
	); err != nil {
		return SignupResult{}, err
	}
	sub, err := getByID(ctx, tx, id)
	if err != nil {
		return SignupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SignupResult{}, err
	}
	return SignupResult{Subscriber: sub, ConfirmToken: token, SendConfirmation: true}, nil
}

// Confirm completes double opt-in. It rejects expired tokens and any row that
// is not pending, and mints the unsubscribe token at the moment of
// confirmation.
func (s *SQLiteStore) Confirm(ctx context.Context, tokenHash []byte, unsubToken string) (Subscriber, error) {
	if len(tokenHash) == 0 || unsubToken == "" {
		return Subscriber{}, ErrInvalidToken
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Subscriber{}, err
	}
	defer tx.Rollback()

	now := s.now().UTC()

	var (
		id      int64
		status  Status
		expires sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, status, confirm_expires_at FROM subscribers WHERE confirm_token_hash = ?`,
		tokenHash,
	).Scan(&id, &status, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Subscriber{}, ErrInvalidToken
	} else if err != nil {
		return Subscriber{}, err
	}

	if status != StatusPending {
		return Subscriber{}, ErrInvalidToken
	}
	if !expires.Valid {
		return Subscriber{}, ErrInvalidToken
	}
	exp, err := parseTime(expires.String)
	if err != nil {
		return Subscriber{}, ErrInvalidToken
	}
	if exp.Before(now) {
		return Subscriber{}, ErrExpiredToken
	}

	// Clearing confirm_token_hash makes a second POST with the same token fail
	// as an unknown token rather than succeeding twice.
	if _, err := tx.ExecContext(ctx,
		`UPDATE subscribers
		    SET status = 'subscribed',
		        confirmed_at = ?,
		        confirm_token_hash = NULL,
		        confirm_expires_at = NULL,
		        unsubscribe_token = COALESCE(unsubscribe_token, ?),
		        unsubscribed_at = NULL,
		        updated_at = ?
		  WHERE id = ?`,
		formatTime(now), unsubToken, formatTime(now), id,
	); err != nil {
		return Subscriber{}, err
	}

	sub, err := getByID(ctx, tx, id)
	if err != nil {
		return Subscriber{}, err
	}
	if err := tx.Commit(); err != nil {
		return Subscriber{}, err
	}
	return sub, nil
}

// UnsubscribeByToken removes an address from the list. It is idempotent for an
// already-unsubscribed token, so a second click (or a mail scanner replaying
// the POST) is not an error.
func (s *SQLiteStore) UnsubscribeByToken(ctx context.Context, token string) error {
	if token == "" {
		return ErrInvalidToken
	}
	now := s.now().UTC()

	res, err := s.db.ExecContext(ctx,
		`UPDATE subscribers
		    SET status = 'unsubscribed',
		        unsubscribed_at = COALESCE(unsubscribed_at, ?),
		        updated_at = ?
		  WHERE unsubscribe_token = ?
		    AND status NOT IN ('complained','bounced')`,
		formatTime(now), formatTime(now), token,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Either the token is unknown or the row is in a state we leave alone.
		// Distinguish those only well enough to stay idempotent; callers must
		// not surface the difference.
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT count(*) FROM subscribers WHERE unsubscribe_token = ?`, token,
		).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrInvalidToken
		}
	}
	return nil
}

// ListSubscribed returns only confirmed subscribers. Everything else —
// pending, unsubscribed, bounced, complained — is excluded.
func (s *SQLiteStore) ListSubscribed(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.db.QueryContext(ctx,
		selectColumns+` FROM subscribers WHERE status = 'subscribed' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Subscriber
	for rows.Next() {
		sub, err := scanSubscriber(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// SetStatusByEmail is the administrative and webhook path for forcing a state.
func (s *SQLiteStore) SetStatusByEmail(ctx context.Context, email string, st Status) error {
	now := s.now().UTC()
	unsubAt := sql.NullString{}
	if st == StatusUnsubscribed {
		unsubAt = sql.NullString{String: formatTime(now), Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscribers
		    SET status = ?,
		        unsubscribed_at = COALESCE(?, unsubscribed_at),
		        updated_at = ?
		  WHERE email = ?`,
		string(st), unsubAt, formatTime(now), NormalizeEmail(email),
	)
	return err
}

// --- helpers ---

func mintConfirm(now time.Time) (token string, hash []byte, expires time.Time, err error) {
	token, err = NewToken()
	if err != nil {
		return "", nil, time.Time{}, err
	}
	return token, HashToken(token), now.Add(ConfirmTTL), nil
}

const selectColumns = `SELECT id, email, status, confirm_sent_at, confirm_expires_at,
	confirmed_at, unsubscribe_token, unsubscribed_at, source, signup_ip,
	signup_user_agent, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func getByID(ctx context.Context, tx *sql.Tx, id int64) (Subscriber, error) {
	return scanSubscriber(tx.QueryRowContext(ctx, selectColumns+` FROM subscribers WHERE id = ?`, id))
}

func scanSubscriber(r rowScanner) (Subscriber, error) {
	var (
		s                                       Subscriber
		sentAt, expiresAt, confirmedAt, unsubAt sql.NullString
		createdAt, updatedAt                    string
		unsubToken, signupIP, signupUA          sql.NullString
	)
	if err := r.Scan(&s.ID, &s.Email, &s.Status, &sentAt, &expiresAt, &confirmedAt,
		&unsubToken, &unsubAt, &s.Source, &signupIP, &signupUA,
		&createdAt, &updatedAt); err != nil {
		return Subscriber{}, err
	}
	s.ConfirmSentAt = parseTimeOrZero(sentAt)
	s.ConfirmExpiresAt = parseTimeOrZero(expiresAt)
	s.ConfirmedAt = parseTimeOrZero(confirmedAt)
	s.UnsubscribedAt = parseTimeOrZero(unsubAt)
	s.UnsubscribeToken = unsubToken.String
	s.SignupIP = signupIP.String
	s.SignupUserAgent = signupUA.String
	s.CreatedAt = parseTimeOrZero(sql.NullString{String: createdAt, Valid: true})
	s.UpdatedAt = parseTimeOrZero(sql.NullString{String: updatedAt, Valid: true})
	return s, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func parseTimeOrZero(ns sql.NullString) time.Time {
	if !ns.Valid || ns.String == "" {
		return time.Time{}
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
