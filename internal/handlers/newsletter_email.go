package handlers

import (
	"context"
	"fmt"
	"html"
	"net/url"

	"github.com/dukerupert/south-hills-coc/internal/mailer"
	"github.com/dukerupert/south-hills-coc/internal/newsletter"
)

// newsletterURL builds an absolute link from SITE_BASE_URL. Links in email
// must never be relative.
func (h *Handler) newsletterURL(path, token string) string {
	u := h.Config.SiteBaseURL + path
	if token != "" {
		u += "?token=" + url.QueryEscape(token)
	}
	return u
}

// ListUnsubscribeHeaders returns the RFC 8058 pair required of bulk senders by
// Gmail and Yahoo. Both headers are required together — one without the other
// does not satisfy the RFC. The token is per-subscriber, so these are built per
// message, not once per batch.
func (h *Handler) ListUnsubscribeHeaders(unsubToken string) map[string]string {
	return map[string]string{
		"List-Unsubscribe":      "<" + h.newsletterURL("/newsletter/unsubscribe/one-click", unsubToken) + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
}

func (h *Handler) sendConfirmationEmail(ctx context.Context, email, token string) error {
	link := h.newsletterURL("/newsletter/confirm", token)
	site := h.Config.Title

	text := fmt.Sprintf(`Confirm your subscription

Someone (we hope it was you) asked to receive the weekly newsletter from
%s in Helena, Montana.

Open this link to confirm:

%s

The link expires in 48 hours. If you didn't ask for this, you don't need to
do anything at all — we won't email you again.

%s
%s
`, site, link, site, h.Config.Address)

	body := fmt.Sprintf(`
<p style="margin:0 0 16px">Someone (we hope it was you) asked to receive the weekly newsletter from %s in Helena, Montana.</p>
<p style="margin:0 0 24px"><a href="%s" style="display:inline-block;padding:12px 22px;background:#35413D;color:#FFDAD1;text-decoration:none;border-radius:3px;font-size:12px;letter-spacing:0.08em;text-transform:uppercase">Confirm subscription</a></p>
<p style="margin:0 0 16px;font-size:13px">Or paste this into your browser:<br><a href="%s" style="color:#975849">%s</a></p>
<p style="margin:0 0 16px;font-size:13px">The link expires in 48 hours.</p>
<p style="margin:0;font-size:13px">If you didn't ask for this, you don't need to do anything at all — we won't email you again.</p>`,
		html.EscapeString(site), html.EscapeString(link), html.EscapeString(link), html.EscapeString(link))

	return h.newsletterMailer.Send(ctx, mailer.Message{
		To:       email,
		Subject:  "Confirm your " + site + " newsletter subscription",
		TextBody: text,
		HTMLBody: h.emailShell("Confirm your subscription", body, ""),
		Stream:   mailer.Stream(h.Config.StreamTransactional),
		Tag:      "newsletter-confirm",
	})
}

func (h *Handler) sendWelcomeEmail(ctx context.Context, sub newsletter.Subscriber) error {
	unsubLink := h.newsletterURL("/newsletter/unsubscribe", sub.UnsubscribeToken)
	site := h.Config.Title

	text := fmt.Sprintf(`You're subscribed

Thanks for confirming. You'll get the weekly newsletter from %s —
service times, what's coming up, and news from the congregation.

Sunday Bible Class: %s
Sunday Worship: %s
%s

You can unsubscribe at any time: %s
`, site, h.Config.BibleClass, h.Config.Worship, h.Config.Address, unsubLink)

	body := fmt.Sprintf(`
<p style="margin:0 0 16px">Thanks for confirming. You'll get the weekly newsletter from %s — service times, what's coming up, and news from the congregation.</p>
<p style="margin:0 0 8px;font-size:13px"><strong>Sunday Bible Class:</strong> %s</p>
<p style="margin:0 0 8px;font-size:13px"><strong>Sunday Worship:</strong> %s</p>
<p style="margin:0;font-size:13px">%s</p>`,
		html.EscapeString(site), html.EscapeString(h.Config.BibleClass),
		html.EscapeString(h.Config.Worship), html.EscapeString(h.Config.Address))

	return h.newsletterMailer.Send(ctx, mailer.Message{
		To:       sub.Email,
		Subject:  "You're subscribed to the " + site + " newsletter",
		TextBody: text,
		HTMLBody: h.emailShell("You're subscribed", body, unsubLink),
		Stream:   mailer.Stream(h.Config.StreamTransactional),
		Tag:      "newsletter-welcome",
	})
}

// emailShell wraps body HTML in the shared email layout. unsubLink, when set,
// renders the visible unsubscribe link — Postmark requires one on broadcast
// messages and will inject its own if it is absent.
func (h *Handler) emailShell(heading, body, unsubLink string) string {
	footer := fmt.Sprintf(
		`<p style="margin:0 0 4px">%s</p><p style="margin:0 0 4px">%s</p><p style="margin:0"><a href="%s" style="color:#975849">%s</a></p>`,
		html.EscapeString(h.Config.Title),
		html.EscapeString(h.Config.Address),
		html.EscapeString(h.Config.SiteBaseURL),
		html.EscapeString(h.Config.SiteBaseURL),
	)
	if unsubLink != "" {
		footer += fmt.Sprintf(
			`<p style="margin:12px 0 0"><a href="%s" style="color:#57443F">Unsubscribe from this newsletter</a></p>`,
			html.EscapeString(unsubLink))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title></head>
<body style="margin:0;padding:0;background:#F4F0EB">
<div style="max-width:560px;margin:0 auto;padding:32px 24px;font-family:'Jost',Helvetica,Arial,sans-serif;font-weight:300;font-size:14px;line-height:1.8;color:#57443F">
  <h1 style="margin:0 0 24px;font-family:'Cormorant Garamond',Georgia,serif;font-weight:300;font-size:32px;line-height:1.1;color:#35413D">%s</h1>
  %s
  <hr style="margin:32px 0 16px;border:0;border-top:1px solid #EAE5DE">
  <div style="font-size:11px;letter-spacing:0.04em;color:#57443F">%s</div>
</div>
</body></html>`, html.EscapeString(heading), html.EscapeString(heading), body, footer)
}
