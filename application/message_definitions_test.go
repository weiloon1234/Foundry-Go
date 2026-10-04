package application_test

import (
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// An application building its own catalog declares exactly what a configured
// application registers, so a new framework message cannot be missed.
func TestMessageDefinitionsAreTheConfiguredCatalogs(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	s.Features.Locales.Enabled = true
	app, err := application.New(s, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	catalog, err := app.Resources().Locales()
	if err != nil {
		t.Fatal(err)
	}
	framework := application.MessageDefinitions()
	registered := catalog.Definitions()
	if len(registered) != len(framework) {
		t.Fatal("configured catalog registers", len(registered), "framework declares", len(framework))
	}
	keys := make([]i18n.MessageKey, 0, len(framework))
	for _, definition := range framework {
		if err := catalog.Accepts(definition); err != nil {
			t.Fatal(definition.Key, err)
		}
		keys = append(keys, definition.Key)
	}
	for _, key := range []i18n.MessageKey{"validation.min", "http.error.validation_failed", "http.input.type", pagination.NumberLabelKey, pagination.SizeLabelKey} {
		if !slices.Contains(keys, key) {
			t.Fatal("framework definitions omit", key)
		}
	}
	framework[0].Key = "changed"
	if application.MessageDefinitions()[0].Key == "changed" {
		t.Fatal("definitions share storage between calls")
	}
}
