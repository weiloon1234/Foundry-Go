package email_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	smtpmail "github.com/weiloon1234/Foundry-Go/email/smtp"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// A submission that failed before any connection existed wrote no request
// bytes, so it is a known non-acceptance and safe to retry.
func TestHTTPConnectionFailureBeforeRequestIsTransient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + listener.Addr().String()
	_ = listener.Close()
	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			config := email.DefaultHTTPConfig()
			config.Endpoint = endpoint
			driver, err := provider.construct(config)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			m := mailer(t, driver, nil, nil)
			if _, err := m.Send(t.Context(), message(t), email.SendOptions{IdempotencyKey: "fixture-key"}); !errors.Is(err, email.Transient) {
				t.Fatal("refused connection was not retryable", err)
			}
		})
	}
}

type smtpSession struct {
	mu          sync.Mutex
	hello       []string
	auth        []string
	resets      int
	messages    int
	connections int
}

// smtpServer is a STARTTLS fixture advertising mechanisms. authReply overrides
// the final AUTH status. It serves any number of sequential connections.
func smtpServer(t *testing.T, mechanisms, authReply string) (string, *tls.Config, *smtpSession) {
	t.Helper()
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	serverTLS := seed.TLS.Clone()
	roots := x509.NewCertPool()
	roots.AddCert(seed.Certificate())
	seed.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	session := &smtpSession{}
	var group sync.WaitGroup
	group.Go(func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			session.mu.Lock()
			session.connections++
			session.mu.Unlock()
			group.Go(func() { serveSMTP(connection, serverTLS, mechanisms, authReply, session) })
		}
	})
	t.Cleanup(func() { _ = listener.Close(); group.Wait() })
	return listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, session
}

func serveSMTP(connection net.Conn, serverTLS *tls.Config, mechanisms, authReply string, session *smtpSession) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
	protocol := textproto.NewConn(connection)
	encrypted := false
	record := func(apply func()) { session.mu.Lock(); defer session.mu.Unlock(); apply() }
	_ = protocol.PrintfLine("220 fixture SMTP")
	for {
		line, err := protocol.ReadLine()
		if err != nil {
			return
		}
		verb, rest, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			record(func() { session.hello = append(session.hello, rest) })
			_ = protocol.PrintfLine("250-fixture")
			if !encrypted {
				_ = protocol.PrintfLine("250-STARTTLS")
			}
			_ = protocol.PrintfLine("250 AUTH %s", mechanisms)
		case "STARTTLS":
			_ = protocol.PrintfLine("220 Ready")
			secured := tls.Server(connection, serverTLS)
			if secured.Handshake() != nil {
				return
			}
			connection, encrypted = secured, true
			protocol = textproto.NewConn(connection)
		case "AUTH":
			mechanism, initial, _ := strings.Cut(rest, " ")
			observed := mechanism
			if strings.EqualFold(mechanism, "LOGIN") {
				_ = protocol.PrintfLine("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
				user, _ := protocol.ReadLine()
				_ = protocol.PrintfLine("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
				password, _ := protocol.ReadLine()
				decodedUser, _ := base64.StdEncoding.DecodeString(user)
				decodedPassword, _ := base64.StdEncoding.DecodeString(password)
				observed += " " + string(decodedUser) + " " + string(decodedPassword)
			} else if initial != "" {
				decoded, _ := base64.StdEncoding.DecodeString(initial)
				observed += " " + string(decoded)
			}
			record(func() { session.auth = append(session.auth, observed) })
			if authReply != "" {
				_ = protocol.PrintfLine("%s", authReply)
			} else {
				_ = protocol.PrintfLine("235 Authenticated")
			}
		case "RSET":
			record(func() { session.resets++ })
			_ = protocol.PrintfLine("250 Reset")
		case "MAIL", "RCPT", "NOOP":
			_ = protocol.PrintfLine("250 OK")
		case "DATA":
			_ = protocol.PrintfLine("354 Send data")
			if _, err := protocol.ReadDotBytes(); err != nil {
				return
			}
			record(func() { session.messages++ })
			_ = protocol.PrintfLine("250 Accepted")
		case "QUIT":
			_ = protocol.PrintfLine("221 Bye")
			return
		default:
			_ = protocol.PrintfLine("500 Unknown")
		}
	}
}

func TestSMTPAuthenticationMechanismsAndHelloName(t *testing.T) {
	for _, test := range []struct {
		mechanism smtpmail.Mechanism
		want      string
	}{
		{smtpmail.AuthPlain, "PLAIN \x00fixture\x00fixture-secret"},
		{smtpmail.AuthLogin, "LOGIN fixture fixture-secret"},
		{smtpmail.AuthXOAuth2, "XOAUTH2 user=fixture\x01auth=Bearer fixture-secret\x01\x01"},
	} {
		t.Run(string(test.mechanism), func(t *testing.T) {
			endpoint, tlsConfig, session := smtpServer(t, "PLAIN LOGIN XOAUTH2", "")
			driver, err := smtpmail.New(smtpmail.Config{Address: endpoint, Security: smtpmail.STARTTLS, TLSConfig: tlsConfig, Timeout: 2 * time.Second,
				Username: "fixture", Password: secret.New("fixture-secret"), Auth: test.mechanism, LocalName: "relay.example.test"})
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if result, err := mailer(t, driver, nil, nil).Send(t.Context(), message(t), email.SendOptions{}); err != nil || !result.Accepted {
				t.Fatal("SMTP did not accept", err)
			}
			session.mu.Lock()
			defer session.mu.Unlock()
			if len(session.auth) != 1 || session.auth[0] != test.want {
				t.Fatalf("authentication exchange: %q", session.auth)
			}
			for _, hello := range session.hello {
				if hello != "relay.example.test" {
					t.Fatal("configured EHLO name was not used", hello)
				}
			}
		})
	}
}

func TestSMTPDefaultHelloIsNeverLocalhost(t *testing.T) {
	endpoint, tlsConfig, session := smtpServer(t, "PLAIN", "")
	driver, err := smtpmail.New(smtpmail.Config{Address: endpoint, Security: smtpmail.STARTTLS, TLSConfig: tlsConfig, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if _, err := mailer(t, driver, nil, nil).Send(t.Context(), message(t), email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.hello) == 0 || strings.EqualFold(session.hello[0], "localhost") || session.hello[0] == "" {
		t.Fatalf("default EHLO identity: %q", session.hello)
	}
}

func TestSMTPUnsupportedMechanismIsPermanent(t *testing.T) {
	for _, test := range []struct{ name, mechanisms, reply string }{
		{"not-advertised", "PLAIN", ""},
		{"504", "PLAIN LOGIN XOAUTH2", "504 5.7.4 Unrecognized authentication type"},
		{"535", "PLAIN LOGIN XOAUTH2", "535 5.7.8 Authentication credentials invalid"},
		{"454", "PLAIN LOGIN XOAUTH2", "454 4.7.0 Temporary authentication failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint, tlsConfig, _ := smtpServer(t, test.mechanisms, test.reply)
			driver, err := smtpmail.New(smtpmail.Config{Address: endpoint, Security: smtpmail.STARTTLS, TLSConfig: tlsConfig, Timeout: 2 * time.Second,
				Username: "fixture", Password: secret.New("fixture-secret"), Auth: smtpmail.AuthXOAuth2})
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			want := email.Permanent
			if test.name == "454" {
				want = email.Transient
			}
			if _, err := mailer(t, driver, nil, nil).Send(t.Context(), message(t), email.SendOptions{}); !errors.Is(err, want) {
				t.Fatal("wrong authentication failure class", err)
			}
		})
	}
}

func TestSMTPPoolReusesIdleSessionWithReset(t *testing.T) {
	endpoint, tlsConfig, session := smtpServer(t, "PLAIN", "")
	driver, err := smtpmail.New(smtpmail.Config{Address: endpoint, Security: smtpmail.STARTTLS, TLSConfig: tlsConfig, Timeout: 2 * time.Second,
		Username: "fixture", Password: secret.New("fixture-secret"), MaxIdle: 1, IdleTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	m := mailer(t, driver, nil, nil)
	for range 3 {
		if result, err := m.Send(context.Background(), message(t), email.SendOptions{}); err != nil || !result.Accepted {
			t.Fatal(err)
		}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.connections != 1 || session.messages != 3 || session.resets != 2 || len(session.auth) != 1 {
		t.Fatalf("pooled session was not reused: connections=%d messages=%d resets=%d auth=%d", session.connections, session.messages, session.resets, len(session.auth))
	}
}

var messageIDHeader = regexp.MustCompile(`(?m)^Message-ID: (<[^>]+>)\r$`)

func TestMessageIDIsStableForDeliveryIdentityAndUsesConfiguredDomain(t *testing.T) {
	driver, err := memory.New(16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	config := email.DefaultConfig()
	config.MessageIDDomain = "mail.example.test"
	m, err := email.New(driver, nil, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	var ids []string
	for _, key := range []email.IdempotencyKey{"email/job-1", "email/job-1", ""} {
		if _, err := m.Send(t.Context(), message(t), email.SendOptions{IdempotencyKey: key}); err != nil {
			t.Fatal(err)
		}
		sent := driver.Messages()
		match := messageIDHeader.FindSubmatch(sent[len(sent)-1].MIME())
		if match == nil {
			t.Fatal("Message-ID header missing")
		}
		ids = append(ids, string(match[1]))
	}
	if ids[0] != ids[1] || ids[0] == ids[2] || !strings.HasSuffix(ids[0], "@mail.example.test>") {
		t.Fatalf("Message-ID identity: %q", ids)
	}
	config.MessageIDDomain = "bad domain"
	if _, err := email.New(driver, nil, config, nil); err == nil {
		t.Fatal("invalid Message-ID domain accepted")
	}
}
