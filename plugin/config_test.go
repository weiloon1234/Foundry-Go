package plugin_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/plugin"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type settings struct {
	Label string
	Token secret.String
	Tags  []string
}

func TestPluginConfigUsesExistingLayeringAndOwnedDefaults(t *testing.T) {
	label := config.String("plugins.reports.label", func(v *settings) *string { return &v.Label })
	token := config.Secret("plugins.reports.token", func(v *settings) *secret.String { return &v.Token })
	schema, err := config.New(label, token)
	if err != nil {
		t.Fatal(err)
	}
	declaration := plugin.Manifest{ID: "reports", Version: "1.0.0", Framework: "^0.1.0"}
	defaults := settings{Label: "default", Tags: []string{"original"}}
	inputs := config.Inputs[settings]{
		Files:       []config.Values{{Name: "application", Data: map[string]string{"plugins.reports.label": "file", "plugins.reports.token": "private-value"}}},
		Environment: func(name string) (string, bool) { return "environment", name == "APP__PLUGINS__REPORTS__LABEL" }, Prefix: "APP",
		Overrides: []config.Override[settings]{label.Set("application override")},
	}
	loaded, report, err := plugin.LoadConfig(declaration, schema, defaults, inputs)
	if err != nil || loaded.Label != "application override" {
		t.Fatalf("layering: %+v %v", loaded, err)
	}
	if strings.Contains(fmt.Sprint(report.Entries()), "private-value") {
		t.Fatal("report contains configuration value")
	}
	var found bool
	for _, entry := range report.Entries() {
		if entry.Name == token.Name() {
			found = entry.Secret && entry.Source == "application"
		}
	}
	if !found {
		t.Fatal("secret provenance missing")
	}
	loaded.Tags[0] = "changed"
	if defaults.Tags[0] != "original" {
		t.Fatal("plugin config mutated shared defaults")
	}
	inputs.Overrides = nil
	loaded, _, err = plugin.LoadConfig(declaration, schema, defaults, inputs)
	if err != nil || loaded.Label != "environment" {
		t.Fatalf("environment layering: %+v %v", loaded, err)
	}
	inputs.Environment = nil
	loaded, _, err = plugin.LoadConfig(declaration, schema, defaults, inputs)
	if err != nil || loaded.Label != "file" {
		t.Fatalf("file layering: %+v %v", loaded, err)
	}
	inputs.Files = nil
	loaded, _, err = plugin.LoadConfig(declaration, schema, defaults, inputs)
	if err != nil || loaded.Label != "default" {
		t.Fatalf("default layering: %+v %v", loaded, err)
	}
}

func TestPluginConfigRejectsForeignKeysAndContainsCallbacks(t *testing.T) {
	declaration := plugin.Manifest{ID: "reports", Version: "1.0.0", Framework: "^0.1.0"}
	foreign := config.String("plugins.other.label", func(v *settings) *string { return &v.Label })
	schema, err := config.New(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := plugin.LoadConfig(declaration, schema, settings{}, config.Inputs[settings]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign namespace accepted", err)
	}
	label := config.String("plugins.reports.label", func(v *settings) *string { return &v.Label })
	schema, err = config.New(label)
	if err != nil {
		t.Fatal(err)
	}
	for _, inputs := range []config.Inputs[settings]{
		{Files: []config.Values{{Name: "application", Data: map[string]string{foreign.Name(): "value"}}}},
		{Overrides: []config.Override[settings]{foreign.Set("value")}},
		{Environment: func(string) (string, bool) { runtime.Goexit(); return "", false }},
		{Validate: func(settings) error { panic("private-value") }},
	} {
		value, report, err := plugin.LoadConfig(declaration, schema, settings{}, inputs)
		if err == nil || value.Label != "" || len(report.Entries()) != 0 || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("failed load leaked state: %+v %v %v", value, report, err)
		}
	}
}
