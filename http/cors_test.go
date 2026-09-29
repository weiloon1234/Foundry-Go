package http

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCORSPreflightAndActualResponse(t *testing.T) {
	original := httptest.NewRecorder()
	called := 0
	config := CORSConfig{
		Origins: []Origin{"https://client.test"}, Methods: []Method{PATCH},
		Headers:       []HeaderName{"Content-Type", "Authorization"},
		ExposeHeaders: []HeaderName{"X-Request-Id"}, Credentials: true, MaxAge: 10 * time.Minute,
	}
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		called++
		if w != original {
			t.Error("CORS replaced the native response writer")
		}
		if _, ok := w.(stdhttp.Flusher); !ok {
			t.Error("native writer capability lost")
		}
		w.Header().Set("X-Request-Id", "request-1")
		w.WriteHeader(201)
	}), CORS(config))
	if err != nil {
		t.Fatal(err)
	}
	preflight := httptest.NewRequest("OPTIONS", "/resource", nil)
	preflight.Header.Set("Origin", "https://client.test")
	preflight.Header.Set("Access-Control-Request-Method", "PATCH")
	preflight.Header.Add("Access-Control-Request-Headers", "authorization, content-type")
	preflight.Header.Add("Access-Control-Request-Headers", "\tAUTHORIZATION ")
	response := httptest.NewRecorder()
	response.Header().Add("Vary", "Accept-Encoding, origin")
	handler.ServeHTTP(response, preflight)
	if response.Code != 204 || called != 0 || response.Body.Len() != 0 {
		t.Fatalf("preflight: status=%d calls=%d body=%q", response.Code, called, response.Body.String())
	}
	for key, expected := range map[string]string{
		"Access-Control-Allow-Origin": "https://client.test", "Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods": "PATCH", "Access-Control-Allow-Headers": "Authorization, Content-Type", "Access-Control-Max-Age": "600",
	} {
		if response.Header().Get(key) != expected {
			t.Errorf("%s = %q; want %q", key, response.Header().Get(key), expected)
		}
	}
	assertCORSVary(t, response.Header(), "accept-encoding", "origin", "access-control-request-method", "access-control-request-headers")
	actual := httptest.NewRequest("PATCH", "/resource", nil)
	actual.Header.Set("Origin", "https://client.test")
	handler.ServeHTTP(original, actual)
	if original.Code != 201 || called != 1 || original.Header().Get("Access-Control-Expose-Headers") != "X-Request-Id" || original.Header().Get("Access-Control-Allow-Origin") != "https://client.test" {
		t.Fatal("actual sharing failed")
	}
	if original.Header().Get("Access-Control-Allow-Methods") != "" || original.Header().Get("Access-Control-Max-Age") != "" {
		t.Fatal("preflight-only headers on ordinary response")
	}
}

func TestCORSRequestDecisions(t *testing.T) {
	for _, test := range []struct {
		name, method string
		headers      stdhttp.Header
		status       int
		calls        int
		shared       string
	}{
		{"no-origin", "POST", nil, 202, 1, ""},
		{"disallowed-ordinary-origin", "POST", stdhttp.Header{"Origin": {"https://other.test"}}, 202, 1, ""},
		{"same-origin-ordinary-request", "POST", stdhttp.Header{"Origin": {"http://example.com"}}, 202, 1, ""},
		{"allowed-ordinary-method-outside-preflight-list", "POST", stdhttp.Header{"Origin": {"https://client.test"}}, 202, 1, "https://client.test"},
		{"ordinary-options", "OPTIONS", stdhttp.Header{"Origin": {"https://client.test"}}, 202, 1, "https://client.test"},
		{"missing-preflight-origin-is-not-preflight", "OPTIONS", stdhttp.Header{"Access-Control-Request-Method": {"PATCH"}}, 202, 1, ""},
		{"denied-preflight-origin", "OPTIONS", stdhttp.Header{"Origin": {"https://other.test"}, "Access-Control-Request-Method": {"PATCH"}}, 403, 0, ""},
		{"denied-preflight-method", "OPTIONS", stdhttp.Header{"Origin": {"https://client.test"}, "Access-Control-Request-Method": {"DELETE"}}, 403, 0, ""},
		{"denied-preflight-header", "OPTIONS", stdhttp.Header{"Origin": {"https://client.test"}, "Access-Control-Request-Method": {"PATCH"}, "Access-Control-Request-Headers": {"Authorization"}}, 403, 0, ""},
		{"malformed-preflight-method", "OPTIONS", stdhttp.Header{"Origin": {"https://client.test"}, "Access-Control-Request-Method": {"patch"}}, 400, 0, ""},
		{"duplicate-method", "OPTIONS", stdhttp.Header{"Origin": {"https://client.test"}, "Access-Control-Request-Method": {"PATCH", "PATCH"}}, 400, 0, ""},
		{"empty-method", "OPTIONS", stdhttp.Header{"Origin": {"https://client.test"}, "Access-Control-Request-Method": {""}}, 400, 0, ""},
		{"empty-origin", "GET", stdhttp.Header{"Origin": {""}}, 202, 1, ""},
		{"multiple-origins", "GET", stdhttp.Header{"Origin": {"https://client.test", "https://other.test"}}, 202, 1, ""},
		{"malformed-origin", "GET", stdhttp.Header{"Origin": {"https://client.test/secret"}}, 202, 1, ""},
		{"capacitor-origin-not-listed", "GET", stdhttp.Header{"Origin": {"capacitor://localhost"}}, 202, 1, ""},
		{"extension-origin-not-listed", "POST", stdhttp.Header{"Origin": {"chrome-extension://abcdefghijklmnopabcdefghijklmnop"}}, 202, 1, ""},
		{"tauri-preflight-not-listed", "OPTIONS", stdhttp.Header{"Origin": {"tauri://localhost"}, "Access-Control-Request-Method": {"PATCH"}}, 403, 0, ""},
		{"malformed-preflight-origin", "OPTIONS", stdhttp.Header{"Origin": {"https://client.test/x"}, "Access-Control-Request-Method": {"patch"}}, 403, 0, ""},
		{"opaque-origin-not-implicitly-allowed", "GET", stdhttp.Header{"Origin": {"null"}}, 202, 1, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { calls++; w.WriteHeader(202) }), CORS(CORSConfig{Origins: []Origin{"https://client.test"}, Methods: []Method{PATCH}}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(test.method, "/", nil)
			request.Header = test.headers
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || calls != test.calls || response.Header().Get("Access-Control-Allow-Origin") != test.shared {
				t.Fatalf("decision: status=%d calls=%d headers=%v", response.Code, calls, response.Header())
			}
			if test.method == "OPTIONS" {
				assertCORSVary(t, response.Header(), "origin", "access-control-request-method", "access-control-request-headers")
			} else {
				assertCORSVary(t, response.Header(), "origin")
			}
			if test.status >= 400 {
				var body ErrorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Status != test.status || strings.Contains(response.Body.String(), "secret") {
					t.Fatalf("unsafe/unstructured denial: %s, %v", response.Body.String(), err)
				}
			}
		})
	}
}

func TestCORSWildcardAndOwnedConfiguration(t *testing.T) {
	config := CORSConfig{Origins: []Origin{"https://CLIENT.test:443", NullOrigin}, AnyMethod: true, AnyHeaders: true, Credentials: true, ExposeHeaders: []HeaderName{"X-Version"}}
	declaration := CORS(config)
	config.Origins[0] = "https://evil.test"
	config.ExposeHeaders[0] = "X-Changed"
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(200) }), declaration)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, origin := range []string{"https://client.test", "null"} {
		for range 8 {
			group.Go(func() {
				request := httptest.NewRequest("OPTIONS", "/", nil)
				request.Header.Set("Origin", origin)
				request.Header.Set("Access-Control-Request-Method", "DELETE")
				request.Header.Set("Access-Control-Request-Headers", "authorization, x-extra")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != 204 || response.Header().Get("Access-Control-Allow-Origin") != origin || response.Header().Get("Access-Control-Allow-Headers") != "Authorization, X-Extra" || response.Header().Get("Access-Control-Max-Age") != "0" {
					t.Errorf("owned/any policy: status=%d headers=%v", response.Code, response.Header())
				}
			})
		}
	}
	group.Wait()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Origin", "https://client.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Expose-Headers") != "X-Version" {
		t.Fatal("caller mutated exposed headers")
	}
	wildcard, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(200) }), CORS(CORSConfig{AnyOrigin: true, AnyMethod: true, AnyHeaders: true}))
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "null", "https://other.test"} {
		request := httptest.NewRequest("GET", "/", nil)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		wildcard.ServeHTTP(response, request)
		if response.Code != 200 || response.Header().Get("Access-Control-Allow-Origin") != "*" || response.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("wildcard actual policy")
		}
	}
}

func TestCORSOriginPatternsAndPathPolicies(t *testing.T) {
	config := CORSConfig{
		Origins:        []Origin{"https://client.test"},
		OriginPatterns: []OriginPattern{"capacitor://localhost", "Chrome-Extension://ABCDEF", "https://*.tenant.test", "http://*.dev.test:8080"},
		Methods:        []Method{PATCH},
		Paths:          []CORSPath{{Prefix: "/public", Policy: CORSConfig{AnyOrigin: true}}, {Prefix: "/public/private", Policy: CORSConfig{}}},
	}
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(200) }), CORS(config))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, origin, shared string }{
		{"/", "capacitor://localhost", "capacitor://localhost"},
		{"/", "CAPACITOR://LocalHost", "CAPACITOR://LocalHost"},
		{"/", "chrome-extension://abcdef", "chrome-extension://abcdef"},
		{"/", "capacitor://localhost:8100", ""},
		{"/", "ionic://localhost", ""},
		{"/", "https://a.tenant.test", "https://a.tenant.test"},
		{"/", "https://a.b.tenant.test:443", "https://a.b.tenant.test:443"},
		{"/", "https://tenant.test", ""},
		{"/", "https://evil-tenant.test", ""},
		{"/", "https://a.tenant.test.evil.test", ""},
		{"/", "http://a.tenant.test", ""},
		{"/", "https://a.tenant.test:8443", ""},
		{"/", "http://x.dev.test:8080", "http://x.dev.test:8080"},
		{"/", "http://x.dev.test", ""},
		{"/", "https://..tenant.test", ""},
		{"/public", "https://other.test", "*"},
		{"/public/file", "capacitor://unknown", "*"},
		{"/publicity", "https://other.test", ""},
		{"/public/private/x", "https://client.test", ""},
	} {
		request := httptest.NewRequest("GET", test.path, nil)
		request.Header.Set("Origin", test.origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 200 || response.Header().Get("Access-Control-Allow-Origin") != test.shared {
			t.Errorf("%s %s: status=%d shared=%q", test.path, test.origin, response.Code, response.Header().Get("Access-Control-Allow-Origin"))
		}
	}
	preflight := httptest.NewRequest("OPTIONS", "/items", nil)
	preflight.Header.Set("Origin", "https://a.tenant.test")
	preflight.Header.Set("Access-Control-Request-Method", "PATCH")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, preflight)
	if response.Code != 204 || response.Header().Get("Access-Control-Allow-Origin") != "https://a.tenant.test" {
		t.Fatalf("wildcard preflight: %d %v", response.Code, response.Header())
	}
	for _, invalid := range []CORSConfig{
		{OriginPatterns: []OriginPattern{"*"}}, {OriginPatterns: []OriginPattern{"https://*"}}, {OriginPatterns: []OriginPattern{"https://*.com"}},
		{OriginPatterns: []OriginPattern{"https://a*.example.test"}}, {OriginPatterns: []OriginPattern{"https://*.*.example.test"}},
		{OriginPatterns: []OriginPattern{"https://*.example.test/path"}}, {OriginPatterns: []OriginPattern{"https://example.test"}},
		{OriginPatterns: []OriginPattern{"https://*.192.0.2.1"}}, {OriginPatterns: []OriginPattern{"capacitor://"}},
		{OriginPatterns: []OriginPattern{"capacitor://local host"}}, {OriginPatterns: []OriginPattern{"capacitor://localhost/path"}},
		{OriginPatterns: []OriginPattern{"null"}}, {OriginPatterns: []OriginPattern{"1app://localhost"}},
		{OriginPatterns: []OriginPattern{"tauri://localhost", "TAURI://LOCALHOST"}},
		{AnyOrigin: true, OriginPatterns: []OriginPattern{"tauri://localhost"}},
		{Paths: []CORSPath{{Prefix: "api"}}}, {Paths: []CORSPath{{Prefix: "/api/"}}}, {Paths: []CORSPath{{Prefix: "/a/../b"}}},
		{Paths: []CORSPath{{Prefix: "/api"}, {Prefix: "/api"}}}, {Paths: []CORSPath{{Prefix: "/api", Policy: CORSConfig{Paths: []CORSPath{{Prefix: "/x"}}}}}},
		{Paths: []CORSPath{{Prefix: "/api", Policy: CORSConfig{AnyOrigin: true, Credentials: true}}}},
	} {
		if invalid.Validate() == nil {
			t.Errorf("accepted invalid config %+v", invalid)
		}
	}
}

func TestCORSConfigurationAndRequestedHeaderBounds(t *testing.T) {
	for _, config := range []CORSConfig{
		{AnyOrigin: true, Credentials: true}, {AnyOrigin: true, Origins: []Origin{"https://client.test"}},
		{AnyMethod: true, Methods: []Method{GET}}, {AnyHeaders: true, Headers: []HeaderName{"Accept"}},
		{Origins: []Origin{"https://client.test/"}}, {Origins: []Origin{"https://client.test", "https://CLIENT.test:443"}},
		{Methods: []Method{"get"}}, {Methods: []Method{GET, GET}}, {Headers: []HeaderName{"x-id", "X-ID"}},
		{Headers: []HeaderName{"*"}}, {ExposeHeaders: []HeaderName{"*"}}, {ExposeHeaders: []HeaderName{"X\r\nBad"}},
		{MaxAge: -time.Second}, {MaxAge: time.Millisecond}, {MaxAge: 25 * time.Hour},
		{Origins: make([]Origin, maxCORSItems+1)}, {Headers: []HeaderName{HeaderName(strings.Repeat("a", 257))}},
	} {
		if err := config.Validate(); err == nil {
			t.Errorf("accepted invalid config %+v", config)
		}
		if _, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {}), CORS(config)); err == nil {
			t.Errorf("assembly accepted invalid config %+v", config)
		}
	}
	for _, values := range [][]string{{""}, {"a,,b"}, {"a,"}, {"a, :bad"}, {"*"}, {"a\r\nb"}, {"a,\u00a0b"}, {strings.Repeat("a,", maxCORSItems) + "a"}, {strings.Repeat("a", maxCORSHeaderBytes+1)}, make([]string, maxCORSItems+1)} {
		if _, err := corsRequestedHeaders(values); err == nil {
			t.Errorf("accepted malformed or unbounded headers: %q", values)
		}
	}
	if got, err := corsRequestedHeaders([]string{"x-id, X-ID", "authorization"}); err != nil || !slices.Equal(got, []string{"X-Id", "Authorization"}) {
		t.Fatalf("header list normalization: %v, %v", got, err)
	}
	if err := (CORSConfig{}).Validate(); err != nil {
		t.Fatal("zero policy must be valid", err)
	}
}

func TestCORSVaryWildcardRemainsUnchanged(t *testing.T) {
	header := stdhttp.Header{"Vary": {"Accept-Encoding", "*"}}
	before := slices.Clone(header.Values("Vary"))
	appendVary(header, "Origin")
	if !slices.Equal(before, header.Values("Vary")) {
		t.Fatal("appended to wildcard Vary")
	}
}

func assertCORSVary(t *testing.T, header stdhttp.Header, expected ...string) {
	t.Helper()
	var actual []string
	for _, value := range header.Values("Vary") {
		for _, name := range strings.Split(value, ",") {
			actual = append(actual, strings.ToLower(strings.TrimSpace(name)))
		}
	}
	slices.Sort(actual)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		t.Fatalf("Vary = %v; want %v", actual, expected)
	}
}
