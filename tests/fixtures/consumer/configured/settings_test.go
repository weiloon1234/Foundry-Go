package configured_test

import (
	"context"
	"foundry.test/consumer/configured"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"strings"
	"testing"
	"time"
)

func TestConfiguredConsumer(t *testing.T) {
	schema, err := configured.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	layer, err := toml.Decode(strings.NewReader("[services.cache.stores.default.config]\ntimeout='3s'\n[services.cache.stores.reports]\ndriver='memory'\n"), schema, toml.Options{Name: "consumer"})
	if err != nil {
		t.Fatal(err)
	}
	keys := configured.SettingsConfigKeys()
	settings, _, err := schema.Load(configured.Defaults(), config.Inputs[configured.Settings]{Files: []config.Values{layer}, Overrides: []config.Override[configured.Settings]{keys.Services.Cache.Default.Set(cache.StoreName("default"))}})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Services.Cache.Stores["default"].Config.Timeout != 3*time.Second {
		t.Fatal("generated table lost typed duration")
	}
	app, err := configured.Build(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services, err := foundation.Resolve(app.Services(), infrastructure.ServicesKey)
	if err != nil {
		t.Fatal(err)
	}
	values, err := configured.Greetings(services)
	if err != nil {
		t.Fatal(err)
	}
	if err = values.Put(t.Context(), "message", "hello", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, hit, err := values.Get(t.Context(), "message"); err != nil || !hit || value != "hello" {
		t.Fatal(value, hit, err)
	}
	reports, err := configured.ReportsCache(services)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := configured.Greeting.Bind(reports)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := bound.Get(t.Context(), "message"); err != nil || hit {
		t.Fatal("named cache collided", err)
	}
}
