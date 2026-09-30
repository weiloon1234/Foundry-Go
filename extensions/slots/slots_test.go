package slots

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

type sample struct{ ID model.ID[sample] }

func TestDeclarationRequiresGeneratedParts(t *testing.T) {
	if err := (Declaration{}).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero declaration was accepted", err)
	}
	if err := (Declaration{}).Register(nil, foundation.Key[*database.DB]{}, func(foundation.Resolver) (Runtime, error) { return Runtime{}, nil }); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero declaration registered", err)
	}
}

func TestDeclarationPartsAreOwnedCopies(t *testing.T) {
	parts := Parts{Translations: []translations.Registration{{}}}
	declaration := Declare(extensions.Declaration{}, parts, nil)
	parts.Translations[0] = translations.Registration{}
	parts.Translations = append(parts.Translations, translations.Registration{})
	if len(declaration.Parts().Translations) != 1 {
		t.Fatal("declaration retained the caller's slice")
	}
	copied := declaration.Parts()
	copied.Translations = nil
	if len(declaration.Parts().Translations) != 1 {
		t.Fatal("Parts exposed the declaration's slice")
	}
}

func TestObserverNameIsStablePerModel(t *testing.T) {
	first, err := ObserverName[sample]()
	if err != nil {
		t.Fatal(err)
	}
	second, err := ObserverName[sample]()
	if err != nil || first != second || !strings.HasPrefix(first, "foundry.extensions.") {
		t.Fatal("observer name is not stable", first, second, err)
	}
	if _, err := ObserverName[int](); !errors.Is(err, fault.Invalid) {
		t.Fatal("a non-struct model received an observer name", err)
	}
}

func TestCleanupRequiresEveryDeclaredKind(t *testing.T) {
	reference := func(sample) model.Reference[sample, model.ID[sample]] {
		return model.Reference[sample, model.ID[sample]]{}
	}
	if _, err := NewCleanup[sample](Runtime{}, extensions.Owner[sample, model.ID[sample]]{}, Parts{}, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("cleanup accepted a missing reference function", err)
	}
	if _, err := NewCleanup(Runtime{}, extensions.Owner[sample, model.ID[sample]]{}, Parts{}, reference); err == nil {
		t.Fatal("cleanup accepted an invalid owner")
	}
	if err := (Cleanup[sample]{}).Deleted(t.Context(), nil, value.Set(sample{}), value.Set(lifecycle.Delete)); !errors.Is(err, fault.Invalid) {
		t.Fatal("uninitialized cleanup ran", err)
	}
	cleanup := Cleanup[sample]{run: func(context.Context, *database.Tx, sample, lifecycle.Operation) error { return nil }}
	if err := cleanup.Deleted(t.Context(), nil, value.Optional[sample]{}, value.Set(lifecycle.Delete)); !errors.Is(err, fault.Missing) {
		t.Fatal("cleanup ran without the deleted model", err)
	}
	if err := cleanup.Deleted(t.Context(), nil, value.Set(sample{}), value.Optional[lifecycle.Operation]{}); !errors.Is(err, fault.Missing) {
		t.Fatal("cleanup ran without a lifecycle operation", err)
	}
	if err := missing("articles", "translations"); !errors.Is(err, fault.Missing) || !strings.Contains(err.Error(), "articles") {
		t.Fatal("missing manager error lost its owner", err)
	}
}
