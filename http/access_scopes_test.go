package http_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestHTTPAccessScopesRunBeforeDecodingAndKeepMetadataImmutable(t *testing.T) {
	required, err := auth.NewAccessScopes(auth.DefineAccessScope[authAccount]("profile.read"))
	if err != nil {
		t.Fatal(err)
	}
	var loads, handled atomic.Int32
	provider := auth.DefineProvider("accounts", (authAccount{}).reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		loads.Add(1)
		return value.Set(authAccount{ID: id, Enabled: true}), nil
	}, func(context.Context, authAccount) (bool, error) { return true, nil })
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, s secret.String) (value.Optional[auth.Proof[authAccount, int64]], error) {
		scopes := required
		assurance := auth.Authenticated
		if s.Reveal() == "empty" {
			scopes = auth.AccessScopes[authAccount]{}
		}
		if s.Reveal() == "pending" {
			assurance = auth.PendingMFA
		}
		proof, err := auth.NewScopedProof(authAccount{ID: 7}.reference(), assurance, scopes)
		if s.Reveal() == "unscoped" {
			proof, err = auth.NewProof(authAccount{ID: 7}.reference(), assurance)
		}
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("api", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	base := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard)
	endpoint := base.WithScopes(required)
	if err := base.Validate(); err != nil {
		t.Fatal("base was mutated", err)
	}
	if err := base.WithScopes(auth.AccessScopes[authAccount]{}).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty requirement", err)
	}
	if err := endpoint.WithScopes(required).Validate(); !errors.Is(err, fault.Duplicate) {
		t.Fatal("replaced scope requirement", err)
	}
	router := newAuthRouter(t, endpoint.Handle(func(ctx context.Context, subject authAccount, _ authInput) (foundryhttp.NoContent, error) {
		handled.Add(1)
		if subject.ID != 7 {
			t.Error(subject)
		}
		if _, err := guard.RequireScopes(ctx, required); err != nil {
			t.Error(err)
		}
		route, ok := foundryhttp.MatchedRoute(ctx)
		if !ok || route.Authentication == nil {
			t.Fatal("missing auth metadata")
		}
		route.Authentication.RequiredScopes[0] = "changed"
		return foundryhttp.NoContent{}, nil
	}))
	description, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(description.Route.Authentication.RequiredScopes, []auth.AccessScopeName{"profile.read"}) {
		t.Fatal(description.Route.Authentication)
	}
	description.Route.Authentication.RequiredScopes[0] = "changed"
	routes := router.Routes()
	routes[0].Authentication.RequiredScopes[0] = "changed"
	for _, test := range []struct {
		token, path string
		status      int
	}{
		{"empty", "/profile?bad=%zz", 403}, {"unscoped", "/profile?bad=%zz", 403}, {"pending", "/profile?bad=%zz", 403},
		{"", "/profile", 401}, {"valid", "/profile?bad=%zz", 400}, {"valid", "/profile", 204},
	} {
		request := httptest.NewRequest("GET", test.path, nil)
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		if w.Code != test.status {
			t.Fatalf("%s %s: %d %s", test.token, test.path, w.Code, w.Body.String())
		}
		if test.status == 403 && w.Header().Get("WWW-Authenticate") != "" {
			t.Fatal("forbidden became authentication failure")
		}
	}
	if handled.Load() != 1 || loads.Load() != 4 {
		t.Fatal("scope handling/model reuse", handled.Load(), loads.Load())
	}
	if router.Routes()[0].Authentication.RequiredScopes[0] != "profile.read" {
		t.Fatal("route scope metadata mutated")
	}
}
