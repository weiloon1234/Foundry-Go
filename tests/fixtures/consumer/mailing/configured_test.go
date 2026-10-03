package mailing_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

const transactional email.MailerName = "transactional"

func TestConfiguredMailProvidersAndFailover(t *testing.T) {
	var cloudflareStatus atomic.Int32
	cloudflareStatus.Store(http.StatusOK)
	var primaryCalls, backupCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Error("wrong provider method")
		}
		switch r.URL.Path {
		case "/accounts/0123456789abcdef0123456789abcdef/email/sending/send_raw":
			primaryCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-primary" {
				t.Error("Cloudflare token not loaded from environment")
			}
			w.WriteHeader(int(cloudflareStatus.Load()))
			_, _ = io.WriteString(w, `{"success":true,"result":{"message_id":"cloudflare-id"}}`)
		case "/emails":
			backupCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-backup" {
				t.Error("Resend token crossed configured providers")
			}
			_, _ = io.WriteString(w, `{"id":"resend-id"}`)
		default:
			t.Error("unexpected provider endpoint")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"APP__SERVICES__MAIL__MAILERS__TRANSACTIONAL__API__TOKEN": "fixture-primary",
		"APP__SERVICES__MAIL__MAILERS__BACKUP__API__TOKEN":        "fixture-backup",
	}
	settings, report, err := toml.LoadFile("testdata/mailers.toml", schema, application.DefaultSettings(), config.Inputs[application.Settings]{
		Prefix:      "APP",
		Environment: func(key string) (string, bool) { value, ok := env[key]; return value, ok },
		Environ: func() []string {
			var entries []string
			for key, value := range env {
				entries = append(entries, key+"="+value)
			}
			return entries
		},
	}, toml.Options{})
	if err != nil {
		t.Fatal(err)
	}
	keys := infrastructure.MailerSettingsConfigKeys()
	primary := settings.Services.Mail.Mailers[transactional]
	if primary.Driver != infrastructure.CloudflareMail || primary.API.AccountID != "0123456789abcdef0123456789abcdef" || keys.API.AccountID.Name() != "api.account_id" {
		t.Fatal("generated mail configuration lost provider settings")
	}
	for _, entry := range report.Entries() {
		if strings.Contains(entry.Name, "mailers") && !entry.Secret {
			t.Fatal("mail credential provenance is not redacted")
		}
	}
	for name, mailer := range settings.Services.Mail.Mailers {
		mailer.API.Endpoint = server.URL
		settings.Services.Mail.Mailers[name] = mailer
	}
	app, err := application.New(settings, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if primaryCalls.Load() != 0 || backupCalls.Load() != 0 {
		t.Fatal("building configured mailers performed network I/O")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services := app.Resources()
	mailer, err := services.Mailers.Mailer(transactional)
	if err != nil {
		t.Fatal(err)
	}
	defaultMailer, err := services.Mailers.Default()
	if err != nil || defaultMailer != mailer {
		t.Fatal("default created a second mailer", err)
	}
	if _, err := services.Mailers.Mailer("missing"); err == nil {
		t.Fatal("unknown mailer silently selected default")
	}
	recipient, err := email.ParseAddress("reader@example.test")
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[email.MailerName]string{transactional: "cloudflare-id", "backup": "resend-id", "delivery": "cloudflare-id"} {
		selected, err := services.Mailers.Mailer(name)
		if err != nil {
			t.Fatal(err)
		}
		message := selected.Message("Welcome", recipient).Text("Hello")
		if message.From().Mailbox() != "hello@example.test" {
			t.Fatal("configured sender was lost")
		}
		result, err := selected.Send(t.Context(), message, email.SendOptions{})
		if err != nil || !result.Accepted || result.Receipt.MessageID != id {
			t.Fatal("configured send failed", err)
		}
	}
	delivery, err := services.Mailers.Mailer("delivery")
	if err != nil {
		t.Fatal(err)
	}
	cloudflareStatus.Store(http.StatusTooManyRequests)
	result, err := delivery.Send(t.Context(), delivery.Message("Welcome", recipient).Text("Hello"), email.SendOptions{})
	if err != nil || !result.Accepted || result.Receipt.MessageID != "resend-id" {
		t.Fatal("transient failure did not reach backup", err)
	}
	cloudflareStatus.Store(http.StatusInternalServerError)
	before := backupCalls.Load()
	result, err = delivery.Send(t.Context(), delivery.Message("Welcome", recipient).Text("Hello"), email.SendOptions{})
	if !errors.Is(err, email.Ambiguous) || result.Accepted || backupCalls.Load() != before {
		t.Fatal("ambiguous submission was retried", err)
	}
}
