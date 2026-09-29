package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func securityHeaderResponse(t *testing.T, config SecurityHeadersConfig) stdhttp.Header {
	t.Helper()
	handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(204) }), SecurityHeaders(config))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	return response.Header()
}

func TestTypedIsolationAndPermissionsPolicyHeaders(t *testing.T) {
	config := DefaultSecurityHeadersConfig()
	config.CrossOriginOpener = OpenerSameOrigin
	config.CrossOriginEmbedder = EmbedderCredentialless
	config.CrossOriginResource = ResourceSameSite
	config.Permissions = []PermissionDirective{
		{Feature: PermissionCamera},
		{Feature: PermissionGeolocation, Self: true, Origins: []Origin{"https://MAPS.example.test:443"}},
		{Feature: PermissionFullscreen, Any: true},
	}
	header := securityHeaderResponse(t, config)
	for name, want := range map[string]string{
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Embedder-Policy": "credentialless",
		"Cross-Origin-Resource-Policy": "same-site",
		"Permissions-Policy":           `camera=(), geolocation=(self "https://maps.example.test"), fullscreen=*`,
	} {
		if got := header.Get(name); got != want {
			t.Errorf("%s = %q; want %q", name, got, want)
		}
	}
	defaults := securityHeaderResponse(t, DefaultSecurityHeadersConfig())
	for _, name := range []string{"Permissions-Policy", "Cross-Origin-Opener-Policy", "Cross-Origin-Embedder-Policy", "Cross-Origin-Resource-Policy"} {
		if defaults.Get(name) != "" {
			t.Fatalf("%s must remain opt-in", name)
		}
	}
	legacy := DefaultSecurityHeadersConfig()
	legacy.Extra = []ResponseHeader{{"Permissions-Policy", "camera=()"}}
	if err := legacy.Validate(); err != nil {
		t.Fatal("untyped Extra policy without a typed field rejected", err)
	}
	for _, mutate := range []func(*SecurityHeadersConfig){
		func(c *SecurityHeadersConfig) { c.CrossOriginOpener = "same-origin-plus" },
		func(c *SecurityHeadersConfig) { c.CrossOriginEmbedder = "require-corp " },
		func(c *SecurityHeadersConfig) { c.CrossOriginResource = "any" },
		func(c *SecurityHeadersConfig) { c.Permissions = []PermissionDirective{{Feature: "Camera"}} },
		func(c *SecurityHeadersConfig) {
			c.Permissions = []PermissionDirective{{Feature: "camera"}, {Feature: "camera"}}
		},
		func(c *SecurityHeadersConfig) {
			c.Permissions = []PermissionDirective{{Feature: "camera", Any: true, Self: true}}
		},
		func(c *SecurityHeadersConfig) {
			c.Permissions = []PermissionDirective{{Feature: "camera", Origins: []Origin{"null"}}}
		},
		func(c *SecurityHeadersConfig) {
			c.CrossOriginOpener = OpenerSameOrigin
			c.Extra = []ResponseHeader{{"Cross-Origin-Opener-Policy", "unsafe-none"}}
		},
	} {
		invalid := DefaultSecurityHeadersConfig()
		mutate(&invalid)
		if invalid.Validate() == nil {
			t.Errorf("invalid isolation policy accepted: %+v", invalid)
		}
	}
}
