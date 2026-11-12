package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCSPAllowlistExpansionAndDeduplication(t *testing.T) {
	base := CSPPolicy{
		DefaultSrc: []CSPSource{CSPSelf()},
		ScriptSrc:  []CSPSource{CSPSelf(), CSPHostSource("https://STATIC.cloudflareinsights.com:443")},
		ConnectSrc: []CSPSource{CSPSelf()},
		ImgSrc:     []CSPSource{CSPNone()},
		FrameSrc:   []CSPSource{},
	}
	allowlist := CSPAllowlist{
		Presets:        []CSPPreset{CSPPresetCloudflareWebAnalytics, CSPPresetCloudflareWebAnalytics},
		ScriptOrigins:  []Origin{"https://static.cloudflareinsights.com", "https://scripts.example.test"},
		ConnectOrigins: []Origin{"https://CLOUDFLAREINSIGHTS.com:443"},
		ImageOrigins:   []Origin{"https://images.example.test:443", "https://IMAGES.example.test"},
		FrameOrigins:   []Origin{"https://frames.example.test:8443"},
	}
	policy, err := ApplyCSPAllowlist(base, allowlist)
	if err != nil {
		t.Fatal(err)
	}
	want := "default-src 'self'; script-src 'self' https://STATIC.cloudflareinsights.com:443 https://scripts.example.test; connect-src 'self' https://cloudflareinsights.com; frame-src https://frames.example.test:8443; img-src https://images.example.test; worker-src 'self' https://STATIC.cloudflareinsights.com:443"
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { w.WriteHeader(204) }), ContentSecurityPolicy(CSPConfig{Enforce: value.Set(policy)}))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 204 || recorder.Header().Get("Content-Security-Policy") != want {
		t.Fatalf("response = %d, %s", recorder.Code, recorder.Header().Get("Content-Security-Policy"))
	}
	again, err := ApplyCSPAllowlist(policy, allowlist)
	if err != nil || !reflect.DeepEqual(again, policy) {
		t.Fatalf("apply twice changed the policy: %v", err)
	}
}

func TestCSPAllowlistPreservesWorkerFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		script []CSPSource
		worker []CSPSource
		child  []CSPSource
	}{
		{name: "self", script: []CSPSource{CSPSelf()}},
		{name: "nonce-only", script: []CSPSource{CSPNonceSource()}},
		{name: "nonce-and-self", script: []CSPSource{CSPNonceSource(), CSPSelf()}},
		{name: "none", script: []CSPSource{CSPNone()}},
		{name: "empty", script: []CSPSource{}},
		{name: "explicit-worker", script: []CSPSource{CSPSelf()}, worker: []CSPSource{CSPHostSource("https://workers.example.test")}},
		{name: "empty-worker", script: []CSPSource{CSPSelf()}, worker: []CSPSource{}},
		{name: "child", script: []CSPSource{CSPSelf()}, child: []CSPSource{CSPNone()}},
		{name: "empty-child", script: []CSPSource{CSPSelf()}, child: []CSPSource{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := CSPPolicy{ScriptSrc: test.script, WorkerSrc: test.worker, ChildSrc: test.child}
			out, err := ApplyCSPAllowlist(base, CSPAllowlist{ScriptOrigins: []Origin{"https://scripts.example.test"}})
			if err != nil {
				t.Fatal(err)
			}
			wantWorker := test.worker
			if test.worker == nil && test.child == nil {
				wantWorker = test.script
			}
			if !reflect.DeepEqual(out.WorkerSrc, wantWorker) || !reflect.DeepEqual(out.ChildSrc, test.child) {
				t.Fatalf("worker/child changed: %+v / %+v", out.WorkerSrc, out.ChildSrc)
			}
			if !reflect.DeepEqual(base.WorkerSrc, test.worker) || !reflect.DeepEqual(base.ScriptSrc, test.script) {
				t.Fatal("input policy changed")
			}
			handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { w.WriteHeader(204) }), ContentSecurityPolicy(CSPConfig{Enforce: value.Set(out)}))
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
			header := recorder.Header().Get("Content-Security-Policy")
			if recorder.Code != 204 {
				t.Fatalf("status = %d", recorder.Code)
			}
			for directive := range strings.SplitSeq(header, "; ") {
				if strings.HasPrefix(directive, "worker-src ") && strings.Contains(directive, "https://scripts.example.test") {
					t.Fatalf("worker inherited added host: %s", header)
				}
			}
			if wantWorker != nil && !strings.Contains(header, "worker-src ") {
				t.Fatalf("missing worker directive: %s", header)
			}
			if wantWorker == nil && strings.Contains(header, "worker-src ") {
				t.Fatalf("unexpected worker directive: %s", header)
			}
			if len(out.WorkerSrc) > 0 {
				original := out.WorkerSrc[0]
				out.ScriptSrc[0] = CSPAny()
				if out.WorkerSrc[0] != original {
					t.Fatal("worker shares script storage")
				}
				out.WorkerSrc[0] = CSPAny()
				if wantWorker[0] != original {
					t.Fatal("worker shares input storage")
				}
			}
		})
	}
	base := CSPPolicy{ScriptSrc: []CSPSource{CSPSelf()}, ConnectSrc: []CSPSource{CSPSelf()}}
	out, err := ApplyCSPAllowlist(base, CSPAllowlist{ConnectOrigins: []Origin{"https://api.example.test"}})
	if err != nil || out.WorkerSrc != nil {
		t.Fatalf("non-script addition pinned workers: %v", err)
	}
}

func TestCSPAllowlistRejectsInvalidOriginsAndPresets(t *testing.T) {
	for _, preset := range []CSPPreset{"", "cloudflare", "CLOUDFLARE-WEB-ANALYTICS", "private-invalid-preset"} {
		if err := preset.Validate(); !errors.Is(err, fault.Invalid) {
			t.Errorf("preset %q: %v", preset, err)
		}
		if err := (CSPAllowlist{Presets: []CSPPreset{preset}}).Validate(); !errors.Is(err, fault.Invalid) {
			t.Errorf("allowlist preset %q: %v", preset, err)
		}
	}
	if err := CSPPresetCloudflareWebAnalytics.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []Origin{
		"", NullOrigin, "http://example.test", "https:", "data:", "blob:https://example.test", "*", "https://*.example.test",
		"https://example.test/", "https://example.test/path", "https://example.test?", "https://example.test?q=private", "https://example.test#fragment",
		"https://user:private@example.test", "https://example.test:*", "https://example.test:65536", "'unsafe-inline'", "'unsafe-eval'",
		"https://example.test; script-src *", "https://example.test\r\nX-Bad: yes", "https://[::1]", "https://bad_host.test",
	} {
		for _, field := range []string{"script", "connect", "image", "frame"} {
			t.Run(field+"/"+string(origin), func(t *testing.T) {
				var a CSPAllowlist
				switch field {
				case "script":
					a.ScriptOrigins = []Origin{origin}
				case "connect":
					a.ConnectOrigins = []Origin{origin}
				case "image":
					a.ImageOrigins = []Origin{origin}
				case "frame":
					a.FrameOrigins = []Origin{origin}
				}
				err := a.Validate()
				if !errors.Is(err, fault.Invalid) {
					t.Fatalf("accepted origin: %v", err)
				}
				if strings.Contains(fmt.Sprintf("%+v", err), "private") {
					t.Fatal("diagnostic exposed rejected input")
				}
			})
		}
	}
	for _, origin := range []Origin{"https://example.test", "https://EXAMPLE.test:443", "https://example.test:8443", "https://127.0.0.1", "https://xn--bcher-kva.example"} {
		if err := (CSPAllowlist{ImageOrigins: []Origin{origin}}).Validate(); err != nil {
			t.Errorf("valid origin %q: %v", origin, err)
		}
	}
}

func TestCSPAllowlistPresenceAndFailure(t *testing.T) {
	base := CSPPolicy{DefaultSrc: []CSPSource{CSPSelf()}}
	if err := (CSPAllowlist{}).Validate(); err != nil {
		t.Fatal(err)
	}
	unchanged, err := ApplyCSPAllowlist(base, CSPAllowlist{})
	if err != nil || !reflect.DeepEqual(base, unchanged) {
		t.Fatalf("empty allowlist changed presence: %v", err)
	}
	for _, a := range []CSPAllowlist{
		{Presets: []CSPPreset{CSPPresetCloudflareWebAnalytics}},
		{ScriptOrigins: []Origin{"https://example.test"}},
		{ConnectOrigins: []Origin{"https://example.test"}},
		{ImageOrigins: []Origin{"https://example.test"}},
		{FrameOrigins: []Origin{"https://example.test"}},
	} {
		p, err := ApplyCSPAllowlist(base, a)
		if !errors.Is(err, fault.Invalid) || !reflect.DeepEqual(p, CSPPolicy{}) {
			t.Fatalf("nil directive accepted: %+v, %v", p, err)
		}
	}
	for _, sources := range [][]CSPSource{nil, {}, {CSPNone()}} {
		p := CSPPolicy{DefaultSrc: []CSPSource{CSPSelf()}, ImgSrc: sources}
		out, err := ApplyCSPAllowlist(p, CSPAllowlist{})
		if err != nil || !reflect.DeepEqual(out, p) {
			t.Fatalf("presence changed without additions: %v", err)
		}
	}
	for _, p := range []CSPPolicy{{}, {ScriptSrc: []CSPSource{CSPNone(), CSPSelf()}}, {ScriptSrc: []CSPSource{{}}}} {
		if _, err := ApplyCSPAllowlist(p, CSPAllowlist{}); err == nil {
			t.Fatal("invalid base policy accepted")
		}
	}
	if _, err := ApplyCSPAllowlist(base, CSPAllowlist{Presets: []CSPPreset{"unknown"}}); err == nil {
		t.Fatal("invalid allowlist accepted")
	}
}

func TestCSPAllowlistCopiesEveryPolicySlice(t *testing.T) {
	p := CSPPolicy{}
	// Populate every slice, including spare capacity, to catch future policy
	// fields that are accidentally left sharing caller-owned storage.
	fields := reflect.ValueOf(&p).Elem()
	for i := range fields.NumField() {
		field := fields.Field(i)
		switch field.Type() {
		case reflect.TypeFor[[]CSPSource]():
			sources := make([]CSPSource, 1, 8)
			sources[0] = CSPSelf()
			field.Set(reflect.ValueOf(sources))
		case reflect.TypeFor[[]CSPSandboxToken]():
			field.Set(reflect.ValueOf([]CSPSandboxToken{CSPSandboxForms}))
		case reflect.TypeFor[[]CSPReportURI]():
			field.Set(reflect.ValueOf([]CSPReportURI{"/reports"}))
		}
	}
	before, err := compileCSPPolicy(p, false)
	if err != nil {
		t.Fatal(err)
	}
	a := CSPAllowlist{Presets: []CSPPreset{CSPPresetCloudflareWebAnalytics}, ImageOrigins: []Origin{"https://images.example.test"}}
	out, err := ApplyCSPAllowlist(p, a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.ScriptSrcElem, p.ScriptSrcElem) {
		t.Fatal("script-src-elem changed")
	}
	outFields := reflect.ValueOf(&out).Elem()
	for i := range outFields.NumField() {
		field := outFields.Field(i)
		if field.Kind() == reflect.Slice && field.Len() > 0 {
			field.Index(0).SetZero()
		}
	}
	after, err := compileCSPPolicy(p, false)
	if err != nil || before != after {
		t.Fatalf("result mutated input: %v", err)
	}
	if a.Presets[0] != CSPPresetCloudflareWebAnalytics || a.ImageOrigins[0] != "https://images.example.test" {
		t.Fatal("allowlist mutated")
	}
	if p.ScriptSrc[:cap(p.ScriptSrc)][1] != (CSPSource{}) {
		t.Fatal("append wrote into caller capacity")
	}
}

func TestCSPAllowlistRetainsPolicyBounds(t *testing.T) {
	p := CSPPolicy{ScriptSrc: []CSPSource{}, ConnectSrc: []CSPSource{}}
	for i := range 256 {
		p.ScriptSrc = append(p.ScriptSrc, CSPHostSource(fmt.Sprintf("https://s%d.test", i)))
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyCSPAllowlist(p, CSPAllowlist{Presets: []CSPPreset{CSPPresetCloudflareWebAnalytics}}); err == nil {
		t.Fatal("combined source bound bypassed")
	}
	a := CSPAllowlist{}
	for i := range 257 {
		a.ImageOrigins = append(a.ImageOrigins, Origin(fmt.Sprintf("https://i%d.test", i)))
	}
	if err := a.Validate(); err == nil {
		t.Fatal("allowlist source bound bypassed")
	}
	a.ImageOrigins = nil
	for i := range 8 {
		a.ImageOrigins = append(a.ImageOrigins, Origin("https://"+strings.Repeat(string(rune('a'+i)), 1100)+".test"))
	}
	if err := a.Validate(); err == nil {
		t.Fatal("allowlist header bound bypassed")
	}
}

func TestCSPAllowlistDoesNotRelaxMaintenancePage(t *testing.T) {
	p, err := ApplyCSPAllowlist(CSPPolicy{ScriptSrc: []CSPSource{CSPSelf()}, ConnectSrc: []CSPSource{CSPSelf()}}, CSPAllowlist{Presets: []CSPPreset{CSPPresetCloudflareWebAnalytics}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if err := writeMaintenancePage(w, r, ""); err != nil {
			t.Error(err)
		}
	}), ContentSecurityPolicy(CSPConfig{Enforce: value.Set(p)}))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != 503 || r.Header().Get("Content-Security-Policy") != maintenancePageCSP || strings.Contains(r.Header().Get("Content-Security-Policy"), "cloudflare") {
		t.Fatal("maintenance policy relaxed")
	}
}
