package idempotenthttp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/secret"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

type disconnectedWriter struct{ header stdhttp.Header }

func (w disconnectedWriter) Header() stdhttp.Header  { return w.header }
func (disconnectedWriter) WriteHeader(int)           {}
func (disconnectedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestFailedSocketWritePreservesCommittedHTTPOutcome(t *testing.T) {
	var calls atomic.Int32
	app := startApp(t, pgtest.Isolate(t), Hooks{Calls: &calls}, nil)
	router, err := foundation.Resolve(app.app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	key, body := "socket-failure-0001", `{"name":"socket"}`
	request := httptest.NewRequest("POST", "/workspaces/1/orders", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer alice")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	func() {
		defer func() {
			if caught := recover(); caught != stdhttp.ErrAbortHandler {
				t.Errorf("unexpected writer failure: %v", caught)
			}
		}()
		router.ServeHTTP(disconnectedWriter{make(stdhttp.Header)}, request)
	}()
	response := app.send(t.Context(), "/workspaces/1/orders", "alice", key, body)
	assertResponse(t, response, 201)
	db, _ := app.app.Resources().Database()
	if calls.Load() != 1 || scalar(t, db, `SELECT count(*) FROM idem_orders`) != 1 {
		t.Fatal("socket failure repeated business work")
	}
}

type providerClock struct{ now time.Time }

func (c providerClock) Now() time.Time { return c.now }
func webhookSignature(key, stamp, body string) string {
	mac := hmac.New(sha256.New, []byte("fixed-loopback-provider-test-key-32-bytes"))
	_, _ = mac.Write([]byte(key + "." + stamp + "." + body))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func TestVerifiedWebhookReauthenticatesEveryReplay(t *testing.T) {
	app := startApp(t, pgtest.Isolate(t), Hooks{}, nil)
	store, _ := app.app.Resources().Idempotency()
	db, _ := app.app.Resources().Database()
	type providerAccount int64
	now := time.Now().UTC().Truncate(time.Second)
	verifier, err := webhook.New(webhook.DefaultConfig("provider", webhook.Standard), providerAccount(99), []secret.String{secret.New("whsec_" + base64.StdEncoding.EncodeToString([]byte("fixed-loopback-provider-test-key-32-bytes")))}, providerClock{now})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	endpoint := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "provider.webhook", Method: http.POST, Access: http.Public}, http.StaticPath("/webhook")), http.EmptyQuery(), http.JSONBody(SubmissionJSON()), http.EmptyResponse(204)).WithMiddleware(http.VerifyWebhook(verifier)).Idempotent(store, idempotency.Definition{ID: "provider.delivery", Version: 1})
	type input = http.Input[http.NoPath, http.NoQuery, Submission]
	router, err := http.NewRouter(endpoint.Handle(func(ctx context.Context, _ input) (idempotency.Scope, error) {
		delivery, ok := verifier.FromContext(ctx)
		if !ok {
			return idempotency.Scope{}, fmt.Errorf("verified delivery missing")
		}
		var account providerAccount = delivery.Account()
		return idempotency.NewScope(string(delivery.Provider()), strconv.FormatInt(int64(account), 10))
	}, func(ctx context.Context, tx *database.Tx, in input) (http.NoContent, error) {
		calls.Add(1)
		_, err := QueryIdemOrders().Create(ctx, tx, OrderDraft{}.SetWorkspaceID(1).SetCaller(99).SetName(in.Body.Name))
		return http.NoContent{}, err
	}))
	if err != nil {
		t.Fatal(err)
	}
	id, body := "provider-delivery-001", `{"name":"webhook"}`
	stamp := strconv.FormatInt(now.Unix(), 10)
	send := func(signature, stamp, untrustedKey string) int {
		request := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Webhook-Id", id)
		request.Header.Set("Webhook-Timestamp", stamp)
		request.Header.Set("Webhook-Signature", signature)
		request.Header.Set("Idempotency-Key", untrustedKey)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	signature := webhookSignature(id, stamp, body)
	expired := strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10)
	if send(signature, stamp, "attacker-chosen-key-1") != 204 || send(signature, stamp, "attacker-chosen-key-2") != 204 || send("invalid", stamp, "") != 401 || send(webhookSignature(id, expired, body), expired, "") != 401 || calls.Load() != 1 || scalar(t, db, `SELECT count(*) FROM idem_orders WHERE caller=99`) != 1 {
		t.Fatal("webhook replay bypassed verification or duplicated effects")
	}
}
