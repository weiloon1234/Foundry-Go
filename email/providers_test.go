package email_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/mailgun"
	"github.com/weiloon1234/Foundry-Go/email/postmark"
	"github.com/weiloon1234/Foundry-Go/email/resend"
	"github.com/weiloon1234/Foundry-Go/email/ses"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type apiDriver interface {
	email.Driver
	Close()
}
type providerCase struct {
	name, path, success string
	construct           func(email.HTTPConfig) (apiDriver, error)
}

func providers() []providerCase {
	return []providerCase{
		{"resend", "/emails", `{"id":"provider-id"}`, func(c email.HTTPConfig) (apiDriver, error) {
			return resend.New(resend.Config{HTTP: c, Token: secret.New("fixture-token")})
		}},
		{"postmark", "/email", `{"MessageID":"provider-id","ErrorCode":0}`, func(c email.HTTPConfig) (apiDriver, error) {
			return postmark.New(postmark.Config{HTTP: c, ServerToken: secret.New("fixture-token")})
		}},
		{"mailgun", "/v3/mail.example.test/messages", `{"id":"provider-id"}`, func(c email.HTTPConfig) (apiDriver, error) {
			return mailgun.New(mailgun.Config{HTTP: c, APIKey: secret.New("fixture-token"), Domain: "mail.example.test"})
		}},
		{"ses", "/", `<SendRawEmailResponse><SendRawEmailResult><MessageId>provider-id</MessageId></SendRawEmailResult></SendRawEmailResponse>`, func(c email.HTTPConfig) (apiDriver, error) {
			return ses.New((ses.Config{HTTP: c, Region: "us-east-1"}).WithCredentials(credentials.ProviderFunc(func(context.Context) (credentials.Value, error) {
				return credentials.Value{AccessKey: secret.New("fixture-access"), SecretKey: secret.New("fixture-secret"), SessionToken: secret.New("fixture-session")}, nil
			})))
		}},
	}
}
func TestProviderRequestContracts(t *testing.T) {
	registry, attachment := attachmentStore(t)
	attachment.ContentID = "inline-logo"
	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != "POST" || r.URL.Path != provider.path {
					t.Error("wrong API operation")
				}
				switch provider.name {
				case "resend":
					if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("Idempotency-Key") != "fixture-key" {
						t.Error("missing authentication or stable key")
					}
					var body struct {
						From                 string
						To, CC, BCC, ReplyTo []string
						Headers              map[string]string
						Attachments          []struct {
							Content, Filename string
							ContentID         string `json:"content_id"`
						}
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.To) != 1 || len(body.BCC) != 1 || body.Headers["X-Custom"] != "original" || len(body.Attachments) != 1 || body.Attachments[0].ContentID != "inline-logo" {
						t.Error("invalid Resend envelope")
					}
					if len(body.Attachments) == 1 {
						data, err := base64.StdEncoding.DecodeString(body.Attachments[0].Content)
						if err != nil || string(data) != "private attachment" {
							t.Error("invalid Resend attachment")
						}
					}
				case "postmark":
					if r.Header.Get("X-Postmark-Server-Token") != "fixture-token" {
						t.Error("missing Postmark authentication")
					}
					var body struct {
						To, Bcc     string
						Headers     []struct{ Name, Value string }
						Attachments []struct{ Content, ContentID string }
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.Bcc == "" || len(body.Headers) != 1 || body.Headers[0].Name != "X-Custom" || len(body.Attachments) != 1 || body.Attachments[0].ContentID != "cid:inline-logo" {
						t.Error("invalid Postmark contract")
					}
				case "mailgun":
					user, pass, ok := r.BasicAuth()
					if !ok || user != "api" || pass != "fixture-token" {
						t.Error("missing Mailgun authentication")
					}
					if r.ParseMultipartForm(1<<20) != nil {
						t.Error("invalid Mailgun multipart")
						return
					}
					defer r.MultipartForm.RemoveAll()
					if r.FormValue("bcc") == "" || r.FormValue("h:X-Custom") != "original" || len(r.MultipartForm.File["inline"]) != 1 || r.MultipartForm.File["inline"][0].Filename != "inline-logo" {
						t.Error("invalid Mailgun envelope/inline contract")
					}
				case "ses":
					if !strings.Contains(r.Header.Get("Authorization"), "/us-east-1/ses/aws4_request") || r.Header.Get("X-Amz-Security-Token") != "fixture-session" {
						t.Error("SES shared SDK signature missing")
					}
					if r.ParseForm() != nil || r.FormValue("Action") != "SendRawEmail" || r.FormValue("Version") != "2010-12-01" || r.FormValue("Destinations.member.2") != "hidden@example.test" {
						t.Error("invalid SES action/envelope")
					}
					wire, err := base64.StdEncoding.DecodeString(r.FormValue("RawMessage.Data"))
					if err != nil || strings.Contains(string(wire), "hidden@example.test") || !strings.Contains(strings.ToLower(string(wire)), "content-id: <inline-logo>") {
						t.Error("invalid SES MIME")
					}
				}
				_, _ = io.WriteString(w, provider.success)
			}))
			defer server.Close()
			config := email.DefaultHTTPConfig()
			config.Endpoint = server.URL
			driver, err := provider.construct(config)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			m := mailer(t, driver, registry, nil)
			msg := message(t).HTML(`<img src="cid:inline-logo">`).Bcc(address(t, "hidden@example.test")).Header("X-Custom", "original").Attach(attachment)
			result, err := m.Send(t.Context(), msg, email.SendOptions{IdempotencyKey: "fixture-key"})
			if err != nil || !result.Accepted || result.Receipt.MessageID != "provider-id" || requests.Load() != 1 {
				t.Fatal("provider did not accept one request", err)
			}
		})
	}
}
func TestProviderStatusAndResponseBounds(t *testing.T) {
	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			for _, test := range []struct {
				name   string
				status int
				body   string
				want   email.Kind
			}{{"rejection", 400, `{"private":"reset-link"}`, email.Permanent}, {"rate", 429, "", email.Transient}, {"server", 503, "private body", email.Ambiguous}, {"malformed-success", 200, "invalid", email.Ambiguous}, {"missing-receipt", 200, "{}", email.Ambiguous}, {"oversize", 200, strings.Repeat("x", (64<<10)+1), email.Ambiguous}} {
				t.Run(test.name, func(t *testing.T) {
					var count atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						count.Add(1)
						w.WriteHeader(test.status)
						_, _ = io.WriteString(w, test.body)
					}))
					defer server.Close()
					config := email.DefaultHTTPConfig()
					config.Endpoint = server.URL
					driver, err := provider.construct(config)
					if err != nil {
						t.Fatal(err)
					}
					defer driver.Close()
					m := mailer(t, driver, nil, nil)
					result, err := m.Send(t.Context(), message(t), email.SendOptions{})
					if !errors.Is(err, test.want) || result.Accepted || count.Load() != 1 {
						t.Fatal("incorrect provider outcome", err, count.Load())
					}
					if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "reset-link") {
						t.Fatal("provider response leaked")
					}
				})
			}
		})
	}
}
func TestProviderSpecificRejectionCodes(t *testing.T) {
	for _, test := range []struct {
		provider string
		status   int
		body     string
		want     email.Kind
	}{{"resend", 409, `{"name":"concurrent_idempotent_requests"}`, email.Transient}, {"resend", 409, `{"name":"invalid_idempotent_request"}`, email.Permanent}, {"postmark", 200, `{"ErrorCode":406,"Message":"private"}`, email.Permanent}, {"postmark", 503, `{"ErrorCode":100}`, email.Transient}, {"postmark", 200, `{"ErrorCode":101}`, email.Ambiguous}, {"ses", 400, `<ErrorResponse><Error><Code>Throttling</Code></Error></ErrorResponse>`, email.Transient}, {"ses", 400, `<ErrorResponse><Error><Code>MessageRejected</Code></Error></ErrorResponse>`, email.Permanent}} {
		t.Run(test.provider+test.body, func(t *testing.T) {
			var provider providerCase
			for _, p := range providers() {
				if p.name == test.provider {
					provider = p
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			config := email.DefaultHTTPConfig()
			config.Endpoint = server.URL
			driver, err := provider.construct(config)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			m := mailer(t, driver, nil, nil)
			if _, err := m.Send(t.Context(), message(t), email.SendOptions{}); !errors.Is(err, test.want) {
				t.Fatal("wrong provider error classification", err)
			}
		})
	}
}
func TestHTTPSubmissionDoesNotRedirectOrReplayLostResponse(t *testing.T) {
	for _, mode := range []string{"redirect", "lost-reply"} {
		t.Run(mode, func(t *testing.T) {
			var forwarded, requests atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if mode == "redirect" {
					w.Header().Set("Location", target.URL)
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				}
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
			}))
			defer server.Close()
			config := email.DefaultHTTPConfig()
			config.Endpoint = server.URL
			driver, err := resend.New(resend.Config{HTTP: config, Token: secret.New("fixture-token")})
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			m := mailer(t, driver, nil, nil)
			_, err = m.Send(t.Context(), message(t), email.SendOptions{IdempotencyKey: "fixture-key"})
			want := email.Ambiguous
			if mode == "redirect" {
				want = email.Permanent
			}
			if !errors.Is(err, want) || requests.Load() != 1 || forwarded.Load() != 0 {
				t.Fatal("submission redirected/replayed", err)
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHTTPNoReplayBodyAndCancelledAdmission(t *testing.T) {
	var calls atomic.Int32
	config := email.DefaultHTTPConfig()
	config.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.GetBody != nil {
			t.Error("replayable POST exposed")
		}
		return nil, errors.New("private transport error")
	})
	driver, err := resend.New(resend.Config{HTTP: config, Token: secret.New("fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	m := mailer(t, driver, nil, nil)
	if _, err := m.Send(t.Context(), message(t), email.SendOptions{IdempotencyKey: "fixture-key"}); !errors.Is(err, email.Ambiguous) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Send(ctx, message(t), email.SendOptions{}); !errors.Is(err, email.Transient) || calls.Load() != 1 {
		t.Fatal("cancelled admission submitted", err)
	}
}
