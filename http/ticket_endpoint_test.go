package http_test

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

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
	return ticketEndpointSetupWithSource(t, "ticket.source")
}

func ticketEndpointSetupWithSource(t *testing.T, source auth.CredentialName) (h.GuardBinding[authAccount], *endpointRedeemer) {
	t.Helper()
	provider := auth.DefineProvider("accounts", authAccount{}.reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		return value.Set(authAccount{ID: id, Enabled: true}), nil
	}, func(context.Context, authAccount) (bool, error) { return true, nil })
	strategy, binder := auth.BindStrategy(auth.DefineStrategy(source, func(context.Context, secret.String) (value.Optional[auth.Proof[authAccount, int64]], error) {
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
	transport, err := h.NewAuthentication(registry, h.BearerCredential(source))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.BindGuard(transport, guard)
	if err != nil {
		t.Fatal(err)
	}
	return binding, &endpointRedeemer{source: source, redeem: func(context.Context, secret.String) (value.Optional[auth.BoundCredential], error) {
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
		if w.Result().Header.Get("Cache-Control") != "no-store" || w.Result().Header.Get("Referrer-Policy") != "no-referrer" || w.Result().Header.Get("WWW-Authenticate") != "" || strings.Contains(w.Body.String(), "private-canary") {
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
	if w.Code != 204 || w.Result().Header.Get("Set-Cookie") != "" {
		t.Fatal("ticket entered browser credential transport", w.Code)
	}
}

// ticketRowsReader checks the guard on every bounded read, including after the
// first response bytes have reached the writer.
type ticketRowsReader struct {
	reader     *strings.Reader
	ctx        context.Context
	guard      auth.Guard[authAccount]
	t          *testing.T
	response   *httptest.ResponseRecorder
	reads      int
	laterChunk bool
	closed     bool
}

func (r *ticketRowsReader) Read(p []byte) (int, error) {
	r.reads++
	actor, err := r.guard.Require(r.ctx)
	if err != nil || actor.ID != 7 {
		r.t.Error("streaming read lost guard scope", err)
	}
	if r.reads > 1 && r.response.Body.Len() > 0 {
		r.laterChunk = true
	}
	return r.reader.Read(p[:min(len(p), len("row-one\n"))])
}

func (r *ticketRowsReader) Seek(offset int64, whence int) (int64, error) {
	return r.reader.Seek(offset, whence)
}

func (r *ticketRowsReader) Close() error {
	r.closed = true
	_, err := r.guard.Require(r.ctx)
	if err != nil {
		r.t.Error("download cleanup lost guard scope", err)
	}
	return nil
}

func TestTicketScopeSurvivesStreamingAndCloses(t *testing.T) {
	for _, mode := range []string{"success", "handler-error", "handler-panic"} {
		t.Run(mode, func(t *testing.T) {
			binding, redeemer := ticketEndpointSetup(t)
			endpoint := h.TicketAuthenticated(h.DefineEndpoint(h.DefineRoute(h.RouteSpec{ID: "stream", Method: h.GET, Access: h.Guarded}, h.StaticPath("/stream")), h.EmptyQuery(), h.EmptyBody(), h.DownloadResponse("text/csv; charset=utf-8")), binding, redeemer)
			w := httptest.NewRecorder()
			var held context.Context
			var body *ticketRowsReader
			router := newAuthRouter(t, endpoint.Handle(func(ctx context.Context, _ authAccount, _ authInput) (h.Download, error) {
				// Ignore route cancellation so the post-request lookup proves scope
				// closure itself, rather than a canceled request budget.
				held = context.WithoutCancel(ctx)
				if mode == "handler-error" {
					return h.Download{}, h.Forbidden
				}
				if mode == "handler-panic" {
					panic("ticket handler failed")
				}
				return h.DownloadFrom(func(ctx context.Context) (h.DownloadContent, error) {
					body = &ticketRowsReader{reader: strings.NewReader("row-one\nrow-two\nrow-end\n"), ctx: ctx, guard: binding.Guard(), t: t, response: w}
					return h.DownloadContent{Body: body, MediaType: "text/csv; charset=utf-8"}, nil
				}), nil
			}))
			router.ServeHTTP(w, httptest.NewRequest("GET", "/stream?ticket=one", nil))
			want := 200
			if mode == "handler-error" {
				want = 403
			} else if mode == "handler-panic" {
				want = 500
			}
			if w.Result().StatusCode != want || held == nil {
				t.Fatalf("handler %s status=%d", mode, w.Code)
			}
			if mode == "success" && (body == nil || !body.closed || !body.laterChunk || body.reads < 3 || w.Body.String() != "row-one\nrow-two\nrow-end\n") {
				t.Fatal("download did not retain scope through multiple streamed chunks and cleanup")
			}
			if _, err := binding.Guard().Require(held); err == nil {
				t.Fatal("authentication scope remained usable after response completion")
			}
		})
	}
}

func TestTicketEndpointBypassesBrowserSessionAdapter(t *testing.T) {
	s := browserPrepare(t)
	initial, redeemer := ticketEndpointSetupWithSource(t, s.sessions.Guard().Source())
	registry, err := auth.NewRegistry(auth.DefaultConfig(), s.sessions.Guard().Registration(), initial.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	config := h.DefaultBrowserSessionConfig()
	config.Clock = s.clock
	browser, err := h.NewBrowserSessions(registry, s.sessions, config)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.BindGuard(browser.Authentication(), initial.Guard())
	if err != nil {
		t.Fatal(err)
	}
	ticket := h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer)
	calls := 0
	router := newAuthRouter(t, ticket.Handle(func(ctx context.Context, actor authAccount, _ authInput) (h.NoContent, error) {
		calls++
		if actor.ID != 7 {
			t.Error("ticket actor lost")
		}
		return h.NoContent{}, nil
	}))
	request := func(handler stdhttp.Handler) *httptest.ResponseRecorder {
		// The actual adapter rejects this insecure request and malformed,
		// repeated cookies if its wrapper or credential reader runs.
		r := httptest.NewRequest("GET", "http://app.test/profile?ticket=one", nil)
		r.Header.Set("Cookie", string(config.Cookie)+"=invalid; "+string(config.Cookie)+"=again")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	ordinary := newAuthRouter(t, h.Authenticated(authEndpoint(h.Guarded), binding).Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) {
		t.Error("ordinary browser authentication accepted malformed transport")
		return h.NoContent{}, nil
	}))
	if w := request(ordinary); w.Code == 204 {
		t.Fatal("browser adapter control did not reject request")
	}
	w := request(router)
	if w.Result().StatusCode != 204 || calls != 1 || s.loads.Load() != 0 || w.Result().Header.Get("Set-Cookie") != "" {
		t.Fatal("ticket route did not bypass the actual browser adapter")
	}
}

func TestTicketRoutingPreservesDomainTicketParameters(t *testing.T) {
	binding, redeemer := ticketEndpointSetup(t)
	ticket := h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer)
	registrations := []h.RouteRegistration{ticket.Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) {
		t.Error("routing probe invoked ticket handler")
		return h.NoContent{}, nil
	})}
	for _, path := range []string{"/folder/", "/support"} {
		registrations = append(registrations, h.HandleRaw(h.DefineRoute(h.RouteSpec{ID: h.RouteID(strings.Trim(path, "/")), Method: h.GET, Access: h.Public}, h.StaticPath(path)), func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ h.NoPath) { w.WriteHeader(204) }))
	}
	router, err := h.NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := h.ApplyMiddleware(router, h.CacheControl(h.CachePolicy{Visibility: h.CachePublic, MaxAge: time.Hour}))
	if err != nil {
		t.Fatal(err)
	}
	serve := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		w.Header().Set("Referrer-Policy", "origin")
		w.Header().Set("WWW-Authenticate", "Basic realm=\"support\"")
		handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	for _, query := range []string{"ticket=case-123", "%74icket=case-123"} {
		redirect := serve("GET", "/folder?"+query)
		if redirect.Result().StatusCode != 307 || redirect.Result().Header.Get("Location") != "/folder/?"+query || redirect.Result().Header.Get("Cache-Control") != "public, max-age=3600" || redirect.Result().Header.Get("Referrer-Policy") != "origin" || redirect.Result().Header.Get("WWW-Authenticate") != "Basic realm=\"support\"" {
			t.Fatal("non-ticket redirect changed")
		}
		for _, tc := range []struct {
			method, path string
			status       int
		}{{"POST", "/support", 405}, {"OPTIONS", "/support", 405}, {"GET", "/unknown", 404}} {
			baseline, actual := serve(tc.method, tc.path), serve(tc.method, tc.path+"?"+query)
			if actual.Result().StatusCode != tc.status || !reflect.DeepEqual(actual.Result().Header, baseline.Result().Header) || actual.Body.String() != baseline.Body.String() || actual.Result().Header.Get("Referrer-Policy") != "origin" || actual.Result().Header.Get("WWW-Authenticate") != "Basic realm=\"support\"" {
				t.Fatalf("domain ticket parameter changed %s %s", tc.method, tc.path)
			}
		}
	}
}

func TestRouterTicketPrivacyOnNativeError(t *testing.T) {
	binding, redeemer := ticketEndpointSetup(t)
	router := newAuthRouter(t, h.TicketAuthenticated(authEndpoint(h.Guarded), binding, redeemer).Handle(func(context.Context, authAccount, authInput) (h.NoContent, error) {
		t.Error("native error reached handler")
		return h.NoContent{}, nil
	}))
	handler, err := h.ApplyMiddleware(router, h.CacheControl(h.CachePolicy{Visibility: h.CachePublic, MaxAge: time.Hour}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/profile?%74icket=private-canary", nil)
	// Exercise ServeMux's native 400 branch for a ticket-route destination.
	request.RequestURI = "*"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Result().StatusCode != 400 || w.Result().Header.Get("Cache-Control") != "no-store" || w.Result().Header.Get("Referrer-Policy") != "no-referrer" || w.Result().Header.Get("Location") != "" || strings.Contains(w.Body.String(), "private-canary") {
		t.Fatal("native routing error bypassed ticket-route privacy")
	}
}
