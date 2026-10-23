package application_test

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
)

func TestStatementDetectorGeneratedConfiguration(t *testing.T) {
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	table, err := toml.Decode(strings.NewReader("[http.statement_detector]\nenabled=true\n[http.statement_detector.limits]\nthreshold=4\nmax_shapes=32\n"), schema, toml.Options{Name: "detector"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := schema.Load(application.DefaultSettings(), config.Inputs[application.Settings]{Files: []config.Values{table}})
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.HTTP.StatementDetector.Enabled || loaded.HTTP.StatementDetector.Limits.Threshold != 4 || loaded.HTTP.StatementDetector.Limits.MaxShapes != 32 {
		t.Fatal("generated config not applied")
	}
	s := settings()
	s.Services.Namespace.Environment = "production"
	s.HTTP.StatementDetector.Enabled = true
	if _, err := application.New(s, quiet()).Build(t.Context()); err == nil {
		t.Fatal("configured application accepted accidental production detector")
	}
}
