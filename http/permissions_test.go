package http_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestHTTPPermissionsRunBeforeDecodingWithVerifiedAttribution(t *testing.T) {
	var loads atomic.Int32
	_, guard, policy := authSetup(t, "bearer", &loads)
	enabled := true
	firstCalls, secondCalls, handlerCalls := 0, 0, 0
	view := auth.DefinePermission("accounts.view", func(ctx context.Context, a authAccount) (bool, error) {
		firstCalls++
		if origin := attribution.FromContext(ctx); origin.Guard() != "api" {
			t.Error("permission evaluated without authenticated origin")
		}
		return enabled && a.ID == 1, nil
	})
	feature := auth.DefinePermission("features.profile", func(context.Context, authAccount) (bool, error) { secondCalls++; return true, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), policy.Registration(), view.Registration(), feature.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	permissions := []auth.Permission[authAccount]{view, feature}
	endpoint := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).WithPermissions(permissions...)
	permissions[0] = auth.Permission[authAccount]{}
	registration := endpoint.Handle(func(ctx context.Context, a authAccount, _ authInput) (foundryhttp.NoContent, error) {
		handlerCalls++
		return foundryhttp.NoContent{}, policy.Authorize(ctx, guard, a.ID)
	})
	router := newAuthRouter(t, registration)
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	want := []auth.PermissionName{"accounts.view", "features.profile"}
	if !slices.Equal(info.Route.Authentication.RequiredPermissions, want) {
		t.Fatal("missing declared permissions")
	}
	info.Route.Authentication.RequiredPermissions[0] = "changed"
	router.Routes()[0].Authentication.RequiredPermissions[0] = "changed"
	if !slices.Equal(router.Routes()[0].Authentication.RequiredPermissions, want) {
		t.Fatal("mutable route permission metadata")
	}
	if response := authServe(router, []string{"Bearer valid"}); response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
	enabled = false
	// The unknown query would be 400 after decoding. A current permission denial
	// must win first and must not call later permission callbacks or the handler.
	request := httptest.NewRequest("GET", "/profile?undeclared=value", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("decoding ran before permission denial", response.Code, response.Body.String())
	}
	if firstCalls != 2 || secondCalls != 1 || handlerCalls != 1 || loads.Load() != 2 {
		t.Fatal("permission or hydration order", firstCalls, secondCalls, handlerCalls, loads.Load())
	}
	enabled = true
	if response := authServe(router, []string{"Bearer valid"}); response.Code != 204 {
		t.Fatal("new grant not observed", response.Code)
	}
	if response := authServe(router, []string{"Bearer pending"}); response.Code == 204 {
		t.Fatal("pending credentials met permissions")
	}
	// A domain permission cannot grant missing token scopes. Scope admission
	// precedes permission callbacks even when the model would otherwise pass.
	required, err := auth.NewAccessScopes(auth.DefineAccessScope[authAccount]("profile.read"))
	if err != nil {
		t.Fatal(err)
	}
	restricted := newAuthRouter(t, endpoint.WithScopes(required).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		t.Error("unscoped credential reached handler")
		return foundryhttp.NoContent{}, nil
	}))
	before := firstCalls
	if response := authServe(restricted, []string{"Bearer valid"}); response.Code != 403 {
		t.Fatal("permission expanded unscoped credential", response.Code)
	}
	if firstCalls != before {
		t.Fatal("permission ran before token ceiling")
	}
	for _, candidate := range []foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, authAccount, foundryhttp.NoContent]{
		endpoint.WithPermissions(view),
		foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).WithPermissions(),
		foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).WithPermissions(view, view),
		foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).WithPermissions(auth.DefinePermission("not.registered", func(context.Context, authAccount) (bool, error) { return true, nil })),
	} {
		if err := candidate.Validate(); err == nil {
			t.Fatal("invalid permission requirements accepted")
		}
	}
	if err := endpoint.WithPermissions(view).Validate(); !errors.Is(err, fault.Duplicate) {
		t.Fatal("repeated declaration lost error", err)
	}
}
