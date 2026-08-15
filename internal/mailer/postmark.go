package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const postmarkEndpoint = "https://api.postmarkapp.com/email"

// ErrNotConfigured is returned when required Postmark settings are missing.
var ErrNotConfigured = errors.New("mailer: postmark not configured")

// Postmark is the production Mailer.
type Postmark struct {
	Token     string
	FromName  string
	FromEmail string
	Client    *http.Client
	// Endpoint overrides the API URL. Only set in tests.
	Endpoint string
}

func NewPostmark(token, fromName, fromEmail string) *Postmark {
	return &Postmark{
		Token:     token,
		FromName:  fromName,
		FromEmail: fromEmail,
		Client:    &http.Client{Timeout: 15 * time.Second},
	}
}

type postmarkMessage struct {
	From          string           `json:"From"`
	To            string           `json:"To"`
	Subject       string           `json:"Subject"`
	HtmlBody      string           `json:"HtmlBody"`
	TextBody      string           `json:"TextBody"`
	MessageStream string           `json:"MessageStream"`
	Headers       []postmarkHeader `json:"Headers,omitempty"`
	Tag           string           `json:"Tag,omitempty"`
}

type postmarkHeader struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

func (p *Postmark) Send(ctx context.Context, m Message) error {
	if p.Token == "" || p.FromEmail == "" {
		return ErrNotConfigured
	}
	if m.Stream == "" {
		return errors.New("mailer: message stream must be set explicitly")
	}

	from := p.FromEmail
	if p.FromName != "" {
		from = fmt.Sprintf("%s <%s>", p.FromName, p.FromEmail)
	}

	pm := postmarkMessage{
		From:          from,
		To:            m.To,
		Subject:       m.Subject,
		HtmlBody:      m.HTMLBody,
		TextBody:      m.TextBody,
		MessageStream: string(m.Stream),
		Tag:           m.Tag,
	}
	for name, value := range m.Headers {
		pm.Headers = append(pm.Headers, postmarkHeader{Name: name, Value: value})
	}

	body, err := json.Marshal(pm)
	if err != nil {
		return err
	}

	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = postmarkEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.Token)

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mailer: postmark returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
}
