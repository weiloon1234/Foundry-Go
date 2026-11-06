package http

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/value"
)

type SPARuntimeTestDTO struct {
	Text string `json:"text"`
}

func spaRuntimeTestCodec() contract.JSON[SPARuntimeTestDTO] {
	return endpointDTO[SPARuntimeTestDTO](contract.Property{Name: "text", Type: "text", Required: true})
}

func spaRuntimeTestDescriptor(text string, config SPARuntimeConfig) SPARuntime {
	return DefineSPARuntime(spaRuntimeTestCodec(), func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) {
		return SPARuntimeTestDTO{Text: text}, nil
	}, config)
}

func spaRuntimeTestRouter(t *testing.T, descriptor SPARuntime, document string) *Router {
	t.Helper()
	assets := assetsForTest(t, DefaultAssetsConfig(FilesystemAssets(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(document)},
		"site.css":   &fstest.MapFile{Data: []byte("abcdefghij")},
	})))
	base, err := NewRouter()
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultSPAConfig()
	config.Prefix = "/app"
	config.Exclude = []string{"/app/api"}
	config.Runtime = descriptor
	router, err := base.WithSPA("runtime", assets, config)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func spaRuntimeRequest(handler stdhttp.Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestSPARuntimeInjectionEscapingAndOriginalMarkup(t *testing.T) {
	text := "</script><!--\u2028\u2029<&>"
	config := DefaultSPARuntimeConfig()
	config.ElementID = `runtime"<&`
	original := `<!doctype html><!-- <head> --><HTML><HEAD data-x="a>b"><script src="/entry.js"></script></HEAD><body>original</body></HTML>`
	router := spaRuntimeTestRouter(t, spaRuntimeTestDescriptor(text, config), original)
	w := spaRuntimeRequest(router, "GET", "/app/dashboard")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	prefix := `<!doctype html><!-- <head> --><HTML><HEAD data-x="a>b"><script id="` + html.EscapeString(config.ElementID) + `" type="application/json">`
	if !strings.HasPrefix(w.Body.String(), prefix) {
		t.Fatal(w.Body.String())
	}
	encoded, rest, ok := strings.Cut(strings.TrimPrefix(w.Body.String(), prefix), "</script>")
	if !ok || rest != `<script src="/entry.js"></script></HEAD><body>original</body></HTML>` {
		t.Fatal("document markup changed", rest)
	}
	for _, unsafe := range []string{"<", ">", "&", "\u2028", "\u2029"} {
		if strings.Contains(encoded, unsafe) {
			t.Fatalf("unsafe script JSON %q", encoded)
		}
	}
	var decoded SPARuntimeTestDTO
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil || decoded.Text != text {
		t.Fatal(decoded, err)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
		t.Fatal(w.Header())
	}
}

func TestSPARuntimeHEADConditionsAndAssets(t *testing.T) {
	router := spaRuntimeTestRouter(t, spaRuntimeTestDescriptor("hello", DefaultSPARuntimeConfig()), "<html><head></head><body>app</body></html>")
	get := spaRuntimeRequest(router, "GET", "/app/dashboard")
	for _, method := range []string{"GET", "HEAD"} {
		for _, path := range []string{"/app/", "/app/index.html", "/app/dashboard"} {
			r := httptest.NewRequest(method, path, nil)
			r.Header.Set("Accept", "text/html")
			for name, val := range map[string]string{"If-None-Match": "*", "If-Modified-Since": time.Now().Add(time.Hour).UTC().Format(stdhttp.TimeFormat), "If-Match": `"stale"`, "If-Unmodified-Since": "Thu, 01 Jan 1970 00:00:00 GMT", "Range": "bytes=0-1", "If-Range": `"stale"`} {
				r.Header.Set(name, val)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != 200 || w.Header().Get("Content-Length") != get.Header().Get("Content-Length") {
				t.Fatal(method, path, w.Code, w.Header())
			}
			for _, name := range []string{"ETag", "Last-Modified", "Accept-Ranges", "Content-Range"} {
				if w.Header().Get(name) != "" {
					t.Fatal(name, w.Header())
				}
			}
			if method == "HEAD" && w.Body.Len() != 0 || method == "GET" && w.Body.String() != get.Body.String() {
				t.Fatal("body semantics", method, w.Body.String())
			}
			if r.Header.Get("Range") != "bytes=0-1" || r.Method != method {
				t.Fatal("mutated caller request")
			}
		}
	}
	r := httptest.NewRequest("GET", "/app/site.css", nil)
	r.Header.Set("Range", "bytes=0-1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "ab" || w.Header().Get("Cache-Control") == "no-store" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	for _, path := range []string{"/app/api/unknown", "/app/missing.js"} {
		if got := spaRuntimeRequest(router, "GET", path); got.Code != 404 {
			t.Fatal(path, got.Code)
		}
	}
}

func TestSPARuntimeBoundsAndInvalidDocuments(t *testing.T) {
	for _, tc := range []struct {
		name, document, text string
		htmlBytes, jsonBytes int
		status               int
	}{
		{"exact", "<head></head>", "ok", 13, 13, 200},
		{"html", "<head></head>x", "ok", 13, 128, 503},
		{"json", "<head></head>", strings.Repeat("x", 128), 32, 32, 503},
		{"escaped json", "<head></head>", "<<<<<<", 32, 32, 503},
		{"no head", "<body>missing head</body>", "ok", 128, 128, 503},
		{"comment head", "<!-- <head> --><body>missing</body>", "ok", 128, 128, 503},
		{"existing", `<head><script id="foundry-portal-runtime"></script></head>`, "ok", 128, 128, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultSPARuntimeConfig()
			config.HTMLBytes, config.JSON.Bytes = tc.htmlBytes, tc.jsonBytes
			router := spaRuntimeTestRouter(t, spaRuntimeTestDescriptor(tc.text, config), tc.document)
			w := spaRuntimeRequest(router, "GET", "/app/")
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Header())
			}
		})
	}
}

func TestSPARuntimeLoaderFailuresAreSafeAndReleaseCapacity(t *testing.T) {
	for _, fail := range []func() error{
		func() error { return errors.New("private-loader-secret") },
		func() error { panic("private-loader-secret") },
		func() error { runtime.Goexit(); return nil },
	} {
		var calls atomic.Int32
		config := DefaultSPARuntimeConfig()
		config.MaxConcurrent = 1
		d := DefineSPARuntime(spaRuntimeTestCodec(), func(ctx context.Context, r *stdhttp.Request) (SPARuntimeTestDTO, error) {
			if r.Context() != ctx || r.Method != "HEAD" && r.Method != "GET" {
				return SPARuntimeTestDTO{}, errors.New("wrong request")
			}
			if calls.Add(1) <= 2 {
				return SPARuntimeTestDTO{}, fail()
			}
			return SPARuntimeTestDTO{Text: "healthy"}, nil
		}, config)
		router := spaRuntimeTestRouter(t, d, "<head></head>")
		w := spaRuntimeRequest(router, "GET", "/app/")
		if w.Code != 503 || strings.Contains(w.Body.String(), "private-loader-secret") {
			t.Fatal(w.Code, w.Body.String())
		}
		w = spaRuntimeRequest(router, "HEAD", "/app/")
		if w.Code != 503 || w.Body.Len() != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = spaRuntimeRequest(router, "GET", "/app/")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "healthy") || strings.Contains(w.Body.String(), "private-loader-secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestSPARuntimeCSPNonceCarrier(t *testing.T) {
	for _, carrier := range []bool{false, true} {
		config := DefaultSPARuntimeConfig()
		config.StyleNonceCarrier = carrier
		router := spaRuntimeTestRouter(t, spaRuntimeTestDescriptor("hello", config), "<head></head>")
		handler, err := ApplyMiddleware(router, ContentSecurityPolicy(CSPConfig{Enforce: value.Set(CSPPolicy{ScriptSrc: []CSPSource{CSPNonceSource()}, StyleSrc: []CSPSource{CSPNonceSource()}})}))
		if err != nil {
			t.Fatal(err)
		}
		w := spaRuntimeRequest(handler, "GET", "/app/")
		_, tail, ok := strings.Cut(w.Header().Get("Content-Security-Policy"), "'nonce-")
		if !ok {
			t.Fatal(w.Header())
		}
		nonce, _, _ := strings.Cut(tail, "'")
		want := 1
		if carrier {
			want = 2
		}
		if w.Code != 200 || strings.Count(w.Body.String(), `nonce="`+nonce+`"`) != want {
			t.Fatal(w.Code, w.Body.String())
		}
		if carrier && !strings.Contains(w.Body.String(), `<style nonce="`+nonce+`"></style>`) {
			t.Fatal(w.Body.String())
		}
		without := spaRuntimeRequest(router, "GET", "/app/")
		if strings.Contains(without.Body.String(), "nonce=") || strings.Contains(without.Body.String(), "<style") {
			t.Fatal("carrier without nonce")
		}
		request := httptest.NewRequest("GET", "/app/", nil)
		request = request.WithContext(context.WithValue(request.Context(), cspNonceContextKey{}, CSPNonce{encoded: `bad"<nonce>`}))
		bad := httptest.NewRecorder()
		router.ServeHTTP(bad, request)
		if bad.Code != 503 || strings.Contains(bad.Body.String(), "bad") {
			t.Fatal(bad.Code, bad.Body.String())
		}
	}
}

func TestSPARuntimeCancellationRetainsAdmissionUntilLoaderExits(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	config := DefaultSPARuntimeConfig()
	config.MaxConcurrent = 1
	d := DefineSPARuntime(spaRuntimeTestCodec(), func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return SPARuntimeTestDTO{Text: "ok"}, nil
	}, config)
	router := spaRuntimeTestRouter(t, d, "<head></head>")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("GET", "/app/", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); router.ServeHTTP(httptest.NewRecorder(), request) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("loader not entered")
	}
	cancel()
	blocked := spaRuntimeRequest(router, "GET", "/app/")
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loader did not exit")
	}
	if blocked.Code != 503 || calls.Load() != 1 {
		t.Fatal("capacity released before callback exit", blocked.Code, calls.Load())
	}
	if got := spaRuntimeRequest(router, "GET", "/app/"); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
}

func TestSPARuntimeRegistrationRejectsInvalidDeclarations(t *testing.T) {
	config := DefaultSPARuntimeConfig()
	load := func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) { return SPARuntimeTestDTO{}, nil }
	for _, d := range []SPARuntime{DefineSPARuntime(spaRuntimeTestCodec(), nil, config), DefineSPARuntime(contract.JSON[SPARuntimeTestDTO]{}, load, config), DefineSPARuntime(spaRuntimeTestCodec(), load, SPARuntimeConfig{})} {
		c := DefaultSPAConfig()
		c.Runtime = d
		if err := c.Validate(); err == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
	if err := DefaultSPAConfig().Validate(); err != nil {
		t.Fatal("zero runtime must remain optional", err)
	}
}
