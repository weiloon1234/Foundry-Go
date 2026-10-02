package typescript_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// portalManifest has admin and web routes and a channel. The two responses use
// schemas whose short names collide, so the full manifest qualifies both.
func portalManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	route := func(id foundryhttp.RouteID, path string) foundryhttp.RouteRegistration {
		endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath(path)), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
		return endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
			t.Fatal("export ran a handler")
			return "", nil
		})
	}
	router, err := foundryhttp.NewRouter(route("admin.orders", "/admin/orders"), route("web.profile", "/profile"))
	if err != nil {
		t.Fatal(err)
	}
	orders := websocket.Public[struct{}]("orders", websocket.DefineRooms(foundryhttp.StringPath[string]()))
	registry, err := websocket.NewRegistry(websocket.Register(orders, websocket.DefineOutgoing(orders, "updated", contract.StringJSON[string]()).Registration()))
	if err != nil {
		t.Fatal(err)
	}
	realtime, err := websocket.DescribeClient(registry, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	built, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Realtime: &realtime})
	if err != nil {
		t.Fatal(err)
	}
	document, err := built.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []contract.TypeID{"consumer/admin.Row", "consumer/web.Row"} {
		document.Types = append(document.Types, contract.Type{ID: id, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "name", Type: "string", Required: true}}})
	}
	for i := range document.HTTP {
		document.HTTP[i].Response.Type = map[foundryhttp.RouteID]contract.TypeID{"admin.orders": "consumer/admin.Row", "web.profile": "consumer/web.Row"}[document.HTTP[i].Route.ID]
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

var portalSurfaces = []typescript.Surface{
	{Name: "admin", Routes: []foundryhttp.RouteID{"admin"}},
	{Name: "web", Routes: []foundryhttp.RouteID{"web"}, Channels: []websocket.ChannelID{"orders"}},
}

func TestSurfacesPublishPortalEntriesBesideSharedRuntimeModules(t *testing.T) {
	source := portalManifest(t)
	options := typescript.Options{Dir: t.TempDir(), Prefix: "api", OpenAPI: openapi.Options{Title: "Portals", APIVersion: "1"}, Surfaces: portalSurfaces}
	report, err := typescript.Generate(t.Context(), source, options)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"api_admin_foundry.gen.ts", "api_foundry.gen.ts", "api_manifest_foundry.gen.json", "api_openapi_foundry.gen.json", "api_runtime_foundry.gen.ts", "api_runtime_realtime_foundry.gen.ts", "api_web_foundry.gen.ts"}; !slices.Equal(report.Written, want) {
		t.Fatal("published files", report.Written)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(options.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	full, admin, web := read("api_foundry.gen.ts"), read("api_admin_foundry.gen.ts"), read("api_web_foundry.gen.ts")
	core, realtime := `from "./api_runtime_foundry.gen.js";`, `from "./api_runtime_realtime_foundry.gen.js";`
	for name, entry := range map[string]string{"full": full, "admin": admin, "web": web} {
		if !strings.Contains(entry, "\nimport { ") || !strings.Contains(entry, " createHTTPInvoker, ") || !strings.Contains(entry, core) || !strings.Contains(entry, `export const manifestJSON = "{\"version\":`) {
			t.Fatal(name, "entry does not import the core module or embed compact JSON")
		}
	}
	// The admin portal has no channels, so it never loads the realtime module.
	if !strings.Contains(admin, `"adminOrders"`) || strings.Contains(admin, `"webProfile"`) || strings.Contains(admin, realtime) || strings.Contains(admin, "createRealtime") {
		t.Fatal("admin entry is not restricted to its surface")
	}
	if !strings.Contains(web, `"webProfile"`) || strings.Contains(web, `"adminOrders"`) || !strings.Contains(web, realtime) || !strings.Contains(web, "export function createRealtime") {
		t.Fatal("web entry is not restricted to its surface")
	}
	// The full entry keeps its complete API, and schema names stay those of the
	// full manifest even where a surface reaches only one of the colliding names.
	if !strings.Contains(full, realtime) || !strings.Contains(full, `"adminOrders"`) || !strings.Contains(full, `"webProfile"`) {
		t.Fatal("full entry lost operations or realtime exports")
	}
	for _, entry := range []string{full, admin} {
		if !strings.Contains(entry, "export type Admin_Row = ") {
			t.Fatal("schema name differs between entries")
		}
	}
	if !strings.Contains(admin, "export const catalogLocales: { readonly default: Locale; readonly supported: readonly Locale[] } | undefined = undefined;") {
		t.Fatal("an entry without a catalog exports locales")
	}
	if !strings.Contains(read("api_runtime_foundry.gen.ts"), "export { runtimePolicy, defaultJSONLimits };") || !strings.Contains(read("api_runtime_realtime_foundry.gen.ts"), core) {
		t.Fatal("runtime modules are not wired to each other")
	}
	options.Check = true
	if _, err := typescript.Generate(t.Context(), source, options); err != nil {
		t.Fatal(err)
	}
	options.Surfaces = portalSurfaces[:1]
	if _, err := typescript.Generate(t.Context(), source, options); err == nil {
		t.Fatal("check ignored a removed surface")
	}
	options.Check = false
	report, err = typescript.Generate(t.Context(), source, options)
	if err != nil || !slices.Equal(report.Removed, []string{"api_web_foundry.gen.ts"}) {
		t.Fatal("removed surface", report, err)
	}
	// Render remains one self-contained module.
	single, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(single, []byte("\nimport ")) || bytes.Contains(single, []byte(`from "./`)) || !bytes.Contains(single, []byte("function createRealtimeEngine")) {
		t.Fatal("Render imports sibling modules")
	}
}

func TestSurfaceDeclarationsAreValidated(t *testing.T) {
	source := portalManifest(t)
	tooMany := make([]typescript.Surface, typescript.MaxSurfaces+1)
	for i := range tooMany {
		tooMany[i] = typescript.Surface{Name: fmt.Sprintf("s%d", i), Routes: []foundryhttp.RouteID{"admin"}}
	}
	for name, test := range map[string]struct {
		surfaces []typescript.Surface
		want     error
	}{
		"artifact name":   {[]typescript.Surface{{Name: "manifest", Routes: []foundryhttp.RouteID{"admin"}}}, fault.Invalid},
		"adapter name":    {[]typescript.Surface{{Name: "React", Routes: []foundryhttp.RouteID{"admin"}}}, fault.Invalid},
		"runtime name":    {[]typescript.Surface{{Name: "runtime_admin", Routes: []foundryhttp.RouteID{"admin"}}}, fault.Invalid},
		"pattern":         {[]typescript.Surface{{Name: "admin-portal", Routes: []foundryhttp.RouteID{"admin"}}}, fault.Invalid},
		"case duplicate":  {[]typescript.Surface{{Name: "Admin", Routes: []foundryhttp.RouteID{"admin"}}, {Name: "admin", Routes: []foundryhttp.RouteID{"web"}}}, fault.Duplicate},
		"too many":        {tooMany, fault.Invalid},
		"unknown route":   {[]typescript.Surface{{Name: "merchant", Routes: []foundryhttp.RouteID{"merchant"}}}, fault.Missing},
		"empty selection": {[]typescript.Surface{{Name: "empty"}}, fault.Invalid},
	} {
		dir := t.TempDir()
		if _, err := typescript.Generate(t.Context(), source, typescript.Options{Dir: dir, OpenAPI: openapi.Options{Title: "Portals", APIVersion: "1"}, Surfaces: test.surfaces}); !errors.Is(err, test.want) {
			t.Fatalf("%s accepted: %v", name, err)
		}
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
			t.Fatalf("%s published output", name)
		}
	}
	sources := func(context.Context, foundation.Resolver) (manifest.Sources, error) { return manifest.Sources{}, nil }
	if _, err := typescript.ExportCommand("contracts.export", openapi.Options{Title: "Portals", APIVersion: "1"}, sources, typescript.Surface{Name: "vue", Routes: []foundryhttp.RouteID{"admin"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("export command accepted a reserved surface name", err)
	}
}
