package http

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCSPWasmUnsafeEvalHeaders(t *testing.T) {
	for _, directive := range []string{"script-src", "default-src"} {
		for _, mode := range []struct {
			name  string
			nonce bool
		}{{"static", false}, {"nonce", true}} {
			t.Run(directive+"/"+mode.name, func(t *testing.T) {
				nonce := mode.nonce
				sources := []CSPSource{CSPSelf(), CSPWasmUnsafeEval()}
				if nonce {
					sources = append(sources, CSPNonceSource(), CSPStrictDynamic())
				}
				policy := CSPPolicy{ScriptSrc: sources}
				if directive == "default-src" {
					policy = CSPPolicy{DefaultSrc: sources}
				}
				var requestNonce string
				handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
					n, present := CSPNonceFromContext(r.Context())
					if present != nonce {
						t.Errorf("nonce present = %v, want %v", present, nonce)
					}
					requestNonce = n.String()
					w.WriteHeader(204)
				}), ContentSecurityPolicy(CSPConfig{Enforce: value.Set(policy), ReportOnly: value.Set(policy)}))
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
				want := directive + " 'self' 'wasm-unsafe-eval'"
				if nonce {
					if requestNonce == "" || response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("missing nonce or no-store", response.Header())
					}
					want += " 'nonce-" + requestNonce + "' 'strict-dynamic'"
				}
				for _, header := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only"} {
					if got := response.Header().Get(header); got != want {
						t.Errorf("%s = %q, want %q", header, got, want)
					}
				}
				if response.Code != 204 {
					t.Fatal(response.Code)
				}
			})
		}
	}
}

func TestCSPWasmUnsafeEvalRejectsOtherDirectives(t *testing.T) {
	sources := []CSPSource{CSPWasmUnsafeEval()}
	for name, policy := range map[string]CSPPolicy{
		"script-src-elem": {ScriptSrcElem: sources}, "script-src-attr": {ScriptSrcAttr: sources},
		"style-src": {StyleSrc: sources}, "style-src-elem": {StyleSrcElem: sources}, "style-src-attr": {StyleSrcAttr: sources},
		"child-src": {ChildSrc: sources}, "connect-src": {ConnectSrc: sources}, "font-src": {FontSrc: sources},
		"frame-src": {FrameSrc: sources}, "img-src": {ImgSrc: sources}, "manifest-src": {ManifestSrc: sources},
		"media-src": {MediaSrc: sources}, "object-src": {ObjectSrc: sources}, "worker-src": {WorkerSrc: sources},
		"base-uri": {BaseURI: sources}, "form-action": {FormAction: sources}, "frame-ancestors": {FrameAncestors: sources},
	} {
		t.Run(name, func(t *testing.T) {
			for _, config := range []CSPConfig{{Enforce: value.Set(policy)}, {ReportOnly: value.Set(policy)}} {
				if err := config.Validate(); !errors.Is(err, fault.Invalid) {
					t.Fatalf("validation = %v", err)
				}
				handler, err := ApplyMiddleware(stdhttp.NotFoundHandler(), ContentSecurityPolicy(config))
				if handler != nil || !errors.Is(err, fault.Invalid) {
					t.Fatalf("assembly = %v, %v", handler, err)
				}
			}
		})
	}
}

func TestCSPWasmUnsafeEvalKeywordValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		sources []CSPSource
		want    error
	}{
		{"valid", []CSPSource{CSPWasmUnsafeEval()}, nil},
		{"zero", []CSPSource{CSPWasmUnsafeEval(), {}}, fault.Invalid},
		{"duplicate", []CSPSource{CSPWasmUnsafeEval(), CSPWasmUnsafeEval()}, fault.Duplicate},
		{"none", []CSPSource{CSPNone(), CSPWasmUnsafeEval()}, fault.Invalid},
		{"host-keyword", []CSPSource{CSPHostSource("'wasm-unsafe-eval'")}, fault.Invalid},
		{"unsafe-eval-host", []CSPSource{CSPHostSource("'unsafe-eval'")}, fault.Invalid},
		{"unsafe-eval-keyword", []CSPSource{{text: "'unsafe-eval'", kind: cspKeyword}}, fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, policy := range []CSPPolicy{{ScriptSrc: test.sources}, {DefaultSrc: test.sources}} {
				handler, err := ApplyMiddleware(stdhttp.NotFoundHandler(), ContentSecurityPolicy(CSPConfig{Enforce: value.Set(policy)}))
				if !errors.Is(err, test.want) || (handler == nil) != (test.want != nil) {
					t.Fatalf("assembly = %v, %v; want %v", handler, err, test.want)
				}
			}
		})
	}
}

func TestCSPWasmUnsafeEvalAllowlistWorkerFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		script []CSPSource
		worker string
	}{
		{"self", []CSPSource{CSPSelf(), CSPWasmUnsafeEval()}, "worker-src 'self'"},
		{"only", []CSPSource{CSPWasmUnsafeEval()}, "worker-src 'none'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := CSPPolicy{ScriptSrc: test.script}
			policy, err := ApplyCSPAllowlist(base, CSPAllowlist{ScriptOrigins: []Origin{"https://scripts.example.test"}})
			if err != nil {
				t.Fatal(err)
			}
			if base.WorkerSrc != nil || test.script[len(test.script)-1] != CSPWasmUnsafeEval() {
				t.Fatal("input policy mutated")
			}
			compiled, err := compileCSPPolicy(policy, false)
			if err != nil || !strings.HasSuffix(compiled.text, "; "+test.worker) || strings.Count(compiled.text, "'wasm-unsafe-eval'") != 1 {
				t.Fatalf("policy = %q, %v", compiled.text, err)
			}
		})
	}
}
