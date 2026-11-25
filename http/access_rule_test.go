package http_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	h "github.com/weiloon1234/Foundry-Go/http"
)

func TestAnyActorAdmitsEveryAuthenticatedActorAndRequiresNothingElse(t *testing.T) {
	required := alternativeScopes(t, "editorial.write")
	binding, _ := alternativeBinding(t, required)
	handler := func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }
	endpoint := h.Authenticated(authEndpoint(h.Guarded), binding, binding.AnyActor())
	info, err := h.InspectAuthenticated(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	authentication := info.Route.Authentication
	if authentication == nil || len(authentication.RequiredScopes) != 0 || len(authentication.RequiredPermissions) != 0 || len(authentication.AnyOf) != 0 {
		t.Fatal("the open rule declared a requirement", authentication)
	}
	router := newAuthRouter(t, endpoint.Handle(handler))
	for token, status := range map[string]int{"": 401, "invalid": 401, "pending": 403, "unscoped": 204, "empty": 204, "valid": 204} {
		if response := authServe(router, []string{"Bearer " + token}); response.Code != status {
			t.Fatal("open rule admission", token, response.Code)
		}
	}
}

func TestGuardedRouteRefusesAMissingOrZeroRuleAtRegistration(t *testing.T) {
	binding, _ := alternativeBinding(t, alternativeScopes(t, "editorial.write"))
	handler := func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }
	for name, rule := range map[string]h.AccessRule[authAccount]{
		"nil":                nil,
		"zero alternative":   h.GuardAlternative[authAccount]{},
		"empty all-of":       h.AllOf[authAccount](),
		"anyof without rule": h.AnyOf[authAccount](),
	} {
		endpoint := h.Authenticated(authEndpoint(h.Guarded), binding, rule)
		err := h.ValidateAuthenticated(endpoint)
		if !errors.Is(err, fault.Invalid) {
			t.Fatal(name, "was not refused", err)
		}
		if !strings.Contains(err.Error(), "GET /profile (profile): ") {
			t.Fatal(name, "refusal does not name the route:", err)
		}
		if _, err := h.NewRouter(endpoint.Handle(handler)); err == nil {
			t.Fatal(name, "registered")
		}
	}
	err := h.ValidateAuthenticated(h.Authenticated(authEndpoint(h.Guarded), binding, nil))
	want := "GET /profile (profile): invalid: a guarded route requires an access rule: http.AllOf, http.AnyOf or binding.AnyActor()"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not name the route and the fix: %v", err)
	}
}

func TestOptionalAuthenticationNeedsNoRule(t *testing.T) {
	binding, _ := alternativeBinding(t, auth.AccessScopes[authAccount]{})
	endpoint := h.OptionallyAuthenticated(authEndpoint(h.Public), binding)
	if err := h.ValidateOptional(endpoint); err != nil {
		t.Fatal(err)
	}
}
