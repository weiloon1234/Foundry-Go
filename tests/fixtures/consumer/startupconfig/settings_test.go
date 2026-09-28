package startupconfig_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"foundry.test/consumer/startupconfig"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
)

func TestGeneratedConfigurationLoadsEverySource(t *testing.T) {
	schema, err := startupconfig.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	keys := startupconfig.SettingsConfigKeys()
	file := filepath.Join(t.TempDir(), "app.toml")
	if err := os.WriteFile(file, []byte("environment='production'\nsigning_key='private'\n[http]\nport=9000\ntimeout='3s'\norigins=['https://example.test']\n[features]\nuploads=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	settings, report, err := toml.LoadFile(file, schema, startupconfig.Defaults(), config.Inputs[startupconfig.Settings]{
		Prefix: "APP", Environment: func(key string) (string, bool) { return "7s", key == "APP__HTTP__TIMEOUT" },
		Overrides: []config.Override[startupconfig.Settings]{keys.HTTP.Port.Set(startupconfig.Port(9001))}, Validate: startupconfig.Validate,
	}, toml.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if settings.HTTP.Port != 9001 || settings.HTTP.Timeout != 7*time.Second || settings.Environment != startupconfig.Production || settings.SigningKey.Reveal() != "private" || !settings.Features["uploads"] {
		t.Fatal("configuration lost its types or precedence")
	}
	secret := false
	for _, entry := range report.Entries() {
		if entry.Name == "signing_key" {
			secret = entry.Secret
		}
	}
	if !secret {
		t.Fatal("generated secret was not marked sensitive")
	}
	for _, raw := range []string{"other", ""} {
		if _, _, err := schema.Load(startupconfig.Defaults(), config.Inputs[startupconfig.Settings]{Overrides: []config.Override[startupconfig.Settings]{keys.Environment.Set(startupconfig.Environment(raw))}}); err == nil {
			t.Fatal("invalid enum override accepted")
		}
	}
	defaults := startupconfig.Defaults()
	defaults.Features = map[string]bool{"uploads": true}
	one, _, err := schema.Load(defaults, config.Inputs[startupconfig.Settings]{})
	if err != nil {
		t.Fatal(err)
	}
	one.Features["uploads"] = false
	two, _, err := schema.Load(defaults, config.Inputs[startupconfig.Settings]{})
	if err != nil || !two.Features["uploads"] {
		t.Fatal("loads shared mutable configuration")
	}
}
