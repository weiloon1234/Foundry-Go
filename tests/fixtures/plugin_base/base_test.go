package pluginbase_test

import (
	"testing"

	"foundry.test/pluginbase"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestIndependentBasePluginWithPublicHarness(t *testing.T) {
	base, err := pluginbase.New(config.Inputs[pluginbase.Settings]{Overrides: []config.Override[pluginbase.Settings]{pluginbase.Prefix.Set("application")}})
	if err != nil {
		t.Fatal(err)
	}
	app := testkit.Plugins(t, base)
	title, err := foundation.Resolve(app.Services(), pluginbase.Title)
	if err != nil || title != "application" {
		t.Fatal(title, err)
	}
	trace, err := foundation.Resolve(app.Services(), pluginbase.Journal)
	if err != nil || len(trace.Entries()) != 1 || trace.Entries()[0] != "base.boot" {
		t.Fatal("base did not boot", err)
	}
	registry, err := foundation.Resolve(app.Services(), pluginbase.Migrations)
	if err != nil || len(registry.Entries()) != 1 || registry.Entries()[0].Version != "1.0.0" {
		t.Fatal("plugin migration lost introducing version", err)
	}
}
