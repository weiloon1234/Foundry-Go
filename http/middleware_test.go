package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestMiddlewareScopeOrderAndMatchedMetadata(t *testing.T) {
	var trace []string
	constructs := 0
	wrapper := func(id MiddlewareID) Middleware {
		return DefineMiddleware(id, func(next stdhttp.Handler) (stdhttp.Handler, error) {
			constructs++
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
				info, ok := MatchedRoute(request.Context())
				if !ok || info.ID != "api.v1.read" {
					t.Error("matched route unavailable to middleware")
				}
				trace = append(trace, string(id)+":before")
				next.ServeHTTP(w, request)
				trace = append(trace, string(id)+":after")
			}), nil
		})
	}
	parent := DefineScope("/api", "api").WithMiddleware(wrapper("parent"))
	child := DefineScope("/v1", "v1").WithMiddleware(wrapper("child")).Within(parent)
	plain := staticRoute("read", GET, "/item")
	route := plain.WithMiddleware(wrapper("route")).Within(child)
	if err := route.Validate(); err != nil || constructs != 0 {
		t.Fatalf("declaration started middleware: %v", err)
	}
	router, err := NewRouter(route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) {
		trace = append(trace, "handler")
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if constructs != 3 {
		t.Fatalf("constructors: %d", constructs)
	}
	for range 2 {
		trace = nil
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/item", nil))
		if response.Code != 204 || !slices.Equal(trace, []string{"parent:before", "child:before", "route:before", "handler", "route:after", "child:after", "parent:after"}) {
			t.Fatalf("middleware order: status=%d %v", response.Code, trace)
		}
	}
	if constructs != 3 {
		t.Fatal("middleware reconstructed per request")
	}
	info := router.Routes()[0]
	if !slices.Equal(info.Middlewares, []MiddlewareID{"parent", "child", "route"}) {
		t.Fatalf("inspection order: %+v", info)
	}
	info.Middlewares[0] = "changed"
	if router.Routes()[0].Middlewares[0] != "parent" || plain.ID() != "read" || plain.Pattern() != "/item" {
		t.Fatal("descriptor snapshots share mutable state")
	}
}

func TestRouteMiddlewareCanRejectBeforePathDecoding(t *testing.T) {
	type input struct{ ID int }
	deny := DefineMiddleware("deny", func(stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if info, ok := MatchedRoute(r.Context()); !ok || info.ID != "integer" {
				t.Error("missing matched identity")
			}
			w.WriteHeader(403)
		}), nil
	})
	route := DefineRoute(RouteSpec{ID: "integer", Method: GET, Access: Public}, DefinePath("/{id}", Param("id", IntegerPath[int](), func(p *input) *int { return &p.ID }))).WithMiddleware(deny)
	router, err := NewRouter(route.HandleRaw(func(stdhttp.ResponseWriter, *stdhttp.Request, input) { t.Error("denied handler executed") }))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/not-an-integer", nil))
	if response.Code != 403 {
		t.Fatalf("decoding preceded middleware: %d", response.Code)
	}
}

func TestGlobalMiddlewareCoversFallbacksAndPreservesNativeWriter(t *testing.T) {
	original := httptest.NewRecorder()
	global := DefineMiddleware("global", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if w != original {
				t.Error("framework replaced native writer")
			}
			if _, ok := w.(stdhttp.Flusher); !ok {
				t.Error("native optional capability lost")
			}
			if _, ok := MatchedRoute(r.Context()); ok {
				t.Error("global middleware fabricated a matched route")
			}
			w.Header().Set("X-Global", "yes")
			next.ServeHTTP(w, r)
		}), nil
	})
	router, err := NewRouter(staticRoute("read", GET, "/item").HandleRaw(noContent))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, global)
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(original, httptest.NewRequest("GET", "/missing", nil))
	if original.Code != 404 || original.Header().Get("X-Global") != "yes" {
		t.Fatal("global middleware missed fallback")
	}
}

func TestMiddlewareRejectsInvalidChainsBeforeConstruction(t *testing.T) {
	calls := 0
	middleware := DefineMiddleware("valid", func(next stdhttp.Handler) (stdhttp.Handler, error) { calls++; return next, nil })
	for _, chain := range [][]Middleware{{{}}, {middleware, {}}, {middleware, middleware}, {DefineMiddleware("bad id", middleware.construct)}} {
		handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {}), chain...)
		if err == nil || handler != nil || calls != 0 {
			t.Fatal("invalid chain constructed a partial handler")
		}
	}
	tooMany := make([]Middleware, maxMiddlewares+1)
	for i := range tooMany {
		tooMany[i] = DefineMiddleware(MiddlewareID(fmt.Sprintf("step.%d", i)), middleware.construct)
	}
	if _, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {}), tooMany...); err == nil || calls != 0 {
		t.Fatal("unbounded declaration accepted")
	}
	parent := DefineScope("/api", "api").WithMiddleware(middleware)
	duplicate := staticRoute("read", GET, "/item").WithMiddleware(middleware).Within(parent)
	if err := duplicate.Validate(); !errors.Is(err, fault.Duplicate) {
		t.Fatalf("scope duplicate: %v", err)
	}
}

func TestMiddlewareConstructorFailuresStayOwnedAndSafe(t *testing.T) {
	private := errors.New("private constructor failure")
	for _, factory := range []func(stdhttp.Handler) (stdhttp.Handler, error){
		func(stdhttp.Handler) (stdhttp.Handler, error) { return nil, private },
		func(stdhttp.Handler) (stdhttp.Handler, error) { panic("private panic") },
		func(stdhttp.Handler) (stdhttp.Handler, error) { runtime.Goexit(); return nil, nil },
		func(stdhttp.Handler) (stdhttp.Handler, error) { return stdhttp.HandlerFunc(nil), nil },
	} {
		handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {}), DefineMiddleware("broken", factory))
		if handler != nil || !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe constructor result: %v", err)
		}
	}
	if _, err := ApplyMiddleware(stdhttp.HandlerFunc(nil)); err == nil {
		t.Fatal("nil native handler accepted")
	}
}

func TestMiddlewareOwnsInputBeforeRunningConstructors(t *testing.T) {
	trace := ""
	first := DefineMiddleware("first", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { trace += "first"; next.ServeHTTP(w, r) }), nil
	})
	chain := []Middleware{first, {}}
	chain[1] = DefineMiddleware("second", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		chain[0] = Middleware{}
		return next, nil
	})
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {}), chain...)
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if trace != "first" {
		t.Fatal("constructor mutated the captured declaration list")
	}
}
