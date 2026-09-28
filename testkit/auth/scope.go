// Package auth provides test-owned production authentication scopes. It never
// installs an authenticated model directly or bypasses a verifier or policy.
package auth

import (
	"testing"

	foundryauth "github.com/weiloon1234/Foundry-Go/auth"
)

// Scope freezes ordinary credential inputs and registers cleanup with the test.
// Use real session/token credentials for integration tests, or an explicitly
// declared test strategy for a focused provider/policy test. The production
// registry still owns verification, eligibility, limits and declaration identity.
// Empty inputs create an anonymous scope, useful for rejection/optional tests.
func Scope(t testing.TB, registry *foundryauth.Registry, inputs ...foundryauth.Credential) *foundryauth.Scope {
	t.Helper()
	credentials, err := foundryauth.NewCredentials(inputs...)
	if err != nil {
		t.Fatalf("prepare authentication test credentials: %v", err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatalf("prepare authentication test scope: %v", err)
	}
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Errorf("close authentication test scope: %v", err)
		}
	})
	return scope
}

// Require asserts a successfully authenticated concrete model through the real
// guard. Policy checks remain explicit; this helper does not authorize resources.
// For rejection assertions call guard.Require(scope.Context()) directly instead.
func Require[M any](t testing.TB, scope *foundryauth.Scope, guard foundryauth.Guard[M]) M {
	t.Helper()
	subject, err := guard.Require(scope.Context())
	if err != nil {
		t.Fatalf("require authenticated test model: %v", err)
	}
	return subject
}
