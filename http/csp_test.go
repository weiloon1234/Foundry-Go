package http

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCSPSourceGrammar(t *testing.T) {
	for _, text := range []string{"*", "example.test", "*.example.test", "https://*.example.test:443/assets/", "http://127.0.0.1:*", "https://example.test/a%20b.js", "example.test.", "wss://events.example.test"} {
		if err := CSPHostSource(text).Validate(); err != nil {
			t.Errorf("valid host %q: %v", text, err)
		}
	}
	for _, text := range []string{"", "'self'", "https:", "https://", "//example.test", "example.test:bad", "example.test:", "example.test:12:34", "foo.*.test", "https://user@example.test", "https://example.test/?q=x", "https://example.test/#x", "https://example.test/a;b", "https://example.test/a,b", "https://example.test/a\\b", "https://example.test/%zz", "https://[::1]", "https://é.test", "example.test 'unsafe-inline'", "example.test\r\nX-Bad: true", "example..test", "example.test\x00"} {
		if CSPHostSource(text).Validate() == nil {
			t.Errorf("accepted invalid host %q", text)
		}
	}
	for _, text := range []string{"https", "data", "blob", "custom+v1.ext"} {
		if CSPSchemeSource(text).Validate() != nil {
			t.Errorf("valid scheme %q", text)
		}
	}
	for _, text := range []string{"", "1bad", "https:", "https; img-src *", "https\n"} {
		if CSPSchemeSource(text).Validate() == nil {
			t.Errorf("accepted scheme %q", text)
		}
	}
	if (CSPSource{}).Validate() == nil {
		t.Fatal("zero source is valid")
	}
}

func TestCSPPolicyPresenceHashAndDeterminism(t *testing.T) {
	digest := sha256.Sum256([]byte("console.log('foundry')"))
	p := CSPPolicy{DefaultSrc: []CSPSource{}, ScriptSrc: []CSPSource{CSPSHA256(digest)}, ImgSrc: []CSPSource{CSPSelf(), CSPSchemeSource("data")}, Sandbox: []CSPSandboxToken{}, ReportTo: "csp", ReportURI: []CSPReportURI{"/reports/csp"}, UpgradeInsecureRequests: true}
	want := "default-src 'none'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'; img-src 'self' data:; sandbox; upgrade-insecure-requests; report-to csp; report-uri /reports/csp"
	for range 3 {
		compiled, err := compileCSPPolicy(p, false)
		if err != nil || compiled.text != want || compiled.nonce {
			t.Fatalf("compiled = %+v, %v", compiled, err)
		}
	}
	all := CSPPolicy{DefaultSrc: []CSPSource{}, ScriptSrc: []CSPSource{}, ScriptSrcElem: []CSPSource{}, ScriptSrcAttr: []CSPSource{}, StyleSrc: []CSPSource{}, StyleSrcElem: []CSPSource{}, StyleSrcAttr: []CSPSource{}, ChildSrc: []CSPSource{}, ConnectSrc: []CSPSource{}, FontSrc: []CSPSource{}, FrameSrc: []CSPSource{}, ImgSrc: []CSPSource{}, ManifestSrc: []CSPSource{}, MediaSrc: []CSPSource{}, ObjectSrc: []CSPSource{}, WorkerSrc: []CSPSource{}, BaseURI: []CSPSource{}, FormAction: []CSPSource{}, FrameAncestors: []CSPSource{}}
	compiled, err := compileCSPPolicy(all, false)
	if err != nil || strings.Count(compiled.text, "'none'") != 19 {
		t.Fatalf("source field lost: %s, %v", compiled.text, err)
	}
}

func TestCSPInvalidConfigurationFailsAssembly(t *testing.T) {
	tests := []CSPConfig{
		{Enforce: value.Set(CSPPolicy{})},
		{Enforce: value.Set(CSPPolicy{DefaultSrc: []CSPSource{CSPNone(), CSPSelf()}})},
		{Enforce: value.Set(CSPPolicy{DefaultSrc: []CSPSource{CSPSelf(), CSPSelf()}})},
		{Enforce: value.Set(CSPPolicy{ImgSrc: []CSPSource{CSPNonceSource()}})},
		{Enforce: value.Set(CSPPolicy{ScriptSrcAttr: []CSPSource{CSPNonceSource()}})},
		{Enforce: value.Set(CSPPolicy{FrameAncestors: []CSPSource{CSPUnsafeInline()}})},
		{Enforce: value.Set(CSPPolicy{DefaultSrc: []CSPSource{{}}})},
		{Enforce: value.Set(CSPPolicy{Sandbox: []CSPSandboxToken{"invented"}})},
		{Enforce: value.Set(CSPPolicy{Sandbox: []CSPSandboxToken{CSPSandboxForms, CSPSandboxForms}})},
		{ReportOnly: value.Set(CSPPolicy{Sandbox: []CSPSandboxToken{}})},
		{ReportOnly: value.Set(CSPPolicy{UpgradeInsecureRequests: true})},
		{Enforce: value.Set(CSPPolicy{ReportTo: "report; img-src *"})},
		{Enforce: value.Set(CSPPolicy{ReportURI: []CSPReportURI{"https://user:password@example.test/"}})},
		{Enforce: value.Set(CSPPolicy{ReportURI: []CSPReportURI{"//example.test/report"}})},
		{Enforce: value.Set(CSPPolicy{ReportURI: []CSPReportURI{"/reports; img-src *"}})},
		{Enforce: value.Set(CSPPolicy{ReportURI: []CSPReportURI{"/reports", "/reports"}})},
		{Enforce: value.Set(CSPPolicy{DefaultSrc: make([]CSPSource, 257)})},
	}
	for i, config := range tests {
		if config.Validate() == nil {
			t.Errorf("case %d validated", i)
		}
		handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) { t.Error("invalid middleware reached handler") }), ContentSecurityPolicy(config))
		if handler != nil || err == nil {
			t.Errorf("case %d assembled", i)
		}
	}
	large := CSPPolicy{ImgSrc: []CSPSource{}}
	for i := range 8 {
		large.ImgSrc = append(large.ImgSrc, CSPHostSource("example.test/"+strings.Repeat(string(rune('a'+i)), 1100)))
	}
	if large.Validate() == nil {
		t.Fatal("oversized header accepted")
	}
}

func TestCSPNonceMatchesBothPoliciesAndPreservesContextAndWriter(t *testing.T) {
	policy := CSPPolicy{DefaultSrc: []CSPSource{CSPNone()}, ScriptSrc: []CSPSource{CSPNonceSource(), CSPStrictDynamic()}, StyleSrc: []CSPSource{CSPNonceSource()}}
	config := CSPConfig{Enforce: value.Set(policy), ReportOnly: value.Set(policy)}
	var original *httptest.ResponseRecorder
	type domainKey struct{}
	nonces := make(map[string]bool)
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if w != original {
			t.Error("native writer was replaced")
		}
		if _, ok := w.(stdhttp.Flusher); !ok {
			t.Error("flush capability was lost")
		}
		if r.Context().Value(domainKey{}) != "domain" || r.Context().Err() != context.Canceled {
			t.Error("parent context was lost")
		}
		nonce, ok := CSPNonceFromContext(r.Context())
		if !ok {
			t.Fatal("nonce missing")
		}
		decoded, err := base64.StdEncoding.DecodeString(nonce.String())
		if err != nil || len(decoded) != 32 || nonces[nonce.String()] {
			t.Fatal("invalid/reused nonce")
		}
		nonces[nonce.String()] = true
		for _, name := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only"} {
			if strings.Count(w.Header().Get(name), "'nonce-"+nonce.String()+"'") != 2 {
				t.Errorf("nonce did not match %s", name)
			}
		}
		w.WriteHeader(204)
	}), ContentSecurityPolicy(config))
	if err != nil {
		t.Fatal(err)
	}
	// Modifying the original declarations cannot change the compiled policy.
	policy.ScriptSrc[0] = CSPUnsafeInline()
	for range 3 {
		ctx, cancel := context.WithCancel(context.WithValue(t.Context(), domainKey{}, "domain"))
		cancel()
		request := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		original = httptest.NewRecorder()
		handler.ServeHTTP(original, request)
		if original.Code != 204 || original.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("nonce response cache policy missing")
		}
		if _, ok := CSPNonceFromContext(request.Context()); ok {
			t.Fatal("nonce escaped into original request")
		}
	}
	if _, ok := CSPNonceFromContext(nil); ok {
		t.Fatal("nil context has nonce")
	}
}

func TestCSPStaticFallbackAndConcurrentNonceRequests(t *testing.T) {
	router, err := NewRouter()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(router, ContentSecurityPolicy(CSPConfig{Enforce: value.Set(CSPPolicy{DefaultSrc: []CSPSource{CSPNone()}})}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	response.Header().Set("Cache-Control", "public, max-age=60")
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/missing", nil))
	// The shared error response owns its no-store policy. Static CSP preserves
	// that downstream decision rather than making the failure cacheable.
	if response.Code != 404 || response.Header().Get("Content-Security-Policy") != "default-src 'none'" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("static/fallback policy mismatch: status=%d, headers=%v", response.Code, response.Header())
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	handler, err = ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		n, ok := CSPNonceFromContext(r.Context())
		if !ok {
			t.Error("missing concurrent nonce")
			return
		}
		mu.Lock()
		if seen[n.String()] {
			t.Error("duplicate concurrent nonce")
		}
		seen[n.String()] = true
		mu.Unlock()
		w.WriteHeader(204)
	}), ContentSecurityPolicy(CSPConfig{ReportOnly: value.Set(CSPPolicy{ScriptSrc: []CSPSource{CSPNonceSource()}})}))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != 204 || w.Header().Get("Content-Security-Policy") != "" || w.Header().Get("Content-Security-Policy-Report-Only") == "" {
				t.Error("report-only policy became enforced")
			}
		})
	}
	wg.Wait()
}

func TestCSPNestedPoliciesShareRequestNonce(t *testing.T) {
	leaf := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		n, ok := CSPNonceFromContext(r.Context())
		if !ok {
			t.Error("nested nonce missing")
			return
		}
		for _, name := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only"} {
			if !strings.Contains(w.Header().Get(name), "'nonce-"+n.String()+"'") {
				t.Errorf("nested %s has a different nonce", name)
			}
		}
	})
	policy := CSPPolicy{ScriptSrc: []CSPSource{CSPNonceSource()}}
	inner, err := ApplyMiddleware(leaf, ContentSecurityPolicy(CSPConfig{ReportOnly: value.Set(policy)}))
	if err != nil {
		t.Fatal(err)
	}
	outer, err := ApplyMiddleware(inner, ContentSecurityPolicy(CSPConfig{Enforce: value.Set(policy)}))
	if err != nil {
		t.Fatal(err)
	}
	outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func FuzzCSPHostSource(f *testing.F) {
	for _, text := range []string{"https://*.example.test/assets/", "example.test 'unsafe-inline'", "data:", "example.test/%20", "example.test; script-src *"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		source := CSPHostSource(text)
		if source.Validate() != nil {
			return
		}
		compiled, err := compileCSPPolicy(CSPPolicy{ImgSrc: []CSPSource{source}}, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(compiled.text, "\r\n\x00;,\t") || strings.Count(compiled.text, " ") != 1 {
			t.Fatalf("source injected a directive: %q", compiled.text)
		}
	})
}

func TestCSPStaticPoliciesPreserveSuccessfulResponseCache(t *testing.T) {
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(204)
	}), ContentSecurityPolicy(CSPConfig{Enforce: value.Set(CSPPolicy{DefaultSrc: []CSPSource{CSPNone()}})}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	response.Header().Set("Cache-Control", "public, max-age=60")
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 204 || response.Header().Get("Content-Security-Policy") != "default-src 'none'" || response.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("static CSP changed successful response policy: status=%d, headers=%v", response.Code, response.Header())
	}
}
