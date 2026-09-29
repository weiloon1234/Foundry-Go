package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type authAccount struct {
	ID      int64
	Enabled bool
}

func (a authAccount) FoundryIdentity() (model.Identity, error) { return a.reference().Identity() }
func (a authAccount) reference() model.Reference[authAccount, int64] {
	return model.NewReference[authAccount]("auth_accounts", a.ID, codec.Signed[int64]())
}

type authInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]

func authEndpoint(access foundryhttp.Access) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent] {
	return foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "profile", Method: foundryhttp.GET, Access: access}, foundryhttp.StaticPath("/profile")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
}
func authSetup(t *testing.T, source auth.CredentialName, loads *atomic.Int32) (*auth.Registry, auth.Guard[authAccount], auth.Policy[authAccount, int64]) {
	t.Helper()
	p := auth.DefineProvider("accounts", (authAccount{}).reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		loads.Add(1)
		if id == 3 {
			return value.Optional[authAccount]{}, nil
		}
		return value.Set(authAccount{ID: id, Enabled: id != 4}), nil
	}, func(_ context.Context, a authAccount) (bool, error) { return a.Enabled, nil })
	strategy := auth.DefineStrategy(source, func(_ context.Context, credential secret.String) (value.Optional[auth.Proof[authAccount, int64]], error) {
		state := auth.Authenticated
		id := int64(1)
		switch credential.Reveal() {
		case "valid":
		case "other":
			id = 2
		case "deleted":
			id = 3
		case "disabled":
			id = 4
		case "pending":
			state = auth.PendingMFA
		default:
			return value.Optional[auth.Proof[authAccount, int64]]{}, nil
		}
		p, err := auth.NewProof(authAccount{ID: id}.reference(), state)
		return value.Set(p), err
	})
	guard := auth.DefineGuard("api", p, strategy)
	policy := auth.DefinePolicy("profile.read", func(_ context.Context, a authAccount, resource int64) (bool, error) { return a.ID == resource, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), policy.Registration())
	if err != nil {
		t.Fatal(err)
	}
	return registry, guard, policy
}
func newAuthRouter(t *testing.T, registration foundryhttp.RouteRegistration) *foundryhttp.Router {
	t.Helper()
	r, err := foundryhttp.NewRouter(registration)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func authServe(h http.Handler, headers []string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", "/profile", nil)
	for _, header := range headers {
		request.Header.Add("Authorization", header)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	return recorder
}

func TestHTTPAuthenticationSuppliesModelOnceAndIsolatesRequests(t *testing.T) {
	var loads atomic.Int32
	registry, guard, policy := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard)
	var contexts []context.Context
	router := newAuthRouter(t, endpoint.Handle(func(ctx context.Context, subject authAccount, _ authInput) (foundryhttp.NoContent, error) {
		contexts = append(contexts, ctx)
		if info, ok := foundryhttp.MatchedRoute(ctx); !ok || info.Access != foundryhttp.Guarded || info.Authentication == nil || info.Authentication.Guard != guard.Name() {
			t.Error("lost guard metadata")
		}
		if err := policy.Authorize(ctx, guard, subject.ID); err != nil {
			return foundryhttp.NoContent{}, err
		}
		again, err := guard.Require(ctx)
		if err != nil || again != subject {
			t.Error("model not reused", err)
		}
		return foundryhttp.NoContent{}, nil
	}))
	for _, token := range []string{"valid", "other", "valid"} {
		if w := authServe(router, []string{"Bearer " + token}); w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if loads.Load() != 3 {
		t.Fatal("expected one model load per request", loads.Load())
	}
	for _, ctx := range contexts {
		if _, err := guard.Require(context.WithoutCancel(ctx)); err == nil {
			t.Fatal("handler retained authority after request")
		}
	}
	info, err := endpoint.Description()
	if err != nil || info.Route.Authentication == nil || info.Route.Access != foundryhttp.Guarded {
		t.Fatal(info, err)
	}
	if info.Response != nil || info.Body != nil {
		t.Fatal("stored model became wire DTO")
	}
	info.Route.Authentication.Guard = "changed"
	if router.Routes()[0].Authentication.Guard != "api" {
		t.Fatal("mutable authentication metadata")
	}
	catalog := router.ErrorDefinitions()
	found := false
	for _, definition := range catalog {
		if definition.Code == foundryhttp.MFARequired {
			found = true
		}
	}
	if !found {
		t.Fatal("MFA error absent from shared contracts")
	}
}

func TestHTTPAuthenticationFailureContractsBeforeHandler(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	var handled atomic.Int32
	router := newAuthRouter(t, foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		handled.Add(1)
		return foundryhttp.NoContent{}, nil
	}))
	for _, test := range []struct {
		name    string
		headers []string
		code    foundryhttp.ErrorCode
	}{
		{"missing", nil, foundryhttp.Unauthenticated},
		{"invalid", []string{"Bearer private-value"}, foundryhttp.Unauthenticated},
		{"duplicate", []string{"Bearer valid", "Bearer valid"}, foundryhttp.Unauthenticated},
		{"empty", []string{"Bearer "}, foundryhttp.Unauthenticated},
		{"wrong-scheme", []string{"Basic valid"}, foundryhttp.Unauthenticated},
		{"whitespace", []string{"Bearer valid other"}, foundryhttp.Unauthenticated},
		{"padding", []string{"Bearer valid=other"}, foundryhttp.Unauthenticated},
		{"padding-only", []string{"Bearer ==="}, foundryhttp.Unauthenticated},
		{"oversize", []string{"Bearer " + strings.Repeat("x", auth.MaxCredentialBytes+1)}, foundryhttp.Unauthenticated},
		{"deleted", []string{"Bearer deleted"}, foundryhttp.Unauthenticated},
		{"disabled", []string{"Bearer disabled"}, foundryhttp.Unauthenticated},
		{"pending", []string{"Bearer pending"}, foundryhttp.MFARequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := authServe(router, test.headers)
			var body foundryhttp.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code == foundryhttp.Unauthenticated && w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing bearer challenge")
			}
			if body.Code != test.code || w.Code != body.Status || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	if handled.Load() != 0 {
		t.Fatal("failure reached domain handler")
	}
	if loads.Load() != 2 {
		t.Fatal("invalid/pending input loaded models", loads.Load())
	}
}

func TestHTTPOptionalAuthenticationRejectsInvalidCredentials(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	var present []bool
	router := newAuthRouter(t, foundryhttp.OptionalAuthentication(authEndpoint(foundryhttp.Public), transport, guard).Handle(func(_ context.Context, subject value.Optional[authAccount], _ authInput) (foundryhttp.NoContent, error) {
		present = append(present, subject.IsSet())
		return foundryhttp.NoContent{}, nil
	}))
	for _, test := range []struct {
		headers []string
		status  int
	}{{nil, 204}, {[]string{"bearer   valid"}, 204}, {[]string{"Bearer invalid"}, 401}, {[]string{"Bearer pending"}, 403}} {
		w := authServe(router, test.headers)
		if w.Code != test.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(present) != 2 || present[0] || !present[1] {
		t.Fatal("optional authentication collapsed failures", present)
	}
	info := router.Routes()[0]
	if info.Access != foundryhttp.Public || info.Authentication == nil || !info.Authentication.Optional {
		t.Fatal(info)
	}
}

func TestHTTPGuardedRegistrationCannotBypassTypedAdapter(t *testing.T) {
	endpoint := authEndpoint(foundryhttp.Guarded)
	if _, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, authInput) (foundryhttp.NoContent, error) { return foundryhttp.NoContent{}, nil })); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbound endpoint served", err)
	}
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "raw", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/raw"))
	if _, err := foundryhttp.NewRouter(route.HandleRaw(func(http.ResponseWriter, *http.Request, foundryhttp.NoPath) {})); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbound raw route served", err)
	}
	if _, err := endpoint.URL(t.Context(), foundryhttp.NoPath{}, foundryhttp.NoQuery{}); err != nil {
		t.Fatal("guarded URL construction required runtime", err)
	}
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("other"))
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Public), transport, guard).Validate(),
		foundryhttp.OptionalAuthentication(endpoint, transport, guard).Validate(),
		foundryhttp.RequireAuthentication(endpoint, wrong, guard).Validate(),
		foundryhttp.RequireAuthentication(endpoint, nil, guard).Validate(),
	} {
		if err == nil {
			t.Fatal("invalid authentication binding accepted")
		}
	}
	if _, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("same"), foundryhttp.BearerCredential("same")); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := foundryhttp.NewAuthentication(registry, foundryhttp.CredentialSource{}); err == nil {
		t.Fatal("zero source accepted")
	}
}

func TestCookieCredentialsReuseBoundedTypedCookieParser(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "session", &loads)
	cookie := foundryhttp.DefineCookie("session", foundryhttp.SecretCookie(), foundryhttp.DefaultCookieOptions())
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.CookieCredential("session", cookie).WithoutOriginProtection())
	if err != nil {
		t.Fatal(err)
	}
	router := newAuthRouter(t, foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	for _, test := range []struct {
		header string
		status int
	}{{"session=valid", 204}, {"session=", 401}, {"session=valid; session=valid", 401}, {"session=invalid", 401}, {"other=valid", 401}} {
		r := httptest.NewRequest("GET", "/profile", nil)
		r.Header.Set("Cookie", test.header)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatal(test.header, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	if err := cookie.Set(t.Context(), w, secret.New("private-secret")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "session=private-secret") {
		t.Fatal("secret cookie serialized redaction instead of credential")
	}
}

type pathologicalAuthError struct{ exit bool }

func (pathologicalAuthError) Error() string { return "private error" }
func (e pathologicalAuthError) Is(error) bool {
	if e.exit {
		runtime.Goexit()
	}
	panic("private error payload")
}
func TestHTTPAuthErrorClassificationOwnsCallbackFailures(t *testing.T) {
	for _, mode := range []string{"forbidden", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			var loads atomic.Int32
			registry, guard, _ := authSetup(t, "bearer", &loads)
			transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
			if err != nil {
				t.Fatal(err)
			}
			router := newAuthRouter(t, foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
				if mode == "forbidden" {
					return foundryhttp.NoContent{}, auth.Forbidden.WithCause(errors.New("private permission"))
				}
				return foundryhttp.NoContent{}, pathologicalAuthError{exit: mode == "goexit"}
			}))
			w := authServe(router, []string{"Bearer valid"})
			want := 500
			if mode == "forbidden" {
				want = 403
			}
			if w.Code != want || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestHTTPAuthenticationRejectsURLCredentialsBeforeDecoding(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	router := newAuthRouter(t, foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		t.Error("URL credential authenticated")
		return foundryhttp.NoContent{}, nil
	}))
	request := httptest.NewRequest("GET", "/profile?access_token=valid", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != 401 || loads.Load() != 0 {
		t.Fatal("URL credential accepted or query decoded before authentication", recorder.Code)
	}
}

func TestPublicEndpointAuthErrorsUseSharedHTTPCatalog(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{auth.Unauthenticated, 401}, {auth.Forbidden, 403}, {auth.MFARequired, 403}, {context.Canceled, 503},
		// The credential cap is a client conflict even when a store wraps it.
		{auth.CredentialLimit, 409}, {fault.Wrap(fault.Internal, "credential operation failed", auth.CredentialLimit), 409},
		{auth.ConfirmationRequired, 403}, {auth.NewDenial("documents.archived", "Archived."), 403}} {
		route := authEndpoint(foundryhttp.Public).Handle(func(context.Context, authInput) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, test.err
		})
		w := authServe(newAuthRouter(t, route), nil)
		if w.Code != test.status {
			t.Fatal("public auth error lost contract", w.Code, w.Body.String())
		}
	}
}
func TestAuthenticationPreservesExplicitDomainError(t *testing.T) {
	var loads atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := foundryhttp.DefineError("account.suspended", 423, "Account suspended")
	endpoint := authEndpoint(foundryhttp.Guarded).WithErrors(declaration)
	route := foundryhttp.RequireAuthentication(endpoint, transport, guard).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, declaration.WithCause(auth.Forbidden)
	})
	w := authServe(newAuthRouter(t, route), []string{"Bearer valid"})
	if w.Code != 423 {
		t.Fatal("explicit domain status overwritten", w.Code, w.Body.String())
	}
}

type cyclicAuthLookupError struct{ visits atomic.Int32 }

func (*cyclicAuthLookupError) Error() string { panic("private auth error must not be formatted") }
func (e *cyclicAuthLookupError) Unwrap() error {
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

func TestCyclicGuardLookupCompletesFailureAndReleasesRequestAuthority(t *testing.T) {
	cycle := new(cyclicAuthLookupError)
	var fail atomic.Bool
	fail.Store(true)
	provider := auth.DefineProvider("cyclic_accounts", (authAccount{}).reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		if fail.Load() {
			return value.Optional[authAccount]{}, cycle
		}
		return value.Set(authAccount{ID: id, Enabled: true}), nil
	}, func(context.Context, authAccount) (bool, error) { return true, nil })
	strategy := auth.DefineStrategy("bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[authAccount, int64]], error) {
		proof, err := auth.NewProof(authAccount{ID: 1}.reference(), auth.Authenticated)
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("cyclic_api", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var owned context.Context
	router := newAuthRouter(t, foundryhttp.RequireAuthentication(authEndpoint(foundryhttp.Guarded), transport, guard).Handle(func(ctx context.Context, _ authAccount, _ authInput) (foundryhttp.NoContent, error) {
		owned = ctx
		calls.Add(1)
		return foundryhttp.NoContent{}, nil
	}))
	response := authServe(router, []string{"Bearer valid"})
	if response.Code != 500 || calls.Load() != 0 || cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("cyclic lookup did not terminate before entering handler")
	}
	fail.Store(false)
	response = authServe(router, []string{"Bearer valid"})
	if response.Code != 204 || calls.Load() != 1 {
		t.Fatal("lookup failure prevented the next request")
	}
	if _, err := guard.Require(context.WithoutCancel(owned)); err == nil {
		t.Fatal("request authority survived completion")
	}
}
