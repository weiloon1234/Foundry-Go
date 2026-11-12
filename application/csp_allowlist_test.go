package application_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
)

func TestCSPAllowlistSettingsDefaultEmpty(t *testing.T) {
	if got := application.DefaultSettings().HTTP.CSPAllowlist; !reflect.DeepEqual(got, http.CSPAllowlist{}) {
		t.Fatalf("default CSP allowlist = %+v", got)
	}
}

func TestCSPAllowlistGeneratedSettingsTOMLEnvironmentAndOverrides(t *testing.T) {
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	keys := application.SettingsConfigKeys().HTTP.CSPAllowlist
	for _, key := range []struct{ got, want string }{
		{keys.Presets.Name(), "http.csp_allowlist.presets"},
		{keys.ScriptOrigins.Name(), "http.csp_allowlist.script_origins"},
		{keys.ConnectOrigins.Name(), "http.csp_allowlist.connect_origins"},
		{keys.ImageOrigins.Name(), "http.csp_allowlist.image_origins"},
		{keys.FrameOrigins.Name(), "http.csp_allowlist.frame_origins"},
	} {
		if key.got != key.want {
			t.Fatalf("key = %s, want %s", key.got, key.want)
		}
	}
	layer, err := toml.Decode(strings.NewReader(`[http.csp_allowlist]
presets = ['cloudflare-web-analytics']
script_origins = ['https://scripts.example.test']
connect_origins = ['https://api.example.test']
image_origins = ['https://images.example.test']
frame_origins = ['https://frames.example.test']
`), schema, toml.Options{Name: "csp"})
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := schema.Load(application.DefaultSettings(), config.Inputs[application.Settings]{Files: []config.Values{layer}})
	want := http.CSPAllowlist{Presets: []http.CSPPreset{http.CSPPresetCloudflareWebAnalytics}, ScriptOrigins: []http.Origin{"https://scripts.example.test"}, ConnectOrigins: []http.Origin{"https://api.example.test"}, ImageOrigins: []http.Origin{"https://images.example.test"}, FrameOrigins: []http.Origin{"https://frames.example.test"}}
	if err != nil || !reflect.DeepEqual(s.HTTP.CSPAllowlist, want) {
		t.Fatalf("TOML settings = %+v, %v", s.HTTP.CSPAllowlist, err)
	}
	override := []http.Origin{"https://override.example.test"}
	loaded, _, err := schema.Load(s, config.Inputs[application.Settings]{Prefix: "APP", Environment: func(name string) (string, bool) {
		switch name {
		case "APP__HTTP__CSP_ALLOWLIST__PRESETS":
			return `[]`, true
		case "APP__HTTP__CSP_ALLOWLIST__IMAGE_ORIGINS":
			return `["https://env.example.test"]`, true
		}
		return "", false
	}, Overrides: []config.Override[application.Settings]{keys.FrameOrigins.Set(override)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.HTTP.CSPAllowlist.Presets) != 0 || loaded.HTTP.CSPAllowlist.ImageOrigins[0] != "https://env.example.test" || loaded.HTTP.CSPAllowlist.FrameOrigins[0] != override[0] {
		t.Fatal("config precedence lost")
	}
	loaded.HTTP.CSPAllowlist.ScriptOrigins[0] = "https://changed.example.test"
	override[0] = "http://invalid.test"
	if s.HTTP.CSPAllowlist.ScriptOrigins[0] != want.ScriptOrigins[0] || loaded.HTTP.CSPAllowlist.FrameOrigins[0] != "https://override.example.test" {
		t.Fatal("configuration retained caller slices")
	}
}

func TestCSPAllowlistSettingsRejectBeforeResources(t *testing.T) {
	for _, a := range []http.CSPAllowlist{
		{Presets: []http.CSPPreset{"unknown"}},
		{ScriptOrigins: []http.Origin{"http://invalid.test"}},
		{ConnectOrigins: []http.Origin{"https://*.invalid.test"}},
		{ImageOrigins: []http.Origin{"data:"}},
		{FrameOrigins: []http.Origin{"https://invalid.test/path"}},
	} {
		for _, enabled := range []bool{false, true} {
			s := application.DefaultSettings()
			s.HTTP.Enabled, s.HTTP.CSPAllowlist = enabled, a
			called := false
			app, err := application.New(s, quiet()).Register(foundation.Module{Name: "csp.must-not-register", OnRegister: func(*foundation.Registrar) error { called = true; return nil }}).Build(t.Context())
			if app != nil || !errors.Is(err, fault.Invalid) || called {
				t.Fatalf("invalid settings reached resources: %v, called=%v", err, called)
			}
		}
	}
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	for key, raw := range map[string]string{"http.csp_allowlist.presets": `["unknown"]`, "http.csp_allowlist.image_origins": `["http://invalid.test"]`} {
		s, _, err := schema.Load(application.DefaultSettings(), config.Inputs[application.Settings]{Files: []config.Values{{Name: "invalid-csp", Data: map[string]string{key: raw}}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := application.New(s).Build(t.Context()); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid config accepted: %v", err)
		}
	}
}

func TestCSPAllowlistBuilderSnapshotsSettings(t *testing.T) {
	s := application.DefaultSettings()
	s.HTTP.Enabled = false
	s.HTTP.CSPAllowlist = http.CSPAllowlist{Presets: []http.CSPPreset{http.CSPPresetCloudflareWebAnalytics}, ScriptOrigins: []http.Origin{"https://scripts.example.test"}}
	builder := application.New(s, quiet())
	s.HTTP.CSPAllowlist.Presets[0] = "invalid"
	s.HTTP.CSPAllowlist.ScriptOrigins[0] = "http://invalid.test"
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
}
