package httpsecurity_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"foundry.test/consumer/bootstrap"
	"foundry.test/consumer/httpsecurity"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestConfiguredCSPAllowlistOverHTTP(t *testing.T) {
	schema, err := bootstrap.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	layer, err := toml.Decode(strings.NewReader(`[app.http.csp_allowlist]
presets = ['cloudflare-web-analytics']
image_origins = ['https://images.example.test']
`), schema, toml.Options{Name: "allowlist"})
	if err != nil {
		t.Fatal(err)
	}
	keys := bootstrap.SettingsConfigKeys().App.HTTP.CSPAllowlist
	s, _, err := schema.Load(bootstrap.Defaults(), config.Inputs[bootstrap.Settings]{Files: []config.Values{layer}, Overrides: []config.Override[bootstrap.Settings]{keys.FrameOrigins.Set([]foundryhttp.Origin{"https://frames.example.test"})}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := httpsecurity.AllowlistedPageHandler(s.App.HTTP)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/page", nil))
	want := "default-src 'self'; script-src 'self' https://static.cloudflareinsights.com; connect-src 'self' https://cloudflareinsights.com; frame-src https://frames.example.test; img-src 'self' https://images.example.test; object-src 'none'; worker-src 'self'"
	if r.Code != 200 || r.Header().Get("Content-Security-Policy") != want {
		t.Fatalf("response = %d, %s", r.Code, r.Header().Get("Content-Security-Policy"))
	}
}
