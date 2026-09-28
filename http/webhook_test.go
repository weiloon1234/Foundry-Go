package http_test

import (
	"context"
	"errors"
	http "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type webhookClock struct{ fail bool }

func (c webhookClock) Now() time.Time {
	if c.fail {
		panic("private clock diagnostic")
	}
	return time.Unix(1700000000, 0).UTC()
}

type unreadableWebhook struct{}

func (unreadableWebhook) Read([]byte) (int, error) { return 0, errors.New("private read diagnostic") }
func (unreadableWebhook) Close() error             { return nil }
func TestWebhookMiddlewareFailureClassificationAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		clock  webhookClock
		mutate func(*stdhttp.Request)
		want   int
	}{
		{"healthy", webhookClock{}, func(*stdhttp.Request) {}, 204},
		{"invalid", webhookClock{}, func(r *stdhttp.Request) { r.Header.Set("Webhook-Signature", "v1,invalid") }, 401},
		{"oversized", webhookClock{}, func(r *stdhttp.Request) { r.ContentLength = 100 }, 413},
		{"unreadable", webhookClock{}, func(r *stdhttp.Request) { r.Body = unreadableWebhook{} }, 400},
		{"canceled", webhookClock{}, func(r *stdhttp.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			*r = *r.WithContext(ctx)
		}, 408},
		{"clock panic", webhookClock{true}, func(*stdhttp.Request) {}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := webhook.DefaultConfig("billing", webhook.Standard)
			config.MaxBodyBytes = 32
			verifier, err := webhook.New(config, int64(7), []secret.String{secret.New("whsec_MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=")}, tc.clock)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			handler, err := http.ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				calls++
				delivery, ok := verifier.FromContext(r.Context())
				body, err := io.ReadAll(r.Body)
				if !ok || delivery.Account() != 7 || string(body) != "{}" || err != nil {
					t.Error("typed original delivery lost")
				}
				if r.Header.Get("Idempotency-Key") == "unsigned" || len(r.Header.Get("Idempotency-Key")) != 64 {
					t.Error("unsigned identity retained")
				}
				w.WriteHeader(204)
			}), http.VerifyWebhook(verifier))
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				request := httptest.NewRequest("POST", "/hook", strings.NewReader("{}"))
				request.Header.Set("Webhook-Id", "delivery_1")
				request.Header.Set("Webhook-Timestamp", "1700000000")
				// Fixed HMAC vector generated independently with Python's standard library.
				request.Header.Set("Webhook-Signature", "v1,hkMcggvUp1HOnbVeYmO9M2Z4ZI0JbDLURtZ6I4qC5as=")
				request.Header.Set("Idempotency-Key", "unsigned")
				want := 204
				if attempt == 0 {
					tc.mutate(request)
					want = tc.want
				} else if tc.clock.fail {
					want = 500
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != want || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private") {
					t.Fatal(response.Code, response.Body.String())
				}
			}
			expected := 1
			if tc.want == 204 {
				expected = 2
			} else if tc.clock.fail {
				expected = 0
			}
			if calls != expected {
				t.Fatal("failure reached handler", calls)
			}
		})
	}
}
