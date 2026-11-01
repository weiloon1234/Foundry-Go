package http_test

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type endpointRedeemer struct {
	source auth.CredentialName
	redeem func(context.Context, secret.String) (value.Optional[auth.BoundCredential], error)
}

func (r *endpointRedeemer) TicketSource() auth.CredentialName { return r.source }
func (r *endpointRedeemer) RedeemTicket(ctx context.Context, raw secret.String) (value.Optional[auth.BoundCredential], error) {
	return r.redeem(ctx, raw)
}

func ticketEndpointSetup(t *testing.T) (h.GuardBinding[authAccount], *endpointRedeemer) {
	t.Helper()
	provider := auth.DefineProvider("accounts", authAccount{}.reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		return value.Set(authAccount{ID: id, Enabled: true}), nil
	}, func(context.Context, authAccount) (bool, error) { return true, nil })
	strategy, binder := auth.BindStrategy(auth.DefineStrategy("ticket.source", func(context.Context, secret.String) (value.Optional[auth.Proof[authAccount, int64]], error) {
		t.Error("transport credential read")
		return value.Optional[auth.Proof[authAccount, int64]]{}, nil
	}), func(_ context.Context, id int64) (value.Optional[auth.Proof[authAccount, int64]], error) {
		p, err := auth.NewProof(authAccount{ID: id}.reference(), auth.Authenticated)
		return value.Set(p), err
	})
	guard := auth.DefineGuard("tickets", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := h.NewAuthentication(registry, h.BearerCredential("ticket.source"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.BindGuard(transport, guard)
	if err != nil {
		t.Fatal(err)
	}
	return binding, &endpointRedeemer{source: "ticket.source", redeem: func(context.Context, secret.String) (value.Optional[auth.BoundCredential], error) {
		bound, err := binder.Bind(int64(7))
		return value.Set(bound), err
	}}
}

func TestTicketEndpointFailuresAndRecovery(t *testing.T) {
	binding, redeemer := ticketEndpointSetup(t)
	good := redeemer.redeem
	actorCalls := 0
	actor := h.DefineMiddleware("test.actor", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			subject, err := binding.Guard().Require(r.Context())
			if err != nil || subject.ID != 7 {
				t.Error("actor middleware lost scope", err)
			}
			if _, present := attribution.FromContext(r.Context()).Model(); !present {
				t.Error("missing attribution")
			}
			actorCalls++
			next.ServeHTTP(w, r)
		}), nil
	})
	endpoint := h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer, h.ActorMiddleware[authAccount](actor))
	router := newAuthRouter(t, endpoint.Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }))
	serve := func(status int) {
		t.Helper()
		r := httptest.NewRequest("GET", "/profile?ticket=private-canary", nil)
		r.Header["Authorization"] = []string{"malformed", "duplicated"}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("status %d want %d", w.Code, status)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("WWW-Authenticate") != "" || strings.Contains(w.Body.String(), "private-canary") {
			t.Fatal("unsafe ticket response")
		}
	}
	for _, mode := range []string{"error", "panic", "goexit", "cycle", "error-panic", "error-goexit", "unauthenticated", "absent", "wrong-bound"} {
		t.Run(mode, func(t *testing.T) {
			redeemer.redeem = func(context.Context, secret.String) (value.Optional[auth.BoundCredential], error) {
				switch mode {
				case "panic":
					panic("private-canary")
				case "goexit":
					runtime.Goexit()
					return value.Optional[auth.BoundCredential]{}, nil
				case "cycle":
					return value.Optional[auth.BoundCredential]{}, &errorgraphtest.Error{Mode: "cycle"}
				case "error-panic":
					return value.Optional[auth.BoundCredential]{}, &errorgraphtest.Error{Mode: "panic"}
				case "error-goexit":
					return value.Optional[auth.BoundCredential]{}, &errorgraphtest.Error{Mode: "goexit"}
				case "unauthenticated":
					return value.Optional[auth.BoundCredential]{}, auth.Unauthenticated
				case "absent":
					return value.Optional[auth.BoundCredential]{}, nil
				case "wrong-bound":
					return value.Set(auth.BoundCredential{}), nil
				default:
					return value.Optional[auth.BoundCredential]{}, errors.New("private-canary")
				}
			}
			want := 503
			if mode == "unauthenticated" || mode == "absent" || mode == "wrong-bound" {
				want = 401
			}
			before := actorCalls
			serve(want)
			if actorCalls != before {
				t.Fatal("failure reached actor")
			}
			redeemer.redeem = good
			serve(204)
		})
	}
}

func TestTicketEndpointDeclarationAndMetadata(t *testing.T) {
	binding, redeemer := ticketEndpointSetup(t)
	for _, method := range []h.Method{h.POST, h.HEAD, h.PUT} {
		endpoint := h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "ticket", Method: method, Access: h.Guarded}, h.StaticPath("/ticket")), h.EmptyQuery(), h.EmptyBody(), h.EmptyResponse(204))
		if h.ValidateTicketAuthenticated(h.TicketAuthenticated(endpoint, binding, redeemer)) == nil {
			t.Fatal("non-GET ticket route accepted")
		}
	}
	query := h.DefineQuery(h.QueryParam("ticket", h.StringQuery[string](), func(q *string) *string { return q }))
	duplicate := h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "ticket", Method: h.GET, Access: h.Guarded}, h.StaticPath("/ticket")), query, h.EmptyBody(), h.EmptyResponse(204))
	if h.ValidateTicketAuthenticated(h.TicketAuthenticated(duplicate, binding, redeemer)) == nil {
		t.Fatal("domain ticket parameter accepted")
	}
	for _, endpoint := range []h.TicketEndpoint[h.NoPath, h.NoQuery, h.NoBody, authAccount, h.NoContent]{
		h.TicketAuthenticated(authEndpoint(h.Public), binding, redeemer),
		h.TicketAuthenticated(authEndpoint(h.Guarded), binding, nil),
		h.TicketAuthenticated(authEndpoint(h.Guarded), h.GuardBinding[authAccount]{}, redeemer),
		h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer, nil),
	} {
		if h.ValidateTicketAuthenticated(endpoint) == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	redeemer.source = "wrong"
	sticky := h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer)
	redeemer.source = "ticket.source"
	if h.ValidateTicketAuthenticated(sticky) == nil {
		t.Fatal("lost sticky validation")
	}
	endpoint := h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer)
	info, err := h.InspectTicketAuthenticated(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	credential := info.Route.Authentication.Credential
	if credential.Source != "ticket.source" || credential.Kind != h.TicketCredentialKind || credential.Name != "ticket" || credential.OriginProtection {
		t.Fatal("ticket metadata")
	}
	if err := credential.Validate(); err != nil {
		t.Fatal(err)
	}
	credential.Name = "other"
	if credential.Validate() == nil {
		t.Fatal("ticket name accepted")
	}
	credential.Name = "ticket"
	credential.OriginProtection = true
	if credential.Validate() == nil {
		t.Fatal("ticket origin protection accepted")
	}
	if _, err := endpoint.URL(t.Context(), h.NoPath{}, h.NoQuery{}, secret.String{}); err == nil {
		t.Fatal("empty ticket URL")
	}
}

func TestTicketPrivacyHeadersWinOverRouteMiddleware(t *testing.T) {
	binding, redeemer := ticketEndpointSetup(t)
	for _, status := range []int{200, 403} {
		middleware := h.DefineMiddleware("test.response", func(stdhttp.Handler) (stdhttp.Handler, error) {
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Cache-Control", "public, max-age=3600")
				w.Header().Set("Referrer-Policy", "unsafe-url")
				w.WriteHeader(status)
			}), nil
		})
		endpoint := h.TicketAuthenticated(h.ConfigureEndpoint(authEndpoint(h.Guarded), h.Use(middleware)), binding, redeemer)
		router := newAuthRouter(t, endpoint.Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) {
			t.Error("unexpected handler")
			return h.NoContent{}, nil
		}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/profile", nil))
		if w.Code != status || w.Result().Header.Get("Cache-Control") != "no-store" || w.Result().Header.Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("privacy headers overwritten")
		}
	}
}

func TestTicketEndpointIgnoresCookieAndCSRFTransport(t *testing.T) {
	binding, redeemer := ticketEndpointSetup(t)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), binding.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	cookie := h.DefineCookie("session", h.SecretCookie(), h.DefaultCookieOptions())
	transport, err := h.NewCookieAuthentication(registry, h.CSRFConfig{}, h.CookieCredential(redeemer.source, cookie))
	if err != nil {
		t.Fatal(err)
	}
	binding, err = h.BindGuard(transport, binding.Guard())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer)
	router := newAuthRouter(t, endpoint.Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) { return h.NoContent{}, nil }))
	r := httptest.NewRequest("GET", "https://example.test/profile?ticket=one", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://untrusted.test")
	r.Header.Set("Cookie", "session=a; session=b")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("ticket entered browser credential transport", w.Code)
	}
}
