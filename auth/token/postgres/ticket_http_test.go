package postgres_test

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestPostgresTicketAuthenticatedRoutes(t *testing.T) {
	s := prepare(t)
	permissionAllowed := true
	permission := auth.DefinePermission("download", func(context.Context, member) (bool, error) { return permissionAllowed, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), s.tokens.Guard().Registration(), permission.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := h.NewAuthentication(registry, h.BearerCredential(s.tokens.TicketSource()))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.BindGuard(transport, s.tokens.Guard())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := h.TicketAuthenticated(h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "download", Method: h.GET, Access: h.Guarded}, h.StaticPath("/download")), h.EmptyQuery(), h.EmptyBody(), h.EmptyResponse(204)), binding, s.tokens, h.Scopes(s.grants), h.Permissions(permission))
	called := 0
	router, err := h.NewRouter(endpoint.Handle(func(ctx context.Context, actor member, _ h.Input[h.NoPath, h.NoQuery, h.NoBody]) (h.NoContent, error) {
		called++
		current, err := s.tokens.Current(ctx)
		if err != nil || actor.ID != 7 || !current.Scopes().ContainsAll(s.grants) {
			t.Error("actor/scopes not preserved", err)
		}
		return h.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	access := issue(t, s, 7, true)
	fresh := func() secret.String {
		t.Helper()
		ticket, err := issueTicket(t, s, access.AccessSecret())
		if err != nil {
			t.Fatal(err)
		}
		return ticket.Secret()
	}
	location := func(ticket secret.String) string { return "/download?ticket=" + url.QueryEscape(ticket.Reveal()) }
	request := func(method, path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+access.AccessSecret().Reveal())
		r.Header.Set("Cookie", "session=ignored")
		w := httptest.NewRecorder()
		before := called
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("response status %d, want %d", w.Code, status)
		}
		wantCalls := before
		if status == 204 {
			wantCalls++
		}
		if called != wantCalls {
			t.Fatal("ticket request reached the handler an unexpected number of times")
		}
		if w.Result().Header.Get("Cache-Control") != "no-store" || w.Result().Header.Get("Referrer-Policy") != "no-referrer" || w.Result().Header.Get("WWW-Authenticate") != "" {
			t.Fatal("ticket privacy/challenge headers")
		}
		return w
	}
	t.Run("malformed-does-not-consume", func(t *testing.T) {
		raw := fresh()
		before := ticketRows(t, s)
		for _, path := range []string{"/download", "/download?ticket=", "/download?ticket=" + strings.Repeat("x", auth.MaxCredentialBytes+1), location(raw) + "&ticket=again", location(raw) + "&%74icket=" + url.QueryEscape(raw.Reveal())} {
			request("GET", path, 401)
		}
		if ticketRows(t, s) != before {
			t.Fatal("malformed request consumed ticket")
		}
		request("GET", location(raw), 204)
	})
	t.Run("head-and-replay", func(t *testing.T) {
		raw := fresh()
		if w := request("HEAD", location(raw), 405); w.Result().Header.Get("Allow") != "GET" {
			t.Fatal("HEAD Allow")
		}
		request("GET", location(raw), 204)
		request("GET", location(raw), 401)
	})
	t.Run("url-and-typed-decoding", func(t *testing.T) {
		raw := fresh()
		path, err := endpoint.URL(t.Context(), h.NoPath{}, h.NoQuery{}, raw)
		if err != nil {
			t.Fatal(err)
		}
		request("GET", path, 204)
	})
	t.Run("unknown-and-expired", func(t *testing.T) {
		request("GET", "/download?ticket=unknown", 401)
		raw := fresh()
		s.clock.Advance(s.config.TicketLifetime)
		request("GET", location(raw), 401)
	})
	t.Run("wrong-guard", func(t *testing.T) {
		other, err := token.New(s.store, "other", s.provider, "other.bearer", s.grants)
		if err != nil {
			t.Fatal(err)
		}
		otherRegistry, err := auth.NewRegistry(auth.DefaultConfig(), other.Guard().Registration())
		if err != nil {
			t.Fatal(err)
		}
		proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
		if err != nil {
			t.Fatal(err)
		}
		issued, err := other.Issue(t.Context(), proof, token.IssueOptions[member]{Scopes: s.grants})
		if err != nil {
			t.Fatal(err)
		}
		credentials, err := auth.NewCredentials(auth.Credential{Name: other.TicketSource(), Secret: issued.AccessSecret()})
		if err != nil {
			t.Fatal(err)
		}
		scope, err := otherRegistry.NewScope(t.Context(), credentials)
		if err != nil {
			t.Fatal(err)
		}
		defer scope.Close()
		ticket, err := other.IssueTicket(scope.Context())
		if err != nil {
			t.Fatal(err)
		}
		request("GET", location(ticket.Secret()), 401)
		if found, err := other.RedeemTicket(t.Context(), ticket.Secret()); err != nil || !found.IsSet() {
			t.Fatal("wrong guard consumed ticket", err)
		}
	})
	t.Run("ineligible", func(t *testing.T) {
		raw := fresh()
		s.enabled.Store(false)
		request("GET", location(raw), 401)
		s.enabled.Store(true)
	})
	t.Run("permission", func(t *testing.T) {
		raw := fresh()
		permissionAllowed = false
		request("GET", location(raw), 403)
		permissionAllowed = true
	})
	t.Run("scope-ceiling", func(t *testing.T) {
		proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
		if err != nil {
			t.Fatal(err)
		}
		restricted, err := s.tokens.Issue(t.Context(), proof, token.IssueOptions[member]{})
		if err != nil {
			t.Fatal(err)
		}
		ticket, err := issueTicket(t, s, restricted.AccessSecret())
		if err != nil {
			t.Fatal(err)
		}
		request("GET", location(ticket.Secret()), 403)
	})
	t.Run("routing-responses-preserve-ticket", func(t *testing.T) {
		folder := h.TicketAuthenticated(h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "folder", Method: h.GET, Access: h.Guarded}, h.StaticPath("/folder/")), h.EmptyQuery(), h.EmptyBody(), h.EmptyResponse(204)), binding, s.tokens)
		routes, err := h.NewRouter(endpoint.Handle(func(context.Context, member, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (h.NoContent, error) {
			t.Error("routing failure reached download handler")
			return h.NoContent{}, nil
		}), folder.Handle(func(context.Context, member, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (h.NoContent, error) {
			t.Error("routing failure reached folder handler")
			return h.NoContent{}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		for _, publicCache := range []bool{false, true} {
			var handler stdhttp.Handler = routes
			if publicCache {
				handler, err = h.ApplyMiddleware(routes, h.CacheControl(h.CachePolicy{Visibility: h.CachePublic, MaxAge: time.Hour}))
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range []struct {
				method, path string
				status       int
			}{
				{"GET", "//download", 404},
				{"GET", "/files/../download", 404},
				{"GET", "/folder", 404},
				{"POST", "/download", 405},
				{"OPTIONS", "/download", 405},
				{"GET", "/unknown", 404},
				{"GET", "/folder/%2F", 400},
			} {
				for _, name := range []string{"ticket", "%74icket", "%74%69%63%6b%65%74"} {
					raw := fresh()
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path+"?"+name+"="+url.QueryEscape(raw.Reveal()), nil))
					result := w.Result()
					referrer := "no-referrer"
					if tc.path == "/unknown" {
						referrer = ""
					}
					if result.StatusCode != tc.status || result.Header.Get("Cache-Control") != "no-store" || result.Header.Get("Referrer-Policy") != referrer || result.Header.Get("Location") != "" {
						t.Fatalf("routing privacy failed: method=%s path=%s public-cache=%t status=%d", tc.method, tc.path, publicCache, result.StatusCode)
					}
					if w.Body.Len() == 0 || strings.Contains(w.Body.String(), raw.Reveal()) || strings.Contains(w.Body.String(), "href=") {
						t.Fatal("routing response leaked a ticket or returned a redirect body")
					}
					if found, err := s.tokens.RedeemTicket(t.Context(), raw); err != nil || !found.IsSet() {
						t.Fatal("routing response consumed the ticket", err)
					}
				}
			}
		}
	})
	t.Run("revoked", func(t *testing.T) {
		raw := fresh()
		if _, err := s.tokens.RevokeID(t.Context(), member{ID: 7}.reference(), access.Info().ID()); err != nil {
			t.Fatal(err)
		}
		request("GET", location(raw), 401)
	})
}
