package http

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func staticRoute(id RouteID, method Method, path string) Route[NoPath] {
	return DefineRoute(RouteSpec{ID: id, Method: method, Access: Public}, StaticPath(path))
}

func noContent(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) }

func TestRouterRejectsConflictsAndIncompleteDeclarations(t *testing.T) {
	for name, registrations := range map[string][]RouteRegistration{
		"duplicate ID":       {staticRoute("same", GET, "/a").HandleRaw(noContent), staticRoute("same", GET, "/b").HandleRaw(noContent)},
		"duplicate pattern":  {staticRoute("a", GET, "/a").HandleRaw(noContent), staticRoute("b", GET, "/a").HandleRaw(noContent)},
		"ambiguous patterns": {textRoute("/a/{text}").HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) {}), DefineRoute(RouteSpec{ID: "other", Method: GET, Access: Public}, DefinePath("/{text}/b", Param("text", StringPath[string](), func(p *textPath) *string { return &p.Text }))).HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) {})},
		"empty registration": {{}},
		"nil callback":       {staticRoute("a", GET, "/a").HandleRaw(nil)},
		"implicit public":    {DefineRoute(RouteSpec{ID: "a", Method: GET}, StaticPath("/a")).HandleRaw(noContent)},
		"invalid access":     {DefineRoute(RouteSpec{ID: "a", Method: GET, Access: Access("unknown")}, StaticPath("/a")).HandleRaw(noContent)},
		"invalid method":     {staticRoute("a", Method("CONNECT"), "/a").HandleRaw(noContent)},
		"invalid identity":   {staticRoute("bad ID", GET, "/a").HandleRaw(noContent)},
	} {
		t.Run(name, func(t *testing.T) {
			router, err := NewRouter(registrations...)
			if err == nil || router != nil {
				t.Fatal("invalid router returned a usable partial registry")
			}
			if name == "duplicate ID" && !errors.Is(err, fault.Duplicate) {
				t.Fatalf("duplicate classification: %v", err)
			}
			if (name == "ambiguous patterns" || name == "duplicate pattern") && !errors.Is(err, fault.Conflict) {
				t.Fatalf("conflict classification: %v", err)
			}
		})
	}
}

func TestRouterNativePrecedenceAndHEAD(t *testing.T) {
	var called string
	variable := textRoute("/users/{text}")
	fixed := staticRoute("users.new", GET, "/users/new")
	head := staticRoute("users.head", HEAD, "/users/new")
	for _, order := range [][]RouteRegistration{
		{variable.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, p textPath) { called = p.Text; w.WriteHeader(204) }), fixed.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { called = "fixed"; w.WriteHeader(204) }), head.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { called = "head"; w.WriteHeader(204) })},
		{head.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { called = "head"; w.WriteHeader(204) }), fixed.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { called = "fixed"; w.WriteHeader(204) }), variable.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, p textPath) { called = p.Text; w.WriteHeader(204) })},
	} {
		router, err := NewRouter(order...)
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range []struct{ method, path, want string }{{"GET", "/users/new", "fixed"}, {"HEAD", "/users/new", "head"}, {"HEAD", "/users/other", "other"}} {
			called = ""
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(check.method, check.path, nil))
			if recorder.Code != 204 || called != check.want {
				t.Fatalf("%s %s chose %q (status %d)", check.method, check.path, called, recorder.Code)
			}
		}
	}
}

func TestRouterUsesSharedFailuresAndNativeAllow(t *testing.T) {
	router, err := NewRouter(staticRoute("read", GET, "/item").HandleRaw(noContent), staticRoute("write", POST, "/item").HandleRaw(noContent))
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		method, path string
		code         ErrorCode
		allow        string
	}{
		{"GET", "/missing", NotFound, ""},
		{"DELETE", "/item", MethodNotAllowed, "GET, HEAD, POST"},
		{"OPTIONS", "/item", MethodNotAllowed, "GET, HEAD, POST"},
	} {
		recorder := httptest.NewRecorder()
		requestBoundary(router.ServeHTTP, 8).ServeHTTP(recorder, httptest.NewRequest(check.method, check.path, nil))
		failure := decodeFailure(t, recorder)
		if failure.Code != check.code || failure.RequestID == "" || recorder.Header().Get("Allow") != check.allow {
			t.Fatalf("fallback %s %s: %+v Allow=%q", check.method, check.path, failure, recorder.Header().Get("Allow"))
		}
	}
	for _, path := range []string{"/missing", "/item/extra"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("HEAD", path, nil))
		if recorder.Code != 404 || recorder.Body.Len() != 0 {
			t.Fatal("HEAD fallback wrote a body")
		}
	}
}

func TestRouterExactTrailingSlashAndRelativeRedirect(t *testing.T) {
	router, err := NewRouter(staticRoute("root", GET, "/").HandleRaw(noContent), staticRoute("folder", POST, "/folder/").HandleRaw(noContent))
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		method, path string
		status       int
	}{{"GET", "/", 204}, {"GET", "/unregistered", 404}, {"POST", "/folder/", 204}, {"POST", "/folder/child", 404}} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(check.method, check.path, nil))
		if recorder.Code != check.status {
			t.Fatalf("%s %s status %d", check.method, check.path, recorder.Code)
		}
	}
	request := httptest.NewRequest("POST", "/folder?keep=a%2Fb", nil)
	request.Host = "untrusted.example"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != stdhttp.StatusTemporaryRedirect || recorder.Header().Get("Location") != "/folder/?keep=a%2Fb" {
		t.Fatalf("redirect lost method/query confinement: status %d Location %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestEncodedSlashOnlyCannotSelectAnotherRoute(t *testing.T) {
	router, err := NewRouter(
		staticRoute("trailing", GET, "/prefix/").HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, NoPath) {
			t.Error("encoded separator selected trailing-slash route")
		}),
		textRoute("/prefix/{text}").HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, textPath) {
			t.Error("ambiguous separator reached wildcard handler")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/prefix/%2F", "/prefix/%2f", "/prefix/%2F/"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if failure := decodeFailure(t, recorder); failure.Code != BadRequest {
			t.Fatalf("ambiguous path %q: %+v", path, failure)
		}
	}
}

func TestScopedRouteURLMetadataAndSnapshotOwnership(t *testing.T) {
	original := textRoute("/users/{text}")
	scope := DefineScope("/v1", "v1").Within(DefineScope("/api", "api"))
	route := original.Within(scope)
	location, err := route.URL(textPath{Text: "a/b"})
	if err != nil || location != "/api/v1/users/a%2Fb" || route.ID() != "api.v1.text.show" {
		t.Fatalf("scoped descriptor: %q %v %s", location, err, route.ID())
	}
	if original.Pattern() != "/users/{text}" || original.ID() != "text.show" {
		t.Fatal("scope mutated the original route")
	}
	var count int
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, p textPath) {
		info, ok := MatchedRoute(r.Context())
		if !ok || info.ID != route.ID() || info.Path != route.Pattern() || !info.Raw || p.Text != "a/b" {
			t.Error("matched route metadata or typed value disagrees")
		}
		info.Parameters[0] = "mutated"
		fresh, _ := MatchedRoute(r.Context())
		if !slices.Equal(fresh.Parameters, []string{"text"}) {
			t.Error("context metadata shared a mutable slice")
		}
		count++
		w.WriteHeader(204)
	}), staticRoute("z", GET, "/z").HandleRaw(noContent))
	if err != nil {
		t.Fatal(err)
	}
	info := router.Routes()
	if info[0].ID != route.ID() || info[1].ID != "z" {
		t.Fatal("route inspection is not deterministic")
	}
	info[0].Parameters[0] = "mutated"
	info[0].Path = "/mutated"
	for range 2 {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
		if recorder.Code != 204 {
			t.Fatal("inspection mutated live routes")
		}
	}
	if count != 2 || router.Routes()[0].Path != route.Pattern() {
		t.Fatal("registration snapshot changed")
	}
	if _, ok := MatchedRoute(t.Context()); ok {
		t.Fatal("unmatched context had a route")
	}
}

func TestScopeRejectsInvalidPrefixesAndCannotRepairInvalidRoutes(t *testing.T) {
	for _, scope := range []Scope{DefineScope("/api/", "api"), DefineScope("/{tenant}", "tenant"), DefineScope("api", "api"), DefineScope("/api", "bad name")} {
		if err := textRoute("/{text}").Within(scope).Validate(); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
	for _, route := range []Route[NoPath]{staticRoute("", GET, "/users"), staticRoute("users", GET, "users")} {
		if err := route.Within(DefineScope("/api", "api")).Validate(); err == nil {
			t.Fatal("scope turned invalid route into a valid one")
		}
	}
}

func TestRouterKeepsNativeWriterAndConcurrentIsolation(t *testing.T) {
	var calls atomic.Int64
	route := textRoute("/{text}")
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, p textPath) {
		if p.Text != r.Header.Get("Expected") {
			t.Error("path values crossed concurrent requests")
		}
		if _, ok := w.(*httptest.ResponseRecorder); !ok {
			t.Error("router hid the native response writer")
		}
		if err := stdhttp.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
		calls.Add(1)
	}))
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := range 32 {
		group.Go(func() {
			value := strings.Repeat("x", i+1)
			location, err := route.URL(textPath{Text: value})
			if err != nil {
				t.Error(err)
				return
			}
			request := httptest.NewRequest("GET", location, nil)
			request.Header.Set("Expected", value)
			router.ServeHTTP(httptest.NewRecorder(), request)
			_ = router.Routes()
		})
	}
	group.Wait()
	if calls.Load() != 32 {
		t.Fatalf("concurrent request count: %d", calls.Load())
	}
}

func TestRouterRejectsLegacyMuxMode(t *testing.T) {
	if os.Getenv("FOUNDRY_HTTP_LEGACY_PROBE") == "1" {
		if router, err := NewRouter(); !errors.Is(err, fault.Invalid) || router != nil {
			t.Fatalf("legacy router accepted: %v", err)
		}
		return
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRouterRejectsLegacyMuxMode$")
	command.Env = append(os.Environ(), "FOUNDRY_HTTP_LEGACY_PROBE=1", "GODEBUG=httpmuxgo121=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("legacy mode probe: %v\n%s", err, output)
	}
}
