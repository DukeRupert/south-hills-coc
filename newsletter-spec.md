# Church Newsletter Subscription — Implementation Spec

**Status:** approved for implementation
**Stack:** Go + templ + htmx + Alpine.js + Tailwind, PostgreSQL, Postmark, Docker Compose behind Caddy

---

## 1. Scope

Three capabilities, added to the existing church website (an existing Go application):

1. **Self-service subscribe.** A QR code on a sanctuary slide points at a signup page. A visitor enters an email address and is added to the weekly newsletter list without administrator involvement.
2. **Self-service unsubscribe.** A link at the bottom of every newsletter, plus a page on the website, removes the address without administrator involvement.
3. **Administrator send.** An authenticated administrator composes a newsletter and sends it to the confirmed list.

The list is expected to stay under 100 subscribers. Design accordingly — do not build a job queue, worker pool, or sharding scheme.

### Out of scope

Segmentation, scheduled/future sends, open and click tracking, A/B testing, WYSIWYG editing, image uploads, multiple lists, subscriber-facing preference centers.

---

## 2. Non-negotiables

These are the decisions most likely to be implemented wrong. Read them before writing code.

**Double opt-in is the primary bot mitigation.** A form submission creates a `pending` row only. Nothing is ever sent a newsletter until a human clicks through from a confirmation email. Everything else in §5 is secondary.

**Mutating routes reached from email must be POST, never GET.** Outlook and Gmail link scanners prefetch URLs found in email bodies. If `GET /newsletter/unsubscribe?token=…` unsubscribes, a scanner will silently unsubscribe people who never clicked. The same applies to confirmation: a prefetched GET confirm link defeats double opt-in entirely, because the address gets confirmed without a human ever acting. Both flows therefore render a page on GET and act on POST.

**Two Postmark message streams.** Confirmation and welcome emails are transactional and go on the default transactional stream. Newsletters are one-to-many and must go on a dedicated **broadcast** stream — separate Postmark infrastructure, separate reputation. `MessageStream` must be set explicitly on every send; omitting it silently falls back to the transactional stream and puts newsletters on the wrong infrastructure.

**RFC 8058 one-click unsubscribe is mandatory, not optional.** Gmail and Yahoo require it of bulk senders. Postmark also requires a visible unsubscribe link on all broadcast messages and will inject its own if one is absent. Build both the visible link and the header-based one-click endpoint.

**Never differentiate responses by subscriber state.** The signup form returns identical output for a new address, an already-subscribed address, an unsubscribed address, and a honeypot-tripped bot. Any variation turns the form into an address-enumeration oracle or teaches a bot to evade the trap.

**The app runs behind Caddy.** `http.Request.RemoteAddr` is the proxy. Per-IP rate limiting keyed on it will bucket every visitor together and either block everyone or no one. Resolve the client IP from `X-Forwarded-For` with Caddy configured as a trusted proxy.

---

## 3. Data model

Migration `0001_newsletter`.

```sql
CREATE TABLE subscribers (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email               TEXT NOT NULL UNIQUE
                          CHECK (email = lower(email) AND email LIKE '%_@_%'),
    status              TEXT NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending','subscribed',
                                            'unsubscribed','bounced','complained')),
    confirm_token_hash  BYTEA,
    confirm_sent_at     TIMESTAMPTZ,
    confirm_expires_at  TIMESTAMPTZ,
    confirmed_at        TIMESTAMPTZ,
    unsubscribe_token   TEXT UNIQUE,
    unsubscribed_at     TIMESTAMPTZ,
    source              TEXT NOT NULL DEFAULT 'web',
    signup_ip           INET,
    signup_user_agent   TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE newsletters (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subject     TEXT NOT NULL,
    body_md     TEXT NOT NULL,
    body_html   TEXT NOT NULL,
    body_text   TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft','sending','sent')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at     TIMESTAMPTZ
);

CREATE TABLE deliveries (
    newsletter_id       BIGINT NOT NULL REFERENCES newsletters(id) ON DELETE CASCADE,
    subscriber_id       BIGINT NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'queued'
                          CHECK (status IN ('queued','sent','failed')),
    provider_message_id TEXT,
    error               TEXT,
    attempted_at        TIMESTAMPTZ,
    PRIMARY KEY (newsletter_id, subscriber_id)
);
```

**Rationale for the non-obvious parts:**

- `CHECK (email = lower(email))` — email is normalized in Go before insert. This check makes the database reject the write if the normalizer regresses, while keeping `ON CONFLICT (email)` simple.
- `confirm_token_hash BYTEA` — store SHA-256 of the token, never the token. A database dump must not allow confirming arbitrary addresses.
- `unsubscribe_token TEXT` — stored in plaintext because it is looked up by value. Random and opaque, so it is revocable; do not derive it by signing the subscriber ID.
- `signup_ip` / `signup_user_agent` — this is the consent record. It is what gets produced if someone claims they were added without permission.
- `deliveries` composite primary key — send idempotency comes from the constraint, not from application logic. A retried batch physically cannot double-mail anyone.
- No additional indexes. At this scale the planner sequential-scans regardless.

---

## 4. Store layer

```go
package newsletter

type Status string

const (
    StatusPending      Status = "pending"
    StatusSubscribed   Status = "subscribed"
    StatusUnsubscribed Status = "unsubscribed"
    StatusBounced      Status = "bounced"
    StatusComplained   Status = "complained"
)

type SignupInput struct {
    Email     string // caller normalizes: trim + lowercase
    Source    string // "web", "qr", etc.
    IP        string
    UserAgent string
}

type SignupResult struct {
    Subscriber       Subscriber
    ConfirmToken     string // plaintext, returned once, never persisted
    SendConfirmation bool
}

type Store interface {
    StartSignup(ctx context.Context, in SignupInput) (SignupResult, error)
    Confirm(ctx context.Context, tokenHash []byte, unsubToken string) (Subscriber, error)
    UnsubscribeByToken(ctx context.Context, token string) error
    ListSubscribed(ctx context.Context) ([]Subscriber, error)
    SetStatusByEmail(ctx context.Context, email string, s Status) error
}
```

### 4.1 StartSignup decision table

An incoming address may hit a row in any prior state. Implement as `SELECT … FOR UPDATE` inside a transaction, then branch in Go. Readability matters more than collapsing this into a clever upsert.

| Existing row | Action | `SendConfirmation` |
|---|---|---|
| none | insert `pending`, mint token | true |
| `pending` | refresh token + expiry | true only if `confirm_sent_at` is older than 10 minutes |
| `subscribed` | none | false |
| `unsubscribed` | reset to `pending`, mint token | true |
| `bounced` | reset to `pending`, mint token | true |
| `complained` | none | false |

`subscribed` does nothing because re-confirming an active member is noise. `complained` stays dead permanently — that person marked a message as spam, Postmark has suppressed them upstream, and resurrecting them through a public form puts the domain back in front of the same filter. Re-adding a complained address is an administrator action, done deliberately, not something the form can do.

The 10-minute floor on `pending` doubles as the per-address rate limit: it caps confirmation emails aimed at any single victim at six per hour.

`Confirm` must reject tokens where `confirm_expires_at < now()` or status is not `pending`, and must mint the `unsubscribe_token` at the moment of confirmation.

---

## 5. Bot mitigation

Layered, in order of importance. All of it is invisible to a legitimate user.

1. **Double opt-in.** See §2. A bot stuffing scraped addresses achieves nothing but a single email to a third party.
2. **Honeypot field.** A plausibly-named input (`phone2`) hidden with `position:absolute; left:-9999px`, plus `aria-hidden="true"`, `tabindex="-1"`, `autocomplete="off"`. If non-empty, return the standard success page and discard the submission. Never signal rejection.
3. **Signed timestamp.** Hidden field containing `<unix>.<hmac-sha256(unix, FORM_HMAC_SECRET)>`. Reject on invalid HMAC, age under 3 seconds, or age over 2 hours. Kills scripted posts and replayed scraped forms.
4. **Rate limits.** Per client IP: 5 submissions/hour. Global: 20/hour. In-memory token buckets are sufficient at this scale; a restart resetting them is acceptable. Read the client IP per §2.
5. **Per-address throttle.** Enforced by the store, see §4.1.

**Deliberately not included:** Turnstile or any CAPTCHA. The audience skews toward people scanning a QR code on a phone in a dim sanctuary; challenge widgets cost real signups. Leave a clean integration point in the handler so it can be added if logs later show genuine abuse.

Validate email syntax only. Do not do MX lookups or disposable-domain blocklists — false positives on a congregation list cost more than they save.

---

## 6. Routes

| Method | Path | Notes |
|---|---|---|
| GET | `/newsletter` | Signup form. QR target. Accepts `?src=` for attribution. |
| POST | `/newsletter/subscribe` | Applies §5. Always renders the same "check your email" result. |
| GET | `/newsletter/confirm?token=` | Renders a confirm page with a button. **Does not mutate.** |
| POST | `/newsletter/confirm` | Performs confirmation. |
| GET | `/newsletter/unsubscribe?token=` | Renders a confirm page with a button. **Does not mutate.** |
| POST | `/newsletter/unsubscribe` | Performs unsubscribe. |
| POST | `/newsletter/unsubscribe/one-click?token=` | RFC 8058. No confirmation page. Returns 200. |
| POST | `/webhooks/postmark` | Bounce and spam complaint handling. |
| GET/POST | `/admin/newsletter…` | See §8. |

**CSRF notes.** The one-click endpoint is POSTed by the recipient's mail provider, not by a browser with a session — it cannot carry a CSRF token and must be exempted from CSRF middleware. The confirm and unsubscribe POST forms are likewise unauthenticated; the bearer token in the request *is* the authorization, and a CSRF token adds nothing. Exempt all three, and do not let a blanket middleware silently 403 them.

Keep the QR URL short and typeable — people will read it off a slide and type it rather than scan. `/newsletter` is good; a signed or hashed path is not.

---

## 7. Email

### 7.1 Configuration

```
POSTMARK_SERVER_TOKEN
POSTMARK_STREAM_TRANSACTIONAL   # e.g. "outbound"
POSTMARK_STREAM_BROADCAST       # e.g. "broadcast" — must be created in Postmark first
NEWSLETTER_FROM_ADDRESS
NEWSLETTER_FROM_NAME
SITE_BASE_URL                   # absolute, used to build all email links
FORM_HMAC_SECRET
```

### 7.2 Mailer abstraction

Define a `Mailer` interface with a Postmark implementation and an in-memory fake. All handler tests use the fake; the Postmark client is exercised by a single manual smoke test. Nothing in the handler layer should import a Postmark type.

### 7.3 Transactional messages

Confirmation email and post-confirmation welcome email. Transactional stream. Both need HTML and plain-text bodies. The confirmation link is absolute, built from `SITE_BASE_URL`, and expires in 48 hours — state the expiry in the body.

### 7.4 Newsletter messages

Broadcast stream. Every message carries:

```
List-Unsubscribe: <https://SITE/newsletter/unsubscribe/one-click?token=TOKEN>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
```

Both headers are required together — one without the other does not satisfy RFC 8058. The token is per-subscriber, so headers are built per message, not once per batch.

The visible unsubscribe link in the footer points at `GET /newsletter/unsubscribe?token=…`.

Both `HtmlBody` and `TextBody` are required. The administrator writes Markdown; render HTML with goldmark and derive the text part from the Markdown source rather than by stripping tags from the HTML.

### 7.5 Sending

`POST https://api.postmarkapp.com/email/batch`, up to 500 messages per call. The whole list fits in one request — do not build a queue.

Before the call, insert one `queued` row per recipient into `deliveries` and set the newsletter to `sending`. The response is an array **positionally matched to the request array**; map each element back to its subscriber by index and record `sent` with the message ID or `failed` with the error. Partial failure is normal and must not abort the batch. Set the newsletter to `sent` when every delivery row is terminal.

A retry after a crash re-sends only rows still `queued`, which the composite primary key makes safe.

### 7.6 Webhooks

Handle `Bounce` (hard bounces only) and `SpamComplaint`, mapping to `bounced` and `complained` via `SetStatusByEmail`. Postmark suppresses these addresses automatically on its side, so this endpoint exists to keep the local database from drifting out of agreement with Postmark. Verify the request with HTTP basic auth on the webhook URL. Ignore soft bounces.

---

## 8. Administrator interface

Reuse the site's existing admin authentication. **If none exists, stop and ask** — do not invent an auth scheme as part of this feature.

- List view of newsletters with status.
- Compose/edit: subject + Markdown body, htmx-driven live HTML preview.
- **Send test** — delivers only to the logged-in administrator, using the real broadcast stream and real headers, so the unsubscribe link and rendering can be verified against a real inbox before the congregation sees it.
- **Send** — requires a typed confirmation showing the recipient count. Irreversible. Refuse if the newsletter is not `draft`.

---

## 9. Implementation steps

Each step is one commit and must be testable before the next begins.

**Step 1 — schema and store.** Migration per §3. Store implementation and interface per §4. No HTTP.
*Tests:* table-driven case per row of §4.1 against a real Postgres in the compose file; expired-token rejection; unknown-token rejection; `ListSubscribed` excludes all non-`subscribed` statuses.

**Step 2 — signup form and handler.** Page, POST handler, all of §5. Creates `pending` rows and logs the confirmation URL instead of emailing.
*Tests:* honeypot returns success and writes nothing; sub-3-second and over-2-hour submissions rejected; forged HMAC rejected; identical response body across new/subscribed/unsubscribed addresses; per-IP limit trips.

**Step 3 — mailer.** Interface, Postmark implementation, fake. Wire the real confirmation email onto the transactional stream.
*Tests:* fake asserts recipient, stream, and that the link is absolute. Manual smoke test against Postmark.

**Step 4 — confirmation.** GET confirm page, POST confirm, mint unsubscribe token, welcome email, thank-you page.
*Tests:* GET does not mutate; expired token shows a re-signup path rather than a dead end; second POST with the same token fails cleanly.

**Step 5 — unsubscribe.** GET landing, POST action, one-click endpoint, headers per §7.4.
*Tests:* GET does not mutate; one-click POST succeeds without CSRF token and returns 200; unknown token does not leak whether the address exists.

**Step 6 — admin compose.** Auth check, list, editor, preview, send test.

**Step 7 — send.** Batch send per §7.5.
*Tests:* partial-failure response maps errors to the correct subscribers; re-running a partially-sent newsletter sends only `queued` rows.

**Step 8 — webhooks.** Per §7.6.

Steps 1–5 produce a working, safe, self-service list. 6–8 make it operable.

---

## 10. Pre-launch checklist

- [ ] Broadcast stream created in Postmark; ID matches `POSTMARK_STREAM_BROADCAST`
- [ ] Sending domain verified in Postmark (DKIM + custom Return-Path CNAME)
- [ ] DMARC published as `p=reject; adkim=s; aspf=r` — SPF alignment must be relaxed because Postmark's Return-Path is on a subdomain
- [ ] `TRUSTED_PROXY_COUNT=2` in the VPS `.env` — Cloudflare *and* Caddy sit in
      front, so `X-Forwarded-For` arrives as `<visitor>, <cloudflare-edge>`.
      At 1 the per-IP limit keys on a Cloudflare edge IP and every visitor
      shares one bucket. Verify after deploy by signing up and confirming the
      logged `signup_ip` is a real visitor address, not `172.71.x.x`.
- [ ] Cloudflare rate limiting rule created — Security rules → Rate limiting:
      match `(http.request.uri.path eq "/newsletter/subscribe" and http.request.method eq "POST")`,
      characteristic `IP`, 30 requests / 10 s, action Block, duration 10 s.
      Threshold is deliberately generous: Free-plan counting is raw IP with no
      NAT support, and a sanctuary on shared wifi presents as one address.
      Block rather than Managed Challenge — §5 rules out challenge widgets for
      this audience. Free allows one rule per zone and fixes both the period
      and the timeout at 10 s, so this stops floods only; the slow-drip case
      rests on the app-level limits and double opt-in.
- [ ] Webhook URL registered in Postmark with basic auth credentials
- [ ] Test send received in Gmail; unsubscribe link visible; "Unsubscribe" appears in Gmail's header UI (confirms one-click is parsed)
- [ ] QR code generated against the production URL and test-scanned from a phone at slide distance
- [ ] Administrator briefed that `complained` addresses are permanently excluded by design
