package email_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/email"
	smtpmail "github.com/weiloon1234/Foundry-Go/email/smtp"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type smtpObservation struct {
	recipients int
	data       []byte
	auth       bool
}

func smtpPeer(t *testing.T, mode string, security smtpmail.Security) (string, *tls.Config, <-chan smtpObservation) {
	t.Helper()
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	serverTLS := seed.TLS.Clone()
	certificate := seed.Certificate()
	seed.Close()
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	observations := make(chan smtpObservation, 1)
	done := make(chan struct{})
	var mu sync.Mutex
	var active net.Conn
	go func() {
		defer close(done)
		var observed smtpObservation
		defer func() { observations <- observed }()
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		mu.Lock()
		active = connection
		mu.Unlock()
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		if security == smtpmail.TLS {
			encrypted := tls.Server(connection, serverTLS)
			if encrypted.Handshake() != nil {
				return
			}
			connection = encrypted
		}
		protocol := textproto.NewConn(connection)
		_ = protocol.PrintfLine("220 fixture SMTP")
		for {
			line, err := protocol.ReadLine()
			if err != nil {
				return
			}
			verb, _, _ := strings.Cut(line, " ")
			switch verb {
			case "EHLO", "HELO":
				_ = protocol.PrintfLine("250-fixture")
				if security == smtpmail.STARTTLS && mode != "no-starttls" {
					_ = protocol.PrintfLine("250-STARTTLS")
				}
				_ = protocol.PrintfLine("250 AUTH PLAIN")
			case "STARTTLS":
				_ = protocol.PrintfLine("220 Ready")
				encrypted := tls.Server(connection, serverTLS)
				if encrypted.Handshake() != nil {
					return
				}
				connection = encrypted
				protocol = textproto.NewConn(connection)
			case "AUTH":
				observed.auth = true
				_ = protocol.PrintfLine("235 Authenticated")
			case "MAIL":
				_ = protocol.PrintfLine("250 Sender accepted")
			case "RCPT":
				observed.recipients++
				if observed.recipients == 2 && mode == "recipient-reject" {
					_ = protocol.PrintfLine("550 Rejected")
				} else if mode == "recipient-temporary" {
					_ = protocol.PrintfLine("450 Later")
				} else {
					_ = protocol.PrintfLine("250 Recipient accepted")
				}
			case "DATA":
				_ = protocol.PrintfLine("354 Send data")
				observed.data, err = protocol.ReadDotBytes()
				if err != nil {
					return
				}
				switch mode {
				case "lost-reply":
					return
				case "cancel":
					_, _ = protocol.ReadLine()
					return
				case "data-temporary":
					_ = protocol.PrintfLine("451 Not accepted")
				default:
					_ = protocol.PrintfLine("250 Accepted")
				}
				return
			case "QUIT":
				_ = protocol.PrintfLine("221 Bye")
				return
			default:
				_ = protocol.PrintfLine("500 Unknown")
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		if active != nil {
			_ = active.Close()
		}
		mu.Unlock()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SMTP fixture did not exit")
		}
	})
	return listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, observations
}
func TestSMTPProtocolSecurityEnvelopeAndOutcomes(t *testing.T) {
	for _, test := range []struct {
		mode     string
		security smtpmail.Security
		want     email.Kind
	}{{"accepted", smtpmail.PlainLoopback, ""}, {"tls", smtpmail.TLS, ""}, {"starttls", smtpmail.STARTTLS, ""}, {"no-starttls", smtpmail.STARTTLS, email.Permanent}, {"recipient-reject", smtpmail.PlainLoopback, email.Permanent}, {"recipient-temporary", smtpmail.PlainLoopback, email.Transient}, {"lost-reply", smtpmail.PlainLoopback, email.Ambiguous}, {"data-temporary", smtpmail.PlainLoopback, email.Transient}, {"cancel", smtpmail.PlainLoopback, email.Ambiguous}} {
		t.Run(test.mode, func(t *testing.T) {
			endpoint, tlsConfig, observations := smtpPeer(t, test.mode, test.security)
			config := smtpmail.Config{Address: endpoint, Security: test.security, TLSConfig: tlsConfig, Timeout: 2 * time.Second}
			if test.security != smtpmail.PlainLoopback {
				config.Username = "fixture"
				config.Password = secret.New("fixture-password")
			}
			driver, err := smtpmail.New(config)
			if err != nil {
				t.Fatal(err)
			}
			m := mailer(t, driver, nil, nil)
			ctx := t.Context()
			if test.mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
			}
			result, err := m.Send(ctx, message(t).Bcc(address(t, "hidden@example.test")), email.SendOptions{})
			if test.want == "" {
				if err != nil || !result.Accepted {
					t.Fatal("SMTP did not accept", err)
				}
			} else if !errors.Is(err, test.want) || result.Accepted {
				t.Fatal("wrong SMTP outcome", err)
			}
			observed := <-observations
			if test.mode == "recipient-reject" || test.mode == "recipient-temporary" {
				if len(observed.data) > 0 {
					t.Fatal("DATA sent after partial recipient rejection")
				}
			}
			if result.Accepted {
				if observed.recipients != 2 || len(observed.data) == 0 || bytes.Contains(observed.data, []byte("hidden@example.test")) {
					t.Fatal("SMTP envelope/BCC incorrect")
				}
				if test.security != smtpmail.PlainLoopback && !observed.auth {
					t.Fatal("SMTP authentication missing")
				}
			}
		})
	}
}
func TestSMTPRejectsUnsafeConfiguration(t *testing.T) {
	for _, config := range []smtpmail.Config{{Address: "mail.example.test:25", Security: smtpmail.PlainLoopback, Timeout: time.Second}, {Address: "127.0.0.1:25", Security: smtpmail.PlainLoopback, Username: "user", Password: secret.New("password"), Timeout: time.Second}, {Address: "mail.example.test:465", Security: smtpmail.TLS, TLSConfig: &tls.Config{InsecureSkipVerify: true}, Timeout: time.Second}, {Address: "mail.example.test:0", Security: smtpmail.TLS, Timeout: time.Second}} {
		if _, err := smtpmail.New(config); err == nil {
			t.Fatal("unsafe SMTP configuration accepted")
		}
	}
}
