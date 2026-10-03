package email_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/cloudflare"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func cloudflareConfig() cloudflare.Config {
	return cloudflare.Config{HTTP: email.DefaultHTTPConfig(), AccountID: "0123456789abcdef0123456789abcdef", Token: secret.New("fixture-private-token")}
}

func TestCloudflareConfigurationAndRedaction(t *testing.T) {
	for _, account := range []string{"", "short", strings.Repeat("z", 32), strings.Repeat("a", 31) + "/", strings.Repeat("a", 31) + "?", strings.Repeat("a", 33)} {
		config := cloudflareConfig()
		config.AccountID = account
		if _, err := cloudflare.New(config); !errors.Is(err, email.Construction) {
			t.Fatal("invalid account identifier accepted", err)
		}
	}
	for _, token := range []string{"", "private\r\nheader", "private token"} {
		config := cloudflareConfig()
		config.Token = secret.New(token)
		if _, err := cloudflare.New(config); !errors.Is(err, email.Construction) {
			t.Fatal("invalid API token accepted", err)
		}
	}
	config := cloudflareConfig()
	config.HTTP.Endpoint = "http://api.cloudflare.com"
	if _, err := cloudflare.New(config); !errors.Is(err, email.Construction) {
		t.Fatal("plaintext remote endpoint accepted", err)
	}
	config = cloudflareConfig()
	driver, err := cloudflare.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	for _, value := range []any{config, driver} {
		if strings.Contains(fmt.Sprintf("%+v", value), "private") || strings.Contains(fmt.Sprintf("%#v", value), config.AccountID) {
			t.Fatal("Cloudflare configuration leaked")
		}
	}
	if _, err := driver.Send(t.Context(), email.Outbound{}); !errors.Is(err, email.Construction) {
		t.Fatal("empty outbound accepted", err)
	}
	var empty *cloudflare.Driver
	empty.Close()
	if _, err := empty.Send(t.Context(), email.Outbound{}); !errors.Is(err, email.Construction) {
		t.Fatal("nil driver accepted", err)
	}
}

func TestCloudflareUsesSharedMIMEAndDefaultEndpoint(t *testing.T) {
	var ids []string
	config := cloudflareConfig()
	config.HTTP.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.cloudflare.com/client/v4/accounts/"+config.AccountID+"/email/sending/send_raw" || r.GetBody != nil || r.Header.Get("Idempotency-Key") != "" {
			t.Error("incorrect endpoint or replay policy")
		}
		var body struct {
			From       string   `json:"from"`
			Recipients []string `json:"recipients"`
			MIME       string   `json:"mime_message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.From != "sender@example.test" || !reflect.DeepEqual(body.Recipients, []string{"to@example.test", "cc@example.test", "hidden@example.test"}) {
			t.Error("display names or recipient headers used as envelope")
		}
		parsed, err := mail.ReadMessage(strings.NewReader(body.MIME))
		if err != nil {
			t.Error(err)
			return nil, err
		}
		if parsed.Header.Get("Bcc") != "" || strings.Contains(body.MIME, "hidden@example.test") || parsed.Header.Get("Content-Language") != "en" {
			t.Error("MIME lost locale or exposed BCC")
		}
		replies, err := parsed.Header.AddressList("Reply-To")
		if err != nil || len(replies) != 2 || replies[0].Address != "reply@example.test" || replies[1].Address != "support@example.test" {
			t.Error("MIME lost reply-to addresses")
		}
		ids = append(ids, parsed.Header.Get("Message-ID"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"result":{"message_id":"accepted","delivered":["to@example.test"],"queued":["cc@example.test"],"permanent_bounces":["hidden@example.test"]}}`)), Header: make(http.Header)}, nil
	})
	driver, err := cloudflare.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	m := mailer(t, driver, nil, nil)
	msg := email.NewMessage(address(t, "Sender <sender@example.test>"), "Welcome", address(t, "To <to@example.test>")).Text("Hello").Cc(address(t, "Copy <cc@example.test>")).Bcc(address(t, "Hidden <hidden@example.test>")).ReplyTo(address(t, "reply@example.test"), address(t, "support@example.test")).Locale("en")
	for range 2 {
		result, err := m.Send(t.Context(), msg, email.SendOptions{IdempotencyKey: "same-delivery"})
		if err != nil || !result.Accepted || result.Receipt.MessageID != "accepted" {
			t.Fatal("partial recipient bounce undid provider acceptance", err)
		}
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatal("logical submission lost stable MIME identity")
	}
}

func TestCloudflareResponseEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		want   email.Kind
	}{
		{"missing-success", 200, `{"result":{"message_id":"id"}}`, email.Ambiguous},
		{"null-success", 200, `{"success":null,"result":{"message_id":"id"}}`, email.Ambiguous},
		{"false-success", 200, `{"success":false,"errors":[{"code":10001}],"result":null}`, email.Ambiguous},
		{"contradictory-success", 200, `{"success":true,"errors":[{"code":10002}],"result":{"message_id":"id"}}`, email.Ambiguous},
		{"missing-result", 200, `{"success":true,"result":null}`, email.Ambiguous},
		{"missing-id", 200, `{"success":true,"result":{}}`, email.Ambiguous},
		{"invalid-id", 200, `{"success":true,"result":{"message_id":"private\nvalue"}}`, email.Ambiguous},
		{"authentication-upstream", 503, `{"success":false,"errors":[{"code":10100}],"result":null}`, email.Transient},
		{"unknown-server-error", 500, `{"success":false,"errors":[{"code":10002}],"result":null}`, email.Ambiguous},
		{"conflicting-server-error", 503, `{"success":false,"errors":[{"code":10100},{"code":10002}],"result":null}`, email.Ambiguous},
		{"server-error-with-receipt", 503, `{"success":false,"errors":[{"code":10100}],"result":{"message_id":"id"}}`, email.Ambiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			config := cloudflareConfig()
			config.HTTP.Endpoint = server.URL
			driver, err := cloudflare.New(config)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			result, err := mailer(t, driver, nil, nil).Send(t.Context(), message(t), email.SendOptions{})
			if !errors.Is(err, test.want) || result.Accepted || requests.Load() != 1 {
				t.Fatal("incorrect Cloudflare outcome", err)
			}
		})
	}
}

func TestCloudflareBoundsAndCancellationBeforeSubmission(t *testing.T) {
	var requests atomic.Int32
	config := cloudflareConfig()
	config.HTTP.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected network call")
	})
	driver, err := cloudflare.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	m := mailer(t, driver, nil, nil)
	if _, err := m.Send(t.Context(), message(t).Text(strings.Repeat("body ", 1<<20)), email.SendOptions{}); !errors.Is(err, email.Construction) {
		t.Fatal("oversized Cloudflare message accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Send(ctx, message(t), email.SendOptions{}); !errors.Is(err, email.Transient) {
		t.Fatal("canceled mail accepted", err)
	}
	if requests.Load() != 0 {
		t.Fatal("invalid send reached provider")
	}
}
