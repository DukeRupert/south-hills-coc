-- Newsletter subscription schema.
--
-- Translated from the Postgres DDL in the spec: IDENTITY becomes INTEGER
-- PRIMARY KEY AUTOINCREMENT, TIMESTAMPTZ becomes TEXT holding RFC3339 UTC,
-- BYTEA becomes BLOB, and INET becomes TEXT.

CREATE TABLE subscribers (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    email               TEXT NOT NULL UNIQUE
                          CHECK (email = lower(email) AND email LIKE '%_@_%'),
    status              TEXT NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending','subscribed',
                                            'unsubscribed','bounced','complained')),
    confirm_token_hash  BLOB,
    confirm_sent_at     TEXT,
    confirm_expires_at  TEXT,
    confirmed_at        TEXT,
    unsubscribe_token   TEXT UNIQUE,
    unsubscribed_at     TEXT,
    source              TEXT NOT NULL DEFAULT 'web',
    signup_ip           TEXT,
    signup_user_agent   TEXT,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE newsletters (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    subject     TEXT NOT NULL,
    body_md     TEXT NOT NULL,
    body_html   TEXT NOT NULL,
    body_text   TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft','sending','sent')),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    sent_at     TEXT
);

CREATE TABLE deliveries (
    newsletter_id       INTEGER NOT NULL REFERENCES newsletters(id) ON DELETE CASCADE,
    subscriber_id       INTEGER NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'queued'
                          CHECK (status IN ('queued','sent','failed')),
    provider_message_id TEXT,
    error               TEXT,
    attempted_at        TEXT,
    PRIMARY KEY (newsletter_id, subscriber_id)
);
