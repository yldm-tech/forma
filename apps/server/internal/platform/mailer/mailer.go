// Package mailer sends forma2's transactional emails: SMTP through go-mail with forma2's environment variables and timeouts, bodies rendered from html/template inside one branded layout, subjects and copy from the same translation catalogue the web app uses.
package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// Message is one email. HTML is the complete document; From defaults to MAIL_FROM_NAME <MAIL_FROM>; ReplyTo is one address or a comma-separated list. MessageID, in its <id@domain> form, replaces the random Message-ID a send otherwise gets, so a message sent again for the same reason carries the same id. Headers are further header fields, written as given (OneClickUnsubscribe builds the List-Unsubscribe pair); they cannot replace the fields above.
type Message struct {
	To        string
	From      string
	ReplyTo   string
	Subject   string
	HTML      string
	MessageID string
	Headers   map[string]string
}

// OneClickUnsubscribe is the List-Unsubscribe and List-Unsubscribe-Post pair of RFC 8058 for an HTTPS unsubscribe URL that turns the subscription off when posted List-Unsubscribe=One-Click: mail clients then offer their own unsubscribe button. Nil without a URL.
func OneClickUnsubscribe(url string) map[string]string {
	if url == "" {
		return nil
	}
	return map[string]string{"List-Unsubscribe": "<" + url + ">", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}
}

// Transport delivers a message. The SMTP transport is the production one; mailertest.Transport captures messages for tests.
type Transport interface {
	Send(ctx context.Context, msg Message) error
}

type Mailer struct {
	transport Transport
	from      string
	renderer  *renderer
	log       *slog.Logger
}

// New builds the mailer from the SMTP settings. Without SMTP_HOST and SMTP_PORT it still renders, but every send is skipped and reported as not sent, which is how forma2 behaved on an instance with no mail server.
func New(cfg config.Config, log *slog.Logger) (*Mailer, error) {
	var t Transport
	if cfg.SMTPConfigured() {
		smtp, err := newSMTPTransport(cfg, defaultTimeouts)
		if err != nil {
			return nil, err
		}
		t = smtp
	}
	return NewWithTransport(cfg, t, log)
}

// NewWithTransport builds a mailer on the given transport; nil means mail is not configured.
func NewWithTransport(cfg config.Config, t Transport, log *slog.Logger) (*Mailer, error) {
	r, err := newRenderer(cfg)
	if err != nil {
		return nil, err
	}
	from := (&mail.Address{Name: cfg.MailFromName, Address: cfg.MailFrom}).String()
	return &Mailer{transport: t, from: from, renderer: r, log: log}, nil
}

// Configured reports whether sends reach a mail server.
func (m *Mailer) Configured() bool {
	return m.transport != nil
}

// Send delivers msg. It returns false without an error when mail is not configured, so a caller can tell "not sent" from "failed"; a delivery failure is logged and returned.
func (m *Mailer) Send(ctx context.Context, msg Message) (bool, error) {
	if m.transport == nil {
		m.log.InfoContext(ctx, "SMTP is not configured, skipping email sending")
		return false, nil
	}
	if msg.From == "" {
		msg.From = m.from
	}
	if err := m.transport.Send(ctx, msg); err != nil {
		m.log.ErrorContext(ctx, "send email failed", "err", err)
		return false, fmt.Errorf("failed to send email: %w", err)
	}
	return true, nil
}

// SendTemplate renders content in locale (falling back to en-US) and sends it to one recipient.
func (m *Mailer) SendTemplate(ctx context.Context, to, locale string, content Content) (bool, error) {
	subject, html, err := m.Render(locale, content)
	if err != nil {
		return false, err
	}
	return m.Send(ctx, Message{To: to, Subject: subject, HTML: html})
}

// Render returns the localized subject and HTML document for content.
func (m *Mailer) Render(locale string, content Content) (subject, html string, err error) {
	return m.renderer.render(locale, content)
}
