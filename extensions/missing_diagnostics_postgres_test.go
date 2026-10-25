package extensions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func TestMissingOwnerDiagnosticsAcrossExtensionPipelines(t *testing.T) {
	f := extensiontest.Open(t, append(metadata.Migrations(), translations.Migrations()...))
	owner := extensiontest.Members
	missing := owner.Reference(99)
	key := metadata.Define(owner, "diagnostic", 1, contract.StringJSON[string]())
	meta, err := metadata.New(f.Store, key.Registration())
	if err != nil {
		t.Fatal(err)
	}
	field := translations.Define(owner, "diagnostic", translations.Options{})
	locales, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	translated, err := translations.New(f.Store, locales, field.Registration())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, message string
		run           func() error
	}{
		{"lock", "consumer: model extension transaction: not_found", func() error {
			return f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
				_, err := owner.Lock(ctx, tx, f.Store.Registry(), missing)
				return err
			})
		}},
		{"metadata all", "consumer: model extension transaction: not_found", func() error { _, err := metadata.All(t.Context(), meta, owner, missing); return err }},
		{"metadata get", "consumer: not_found", func() error { _, err := key.Get(t.Context(), meta, missing); return err }},
		{"translation all", "consumer: model extension transaction: not_found", func() error { _, err := translations.All(t.Context(), translated, owner, missing); return err }},
		{"translation get", "consumer: not_found", func() error { _, err := field.Get(t.Context(), translated, missing, "en"); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := test.run()
			got := callback.Isolated("consumer", func() error { return source })
			if got == nil || got.Error() != test.message || !errors.Is(got, source) || !errors.Is(got, database.NotFound) {
				t.Fatal("missing owner diagnostic, identity or classification was lost")
			}
		})
	}
}
