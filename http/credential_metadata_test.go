package http_test

import (
	"sync/atomic"
	"testing"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestBoundCredentialMetadataUsesActualHTTPSource(t *testing.T) {
	for _, kind := range []foundryhttp.CredentialKind{foundryhttp.BearerCredentialKind, foundryhttp.CookieCredentialKind} {
		var loads atomic.Int32
		registry, guard, _ := authSetup(t, "credential", &loads)
		source := foundryhttp.BearerCredential("credential")
		want := "Authorization"
		if kind == foundryhttp.CookieCredentialKind {
			want = "app_session"
			source = foundryhttp.CookieCredential("credential", foundryhttp.DefineCookie(foundryhttp.CookieName(want), foundryhttp.SecretCookie(), foundryhttp.DefaultCookieOptions()))
		}
		transport, err := foundryhttp.NewAuthentication(registry, source)
		if err != nil {
			t.Fatal(err)
		}
		route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "credential.inspect", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/"))
		info, err := foundryhttp.RequireRouteAuthentication(route, transport, guard).Description()
		if err != nil {
			t.Fatal(err)
		}
		if info.Authentication.Credential.Kind != kind || info.Authentication.Credential.Name != want || info.Authentication.Credential.Source != "credential" || info.Authentication.Credential.Validate() != nil || loads.Load() != 0 {
			t.Fatal("credential metadata differs from bound source")
		}
	}
}
