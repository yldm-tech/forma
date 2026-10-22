package mailer

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/config"
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
