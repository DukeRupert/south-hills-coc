package mailer

import (
	"context"
	"log"
	"sync"
)

// Fake records messages instead of sending them. Handler tests use this.
type Fake struct {
	mu   sync.Mutex
	sent []Message
	// Err, if set, is returned by every Send.
	Err error
}

func (f *Fake) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.sent = append(f.sent, m)
	return nil
}

// Sent returns a copy of everything sent so far.
func (f *Fake) Sent() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Message, len(f.sent))
	copy(out, f.sent)
	return out
}

// Last returns the most recent message and whether one exists.
func (f *Fake) Last() (Message, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return Message{}, false
	}
	return f.sent[len(f.sent)-1], true
}

func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
}

// Logger writes messages to the standard logger instead of sending them. It is
// the development fallback when Postmark credentials are absent, so the
// confirmation link can be copied out of the server log.
type Logger struct{}

func (Logger) Send(_ context.Context, m Message) error {
	log.Printf("[mailer] would send to %s on stream %q\n  subject: %s\n%s",
		m.To, m.Stream, m.Subject, m.TextBody)
	return nil
}
