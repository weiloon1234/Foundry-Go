package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestCapturedCredentialsDoNotRetainRequestOrResolvedModel(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	adapter, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "https://app.test/ws", nil)
	request.Header.Set("Authorization", "Bearer valid")
	credentials, err := adapter.CaptureCredentials(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer other")
	if loads.Load() != 0 || adapter.Registry() != registry {
		t.Fatal("capture resolved a model or changed registry")
	}
	for i := 0; i < 2; i++ {
		scope, err := adapter.Registry().NewScope(t.Context(), credentials)
		if err != nil {
			t.Fatal(err)
		}
		account, err := guard.Require(scope.Context())
		scope.Close()
		if err != nil || account.ID != 1 {
			t.Fatal("captured credential changed with request")
		}
	}
	if loads.Load() != 2 {
		t.Fatal("scopes shared a resolved subject")
	}
	if _, err := adapter.CaptureCredentials(nil); err == nil {
		t.Fatal("nil capture accepted")
	}
}

func TestBrowserCaptureReusesSecureCookieAndRefreshesPersistentSession(t *testing.T) {
	s := browserPrepare(t)
	issued, err := s.sessions.Issue(t.Context(), browserProof(t), session.IssueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.sessions.Revoke(context.Background(), issued.Secret())
	request := httptest.NewRequest("GET", "https://app.test/ws", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-foundry_session", Value: issued.Secret().Reveal()})
	adapter := s.web.Authentication()
	credentials, err := adapter.CaptureCredentials(request)
	if err != nil {
		t.Fatal("standalone handshake capture required a retained HTTP scope")
	}
	scope, err := adapter.Registry().NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	account, err := s.web.Guard().Require(scope.Context())
	scope.Close()
	if err != nil || account.ID != 7 {
		t.Fatal("captured session did not authenticate")
	}
	insecure := httptest.NewRequest("GET", "http://app.test/ws", nil)
	insecure.Header = request.Header.Clone()
	if _, err := adapter.CaptureCredentials(insecure); err == nil {
		t.Fatal("secure browser cookie accepted on insecure request")
	}
	// Repeated cookies of one name read as absent: path ordering is not identity.
	request.Header.Add("Cookie", request.Header.Get("Cookie"))
	if duplicated, err := adapter.CaptureCredentials(request); err != nil {
		t.Fatal(err)
	} else if scope, err := adapter.Registry().NewScope(t.Context(), duplicated); err != nil {
		t.Fatal(err)
	} else {
		_, err := s.web.Guard().Require(scope.Context())
		scope.Close()
		if !errors.Is(err, auth.Unauthenticated) {
			t.Fatal("duplicate browser cookie accepted", err)
		}
	}
	if _, err := s.sessions.Revoke(t.Context(), issued.Secret()); err != nil {
		t.Fatal(err)
	}
	scope, err = adapter.Registry().NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	if _, err := s.web.Guard().Require(scope.Context()); err == nil {
		t.Fatal("captured session bypassed revocation")
	}
}
