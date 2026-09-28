package http_test

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestApplicationRequiresCookieProtectionRegardlessOfPath(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "cookie", &loads)
	source := foundryhttp.CookieCredential("cookie", foundryhttp.DefineCookie("session", foundryhttp.SecretCookie(), foundryhttp.DefaultCookieOptions()))
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "api.cookie", Method: foundryhttp.POST, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/api/change")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	for _, protected := range []bool{false, true} {
		var transport *foundryhttp.Authentication
		var err error
		if protected {
			transport, err = foundryhttp.NewCookieAuthentication(registry, foundryhttp.CSRFConfig{}, source)
		} else {
			transport, err = foundryhttp.NewAuthentication(registry, source)
		}
		if err != nil {
			t.Fatal(err)
		}
		group, err := foundryhttp.BindGuard(transport, guard)
		if err != nil {
			t.Fatal(err)
		}
		registration := foundryhttp.Authenticated(endpoint, group).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		})
		settings := application.DefaultSettings()
		app, err := application.New(settings, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).HTTP(func(application.Services) ([]foundryhttp.RouteRegistration, error) {
			return []foundryhttp.RouteRegistration{registration}, nil
		}).Build(t.Context())
		if !protected {
			if err == nil {
				t.Fatal("unprotected cookie route entered ordinary application")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		_ = app.Shutdown(t.Context())
		router := newAuthRouter(t, registration)
		for _, origin := range []string{"", "https://evil.test", "http://example.com"} {
			r := httptest.NewRequest("POST", "http://example.com/api/change", nil)
			r.AddCookie(&stdhttp.Cookie{Name: "session", Value: "valid"})
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			want := 403
			if origin == "http://example.com" {
				want = 204
			}
			if w.Code != want {
				t.Fatal(origin, w.Code, w.Body.String())
			}
		}
		// Same-name, different declarations do not substitute for registry membership.
		otherRegistry, other, _ := authSetup(t, "cookie", &loads)
		_ = otherRegistry
		if _, err := group.Select(other); err == nil {
			t.Fatal("unregistered alternate guard accepted")
		}
	}
	if _, err := foundryhttp.NewCookieAuthentication(registry, foundryhttp.CSRFConfig{}, foundryhttp.BearerCredential(auth.CredentialName("cookie"))); err == nil {
		t.Fatal("bearer source entered browser policy")
	}
}
