package typescript

import (
	"errors"
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
