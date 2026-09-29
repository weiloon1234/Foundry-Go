package http_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type browserSetup struct {
	web      *foundryhttp.BrowserSessions[authAccount, int64]
	sessions *session.Sessions[authAccount, int64]
	clock    *testkit.Clock
	loads    atomic.Int32
}

func browserPrepare(t *testing.T) *browserSetup {
	t.Helper()
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, definition := range sessionpg.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &browserSetup{clock: testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC))}
	config := sessionpg.DefaultConfig()
	config.Schema = schema
	config.Clock = s.clock
	backend, err := sessionpg.New(db, config)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(backend, session.DefaultConfig(keyspace.Namespace{Application: "http-browser-test", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("browser.accounts", (authAccount{}).reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		s.loads.Add(1)
		return value.Set(authAccount{ID: id, Enabled: true}), nil
	}, func(_ context.Context, a authAccount) (bool, error) { return a.Enabled, nil })
	s.sessions, err = session.New(store, "browser.web", provider, "browser.cookie")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := auth.NewRegistry(auth.DefaultConfig(), s.sessions.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	browser := foundryhttp.DefaultBrowserSessionConfig()
	browser.Clock = s.clock
	s.web, err = foundryhttp.NewBrowserSessions(registry, s.sessions, browser)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func browserProof(t *testing.T) auth.Proof[authAccount, int64] {
	t.Helper()
	proof, err := auth.NewProof(authAccount{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}
func browserEndpoint(path string, method foundryhttp.Method, access foundryhttp.Access) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent] {
	return foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: foundryhttp.RouteID(path[1:]), Method: method, Access: access}, foundryhttp.StaticPath(path)), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
}
func browserServe(h http.Handler, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://app.test"+path, nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func issuedCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("expected one session cookie")
	}
	return cookies[0]
}
func TestBrowserSessionLoginRotateLogoutWithConcreteModel(t *testing.T) {
	s := browserPrepare(t)
	proof := browserProof(t)
	var held context.Context
	login := browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
		held = ctx
		_, err := s.web.Login(ctx, proof, session.IssueOptions{})
		return foundryhttp.NoContent{}, err
	})
	profile := foundryhttp.RequireAuthentication(browserEndpoint("/profile", foundryhttp.GET, foundryhttp.Guarded), s.web.Authentication(), s.web.Guard()).Handle(func(ctx context.Context, a authAccount, _ authInput) (foundryhttp.NoContent, error) {
		if a.ID != 7 {
			t.Error("wrong authenticated model")
		}
		_, err := s.web.Guard().Require(ctx)
		return foundryhttp.NoContent{}, err
	})
	rotate := foundryhttp.RequireAuthentication(browserEndpoint("/rotate", foundryhttp.POST, foundryhttp.Guarded), s.web.Authentication(), s.web.Guard()).Handle(func(ctx context.Context, _ authAccount, _ authInput) (foundryhttp.NoContent, error) {
		_, err := s.web.Rotate(ctx)
		return foundryhttp.NoContent{}, err
	})
	logout := browserEndpoint("/logout", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, s.web.Logout(ctx)
	})
	router, err := foundryhttp.NewRouter(login, profile, rotate, logout)
	if err != nil {
		t.Fatal(err)
	}
	response := browserServe(router, "POST", "/login", nil)
	first := issuedCookie(t, response)
	if !first.Secure || !first.HttpOnly || first.SameSite != http.SameSiteLaxMode || first.Path != "/" || first.MaxAge != 0 || !first.Expires.IsZero() {
		t.Fatal("session cookie policy")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session response cacheable")
	}
	if _, err := s.web.Login(context.WithoutCancel(held), proof, session.IssueOptions{}); err == nil {
		t.Fatal("retained context issued credential")
	}
	if w := browserServe(router, "GET", "/profile", first); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.loads.Load() != 1 {
		t.Fatal("duplicate model hydration")
	}
	rotated := issuedCookie(t, browserServe(router, "POST", "/rotate", first))
	if first.Value == rotated.Value {
		t.Fatal("rotation reused secret")
	}
	if w := browserServe(router, "GET", "/profile", first); w.Code != 401 {
		t.Fatal("old secret accepted", w.Code)
	}
	if w := browserServe(router, "GET", "/profile", rotated); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	removed := issuedCookie(t, browserServe(router, "POST", "/logout", rotated))
	if removed.Name != rotated.Name || removed.Path != rotated.Path || removed.Domain != rotated.Domain || removed.MaxAge != -1 || removed.Value != "" {
		t.Fatal("cookie clear scope changed")
	}
	if w := browserServe(router, "GET", "/profile", rotated); w.Code != 401 {
		t.Fatal("revoked secret accepted", w.Code)
	}
}
func TestBrowserSessionRememberAndLoginInvalidatePreviousCookie(t *testing.T) {
	s := browserPrepare(t)
	proof := browserProof(t)
	route := browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
		_, err := s.web.Login(ctx, proof, session.IssueOptions{Remember: true})
		return foundryhttp.NoContent{}, err
	})
	router := newAuthRouter(t, route)
	first := issuedCookie(t, browserServe(router, "POST", "/login", nil))
	second := issuedCookie(t, browserServe(router, "POST", "/login", first))
	if second.MaxAge != 30*24*60*60 || !second.Expires.Equal(s.clock.Now().Add(30*24*time.Hour)) {
		t.Fatal("remember cookie lifetime")
	}
	if first.Value == second.Value {
		t.Fatal("login retained secret")
	}
	list, err := s.sessions.List(t.Context(), authAccount{ID: 7}.reference())
	if err != nil || len(list) != 1 {
		t.Fatal("old login session not revoked", err)
	}
}
func TestBrowserSessionFailuresNeverPublishCookie(t *testing.T) {
	for _, kind := range []string{"handler-error", "panic", "goexit", "expired", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			s := browserPrepare(t)
			proof := browserProof(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			route := browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
				if _, err := s.web.Login(ctx, proof, session.IssueOptions{}); err != nil {
					return foundryhttp.NoContent{}, err
				}
				switch kind {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "expired":
					s.clock.Advance(25 * time.Hour)
				case "canceled":
					cancel()
				default:
					return foundryhttp.NoContent{}, foundryhttp.BadRequest
				}
				return foundryhttp.NoContent{}, nil
			})
			r := httptest.NewRequest("POST", "https://app.test/login", nil).WithContext(ctx)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			w := httptest.NewRecorder()
			newAuthRouter(t, route).ServeHTTP(w, r)
			// Session credentials are never published after the request ended.
			want := map[string]int{"handler-error": 400, "panic": 500, "goexit": 500, "expired": 401, "canceled": 503}[kind]
			if w.Header().Get("Set-Cookie") != "" || w.Code != want {
				t.Fatal("failed response contract or cookie", w.Code, w.Body.String())
			}
		})
	}
}
func TestBrowserSessionCSRFAndCookieParsingRunBeforeHandler(t *testing.T) {
	s := browserPrepare(t)
	var calls atomic.Int32
	route := browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(context.Context, authInput) (foundryhttp.NoContent, error) {
		calls.Add(1)
		return foundryhttp.NoContent{}, nil
	})
	router := newAuthRouter(t, route)
	for _, kind := range []string{"cross-origin", "missing-evidence", "malformed-cookie", "insecure"} {
		t.Run(kind, func(t *testing.T) {
			target := "https://app.test/login"
			if kind == "insecure" {
				target = "http://app.test/login"
			}
			r := httptest.NewRequest("POST", target, nil)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			switch kind {
			case "cross-origin":
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				r.Header.Set("Origin", "https://evil.test")
			case "missing-evidence":
				r.Header.Del("Sec-Fetch-Site")
			case "malformed-cookie":
				r.Header.Set("Cookie", "__Host-foundry_session=invalid")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			want := map[string]int{"cross-origin": 403, "missing-evidence": 403, "malformed-cookie": 401, "insecure": 400}[kind]
			if w.Code != want || calls.Load() != 0 {
				t.Fatal("untrusted input contract", w.Code, w.Body.String())
			}
		})
	}
}
func TestBrowserSessionRejectsSafeAndRawMutation(t *testing.T) {
	s := browserPrepare(t)
	proof := browserProof(t)
	route := browserEndpoint("/login", foundryhttp.GET, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
		_, err := s.web.Login(ctx, proof, session.IssueOptions{})
		if err == nil {
			t.Error("GET login accepted")
		}
		return foundryhttp.NoContent{}, err
	})
	if w := browserServe(newAuthRouter(t, route), "GET", "/login", nil); w.Code < 400 {
		t.Fatal("GET changed credential")
	}
	var rejected bool
	raw, err := foundryhttp.ApplyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := s.web.Login(r.Context(), proof, session.IssueOptions{})
		rejected = err != nil
		w.WriteHeader(204)
	}), s.web.Middleware())
	if err != nil {
		t.Fatal(err)
	}
	browserServe(raw, "POST", "/login", nil)
	if !rejected {
		t.Fatal("raw login accepted")
	}
	if list, err := s.sessions.List(t.Context(), authAccount{ID: 7}.reference()); err != nil || len(list) != 0 {
		t.Fatal("rejected mutation reached persistence", err)
	}
	if err := s.web.Logout(context.Background()); err == nil {
		t.Fatal("unbound logout accepted")
	}
}

type BrowserSessionReply struct {
	Amount float64 `json:"amount"`
}

func TestBrowserSessionResponseEncodingFailureWithholdsCookie(t *testing.T) {
	s := browserPrepare(t)
	proof := browserProof(t)
	typ := reflect.TypeFor[BrowserSessionReply]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	dto := contract.DefineJSON[BrowserSessionReply](contract.Schema{Root: root, Types: []contract.Type{{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "amount", Type: "number", Required: true}}}, {ID: "number", Kind: contract.NumberKind, Bits: 64}}})
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "login", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/login")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, dto))
	route := endpoint.WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (BrowserSessionReply, error) {
		_, err := s.web.Login(ctx, proof, session.IssueOptions{})
		return BrowserSessionReply{Amount: math.NaN()}, err
	})
	w := browserServe(newAuthRouter(t, route), "POST", "/login", nil)
	if w.Code != 500 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("invalid JSON published credential", w.Code, w.Body.String())
	}
}
func TestBrowserSessionMiddlewareCompositionAndConfig(t *testing.T) {
	s := browserPrepare(t)
	profile := foundryhttp.RequireAuthentication(browserEndpoint("/profile", foundryhttp.GET, foundryhttp.Guarded).WithMiddleware(s.web.Middleware()), s.web.Authentication(), s.web.Guard()).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
	if w := browserServe(newAuthRouter(t, profile), "GET", "/profile", nil); w.Code != 401 {
		t.Fatal("nested binding lost ordinary missing-auth failure", w.Code)
	}
	for _, change := range []func(*foundryhttp.BrowserSessionConfig){func(c *foundryhttp.BrowserSessionConfig) { c.Options.HTTPOnly = false }, func(c *foundryhttp.BrowserSessionConfig) { c.Options.MaxAge = time.Hour }, func(c *foundryhttp.BrowserSessionConfig) { c.Clock = nil }, func(c *foundryhttp.BrowserSessionConfig) { c.Options.Domain = "example.test" }} {
		config := foundryhttp.DefaultBrowserSessionConfig()
		change(&config)
		if err := config.Validate(); err == nil {
			t.Fatal("unsafe browser configuration accepted")
		}
	}
}
