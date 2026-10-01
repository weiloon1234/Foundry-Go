package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAFallbackPreservesAPIsMethodsAndMissingAssets(t *testing.T) {
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(assetFixture())))
	original, err := NewRouter(
		DefineRoute(RouteSpec{ID: "api.missing", Method: GET, Access: Public}, StaticPath("/api/missing")).HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ NoPath) { writeRoutingError(w, r, NotFound) }),
		DefineRoute(RouteSpec{ID: "api.present", Method: POST, Access: Public}, StaticPath("/api/present")).HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(204) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultSPAConfig()
	config.Exclude = []string{"/api"}
	router, err := original.WithSPA("web", assets, config)
	if err != nil {
		t.Fatal(err)
	}
	config.Exclude[0] = "/changed"
	for _, tc := range []struct {
		method, path, accept string
		status               int
		body                 string
		vary                 bool
	}{
		{"GET", "/dashboard", "text/html", 200, "<main>SPA shell</main>", true},
		{"HEAD", "/dashboard", "text/html", 200, "", true},
		{"GET", "/dashboard", "application/json", 404, "", false},
		{"GET", "/dashboard", "*/*", 404, "", false},
		{"GET", "/dashboard", "", 404, "", false},
		{"GET", "/dashboard", "text/html;q=0, */*;q=1", 404, "", false},
		{"GET", "/dashboard", "text/html;q=0.8", 200, "<main>SPA shell</main>", true},
		{"GET", "/dashboard", "text/html; note=\"a,b\"", 200, "<main>SPA shell</main>", true},
		{"GET", "/missing.js", "text/html", 404, "", false},
		{"GET", "/api/unknown", "text/html", 404, "", false},
		{"GET", "/api/missing", "text/html", 404, "", false},
		{"GET", "/api/present", "text/html", 405, "", false},
		{"POST", "/dashboard", "text/html", 404, "", false},
		{"GET", "/.env", "text/html", 404, "", false},
		{"GET", "/site.css", "*/*", 200, "abcdefghij", false},
		{"GET", "/", "", 200, "<main>SPA shell</main>", false},
	} {
		t.Run(tc.method+tc.path+tc.accept, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			recorder.Header().Set("Vary", "Origin")
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("Accept", tc.accept)
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatal(recorder.Code, recorder.Body.String())
			}
			if tc.status == 200 && recorder.Body.String() != tc.body {
				t.Fatal("body", recorder.Body.String())
			}
			if tc.status >= 400 && strings.Contains(recorder.Body.String(), "SPA shell") {
				t.Fatal("failure rewritten to shell")
			}
			if tc.status == 405 && recorder.Header().Get("Allow") != "POST" {
				t.Fatal("native method contract lost", recorder.Header())
			}
			vary := []string{"origin"}
			if tc.vary {
				vary = append(vary, "accept")
			}
			assertCORSVary(t, recorder.Header(), vary...)
		})
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/dashboard", nil)
	request.Header.Set("Accept", "text/html")
	original.ServeHTTP(recorder, request)
	if recorder.Code != 404 {
		t.Fatal("original router was mutated")
	}
	if _, err := router.WithSPA("second", assets, DefaultSPAConfig()); err == nil {
		t.Fatal("second fallback accepted")
	}
	if _, err := original.WithSPA("api.missing", assets, DefaultSPAConfig()); err == nil {
		t.Fatal("duplicate API identity accepted")
	}
	var fallback *AssetRouteInfo
	for _, info := range router.Routes() {
		if info.ID == "web" {
			fallback = info.Assets
		}
	}
	if fallback == nil || !fallback.Fallback || len(fallback.Excluded) != 1 || fallback.Excluded[0] != "/api" {
		t.Fatal("fallback metadata", fallback)
	}
	fallback.Excluded[0] = "mutated"
	for _, info := range router.Routes() {
		if info.ID == "web" && info.Assets.Excluded[0] != "/api" {
			t.Fatal("metadata escaped")
		}
	}
}

func TestSPAPrefixAndEntryCannotLoopOnADirectory(t *testing.T) {
	source := assetFixture()
	source["other.html"] = &fstest.MapFile{Data: []byte("custom shell")}
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(source)))
	base, err := NewRouter()
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultSPAConfig()
	config.Prefix = "/app"
	config.Index = "other.html"
	router, err := base.WithSPA("app", assets, config)
	if err != nil {
		t.Fatal(err)
	}
	for _, location := range []string{"/app", "/app/", "/app/dashboard", "/elsewhere"} {
		request := httptest.NewRequest("GET", location, nil)
		request.Header.Set("Accept", "text/html")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		switch location {
		case "/app":
			if recorder.Code != 308 || recorder.Header().Get("Location") != "/app/" {
				t.Fatal("mount redirect", recorder.Code, recorder.Header())
			}
		case "/elsewhere":
			if recorder.Code != 404 {
				t.Fatal("prefix escaped")
			}
		default:
			if recorder.Code != 200 || recorder.Body.String() != "custom shell" {
				t.Fatal("entry mismatch", recorder.Code, recorder.Body.String())
			}
		}
	}
	config.Index = "docs"
	bad, err := base.WithSPA("bad-entry", assets, config)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/app/dashboard", nil)
	request.Header.Set("Accept", "text/html")
	recorder := httptest.NewRecorder()
	bad.ServeHTTP(recorder, request)
	if recorder.Code != 500 || recorder.Header().Get("Location") != "" {
		t.Fatal("directory entry caused a redirect loop", recorder.Code, recorder.Header())
	}
}

func TestHTMLNavigationBudgetsAndQuality(t *testing.T) {
	for _, accept := range []string{"text/html;q=-1", "text/html;q=1.1", "text/html;q=NaN", "text/html;q=1e0", "text/html;q=0", "text/html;q=0.000", "text/html; note=\"unterminated"} {
		if htmlNavigation([]string{accept}, "view") {
			t.Fatal("invalid acceptance", accept)
		}
	}
	if htmlNavigation([]string{"text/html", strings.Repeat("x", 4097)}, "view") {
		t.Fatal("oversized header accepted")
	}
	if htmlNavigation([]string{strings.Repeat("text/html,", 32) + "text/html"}, "view") {
		t.Fatal("too many media entries")
	}
	if htmlNavigation([]string{"text/html"}, "missing.css") {
		t.Fatal("missing asset became HTML")
	}
	for _, accept := range []string{"text/html;q=1", "text/html;q=1.000", "text/html;q=0.001", "text/html; note=\"a,b\""} {
		if !htmlNavigation([]string{accept}, "view") {
			t.Fatal("valid HTML navigation rejected", accept)
		}
	}
}

func TestAssetRangeBudgetRunsBeforeFilesystem(t *testing.T) {
	source := &countingAssetFS{MapFS: assetFixture()}
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(source)))
	router, err := NewRouter(assets.Mount("files", "/files").Register())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/files/site.css", nil)
	request.Header["Range"] = []string{"bytes=0-1", "bytes=2-3"}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != 400 || source.stats != 0 || source.opens != 0 {
		t.Fatal("range budget reached source", recorder.Code, source.stats, source.opens)
	}
}

func TestMultipleSPAPrefixesDoNotLeakIntoOuterApplication(t *testing.T) {
	source := assetFixture()
	source["admin.html"] = &fstest.MapFile{Data: []byte("admin shell")}
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(source)))
	base, err := NewRouter()
	if err != nil {
		t.Fatal(err)
	}
	outer, err := base.WithSPA("public", assets, DefaultSPAConfig())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultSPAConfig()
	config.Prefix = "/admin"
	config.Index = "admin.html"
	config.Exclude = []string{"/admin/api"}
	router, err := outer.WithSPA("admin", assets, config)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{{"/admin/page", 200, "admin shell"}, {"/public/page", 200, "<main>SPA shell</main>"}, {"/admin/api/missing", 404, ""}, {"/admin/missing.js", 404, ""}} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", tc.path, nil)
		request.Header.Set("Accept", "text/html")
		router.ServeHTTP(recorder, request)
		if recorder.Code != tc.status || tc.status == 200 && recorder.Body.String() != tc.body {
			t.Fatal("SPA prefix selection", tc.path, recorder.Code, recorder.Body.String())
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/admin/page", nil)
	request.Header.Set("Accept", "text/html")
	outer.ServeHTTP(recorder, request)
	if recorder.Body.String() != "<main>SPA shell</main>" {
		t.Fatal("outer router mutated")
	}
}

// A root public mount matches every path natively. A SPA with a more specific
// prefix owns its subtree below it, including what it declines; a SPA with the
// same prefix answers the mount's misses. More specific mounts still win.
func TestAssetMountsDeferToCoveringSPAs(t *testing.T) {
	public := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{
		"robots.txt":      {Data: []byte("robots")},
		"admin/shadow.js": {Data: []byte("public copy")},
	})))
	admin := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{
		"index.html":      {Data: []byte("admin shell")},
		"assets/other.js": {Data: []byte("unreachable: the bundle mount is more specific")},
	})))
	home := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{
		"index.html": {Data: []byte("home shell")},
		"main.js":    {Data: []byte("home bundle")},
	})))
	bundles := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{"app.js": {Data: []byte("immutable bundle")}})))
	// The root mount's middleware marks what the mount answers. A more specific
	// SPA answers outside it; the equal root SPA extends the mount, inside it.
	marked := DefineMiddleware("public.marked", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("X-Public-Mount", "1")
			next.ServeHTTP(w, r)
		}), nil
	})
	base, err := NewRouter(public.Mount("public", "/").WithMiddleware(marked).Register(), bundles.Mount("admin.bundles", "/admin/assets").Register())
	if err != nil {
		t.Fatal(err)
	}
	adminConfig := DefaultSPAConfig()
	adminConfig.Prefix, adminConfig.Exclude = "/admin", []string{"/admin/api"}
	withAdmin, err := base.WithSPA("admin", admin, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	router, err := withAdmin.WithSPA("home", home, DefaultSPAConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, accept string
		status       int
		body         string
		mount        bool
	}{
		{"/admin/login", "text/html", 200, "admin shell", false},
		{"/admin/shadow.js", "", 404, "", false},
		{"/admin/api/users", "text/html", 404, "", false},
		{"/admin/assets/app.js", "", 200, "immutable bundle", false},
		{"/admin/assets/other.js", "", 404, "", false},
		{"/robots.txt", "", 200, "robots", true},
		{"/main.js", "", 200, "home bundle", true},
		{"/dashboard", "text/html", 200, "home shell", true},
		{"/missing.js", "text/html", 404, "", true},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", tc.path, nil)
		if tc.accept != "" {
			request.Header.Set("Accept", tc.accept)
		}
		router.ServeHTTP(recorder, request)
		if recorder.Code != tc.status || tc.status == 200 && recorder.Body.String() != tc.body {
			t.Fatal("mount and SPA precedence", tc.path, recorder.Code, recorder.Body.String())
		}
		if (recorder.Header().Get("X-Public-Mount") != "") != tc.mount {
			t.Fatal("mount middleware ownership", tc.path, recorder.Header())
		}
	}
	// The router without SPAs keeps the mount's own miss.
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/admin/login", nil)
	request.Header.Set("Accept", "text/html")
	base.ServeHTTP(recorder, request)
	if recorder.Code != 404 {
		t.Fatal("a router view without SPAs changed", recorder.Code)
	}
}
