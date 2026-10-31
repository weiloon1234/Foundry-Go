package typescript

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/contract"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Manifest decoding also rejects these names. Exercise the emitter's guard
// directly so a future metadata boundary change cannot bypass auth validation.
func TestSurfaceAuthRequiresHostCookie(t *testing.T) {
	surface := Surface{Name: "user", Auth: &SurfaceAuth{Guard: "user", Login: "login", Refresh: "refresh", Logout: "logout", Profile: "profile", Identifier: "email"}}
	document := manifest.Document{HTTP: []manifest.Operation{
		{Route: foundryhttp.RouteInfo{ID: "login"}, RefreshCookie: &foundryhttp.RefreshCookieInfo{Name: "refresh-user", Sets: true}},
		{Route: foundryhttp.RouteInfo{ID: "refresh"}, RefreshCookie: &foundryhttp.RefreshCookieInfo{Name: "refresh-user", Reads: true, Sets: true}},
		{Route: foundryhttp.RouteInfo{ID: "logout"}, RefreshCookie: &foundryhttp.RefreshCookieInfo{Name: "refresh-user", Reads: true, Clears: true}},
		{Route: foundryhttp.RouteInfo{ID: "profile", Authentication: &foundryhttp.AuthenticationInfo{Guard: "user"}}},
	}}
	_, err := renderSurfaceAuth("api", surface, document, document, naming{})
	const want = `surface "user" refresh cookie "refresh-user" requires a __Host- name`
	if !errors.Is(err, fault.Invalid) || err.Error() != "invalid: "+want {
		t.Fatalf("expected invalid with message %q, got %v", want, err)
	}
}

func TestSurfaceAuthRejectsUnsuppliedInputs(t *testing.T) {
	for _, route := range []foundryhttp.RouteID{"login", "refresh", "logout", "profile"} {
		for _, input := range []string{"path", "query", "body", "idempotency", "signedURL"} {
			if route == "login" && input == "body" {
				continue
			}
			t.Run(string(route)+"/"+input, func(t *testing.T) {
				surface := Surface{Name: "user", Auth: &SurfaceAuth{Guard: "user", Login: "login", Refresh: "refresh", Logout: "logout", Profile: "profile", Identifier: "email"}}
				document := manifest.Document{HTTP: []manifest.Operation{
					{Route: foundryhttp.RouteInfo{ID: "login"}, Body: &manifest.Payload{Type: "LoginBody"}, RefreshCookie: &foundryhttp.RefreshCookieInfo{Name: "__Host-refresh-user", Sets: true}},
					{Route: foundryhttp.RouteInfo{ID: "refresh"}, RefreshCookie: &foundryhttp.RefreshCookieInfo{Name: "__Host-refresh-user", Reads: true, Sets: true}},
					{Route: foundryhttp.RouteInfo{ID: "logout"}, RefreshCookie: &foundryhttp.RefreshCookieInfo{Name: "__Host-refresh-user", Reads: true, Clears: true}},
					{Route: foundryhttp.RouteInfo{ID: "profile", Authentication: &foundryhttp.AuthenticationInfo{Guard: "user"}}},
				}, Types: []contract.Type{{ID: "LoginBody", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "email"}, {Name: "password"}}}}}
				for i := range document.HTTP {
					op := &document.HTTP[i]
					if op.Route.ID != route {
						continue
					}
					switch input {
					case "path":
						op.Path = []manifest.Parameter{{Name: "tenant", Required: true}}
					case "query":
						op.Query = []manifest.Parameter{{Name: "tenant", Required: true}}
					case "body":
						op.Body = &manifest.Payload{Type: "LoginBody"}
					case "idempotency":
						op.Idempotency = &foundryhttp.IdempotencyInfo{}
					case "signedURL":
						op.Route.SignedURL = &foundryhttp.SignedURLInfo{}
					}
				}
				_, err := renderSurfaceAuth("api", surface, document, document, naming{})
				if !errors.Is(err, fault.Invalid) {
					t.Fatalf("unsupplied %s on %s accepted: %v", input, route, err)
				}
			})
		}
	}
}
