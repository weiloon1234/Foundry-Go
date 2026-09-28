package message_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
)

type EncodedArgs struct {
	Calls *atomic.Int32 `json:"-"`
}

func (a EncodedArgs) MarshalJSON() ([]byte, error) {
	a.Calls.Add(1)
	return []byte(`{"name":"Ada"}`), nil
}

func TestInvalidCatalogLocaleAndContextDoNotEvaluateArgumentEncoder(t *testing.T) {
	typ := reflect.TypeFor[EncodedArgs]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	descriptor := contract.DefineJSON[EncodedArgs](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "name", Type: "text", Required: true}}},
		{ID: "text", Kind: contract.StringKind},
	}})
	m := message.Define("welcome", descriptor, message.Options{})
	definition, err := m.Definition()
	if err != nil {
		t.Fatal(err)
	}
	set, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := i18n.NewCatalog(t.Context(), set, i18n.CatalogOptions{}, []i18n.MessageDefinition{definition}, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{"en": {"welcome": {Text: "Hello {{name}}"}}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	args := EncodedArgs{&calls}
	for _, locale := range []i18n.LocaleID{"", "fr", "EN"} {
		if _, err := m.Format(t.Context(), catalog, locale, args); err == nil {
			t.Fatal("invalid locale accepted")
		}
	}
	if _, err := m.Format(t.Context(), nil, "en", args); err == nil {
		t.Fatal("nil catalog accepted")
	}
	if _, err := m.Format(nil, catalog, "en", args); err == nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Format(ctx, catalog, "en", args); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	definition.Parameters[0].Name = "other"
	other, err := i18n.NewCatalog(t.Context(), set, i18n.CatalogOptions{}, []i18n.MessageDefinition{definition}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Format(t.Context(), other, "en", args); err == nil {
		t.Fatal("mismatched signature accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request evaluated a custom encoder")
	}
	result, err := m.Format(t.Context(), catalog, "ms", args)
	if err != nil || result.Text != "Hello Ada" || !result.Fallback || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
}
