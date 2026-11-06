package http

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func spaRuntimeProxyForTest(t *testing.T, descriptor SPARuntime, upstream stdhttp.Handler, navigation func(*stdhttp.Request) bool) stdhttp.Handler {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(target) }}
	handler, err := descriptor.WrapProxy(proxy, navigation)
	if err != nil {
		t.Fatal(err)
	}
	if proxy.ModifyResponse != nil || proxy.ErrorHandler != nil {
		t.Fatal("mutated proxy configuration")
	}
	return handler
}

func TestSPARuntimeProxyDocumentsHEADAndRequestHeaders(t *testing.T) {
	var calls atomic.Int32
	d := DefineSPARuntime(spaRuntimeTestCodec(), func(_ context.Context, r *stdhttp.Request) (SPARuntimeTestDTO, error) {
		calls.Add(1)
		if r.Header.Get("Range") != "bytes=0-1" {
			return SPARuntimeTestDTO{}, errors.New("loader did not receive original request")
		}
		return SPARuntimeTestDTO{Text: r.Method}, nil
	}, DefaultSPARuntimeConfig())
	upstream := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != "GET" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("wrong upstream request", r.Method, r.Header)
		}
		for _, name := range []string{"If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "Range", "If-Range"} {
			if r.Header.Get(name) != "" {
				t.Error("upstream request retained", name)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"upstream"`)
		w.Header().Set("Last-Modified", "Thu, 01 Jan 1970 00:00:00 GMT")
		w.Header().Set("Content-Length", "13")
		w.Header().Set("Accept-Ranges", "bytes")
		_, _ = io.WriteString(w, "<head></head>")
	})
	handler := spaRuntimeProxyForTest(t, d, upstream, func(*stdhttp.Request) bool { return true })
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, "/dashboard", nil)
		for _, name := range []string{"If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "If-Range"} {
			r.Header.Set(name, "old")
		}
		r.Header.Set("Range", "bytes=0-1")
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String(), w.Header())
		}
		for _, name := range []string{"ETag", "Last-Modified", "Accept-Ranges", "Content-Range", "Content-Encoding"} {
			if w.Header().Get(name) != "" {
				t.Fatal(name, w.Header())
			}
		}
		length, err := strconv.Atoi(w.Header().Get("Content-Length"))
		if err != nil || length < 13 {
			t.Fatal(w.Header())
		}
		if method == "HEAD" && w.Body.Len() != 0 || method == "GET" && (w.Body.Len() != length || !strings.Contains(w.Body.String(), `"text":"GET"`)) {
			t.Fatal(method, w.Body.String())
		}
		if r.Method != method || r.Header.Get("Accept-Encoding") != "gzip" || r.Header.Get("If-Match") != "old" {
			t.Fatal("original request changed")
		}
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
}

func TestSPARuntimeProxyPassThroughAndMiddleware(t *testing.T) {
	var loads atomic.Int32
	config := DefaultSPARuntimeConfig()
	config.HTMLBytes = 16
	d := DefineSPARuntime(spaRuntimeTestCodec(), func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) {
		loads.Add(1)
		return SPARuntimeTestDTO{}, nil
	}, config)
	upstream := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"asset"`)
		w.Header().Set("X-Replay", "original")
		if r.URL.Path == "/asset.css" {
			if r.Header.Get("Range") != "bytes=0-1" {
				t.Error("asset range stripped")
			}
			w.WriteHeader(206)
			_, _ = io.WriteString(w, "ab")
			return
		}
		if r.URL.Path == "/partial-text" {
			w.WriteHeader(206)
			_, _ = io.WriteString(w, "ab")
			return
		}
		if r.Method == "POST" {
			w.WriteHeader(201)
			_, _ = io.WriteString(w, "stored-response")
			return
		}
		_, _ = io.WriteString(w, strings.Repeat("x", 1024))
	})
	proxy := spaRuntimeProxyForTest(t, d, upstream, func(r *stdhttp.Request) bool { return r.URL.Path != "/asset.css" })
	var wrappedCalls atomic.Int32
	middleware := DefineMiddleware("test.replay", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			wrappedCalls.Add(1)
			w.Header().Set("X-Outer", "kept")
			next.ServeHTTP(w, r)
		}), nil
	}).PreservesIdempotentResponses()
	// Exercise the same assembly marker used by routers with idempotent endpoints.
	handler, err := ApplyMiddleware(idempotentHTTPHandler{proxy}, middleware)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/asset.css", "ab", 206},
		{"GET", "/partial-text", "ab", 206},
		{"POST", "/api/write", "stored-response", 201},
		{"GET", "/non-html", strings.Repeat("x", 1024), 200},
		{"HEAD", "/non-html", "", 200},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("Range", "bytes=0-1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || w.Body.String() != tc.body || w.Header().Get("ETag") != `"asset"` || w.Header().Get("X-Replay") != "original" || w.Header().Get("X-Outer") != "kept" {
			t.Fatal(tc.path, w.Code, w.Header(), w.Body.String())
		}
	}
	if loads.Load() != 0 || wrappedCalls.Load() != 5 {
		t.Fatal(loads.Load(), wrappedCalls.Load())
	}
}

func TestSPARuntimeProxyFailuresAndRecovery(t *testing.T) {
	for _, scenario := range []string{"oversize", "encoded", "partial", "not-modified", "loader", "missing-head", "trailers"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			config := DefaultSPARuntimeConfig()
			config.HTMLBytes, config.MaxConcurrent = 64, 1
			d := DefineSPARuntime(spaRuntimeTestCodec(), func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) {
				if scenario == "loader" && calls.Add(1) == 1 {
					return SPARuntimeTestDTO{}, errors.New("secret upstream data")
				}
				return SPARuntimeTestDTO{Text: "healthy"}, nil
			}, config)
			var upstreamCalls atomic.Int32
			handler := spaRuntimeProxyForTest(t, d, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Content-Type", "text/html")
				if upstreamCalls.Add(1) == 1 {
					switch scenario {
					case "oversize":
						_, _ = io.WriteString(w, "<head>"+strings.Repeat("x", 128))
						return
					case "encoded":
						w.Header().Set("Content-Encoding", "gzip")
					case "partial":
						w.WriteHeader(206)
					case "not-modified":
						w.WriteHeader(304)
						return
					case "missing-head":
						_, _ = io.WriteString(w, "no head")
						return
					case "trailers":
						w.Header().Set("Trailer", "X-Later")
					}
				}
				_, _ = io.WriteString(w, "<head></head>")
			}), func(*stdhttp.Request) bool { return true })
			first := spaRuntimeRequest(handler, "GET", "/")
			if first.Code != 503 || strings.Contains(first.Body.String(), "secret") || strings.Contains(first.Body.String(), "<head>") {
				t.Fatal(first.Code, first.Body.String())
			}
			second := spaRuntimeRequest(handler, "GET", "/")
			if second.Code != 200 || !strings.Contains(second.Body.String(), "healthy") {
				t.Fatal(second.Code, second.Body.String())
			}
		})
	}
}

func TestSPARuntimeProxyUsesOuterCSPAndDisablesAutomaticETags(t *testing.T) {
	config := DefaultSPARuntimeConfig()
	config.StyleNonceCarrier = true
	d := spaRuntimeTestDescriptor("ok", config)
	proxy := spaRuntimeProxyForTest(t, d, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<head></head>")
	}), func(*stdhttp.Request) bool { return true })
	handler, err := ApplyMiddleware(proxy, ContentSecurityPolicy(CSPConfig{Enforce: value.Set(CSPPolicy{ScriptSrc: []CSPSource{CSPNonceSource()}, StyleSrc: []CSPSource{CSPNonceSource()}})}), ETags(DefaultETagConfig()), Compression(DefaultCompressionConfig()))
	if err != nil {
		t.Fatal(err)
	}
	w := spaRuntimeRequest(handler, "GET", "/")
	_, tail, ok := strings.Cut(w.Header().Get("Content-Security-Policy"), "'nonce-")
	if !ok {
		t.Fatal(w.Header())
	}
	nonce, _, _ := strings.Cut(tail, "'")
	if w.Code != 200 || strings.Count(w.Body.String(), `nonce="`+nonce+`"`) != 2 || w.Header().Get("ETag") != "" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
}

func TestSPARuntimeProxyRetainsCapacityWhileResponseBodyIsOwned(t *testing.T) {
	config := DefaultSPARuntimeConfig()
	config.MaxConcurrent = 1
	d := spaRuntimeTestDescriptor("ok", config)
	response := &stdhttp.Response{StatusCode: 200, Header: stdhttp.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<head></head>"))}
	request := httptest.NewRequest("GET", "/", nil)
	if err := d.state.proxyDocument(response, request); err != nil {
		t.Fatal(err)
	}
	router := spaRuntimeTestRouter(t, d, "<head></head>")
	if got := spaRuntimeRequest(router, "GET", "/app/"); got.Code != 503 {
		t.Fatal("proxy and built documents did not share capacity", got.Code)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if got := spaRuntimeRequest(router, "GET", "/app/"); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
}

type spaRuntimeTestTransport func(*stdhttp.Request) (*stdhttp.Response, error)

func (f spaRuntimeTestTransport) RoundTrip(r *stdhttp.Request) (*stdhttp.Response, error) {
	return f(r)
}

func TestSPARuntimeProxyRetainsCallerHooksAndClosesBodies(t *testing.T) {
	var modified, failures, closed atomic.Int32
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(&url.URL{Scheme: "http", Host: "example.test"}) },
		Transport: spaRuntimeTestTransport(func(r *stdhttp.Request) (*stdhttp.Response, error) {
			if r.URL.Path == "/offline" {
				return nil, errors.New("private upstream failure")
			}
			// A valid custom transport need not populate Response.Request.
			return &stdhttp.Response{StatusCode: 200, Header: make(stdhttp.Header), Body: &spaRuntimeTestBody{Reader: strings.NewReader("<head></head>"), closed: &closed}}, nil
		}),
		ModifyResponse: func(r *stdhttp.Response) error {
			modified.Add(1)
			r.Header.Set("Content-Type", "text/html")
			r.Header.Set("X-Upstream-Hook", "kept")
			return nil
		},
		ErrorHandler: func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ error) {
			failures.Add(1)
			w.WriteHeader(502)
			_, _ = io.WriteString(w, "upstream unavailable")
		},
	}
	d := spaRuntimeTestDescriptor("ok", DefaultSPARuntimeConfig())
	handler, err := d.WrapProxy(proxy, func(*stdhttp.Request) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	w := spaRuntimeRequest(handler, "GET", "/")
	if w.Code != 200 || w.Header().Get("X-Upstream-Hook") != "kept" || modified.Load() != 1 || closed.Load() != 1 {
		t.Fatal(w.Code, w.Header(), modified.Load(), closed.Load())
	}
	w = spaRuntimeRequest(handler, "HEAD", "/offline")
	if w.Code != 502 || w.Body.Len() != 0 || failures.Load() != 1 {
		t.Fatal(w.Code, w.Body.String(), failures.Load())
	}
	bad := DefineSPARuntime(spaRuntimeTestCodec(), func(context.Context, *stdhttp.Request) (SPARuntimeTestDTO, error) {
		return SPARuntimeTestDTO{}, errors.New("private loader failure")
	}, DefaultSPARuntimeConfig())
	handler, err = bad.WrapProxy(proxy, func(*stdhttp.Request) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	w = spaRuntimeRequest(handler, "GET", "/")
	if w.Code != 503 || failures.Load() != 1 || closed.Load() != 2 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String(), failures.Load(), closed.Load())
	}
}

type spaRuntimeTestBody struct {
	*strings.Reader
	closed *atomic.Int32
}

func (b *spaRuntimeTestBody) Close() error { b.closed.Add(1); return nil }

func TestSPARuntimeProxyPredicateFailureAndUpgradeBypass(t *testing.T) {
	var predicateCalls atomic.Int32
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(&url.URL{Scheme: "http", Host: "example.test"}) },
		Transport: spaRuntimeTestTransport(func(r *stdhttp.Request) (*stdhttp.Response, error) {
			return &stdhttp.Response{StatusCode: 204, Header: make(stdhttp.Header), Body: stdhttp.NoBody}, nil
		}),
	}
	d := spaRuntimeTestDescriptor("ok", DefaultSPARuntimeConfig())
	handler, err := d.WrapProxy(proxy, func(*stdhttp.Request) bool { predicateCalls.Add(1); panic("private predicate failure") })
	if err != nil {
		t.Fatal(err)
	}
	w := spaRuntimeRequest(handler, "GET", "/")
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "/hmr", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "upgrade")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 || predicateCalls.Load() != 1 {
		t.Fatal(w.Code, predicateCalls.Load())
	}
}
