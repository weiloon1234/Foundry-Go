package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func fallbackPrefix(t *testing.T, text string) PathPrefix {
	t.Helper()
	prefix, err := ParsePathPrefix(text)
	if err != nil {
		t.Fatal(err)
	}
	return prefix
}
func fallbackRequest(handler stdhttp.Handler, method, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Accept", "text/html")
	handler.ServeHTTP(response, request)
	return response
}

func TestFallbackNativeRoutingAndScopedOwnership(t *testing.T) {
	config := DefaultFallbackConfig()
	config.Exclude = []PathPrefix{fallbackPrefix(t, "/api"), fallbackPrefix(t, "/account/blocked")}
	root := fallbackPrefix(t, "/")
	account := fallbackPrefix(t, "/account")
	var calls []string
	handler := func(id string) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			info, ok := MatchedRoute(r.Context())
			if !ok || info.ID != RouteID(id) {
				t.Errorf("matched route: %+v", info)
			}
			calls = append(calls, id)
			w.Header().Set("X-Owner", id)
			w.WriteHeader(200)
			if r.Method != "HEAD" {
				_, _ = w.Write([]byte(r.URL.RequestURI()))
			}
		})
	}
	route := func(id RouteID, method Method, path string, status int) RouteRegistration {
		return HandleRaw(DefineRoute(RouteSpec{ID: id, Method: method, Access: Public}, StaticPath(path)), func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ NoPath) { w.WriteHeader(status) })
	}
	base, err := NewRouter(route("api.post", POST, "/api/session", 204), route("api.delete", DELETE, "/api/session", 204), route("native.missing", GET, "/missing", 404), route("native.options", OPTIONS, "/options", 204), route("native.redirect", GET, "/canonical/", 204))
	if err != nil {
		t.Fatal(err)
	}
	router, err := base.WithFallbacks(DefineFallback("root", root, handler("root"), config), DefineFallback("account", account, handler("account"), config))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, target string
		status         int
		owner, allow   string
	}{
		{"GET", "/api/session", 405, "", "DELETE, POST"}, {"HEAD", "/api/session", 405, "", "DELETE, POST"}, {"PUT", "/api/session", 405, "", "DELETE, POST"},
		{"POST", "/api/session", 204, "", ""}, {"GET", "/api/unknown", 404, "", ""}, {"POST", "/unknown", 404, "", ""},
		{"OPTIONS", "/unknown", 404, "", ""}, {"OPTIONS", "/api/session", 405, "", "DELETE, POST"}, {"OPTIONS", "/options", 204, "", ""},
		{"GET", "/missing", 404, "", ""}, {"GET", "/apix", 200, "root", ""}, {"GET", "/api", 404, "", ""},
		{"GET", "/account/orders/?q=1", 200, "account", ""}, {"GET", "/account", 200, "account", ""}, {"GET", "/accounting", 200, "root", ""},
		{"GET", "/account/blocked/child", 404, "", ""}, {"HEAD", "/account/orders/", 200, "account", ""},
	} {
		t.Run(tc.method+tc.target, func(t *testing.T) {
			response := fallbackRequest(router, tc.method, tc.target)
			if response.Code != tc.status || response.Header().Get("Allow") != tc.allow || response.Header().Get("X-Owner") != tc.owner {
				t.Fatalf("status %d headers %v body %s", response.Code, response.Header(), response.Body.String())
			}
			if tc.owner != "" && tc.method == "GET" && response.Body.String() != tc.target {
				t.Fatal("URL changed", response.Body.String())
			}
			if tc.method == "HEAD" && response.Body.Len() != 0 {
				t.Fatal("HEAD body", response.Body.String())
			}
			if tc.status == 404 && tc.target != "/missing" && decodeFailure(t, response).Code != NotFound {
				t.Fatal("not JSON not_found")
			}
		})
	}
	originalRedirect := fallbackRequest(base, "GET", "/canonical")
	fallbackRedirect := fallbackRequest(router, "GET", "/canonical")
	if originalRedirect.Code < 300 || originalRedirect.Code >= 400 || fallbackRedirect.Code != originalRedirect.Code || fallbackRedirect.Header().Get("Location") != originalRedirect.Header().Get("Location") {
		t.Fatal("native redirect changed", originalRedirect.Code, fallbackRedirect.Code, fallbackRedirect.Header())
	}
	if len(calls) != 5 {
		t.Fatal("ineligible request invoked fallback", calls)
	}
	if response := fallbackRequest(base, "GET", "/account/orders/"); response.Code != 404 {
		t.Fatal("base router changed")
	}
	// A selected owner that does not declare HEAD cannot leak to its root owner.
	headConfig := FallbackConfig{Methods: []Method{GET}}
	router, err = base.WithFallbacks(DefineFallback("root", root, handler("root"), DefaultFallbackConfig()), DefineFallback("account", account, handler("account"), headConfig))
	if err != nil {
		t.Fatal(err)
	}
	if response := fallbackRequest(router, "HEAD", "/account/orders/"); response.Code != 404 {
		t.Fatal("method miss escaped owner", response.Code)
	}
}

func TestFallbackValidationAndSnapshots(t *testing.T) {
	for _, prefix := range []string{"", "api", "/api/", "//api", "/a/../b", "/a/%62", "/{path...}", "/a?b", "/a\\b", strings.Repeat("a", 4097)} {
		if _, err := ParsePathPrefix(prefix); err == nil {
			t.Errorf("accepted prefix %q", prefix)
		}
	}
	root := fallbackPrefix(t, "/")
	api := fallbackPrefix(t, "/api")
	config := DefaultFallbackConfig()
	config.Exclude = []PathPrefix{api}
	declaration := DefineFallback("root", root, stdhttp.NotFoundHandler(), config)
	config.Methods[0] = POST
	config.Exclude[0] = root
	router, _ := NewRouter()
	router, err := router.WithFallbacks(declaration)
	if err != nil {
		t.Fatal(err)
	}
	info := router.Routes()[0]
	if !slices.Equal(info.FallbackDetails.Methods, []Method{GET, HEAD}) || info.FallbackDetails.Exclude[0] != api {
		t.Fatal("declaration not snapshotted")
	}
	info.FallbackDetails.Methods[0] = POST
	info.FallbackDetails.Exclude[0] = root
	if got := router.Routes()[0].FallbackDetails; got.Methods[0] != GET || got.Exclude[0] != api {
		t.Fatal("inspection not owned")
	}
	for name, config := range map[string]FallbackConfig{
		"empty": {}, "method": {Methods: []Method{"bad"}}, "duplicate method": {Methods: []Method{GET, GET}},
		"zero prefix": {Methods: []Method{GET}, Exclude: []PathPrefix{{}}}, "duplicate prefix": {Methods: []Method{GET}, Exclude: []PathPrefix{api, api}},
		"exclusion limit": {Methods: []Method{GET}, Exclude: make([]PathPrefix, 65)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := config.Validate(); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	if err := (Fallback{}).Validate(); err == nil {
		t.Fatal("accepted empty declaration")
	}
	if err := DefineFallback("missing", PathPrefix{}, stdhttp.NotFoundHandler(), DefaultFallbackConfig()).Validate(); err == nil {
		t.Fatal("accepted empty prefix")
	}
	many := make([]Fallback, 65)
	for i := range many {
		many[i] = DefineFallback(RouteID(fmt.Sprintf("fallback.%d", i)), fallbackPrefix(t, fmt.Sprintf("/p%d", i)), stdhttp.NotFoundHandler(), DefaultFallbackConfig())
	}
	base, _ := NewRouter()
	if _, err := base.WithFallbacks(many...); !errors.Is(err, fault.Invalid) {
		t.Fatal("accepted too many", err)
	}
	if _, err := base.WithFallbacks(many[:64]...); err != nil {
		t.Fatal("rejected limit", err)
	}
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(assetFixture())))
	for _, reverse := range []bool{false, true} {
		first, _ := NewRouter()
		spa := DefaultSPAConfig()
		var err error
		if reverse {
			first, err = first.WithFallbacks(declaration)
			if err == nil {
				_, err = first.WithSPA("spa", assets, spa)
			}
		} else {
			first, err = first.WithSPA("spa", assets, spa)
			if err == nil {
				_, err = first.WithFallbacks(declaration)
			}
		}
		if !errors.Is(err, fault.Conflict) {
			t.Fatal("raw/SPA conflict accepted", err)
		}
	}
	if err := assets.Mount("asset", "/").Fallback(FallbackConfig{Methods: []Method{POST}}).Validate(); err == nil {
		t.Fatal("asset POST accepted")
	}
	if err := assets.Mount("asset", "relative").Fallback(DefaultFallbackConfig()).Validate(); err == nil {
		t.Fatal("bad mount accepted")
	}
}

func TestFallbackAssetsSPAAndNavigation(t *testing.T) {
	public := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(assetFixture())))
	spaAssets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{"index.html": {Data: []byte("portal")}})))
	var navigation int
	mount := public.Mount("public", "/").WithNavigationFallback(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		navigation++
		w.Header().Set("X-Navigation", "yes")
		w.WriteHeader(404)
	})).WithMiddleware(DefineMiddleware("public.marker", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("X-Public", "yes")
			next.ServeHTTP(w, r)
		}), nil
	}))
	config := DefaultFallbackConfig()
	config.Exclude = []PathPrefix{fallbackPrefix(t, "/api"), fallbackPrefix(t, "/admin")}
	base, _ := NewRouter()
	base, err := base.WithFallbacks(mount.Fallback(config))
	if err != nil {
		t.Fatal(err)
	}
	// Without a SPA, B30 owns missing HTML navigations, including a trailing slash.
	if got := fallbackRequest(base, "GET", "/lost/"); got.Header().Get("X-Navigation") != "yes" || got.Header().Get("X-Public") != "yes" {
		t.Fatal("navigation missing", got.Header())
	}
	for _, target := range []string{"/missing.js", "/.env", "/api/unknown"} {
		if got := fallbackRequest(base, "GET", target); got.Header().Get("X-Navigation") != "" {
			t.Fatal("unexpected navigation", target)
		}
	}
	rootSPA := DefaultSPAConfig()
	rootSPA.Exclude = []string{"/private"}
	router, err := base.WithSPA("home", spaAssets, rootSPA)
	if err != nil {
		t.Fatal(err)
	}
	adminSPA := DefaultSPAConfig()
	adminSPA.Prefix = "/admin"
	adminSPA.Exclude = []string{"/admin/api"}
	router, err = router.WithSPA("admin", spaAssets, adminSPA)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		target       string
		status       int
		body, public string
	}{
		{"/site.css", 200, "abcdefghij", "yes"}, {"/account/orders/", 200, "portal", "yes"},
		{"/admin/orders/", 200, "portal", ""}, {"/admin/api/no", 404, "", ""}, {"/admin/site.css", 404, "", ""},
		{"/private", 404, "", "yes"}, {"/missing.js", 404, "", "yes"}, {"/api/unknown", 404, "", ""},
	} {
		got := fallbackRequest(router, "GET", tc.target)
		if got.Code != tc.status || got.Header().Get("X-Public") != tc.public || tc.body != "" && got.Body.String() != tc.body {
			t.Fatal(tc.target, got.Code, got.Header(), got.Body.String())
		}
	}
	if navigation != 1 {
		t.Fatal("SPA miss reached navigation", navigation)
	}
	for _, tc := range []struct {
		method, target, header, value string
		status                        int
		body                          string
	}{
		{"HEAD", "/site.css", "", "", 200, ""}, {"GET", "/site.css", "Range", "bytes=2-5", 206, "cdef"},
		{"GET", "/site.css", "If-Modified-Since", "Thu, 02 Jan 2020 03:04:05 GMT", 304, ""},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, tc.target, nil)
		if tc.header != "" {
			r.Header.Set(tc.header, tc.value)
		}
		router.ServeHTTP(w, r)
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestFallbackAssetsRejectEscapes(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("PRIVATE"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "safe.txt"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	assets := assetsForTest(t, DefaultAssetsConfig(DirectoryAssets(directory)))
	router, _ := NewRouter()
	router, err := router.WithFallbacks(assets.Mount("public", "/").Fallback(DefaultFallbackConfig()))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/a%2Fb.txt", "/a%5cb.txt", "/%2e%2e/secret.txt", "/escape.txt"} {
		got := fallbackRequest(router, "GET", target)
		if got.Code < 400 || strings.Contains(got.Body.String(), "PRIVATE") {
			t.Fatal("unsafe lookup", target, got.Code, got.Body.String())
		}
	}
	if got := fallbackRequest(router, "GET", "/safe.txt"); got.Code != 200 || got.Body.String() != "safe" {
		t.Fatal("healthy lookup after rejection", got.Code)
	}
}

func TestFallbackExplicitMethods(t *testing.T) {
	base, err := NewRouter()
	if err != nil {
		t.Fatal(err)
	}
	router, err := base.WithFallbacks(DefineFallback("custom", fallbackPrefix(t, "/"), stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { w.WriteHeader(204) }), FallbackConfig{Methods: []Method{POST, OPTIONS}}))
	if err != nil {
		t.Fatal(err)
	}
	for method, status := range map[string]int{"POST": 204, "OPTIONS": 204, "GET": 404, "HEAD": 404} {
		w := fallbackRequest(router, method, "/missing")
		if w.Code != status || w.Header().Get("Allow") != "" {
			t.Fatal(method, w.Code, w.Header())
		}
	}
	if got := router.Routes()[0].FallbackDetails.Methods; !slices.Equal(got, []Method{POST, OPTIONS}) {
		t.Fatal("incorrect method inspection", got)
	}
}
