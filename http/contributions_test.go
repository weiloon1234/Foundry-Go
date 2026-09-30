package http_test

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/plugin"
)

func TestPluginRoutesAndMiddlewareUseNativeRouter(t *testing.T) {
	key := foundation.NewKey[*foundryhttp.Router]("reports.router")
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "reports.index", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/reports"))
	register := func(message string) func(*foundation.Registrar) error {
		return func(r *foundation.Registrar) error {
			return foundryhttp.RegisterRoute(r, key, route, func(foundation.Resolver) (foundryhttp.RouteRegistration, error) {
				return route.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ foundryhttp.NoPath) {
					_, _ = w.Write([]byte(message))
				}), nil
			})
		}
	}
	extension := plugin.Module{Declaration: plugin.Manifest{ID: "reports", Version: "1.0.0", Framework: "^0.1.0"}, OnRegister: func(r *plugin.Registrar) error {
		if err := register("default")(r); err != nil {
			return err
		}
		return foundryhttp.RegisterMiddleware(r, key, foundryhttp.DefineMiddleware("reports.header", func(next stdhttp.Handler) (stdhttp.Handler, error) {
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
				w.Header().Set("X-Plugin", "reports")
				next.ServeHTTP(w, request)
			}), nil
		}))
	}}
	routerModule := foundation.Module{Name: "http", OnRegister: func(r *foundation.Registrar) error { return foundryhttp.RegisterRouter(r, key) }}
	app, err := foundation.NewBuilder().Register(routerModule).RegisterPlugin(extension).OverrideContributions("application", register("custom")).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/reports", nil))
	if response.Code != 200 || response.Body.String() != "custom" || response.Header().Get("X-Plugin") != "reports" {
		t.Fatalf("contributed route response: %v", response.Result())
	}
	if routes := router.Routes(); len(routes) != 1 || len(routes[0].Middlewares) != 1 || routes[0].Middlewares[0] != "reports.header" {
		t.Fatalf("middleware metadata: %+v", routes)
	}
	duplicate := plugin.Module{Declaration: plugin.Manifest{ID: "duplicate", Version: "1.0.0", Framework: "*"}, OnRegister: register("duplicate")}
	_, err = foundation.NewBuilder().Register(routerModule).RegisterPlugin(extension, duplicate).Build(t.Context())
	if !errors.Is(err, fault.Duplicate) || !strings.Contains(err.Error(), "plugin:reports") || !strings.Contains(err.Error(), "plugin:duplicate") {
		t.Fatalf("duplicate route owners: %v", err)
	}
	_, err = foundation.NewBuilder().RegisterPlugin(extension).Build(t.Context())
	if !errors.Is(err, fault.Missing) {
		t.Fatalf("orphan route contributions accepted: %v", err)
	}
}

// RegisterSPA composes a SPA with a router assembled from contributions; its
// declaration is checked when registered and its assets when the router is
// built, both during Build.
func TestRegisterSPAComposesWithContributedRouter(t *testing.T) {
	key := foundation.NewKey[*foundryhttp.Router]("portal.router")
	assets := foundation.NewKey[*foundryhttp.Assets]("portal.assets")
	source := foundryhttp.FilesystemAssets(fstest.MapFS{"index.html": {Data: []byte("portal shell")}})
	build := func(id foundryhttp.RouteID, assetsKey foundation.Key[*foundryhttp.Assets], prefix string) (*foundation.App, error) {
		config := foundryhttp.DefaultSPAConfig()
		config.Prefix = prefix
		return foundation.NewBuilder().Register(foundryhttp.AssetsModule("assets", assets, foundryhttp.DefaultAssetsConfig(source)), foundation.Module{Name: "http", OnRegister: func(r *foundation.Registrar) error {
			if err := foundryhttp.RegisterRouter(r, key); err != nil {
				return err
			}
			return foundryhttp.RegisterSPA(r, key, id, assetsKey, config)
		}}).Build(t.Context())
	}
	app, err := build("portal", assets, "/portal")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	router, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/portal/settings", nil)
	request.Header.Set("Accept", "text/html")
	router.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != "portal shell" {
		t.Fatal("contributed SPA did not serve its entry", response.Code, response.Body.String())
	}
	for name, test := range map[string]struct {
		id     foundryhttp.RouteID
		assets foundation.Key[*foundryhttp.Assets]
		prefix string
		kind   error
	}{
		"unknown assets": {"portal", foundation.NewKey[*foundryhttp.Assets]("portal.none"), "/portal", fault.Missing},
		"no assets key":  {"portal", foundation.Key[*foundryhttp.Assets]{}, "/portal", fault.Invalid},
		"route ID":       {"Portal Route", assets, "/portal", fault.Invalid},
		"prefix":         {"portal", assets, "portal", fault.Invalid},
	} {
		if _, err := build(test.id, test.assets, test.prefix); !errors.Is(err, test.kind) {
			t.Fatalf("%s: expected %v, got %v", name, test.kind, err)
		}
	}
}
