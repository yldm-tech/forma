package mailer

import (
	"bytes"
	"context"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// A relay that accepts the connection and never greets must fail the send within the socket timeout instead of holding the caller (forma2 bounded nodemailer for the same reason).
func TestSMTPStalledServerTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	tr, err := newSMTPTransport(config.Config{SMTPHost: host, SMTPPort: port, SMTPRejectUnauthorizedTLS: true}, timeouts{connect: time.Second, socket: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = tr.Send(context.Background(), Message{From: "noreply@example.com", To: "user@example.com", Subject: "s", HTML: "<p>h</p>"})
	if err == nil {
		t.Fatal("send to a silent server succeeded")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("send took %v; the socket timeout did not apply", elapsed)
	}
}

func TestSMTPTransportRejectsAMalformedPort(t *testing.T) {
	if _, err := newSMTPTransport(config.Config{SMTPHost: "smtp.example.com", SMTPPort: "smtp"}, defaultTimeouts); err == nil {
		t.Fatal("accepted a non-numeric port")
	}
}

// A message with a MessageID carries exactly that Message-ID, so a workflow step sent again is recognizable as the same message; one without gets a random id.
func TestMessageIDHeader(t *testing.T) {
	header := func(msg Message) string {
		t.Helper()
		m, err := newMsg(msg)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := m.WriteTo(&buf); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(buf.String(), "\r\n") {
			if strings.HasPrefix(line, "Message-ID: ") {
				return strings.TrimPrefix(line, "Message-ID: ")
			}
		}
		t.Fatalf("no Message-ID in %s", buf.String())
		return ""
	}
	base := Message{From: "noreply@example.com", To: "user@example.com", Subject: "s", HTML: "<p>h</p>"}
	fixed := base
	fixed.MessageID = "<0123456789abcdef0123456789abcdef@example.com>"
	if got := header(fixed); got != fixed.MessageID {
		t.Fatalf("Message-ID %q, want %q", got, fixed.MessageID)
	}
	if a, b := header(base), header(base); a == b || !strings.HasPrefix(a, "<") {
		t.Fatalf("random Message-IDs %q and %q", a, b)
	}
}

// A notification carries RFC 8058's one-click pair, the URL whole however long the token (folded onto its own line, never split), so mail clients offer their own unsubscribe button; extra headers never replace the message's own fields.
func TestOneClickUnsubscribeHeaders(t *testing.T) {
	url := "https://app.example.com/api/unsubscribe?token=" + strings.Repeat("a", 300)
	headers := OneClickUnsubscribe(url)
	headers["Subject"] = "spoofed"
	m, err := newMsg(Message{From: "noreply@example.com", To: "user@example.com", Subject: "s", HTML: "<p>h</p>", Headers: headers})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"List-Unsubscribe": "<" + url + ">", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click", "Subject": "s"} {
		if got := parsed.Header[name]; len(got) != 1 || got[0] != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	if OneClickUnsubscribe("") != nil {
		t.Error("headers without a URL")
	}
}
