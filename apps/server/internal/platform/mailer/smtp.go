package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// timeouts bound one delivery. Without them a relay that accepts the connection and then stalls holds the request, or a worker slot, indefinitely.
type timeouts struct {
	// connect covers the TCP (and implicit TLS) handshake.
	connect time.Duration
	// socket is the deadline for the greeting and for each command after it.
	socket time.Duration
}

// defaultTimeouts are forma2's (nodemailer connectionTimeout and greetingTimeout 10 s, socketTimeout 20 s).
var defaultTimeouts = timeouts{connect: 10 * time.Second, socket: 20 * time.Second}

type smtpTransport struct {
	client   *gomail.Client
	timeouts timeouts
}

func newSMTPTransport(cfg config.Config, t timeouts) (*smtpTransport, error) {
	port, err := strconv.Atoi(cfg.SMTPPort)
	if err != nil {
		return nil, fmt.Errorf("SMTP_PORT: %w", err)
	}
	opts := []gomail.Option{
		gomail.WithPort(port),
		gomail.WithTimeout(t.socket),
		gomail.WithTLSConfig(&tls.Config{
			ServerName: cfg.SMTPHost,
			MinVersion: tls.VersionTLS12,
			// SMTP_REJECT_UNAUTHORIZED_TLS=0 is the operator's explicit opt-in for a relay with a self-signed certificate, as in forma2.
			InsecureSkipVerify: !cfg.SMTPRejectUnauthorizedTLS, //nolint:gosec // operator opt-in, see above
		}),
	}
	// Port 465 is implicit TLS whatever SMTP_SECURE_ENABLED says; any other port upgrades with STARTTLS when the server offers it.
	if cfg.SMTPSecureEnabled || port == 465 {
		opts = append(opts, gomail.WithSSL())
	} else {
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSOpportunistic))
	}
	// LOGIN, as forma2 used. go-mail only sends it over an encrypted connection, so credentials never cross the network in the clear.
	if cfg.SMTPAuthenticated && cfg.SMTPUser != "" {
		opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthLogin), gomail.WithUsername(cfg.SMTPUser), gomail.WithPassword(cfg.SMTPPassword))
	}
	client, err := gomail.NewClient(cfg.SMTPHost, opts...)
	if err != nil {
		return nil, fmt.Errorf("configure SMTP client: %w", err)
	}
	return &smtpTransport{client: client, timeouts: t}, nil
}

func (s *smtpTransport) Send(ctx context.Context, msg Message) error {
	m, err := newMsg(msg)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, s.timeouts.connect)
	defer cancel()
	return s.client.DialAndSendWithContext(dialCtx, m)
}

// newMsg builds the MIME message of msg.
func newMsg(msg Message) (*gomail.Msg, error) {
	m := gomail.NewMsg()
	if err := m.From(msg.From); err != nil {
		return nil, fmt.Errorf("from address: %w", err)
	}
	if err := m.To(msg.To); err != nil {
		return nil, fmt.Errorf("recipient address: %w", err)
	}
	if msg.ReplyTo != "" {
		// A follow-up replies to every address its author listed, joined with ", " as forma2 joined them.
		addrs, err := mail.ParseAddressList(msg.ReplyTo)
		if err != nil {
			return nil, fmt.Errorf("reply-to address: %w", err)
		}
		m.ReplyToMailAddress(addrs...)
	}
	m.Subject(msg.Subject)
	m.SetDate()
	if msg.MessageID != "" {
		m.SetMessageIDWithValue(strings.TrimSuffix(strings.TrimPrefix(msg.MessageID, "<"), ">"))
	} else {
		m.SetMessageID()
	}
	m.SetBodyString(gomail.TypeTextHTML, msg.HTML)
	return m, nil
}
