package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

func TestLockoutHTTPUsesSharedEnvelopeAndRoundedRetry(t *testing.T) {
	backend, err := memory.New(10, rateTestClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	store, err := lockout.NewStore(backend, lockout.DefaultConfig(keyspace.Namespace{Application: "http", Environment: "lockout"}))
	if err != nil {
		t.Fatal(err)
	}
	policy := lockout.Policy{MaxFailures: 1, Window: time.Minute, LockFor: 1500 * time.Millisecond}
	throttle, err := lockout.Define("password", keyspace.StringKeys[string](), policy).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	_, rejected := throttle.Run(t.Context(), "private@example.test", func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(rejected, lockout.Locked) {
		t.Fatal(rejected)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", nil)
	if err := WriteError(w, r, rejected); err != nil {
		t.Fatal(err)
	}
	var payload ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if w.Code != 429 || payload.Code != RateLimited || w.Header().Get("Retry-After") != "2" || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private") {
		t.Fatal("unsafe lockout response", w.Code, w.Header(), payload)
	}
	if code, ok := authenticationCode(lockout.Expired); !ok || code != Unauthenticated {
		t.Fatal("expired attempt response", code)
	}
	// A protection store outage has no lock duration and must not allow work.
	backend.Close()
	_, unavailable := throttle.Run(t.Context(), "another", func(context.Context) (bool, error) { t.Error("closed store admitted callback"); return true, nil })
	w = httptest.NewRecorder()
	if err := WriteError(w, r, unavailable); err != nil {
		t.Fatal(err)
	}
	if w.Code != 503 || w.Header().Get("Retry-After") != "" {
		t.Fatal("backend error became a confirmed lock", w.Code, w.Header())
	}
}
