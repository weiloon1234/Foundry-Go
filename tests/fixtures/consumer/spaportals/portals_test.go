package spaportals_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/spaportals"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

const html = "text/html"

func quiet() application.Option {
	return application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func settings() application.Settings {
	s := application.DefaultSettings()
	s.HTTP.Probes.Liveness = true
	return s
}

// started builds and boots the portals (opening their assets) and returns the
// application router; its HTTP listener is never started.
func started(t *testing.T, home bool) *foundryhttp.Router {
	t.Helper()
	app, err := spaportals.Build(t.Context(), settings(), home, quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	router, err := foundation.Resolve(app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

type exchange struct {
	method, target, accept string
	status                 int
	body                   string
	cache                  foundryhttp.HeaderValue
}

func check(t *testing.T, router stdhttp.Handler, tests map[string]exchange) {
	t.Helper()
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			if test.accept != "" {
				request.Header.Set("Accept", test.accept)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("status %d body %q", response.Code, response.Body.String())
			}
			if test.cache != "" && response.Header().Get("Cache-Control") != string(test.cache) {
				t.Fatalf("cache control %q", response.Header().Get("Cache-Control"))
			}
			if test.method == stdhttp.MethodHead && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
}

func TestPortalsServeClientRoutesBesideRootAssetsAndAPIs(t *testing.T) {
	router := started(t, false)
	get, head := stdhttp.MethodGet, stdhttp.MethodHead
	check(t, router, map[string]exchange{
		"client route falls back":   {get, "/admin/login", html, 200, "admin portal", "no-cache"},
		"HEAD falls back":           {head, "/admin/login", html, 200, "", "no-cache"},
		"portal root":               {get, "/admin/", html, 200, "admin portal", "no-cache"},
		"portal file":               {get, "/admin/logo.svg", "", 200, "<svg", "no-cache"},
		"hashed bundle":             {get, "/admin/assets/app-3f2a9c.js", "", 200, "admin bundle", spaportals.ImmutableCache},
		"missing bundle":            {get, "/admin/assets/missing.js", html, 404, "", ""},
		"missing script":            {get, "/admin/missing.js", html, 404, "", ""},
		"non-HTML navigation":       {get, "/admin/login", "application/json", 404, "", ""},
		"portal owns its prefix":    {get, "/admin/shadow.js", "", 404, "", ""},
		"API route wins":            {get, "/admin/api/session", "application/json", 200, `"admin"`, ""},
		"excluded API namespace":    {get, "/admin/api/unknown", html, 404, "", ""},
		"root public file":          {get, "/robots.txt", "", 200, "User-agent", "no-cache"},
		"no portal at the root":     {get, "/dashboard", html, 404, "", ""},
		"second portal":             {get, "/merchant/orders/7", html, 200, "merchant portal", "no-cache"},
		"health route":              {get, "/up", "", 200, "", ""},
		"deep client route":         {get, "/admin/users/7/edit", html, 200, "admin portal", "no-cache"},
		"HEAD on the portal prefix": {head, "/admin/", html, 200, "", "no-cache"},
	})
	// A prefix redirects to its directory so relative frontend assets resolve.
	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest(stdhttp.MethodGet, "/admin", nil))
	if redirect.Code != 308 || redirect.Header().Get("Location") != "/admin/" {
		t.Fatal("portal prefix did not redirect", redirect.Code, redirect.Header().Get("Location"))
	}
	// Route inspection lists each SPA with its fallback metadata.
	routes := map[foundryhttp.RouteID]foundryhttp.RouteInfo{}
	for _, route := range router.Routes() {
		routes[route.ID] = route
	}
	admin, merchant := routes["portals.admin"], routes["portals.merchant"]
	if admin.Assets == nil || !admin.Assets.Fallback || admin.Assets.Index != "index.html" || !slices.Equal(admin.Assets.Excluded, []string{"/admin/api"}) {
		t.Fatal("admin SPA metadata", admin.Assets)
	}
	if merchant.Assets == nil || !merchant.Assets.Fallback {
		t.Fatal("merchant SPA metadata", merchant.Assets)
	}
	// Contract export is unchanged: SPAs and asset mounts are not operations.
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	document, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.HTTP) != 1 || document.HTTP[0].Route.ID != "admin.session" {
		t.Fatal("contract export changed", len(document.HTTP))
	}
}

// A portal at "/" composes with the root public mount: the mount serves its
// own files first and the portal answers its misses; more specific portals,
// their exclusions and API routes keep precedence.
func TestRootPortalAnswersPublicMountMisses(t *testing.T) {
	router := started(t, true)
	get := stdhttp.MethodGet
	check(t, router, map[string]exchange{
		"public file first":        {get, "/robots.txt", "", 200, "User-agent", "no-cache"},
		"portal file on a miss":    {get, "/main.js", "", 200, "home", "no-cache"},
		"portal entry at the root": {get, "/", html, 200, "home portal", "no-cache"},
		"client route":             {get, "/dashboard/reports", html, 200, "home portal", "no-cache"},
		"missing script":           {get, "/missing.js", html, 404, "", ""},
		"more specific portal":     {get, "/admin/login", html, 200, "admin portal", "no-cache"},
		"no outer fallback":        {get, "/admin/api/unknown", html, 404, "", ""},
		"API route":                {get, "/admin/api/session", "application/json", 200, `"admin"`, ""},
		"health route":             {get, "/up", "", 200, "", ""},
	})
}

// Declarations are checked by Build, before any resource opens or starts.
func TestSPADeclarationsFailBuild(t *testing.T) {
	config := func(prefix string) foundryhttp.SPAConfig {
		spa := foundryhttp.DefaultSPAConfig()
		spa.Prefix = prefix
		return spa
	}
	for name, test := range map[string]struct {
		declare func(*application.Builder) *application.Builder
		kind    error
	}{
		"unknown assets key": {func(b *application.Builder) *application.Builder {
			return b.SPA("portals.unknown", foundation.NewKey[*foundryhttp.Assets]("consumer.portals.none"), config("/unknown"))
		}, fault.Missing},
		"duplicate route ID": {func(b *application.Builder) *application.Builder {
			return spaportals.Declare(b, false).SPA("portals.admin", spaportals.HomeKey, config("/other"))
		}, fault.Duplicate},
		"duplicate prefix": {func(b *application.Builder) *application.Builder {
			return spaportals.Declare(b, false).SPA("portals.other", spaportals.HomeKey, config("/admin"))
		}, fault.Duplicate},
		"ID of a declared route": {func(b *application.Builder) *application.Builder {
			return b.SPA("admin.session", spaportals.HomeKey, config("/session"))
		}, fault.Duplicate},
		"invalid prefix": {func(b *application.Builder) *application.Builder {
			return b.SPA("portals.invalid", spaportals.HomeKey, config("admin"))
		}, fault.Invalid},
	} {
		t.Run(name, func(t *testing.T) {
			builder, err := spaportals.New(settings(), quiet())
			if err != nil {
				t.Fatal(err)
			}
			app, err := test.declare(builder).Build(t.Context())
			if app != nil || !errors.Is(err, test.kind) {
				t.Fatalf("expected %v, got %v", test.kind, err)
			}
		})
	}
	disabled := settings()
	disabled.HTTP.Enabled = false
	if _, err := application.New(disabled, quiet()).SPA("portals.home", spaportals.HomeKey, foundryhttp.DefaultSPAConfig()).Build(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("SPA accepted without HTTP", err)
	}
	// The caller's exclusion slice is snapshotted when declared.
	builder, err := spaportals.New(settings(), quiet())
	if err != nil {
		t.Fatal(err)
	}
	admin := config("/admin")
	admin.Exclude = []string{"/admin/api"}
	builder.SPA("portals.admin", spaportals.AdminKey, admin)
	admin.Exclude[0] = "/admin"
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundation.Resolve(app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range router.Routes() {
		if route.ID == "portals.admin" && !slices.Equal(route.Assets.Excluded, []string{"/admin/api"}) {
			t.Fatal("declared exclusions were not snapshotted", route.Assets.Excluded)
		}
	}
}
