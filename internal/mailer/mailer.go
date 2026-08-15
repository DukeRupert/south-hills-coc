// Package mailer sends transactional and broadcast email. Nothing outside
// this package should import a Postmark type.
package mailer

import "context"

// Stream names a Postmark message stream. It must be set explicitly on every
// send: omitting it silently falls back to the transactional stream, which
// would put newsletters on the wrong infrastructure and the wrong reputation.
type Stream string

type Message struct {
	To       string
	Subject  string
	HTMLBody string
	TextBody string
	Stream   Stream
	// Headers carries per-recipient headers, notably the RFC 8058
	// List-Unsubscribe pair on broadcast messages.
	Headers map[string]string
	Tag     string
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
}
