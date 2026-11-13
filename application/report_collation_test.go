package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func TestReportTextCollationSettingsDecoding(t *testing.T) {
	schema, err := SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	defaults := DefaultSettings()
	defaults.Features.Reports.Enabled = true
	defaults.Services.Database.Connections = infrastructure.DatabaseConnections{"default": infrastructure.DefaultConnectionSettings()}
	for _, tc := range []struct {
		name, document string
		want           expr.Collation
	}{
		{"omitted", "", ""},
		{"empty", "[features.reports.config]\ntext_collation = ''", ""},
		{"unicode", "[features.reports.config]\ntext_collation = 'und-x-icu'", expr.CollationUnicode},
		{"language", "[features.reports.config]\ntext_collation = 'en-US-x-icu'", expr.ICUCollation("en-US")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layer, err := toml.Decode(strings.NewReader(tc.document), schema, toml.Options{Name: "reports"})
			if err != nil {
				t.Fatal(err)
			}
			settings, _, err := schema.Load(defaults, config.Inputs[Settings]{Files: []config.Values{layer}})
			if err != nil || settings.Features.Reports.Config.TextCollation != tc.want {
				t.Fatal("decoded report collation", settings.Features.Reports.Config.TextCollation, err)
			}
			features, err := prepareFeatureSettings(settings, clock.System{})
			if err != nil || features.Reports.Config.TextCollation != tc.want {
				t.Fatal("assembly lost report collation", features.Reports.Config.TextCollation, err)
			}
		})
	}
	key := SettingsConfigKeys().Features.Reports.Config.TextCollation
	settings, _, err := schema.Load(defaults, config.Inputs[Settings]{Overrides: []config.Override[Settings]{key.Set(expr.CollationC)}})
	if err != nil || settings.Features.Reports.Config.TextCollation != expr.CollationC || defaults.Features.Reports.Config.TextCollation != "" {
		t.Fatal("typed report collation override", err)
	}
	for _, invalid := range []expr.Collation{"bad\nname", expr.Collation(strings.Repeat("x", 64))} {
		settings.Features.Reports.Config.TextCollation = invalid
		if _, err := prepareFeatureSettings(settings, clock.System{}); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid report collation reached assembly", err)
		}
	}
}
