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
	if !strings.Contains(admin, `Admin_Row: "consumer/admin.Row"`) || strings.Contains(admin, `Web_Row: "consumer/web.Row"`) {
		t.Fatal("admin schemas not restricted to reached types")
	}
	if !strings.Contains(web, `Web_Row: "consumer/web.Row"`) || strings.Contains(web, `Admin_Row: "consumer/admin.Row"`) {
		t.Fatal("web schemas not restricted to reached types")
	}
	if !strings.Contains(full, `Admin_Row: "consumer/admin.Row"`) || !strings.Contains(full, `Web_Row: "consumer/web.Row"`) {
		t.Fatal("full schemas missing types")
	}
	for name, entry := range map[string]string{"full": full, "admin": admin, "web": web} {
		if !strings.Contains(entry, "export const ErrorCode = /* @__PURE__ */ Object.freeze({") {
			t.Fatal(name, "missing ErrorCode")
		}
	}
	runtimeModule := read("api_runtime_foundry.gen.ts")
	if !strings.Contains(runtimeModule, "export function newIdentity") || !strings.Contains(runtimeModule, "export function enumLabel") {
		t.Fatal("runtime missing newIdentity or enumLabel")
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

func authPortalManifest(t *testing.T, mutators ...func(*manifest.Document)) *manifest.Manifest {
	t.Helper()
	cookie := foundryhttp.DefineRefreshCookie("__Host-refresh-user", foundryhttp.CSRFConfig{})
	endpoint := func(id foundryhttp.RouteID, path string, method foundryhttp.Method) foundryhttp.RouteRegistration {
		route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
		ep := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
		return ep.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
			return "", nil
		})
	}
	router, err := foundryhttp.NewRouter(
		endpoint("user.login", "/api/user/login", foundryhttp.POST),
		endpoint("user.refresh", "/api/user/refresh", foundryhttp.POST),
		endpoint("user.logout", "/api/user/logout", foundryhttp.POST),
		endpoint("user.profile", "/api/user/profile", foundryhttp.GET),
	)
	if err != nil {
		t.Fatal(err)
	}
	built, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	document, err := built.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	document.Types = append(document.Types,
		contract.Type{
			ID:   "LoginBody",
			Kind: contract.ObjectKind,
			Properties: []contract.Property{
				{Name: "email", Type: "string", Required: true},
				{Name: "password", Type: "string", Required: true, Presentation: contract.Presentation{Kind: contract.PasswordPresentation}},
			},
		},
		contract.Type{
			ID:   "ProfileResp",
			Kind: contract.ObjectKind,
			Properties: []contract.Property{
				{Name: "name", Type: "string", Required: true},
			},
		},
	)
	for i := range document.HTTP {
		op := &document.HTTP[i]
		switch op.Route.ID {
		case "user.login":
			op.Body = &manifest.Payload{MediaType: "application/json", Type: "LoginBody"}
			op.RefreshCookie = &foundryhttp.RefreshCookieInfo{Name: cookie.Name(), Sets: true}
		case "user.refresh":
			op.RefreshCookie = &foundryhttp.RefreshCookieInfo{Name: cookie.Name(), Reads: true, Sets: true}
		case "user.logout":
			op.RefreshCookie = &foundryhttp.RefreshCookieInfo{Name: cookie.Name(), Reads: true, Clears: true}
		case "user.profile":
			op.Response = &manifest.Payload{MediaType: "application/json", Type: "ProfileResp"}
			op.Route.Access = foundryhttp.Guarded
			op.Route.Authentication = &foundryhttp.AuthenticationInfo{
				Guard:    "user",
				Provider: "users",
				Credential: foundryhttp.CredentialInfo{
					Source: "bearer",
					Kind:   foundryhttp.BearerCredentialKind,
					Name:   "Authorization",
				},
			}
		}
	}
	for _, mutator := range mutators {
		mutator(&document)
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

func TestSurfaceAuthValidationAndGeneration(t *testing.T) {
	validAuth := &typescript.SurfaceAuth{
		Guard:      "user",
		Login:      "user.login",
		Refresh:    "user.refresh",
		Logout:     "user.logout",
		Profile:    "user.profile",
		Identifier: "email",
	}

	// 1. Positive case: valid surface with auth generates auth module & portal session
	source := authPortalManifest(t)
	dir := t.TempDir()
	surfaces := []typescript.Surface{
		{
			Name:   "user",
			Routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			Auth:   validAuth,
		},
	}
	report, err := typescript.Generate(t.Context(), source, typescript.Options{
		Dir:      dir,
		Prefix:   "api",
		OpenAPI:  openapi.Options{Title: "Portals", APIVersion: "1"},
		Surfaces: surfaces,
	})
	if err != nil {
		t.Fatalf("unexpected generation error: %v", err)
	}
	if !slices.Contains(report.Written, "api_runtime_auth_foundry.gen.ts") {
		t.Errorf("expected api_runtime_auth_foundry.gen.ts in written files, got: %v", report.Written)
	}
	if !slices.Contains(report.Written, "api_user_foundry.gen.ts") {
		t.Errorf("expected api_user_foundry.gen.ts in written files, got: %v", report.Written)
	}
	userModule, err := os.ReadFile(filepath.Join(dir, "api_user_foundry.gen.ts"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(userModule)
	for _, want := range []string{
		`import { createPortalSession as runtimeCreatePortalSession } from "./api_runtime_auth_foundry.gen.js";`,
		`export type PasswordCredentials = LoginBody;`,
		`export const signInIdentifier: keyof PasswordCredentials = "email";`,
		`export const authDescriptor: SurfaceAuthDescriptor<Client, Operations["userProfile"]["response"], PasswordCredentials> = {`,
		`guard: "user",`,
		`identifier: "email",`,
		`export function createPortalSession(`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in user surface module:\n%s", want, content)
		}
	}

	if strings.Contains(content, "cookieName") {
		t.Fatal("auth descriptor exposes the refresh cookie name")
	}
	fullModule, err := os.ReadFile(filepath.Join(dir, "api_foundry.gen.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(fullModule), "api_runtime_auth_foundry.gen") {
		t.Fatal("full entry imports the optional auth runtime")
	}
	surfaces[0].Auth = nil
	report, err = typescript.Generate(t.Context(), source, typescript.Options{
		Dir: dir, Prefix: "api", OpenAPI: openapi.Options{Title: "Portals", APIVersion: "1"}, Surfaces: surfaces,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(report.Removed, "api_runtime_auth_foundry.gen.ts") {
		t.Fatal("removing the last Auth declaration retains the optional runtime")
	}

	// Negative validation tests.
	for name, tc := range map[string]struct {
		mutator func(*manifest.Document)
		auth    *typescript.SurfaceAuth
		routes  []foundryhttp.RouteID
		want    error
		message string
	}{
		"missing required route": {
			auth:    &typescript.SurfaceAuth{Guard: "user", Refresh: "user.refresh", Logout: "user.logout", Profile: "user.profile", Identifier: "email"},
			routes:  []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:    fault.Invalid,
			message: "surface auth requires guard, login, refresh, logout, profile and identifier",
		},
		"missing route in document": {
			message: `surface "user" auth route "unknown.login" is not registered`,
			auth: &typescript.SurfaceAuth{
				Guard:      "user",
				Login:      "unknown.login",
				Refresh:    "user.refresh",
				Logout:     "user.logout",
				Profile:    "user.profile",
				Identifier: "email",
			},
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Missing,
		},
		"route not in surface": {
			message: `surface "user" auth references route "user.logout" which is not included in the surface`,
			auth:    validAuth,
			routes:  []foundryhttp.RouteID{"user.login", "user.refresh"}, // missing logout, profile
			want:    fault.Invalid,
		},
		"profile guard mismatch": {
			message: `surface "user" auth guard "admin" does not match profile route "user.profile" guard "user"`,
			auth: &typescript.SurfaceAuth{
				Guard:      "admin",
				Login:      "user.login",
				Refresh:    "user.refresh",
				Logout:     "user.logout",
				Profile:    "user.profile",
				Identifier: "email",
			},
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"login missing token cookie response": {
			message: `surface "user" login route "user.login" must use TokenCookieResponse`,
			mutator: func(d *manifest.Document) {
				for i := range d.HTTP {
					if d.HTTP[i].Route.ID == "user.login" {
						d.HTTP[i].RefreshCookie = nil
					}
				}
			},
			auth:   validAuth,
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"refresh missing reads cookie": {
			message: `surface "user" refresh route "user.refresh" must use RefreshTokenCookie and TokenCookieResponse`,
			mutator: func(d *manifest.Document) {
				for i := range d.HTTP {
					if d.HTTP[i].Route.ID == "user.refresh" {
						d.HTTP[i].RefreshCookie.Reads = false
					}
				}
			},
			auth:   validAuth,
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"logout missing clears cookie": {
			message: `surface "user" logout route "user.logout" must use RefreshTokenCookieLogout and clear the cookie`,
			mutator: func(d *manifest.Document) {
				for i := range d.HTTP {
					if d.HTTP[i].Route.ID == "user.logout" {
						d.HTTP[i].RefreshCookie.Clears = false
						d.HTTP[i].RefreshCookie.Sets = true
					}
				}
			},
			auth:   validAuth,
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"cookie name mismatch": {
			message: `surface "user" auth endpoints must share the same refresh cookie, found "__Host-refresh-user" and "__Host-other-cookie"`,
			mutator: func(d *manifest.Document) {
				for i := range d.HTTP {
					if d.HTTP[i].Route.ID == "user.refresh" {
						d.HTTP[i].RefreshCookie.Name = "__Host-other-cookie"
					}
				}
			},
			auth:   validAuth,
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"logout cookie name mismatch": {
			mutator: func(d *manifest.Document) {
				for i := range d.HTTP {
					if d.HTTP[i].Route.ID == "user.logout" {
						d.HTTP[i].RefreshCookie.Name = "__Host-logout-cookie"
					}
				}
			},
			auth:    validAuth,
			routes:  []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:    fault.Invalid,
			message: `surface "user" auth endpoints must share the same refresh cookie, found "__Host-refresh-user" and "__Host-logout-cookie"`,
		},
		"login body not an object": {
			message: `surface "user" login route "user.login" request body "LoginBody" must be an object`,
			mutator: func(d *manifest.Document) {
				for i := range d.Types {
					if d.Types[i].ID == "LoginBody" {
						d.Types[i].Kind = contract.StringKind
						d.Types[i].Properties = nil
					}
				}
			},
			auth:   validAuth,
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"unknown wire identifier in login body": {
			message: `surface "user" auth login route "user.login" body "LoginBody": declared identifier "phone" must exist as a non-secret JSON property`,
			auth: &typescript.SurfaceAuth{
				Guard:      "user",
				Login:      "user.login",
				Refresh:    "user.refresh",
				Logout:     "user.logout",
				Profile:    "user.profile",
				Identifier: "phone",
			},
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"Go-style identifier is not a wire name": {
			message: `surface "user" auth login route "user.login" body "LoginBody": declared identifier "Email" must exist as a non-secret JSON property`,
			auth: &typescript.SurfaceAuth{
				Guard: "user", Login: "user.login", Refresh: "user.refresh", Logout: "user.logout", Profile: "user.profile", Identifier: "Email",
			},
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"wire identifier is case sensitive": {
			message: `surface "user" auth login route "user.login" body "LoginBody": declared identifier "EMAIL" must exist as a non-secret JSON property`,
			auth: &typescript.SurfaceAuth{
				Guard: "user", Login: "user.login", Refresh: "user.refresh", Logout: "user.logout", Profile: "user.profile", Identifier: "EMAIL",
			},
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"secret identifier in login body": {
			message: `surface "user" auth login route "user.login" body "LoginBody": identifier "password" cannot be a secret or password field`,
			auth: &typescript.SurfaceAuth{
				Guard:      "user",
				Login:      "user.login",
				Refresh:    "user.refresh",
				Logout:     "user.logout",
				Profile:    "user.profile",
				Identifier: "password", // password cannot be identifier
			},
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"login body missing password field": {
			message: `surface "user" login route "user.login" body "LoginBody" contains no password field`,
			mutator: func(d *manifest.Document) {
				for i := range d.Types {
					if d.Types[i].ID == "LoginBody" {
						d.Types[i].Properties = []contract.Property{
							{Name: "email", Type: "string", Required: true},
						}
					}
				}
			},
			auth:   validAuth,
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
		"login body multiple password fields": {
			message: `surface "user" login route "user.login" body "LoginBody" contains multiple password fields; exactly one is permitted`,
			mutator: func(d *manifest.Document) {
				for i := range d.Types {
					if d.Types[i].ID == "LoginBody" {
						d.Types[i].Properties = []contract.Property{
							{Name: "email", Type: "string", Required: true},
							{Name: "password", Type: "string", Required: true, Presentation: contract.Presentation{Kind: contract.PasswordPresentation}},
							{Name: "confirm_password", Type: "string", Required: true, Presentation: contract.Presentation{Kind: contract.PasswordPresentation}},
						}
					}
				}
			},
			auth:   validAuth,
			routes: []foundryhttp.RouteID{"user.login", "user.refresh", "user.logout", "user.profile"},
			want:   fault.Invalid,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var mutators []func(*manifest.Document)
			if tc.mutator != nil {
				mutators = append(mutators, tc.mutator)
			}
			src := authPortalManifest(t, mutators...)
			d := t.TempDir()
			surf := []typescript.Surface{
				{
					Name:   "user",
					Routes: tc.routes,
					Auth:   tc.auth,
				},
			}
			_, err := typescript.Generate(t.Context(), src, typescript.Options{
				Dir:      d,
				Prefix:   "api",
				OpenAPI:  openapi.Options{Title: "Portals", APIVersion: "1"},
				Surfaces: surf,
			})
			if !errors.Is(err, tc.want) || err.Error() != fmt.Sprintf("%v: %s", tc.want, tc.message) {
				t.Fatalf("expected %v with message %q, got: %v", tc.want, tc.message, err)
			}
		})
	}
}
