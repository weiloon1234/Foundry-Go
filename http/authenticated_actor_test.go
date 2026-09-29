package http_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	"github.com/weiloon1234/Foundry-Go/value"
)

func actorLimiter(t *testing.T, name ratelimit.Name, limit ratelimit.Limit) ratelimit.Limiter[foundryhttp.ActorKey] {
	t.Helper()
	local, err := memory.New(100, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { local.Close() })
	store, err := ratelimit.NewStore(local, ratelimit.DefaultConfig(keyspace.Namespace{Application: "http", Environment: "actor"}))
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.Define(name, foundryhttp.ActorKeys(), limit).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

// Actor-stage middleware runs after authentication and reads the concrete
// model from the request scope without another provider lookup; ordinary
// route middleware still runs before authentication.
func TestActorMiddlewareRunsAfterAuthenticationWithConcreteModel(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	before := foundryhttp.DefineMiddleware("app.before", func(next http.Handler) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := guard.Require(r.Context()); err == nil {
				t.Error("pre-authentication middleware saw an actor")
			}
			order = append(order, "before")
			next.ServeHTTP(w, r)
		}), nil
	})
	tenant := foundryhttp.DefineMiddleware("app.tenant", func(next http.Handler) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, err := guard.Require(r.Context())
			if err != nil || actor.ID != 1 {
				t.Error("actor stage lacks the authenticated model", err)
			}
			order = append(order, "tenant")
			next.ServeHTTP(w, r)
		}), nil
	})
	route := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded).WithMiddleware(before), transport, guard).WithActorMiddleware(tenant).Handle(func(_ context.Context, actor authAccount, _ authInput) (foundryhttp.NoContent, error) {
		order = append(order, "handler")
		return foundryhttp.NoContent{}, nil
	})
	router := newAuthRouter(t, route)
	if w := authServe(router, []string{"Bearer valid"}); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(order) != 3 || order[0] != "before" || order[1] != "tenant" || order[2] != "handler" || loads.Load() != 1 {
		t.Fatal("unexpected middleware order or model lookups", order, loads.Load())
	}
	order = nil
	if w := authServe(router, nil); w.Code != 401 || len(order) != 1 {
		t.Fatal("actor stage ran without authentication", w.Code, order)
	}
	if registration := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).WithActorMiddleware().Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}); func() error { _, err := foundryhttp.NewRouter(registration); return err }() == nil {
		t.Fatal("empty actor middleware declaration accepted")
	}
}

// Actor quotas are per authenticated model and fall back to the client network
// for anonymous requests; installed before authentication, the request fails
// instead of silently limiting by IP.
func TestRateLimitByActorKeysAuthenticatedModels(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	limiter := actorLimiter(t, "actors", ratelimit.PerMinute(1))
	handler := func(context.Context, value.Optional[authAccount], authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}
	router := newAuthRouter(t, foundryhttp.OptionalAuthentication(authEndpoint(foundryhttp.Public), transport, guard).WithActorMiddleware(foundryhttp.RateLimitByActor(limiter, guard)).Handle(handler))
	for _, step := range []struct {
		header string
		status int
	}{{"Bearer valid", 204}, {"Bearer valid", 429}, {"Bearer other", 204}, {"", 204}, {"", 429}} {
		var headers []string
		if step.header != "" {
			headers = []string{step.header}
		}
		if w := authServe(router, headers); w.Code != step.status {
			t.Fatal(step.header, w.Code, w.Body.String())
		}
	}
	misplaced := actorLimiter(t, "misplaced", ratelimit.PerMinute(10))
	router = newAuthRouter(t, foundryhttp.OptionalAuthentication(authEndpoint(foundryhttp.Public).WithMiddleware(foundryhttp.RateLimitByActor(misplaced, guard)), transport, guard).Handle(handler))
	if w := authServe(router, []string{"Bearer valid"}); w.Code < 500 {
		t.Fatal("pre-authentication actor limit fell back to another key", w.Code)
	}
}

// Sensitive browser routes require a recent password confirmation on the
// current session; the confirmation window uses the session store's clock.
func TestBrowserRouteRequiresRecentPasswordConfirmation(t *testing.T) {
	s := browserPrepare(t)
	proof := browserProof(t)
	login := browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
		_, err := s.web.Login(ctx, proof, session.IssueOptions{})
		return foundryhttp.NoContent{}, err
	})
	confirm := foundryhttp.RequireAuthentication(browserEndpoint("/confirm", foundryhttp.POST, foundryhttp.Guarded), s.web.Authentication(), s.web.Guard()).Handle(func(ctx context.Context, _ authAccount, _ authInput) (foundryhttp.NoContent, error) {
		// A real route re-verifies the password with auth.PasswordLogin.Confirm first.
		_, err := s.sessions.ConfirmCurrent(ctx)
		return foundryhttp.NoContent{}, err
	})
	sensitive := foundryhttp.RequireAuthentication(browserEndpoint("/sensitive", foundryhttp.POST, foundryhttp.Guarded), s.web.Authentication(), s.web.Guard()).WithActorMiddleware(s.web.RequirePasswordConfirmation(15 * time.Minute)).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
	router, err := foundryhttp.NewRouter(login, confirm, sensitive)
	if err != nil {
		t.Fatal(err)
	}
	cookie := issuedCookie(t, browserServe(router, "POST", "/login", nil))
	if w := browserServe(router, "POST", "/sensitive", cookie); w.Code != 403 {
		t.Fatal("unconfirmed session reached a sensitive route", w.Code)
	}
	if w := browserServe(router, "POST", "/confirm", cookie); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := browserServe(router, "POST", "/sensitive", cookie); w.Code != 204 {
		t.Fatal("confirmed session rejected", w.Code, w.Body.String())
	}
	s.clock.Advance(16 * time.Minute)
	if w := browserServe(router, "POST", "/sensitive", cookie); w.Code != 403 {
		t.Fatal("stale confirmation accepted", w.Code)
	}
	if w := browserServe(router, "POST", "/sensitive", nil); w.Code != 401 {
		t.Fatal("anonymous request reached confirmation", w.Code)
	}
}
