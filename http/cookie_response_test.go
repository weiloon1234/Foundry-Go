package http_test

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	http "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCookieAuthenticationPreventsPersonalizedCaching(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "cookie", &loads)
	cookie := http.DefineCookie("session", http.SecretCookie(), http.DefaultCookieOptions())
	transport, err := http.NewCookieAuthentication(registry, http.CSRFConfig{}, http.CookieCredential("cookie", cookie))
	if err != nil {
		t.Fatal(err)
	}
	route := http.DefineRoute(http.RouteSpec{ID: "profile", Method: http.GET, Access: http.Guarded}, http.StaticPath("/profile"))
	typed := http.DefineEndpoint(route, http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, contract.StringJSON[string]()))
	raw := http.RequireRouteAuthentication(route, transport, guard).HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, a authAccount, _ http.NoPath) {
		w.Header().Set("Last-Modified", "Tue, 01 Sep 2026 00:00:00 GMT")
		fmt.Fprintf(w, "account-%d", a.ID)
	})
	optionalRoute := http.DefineRoute(http.RouteSpec{ID: "profile", Method: http.GET, Access: http.Public}, http.StaticPath("/profile"))
	optional := http.DefineEndpoint(optionalRoute, http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, contract.StringJSON[string]()))
	cases := []struct {
		name         string
		registration http.RouteRegistration
		anonymous    bool
	}{
		{"raw", raw, false},
		{"typed", http.RequireAuthentication(typed, transport, guard).Handle(func(_ context.Context, a authAccount, _ authInput) (string, error) {
			return fmt.Sprintf("account-%d", a.ID), nil
		}), false},
		{"optional", http.OptionalAuthentication(optional, transport, guard).Handle(func(_ context.Context, a value.Optional[authAccount], _ authInput) (string, error) {
			actor, ok := a.Get()
			if !ok {
				return "guest", nil
			}
			return fmt.Sprintf("account-%d", actor.ID), nil
		}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, err := http.ApplyMiddleware(newAuthRouter(t, tc.registration), http.ETags(http.DefaultETagConfig()))
			if err != nil {
				t.Fatal(err)
			}
			bodies := map[string]string{}
			for _, token := range []string{"valid", "other", "invalid", ""} {
				request := httptest.NewRequest("GET", "https://example.test/profile", nil)
				request.Header.Set("If-None-Match", "*")
				if token != "" {
					request.AddCookie(&stdhttp.Cookie{Name: "session", Value: token})
				}
				response := httptest.NewRecorder()
				response.Header().Set("Cache-Control", "public, max-age=60")
				router.ServeHTTP(response, request)
				want := 200
				if token == "invalid" || token == "" && !tc.anonymous {
					want = 401
				}
				if response.Code != want || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("ETag") != "" {
					t.Fatalf("token=%q status=%d cache=%q", token, response.Code, response.Header().Get("Cache-Control"))
				}
				bodies[token] = response.Body.String()
			}
			if bodies["valid"] == bodies["other"] {
				t.Fatal("distinct actors did not reach handler")
			}
		})
	}
	// Origin rejection happens before credential/handler work and needs the same policy.
	write := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "change", Method: http.POST, Access: http.Guarded}, http.StaticPath("/profile")), http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204))
	router := newAuthRouter(t, http.RequireAuthentication(write, transport, guard).Handle(func(context.Context, authAccount, authInput) (http.NoContent, error) {
		t.Fatal("cross-origin handler ran")
		return http.NoContent{}, nil
	}))
	request := httptest.NewRequest("POST", "https://example.test/profile", nil)
	request.Header.Set("Origin", "https://evil.test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("ETag") != "" {
		t.Fatal(response.Code, response.Header())
	}
}
